//go:build knotreport

// Storage report for knot.db. It fills a database with generated dnstap
// minutes (or reads a copy of a real one) and measures:
//   - size per table and index (dbstat) and bytes per row,
//   - write, rollup, prune and VACUUM times,
//   - the page's drill-down queries through Store.Dnstap,
//   - which indexes the query plans actually use.
//
// It measures; it does not check behavior, so it only builds with the
// knotreport tag:
//
//	GOFLAGS=-p=1 nice go test -tags knotreport -run TestStorageReport -v -timeout 3h ./internal/collector/type/knot/
//
// Settings (env):
//
//	KNOTREPORT_DAYS           days of minute rows to generate (7)
//	KNOTREPORT_HOURLY_DAYS    extra days of hourly-only rows before them (30)
//	KNOTREPORT_TOP            name rows per minute (1000)
//	KNOTREPORT_CLIENT_TOP     client rows per minute (1000)
//	KNOTREPORT_CLIENTS        client subnets kept per minute (50, the collector default)
//	KNOTREPORT_COLLECTORS     collectors of one account (1)
//	KNOTREPORT_ZONES          zones (20)
//	KNOTREPORT_NAMES          size of the name pool (50000)
//	KNOTREPORT_NETS           size of the client network pool (20000)
//	KNOTREPORT_QPM            queries simulated per minute and collector (20000)
//	KNOTREPORT_RUNS           runs per query case; the median is reported (3).
//	                          A case slower than 10s runs once.
//	KNOTREPORT_QUERY_TIMEOUT  seconds before one query is stopped and reported as a timeout (60)
//	KNOTREPORT_MINUTE_RETENTION_DAYS  minute retention the page assumes (7)
//	KNOTREPORT_DIR            where the database is written (~/.cache/certainstats-knotreport;
//	                          keep it off tmpfs, a busy run writes several GB)
//	KNOTREPORT_KEEP=true      keep the generated database
//	KNOTREPORT_VACUUM=true    also time VACUUM INTO a copy and report its size
//	KNOTREPORT_OUT=path       write the report as JSON, refreshed after every phase
//	KNOTREPORT_DB=path        measure this database instead (use a copy: Open may migrate it)
//	KNOTREPORT_MUTATE=true    with KNOTREPORT_DB, also run rollup and prune on it
//	KNOTREPORT_FOLD=subnet    fold client rows below the cap into one (other) row per
//	                          subnet instead of per subnet, zone, qtype and rcode (full)
//	KNOTREPORT_ROUTE=hourly   windows of 24h or more read hourly rows up to the last
//	                          rolled hour, instead of minutes within retention (minutes)
//	KNOTREPORT_STORE=v2       measure the prototype schema in storage_v2_report_test.go
//	                          instead of the current Store (current); generated data only.
//	                          topn-sql and topn-blocks measure Knot on internal/tblk.
//	KNOTREPORT_DAILY_DAYS     days of daily-only history before the hourly-only days (0;
//	                          topn stores only)
package knot

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"math/rand"
	"net/netip"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"text/tabwriter"
	"time"

	"certainstats/internal/collector/type/knot/wire"
	"certainstats/internal/metrics"
	"certainstats/internal/tblk"
)

const dayMs = 24 * hourMs

type reportCfg struct {
	Days                int    `json:"days"`
	HourlyDays          int    `json:"hourly_days"`
	Top                 int    `json:"top"`
	ClientTop           int    `json:"client_top"`
	Clients             int    `json:"clients"`
	Collectors          int    `json:"collectors"`
	Zones               int    `json:"zones"`
	Names               int    `json:"names"`
	Nets                int    `json:"nets"`
	QPM                 int    `json:"qpm"`
	Runs                int    `json:"runs"`
	MinuteRetentionDays int    `json:"minute_retention_days"`
	Dir                 string `json:"dir"`
	DB                  string `json:"db,omitempty"`
	Out                 string `json:"-"`
	Keep                bool   `json:"keep"`
	Mutate              bool   `json:"mutate"`
	Vacuum              bool   `json:"vacuum"`
	QueryTimeoutS       int    `json:"query_timeout_s"`
	Fold                string `json:"fold"`
	Route               string `json:"route"`
	Store               string `json:"store"`
	DailyDays           int    `json:"daily_days"`
}

// envChoice reads a setting that must be one of choices; the first is the default.
func envChoice(t *testing.T, name string, choices ...string) string {
	v := os.Getenv(name)
	if v == "" {
		return choices[0]
	}
	for _, c := range choices {
		if v == c {
			return v
		}
	}
	t.Fatalf("%s must be one of %s, got %q", name, strings.Join(choices, ", "), v)
	return ""
}

func envNum(t *testing.T, name string, def, lo int) int {
	raw := os.Getenv(name)
	if raw == "" {
		return def
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < lo {
		t.Fatalf("%s must be a whole number of at least %d, got %q", name, lo, raw)
	}
	return n
}

func envFlag(name string) bool {
	v := os.Getenv(name)
	return v == "1" || strings.EqualFold(v, "true")
}

func loadCfg(t *testing.T) reportCfg {
	c := reportCfg{
		Days:                envNum(t, "KNOTREPORT_DAYS", 7, 1),
		HourlyDays:          envNum(t, "KNOTREPORT_HOURLY_DAYS", 30, 0),
		Top:                 envNum(t, "KNOTREPORT_TOP", 1000, 1),
		ClientTop:           envNum(t, "KNOTREPORT_CLIENT_TOP", 1000, 0),
		Clients:             envNum(t, "KNOTREPORT_CLIENTS", 50, 0),
		Collectors:          envNum(t, "KNOTREPORT_COLLECTORS", 1, 1),
		Zones:               envNum(t, "KNOTREPORT_ZONES", 20, 1),
		Names:               envNum(t, "KNOTREPORT_NAMES", 50000, 10),
		Nets:                envNum(t, "KNOTREPORT_NETS", 20000, 10),
		QPM:                 envNum(t, "KNOTREPORT_QPM", 20000, 1),
		Runs:                envNum(t, "KNOTREPORT_RUNS", 3, 1),
		MinuteRetentionDays: envNum(t, "KNOTREPORT_MINUTE_RETENTION_DAYS", 7, 1),
		Dir:                 os.Getenv("KNOTREPORT_DIR"),
		DB:                  os.Getenv("KNOTREPORT_DB"),
		Out:                 os.Getenv("KNOTREPORT_OUT"),
		Keep:                envFlag("KNOTREPORT_KEEP"),
		Mutate:              envFlag("KNOTREPORT_MUTATE"),
		Vacuum:              envFlag("KNOTREPORT_VACUUM"),
		QueryTimeoutS:       envNum(t, "KNOTREPORT_QUERY_TIMEOUT", 60, 1),
		Fold:                envChoice(t, "KNOTREPORT_FOLD", "full", "subnet"),
		Route:               envChoice(t, "KNOTREPORT_ROUTE", "minutes", "hourly"),
		Store:               envChoice(t, "KNOTREPORT_STORE", "current", "v2", "topn-sql", "topn-blocks"),
		DailyDays:           envNum(t, "KNOTREPORT_DAILY_DAYS", 0, 0),
	}
	if c.Store != "current" && c.DB != "" {
		t.Fatal("KNOTREPORT_STORE=" + c.Store + " measures generated data only; unset KNOTREPORT_DB")
	}
	if c.Dir == "" {
		cache, err := os.UserCacheDir()
		if err != nil {
			t.Fatalf("set KNOTREPORT_DIR: %v", err)
		}
		c.Dir = filepath.Join(cache, "certainstats-knotreport")
	}
	return c
}

// Statement tracing. The store's connections are reopened through a driver
// that records every prepared statement and the phase it ran in, so the plans
// checked later are exactly what the store ran; query.go needs no hook.

type traceDriver struct {
	base  driver.Driver
	mu    sync.Mutex
	phase string
	seen  map[string]map[string]bool
}

var (
	traceOnce sync.Once
	tracer    = &traceDriver{seen: map[string]map[string]bool{}}
)

func (d *traceDriver) setPhase(p string) {
	d.mu.Lock()
	d.phase = p
	d.mu.Unlock()
}

func (d *traceDriver) note(q string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.seen[q] == nil {
		d.seen[q] = map[string]bool{}
	}
	d.seen[q][d.phase] = true
}

func (d *traceDriver) Open(name string) (driver.Conn, error) {
	c, err := d.base.Open(name)
	if err != nil {
		return nil, err
	}
	return &traceConn{Conn: c, d: d}, nil
}

// traceConn hides the driver's direct Exec/Query paths, so database/sql
// prepares every statement and the text passes through note.
type traceConn struct {
	driver.Conn
	d *traceDriver
}

func (c *traceConn) Prepare(q string) (driver.Stmt, error) {
	c.d.note(q)
	return c.Conn.Prepare(q)
}

func (c *traceConn) PrepareContext(ctx context.Context, q string) (driver.Stmt, error) {
	c.d.note(q)
	if p, ok := c.Conn.(driver.ConnPrepareContext); ok {
		return p.PrepareContext(ctx, q)
	}
	return c.Conn.Prepare(q)
}

func (c *traceConn) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	if b, ok := c.Conn.(driver.ConnBeginTx); ok {
		return b.BeginTx(ctx, opts)
	}
	return c.Conn.Begin() //nolint:staticcheck // fallback for drivers without BeginTx
}

// openTraced opens knot.db normally (schema, migration), then swaps in traced connections.
func openTraced(t *testing.T, path string) *Store {
	t.Helper()
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	s.read.Close()
	s.write.Close()
	s.write, s.read = tracedDBs(t, path)
	return s
}

// tracedDBs opens a writer (one connection) and a reader pool (four) on path
// through the tracing driver, the way Open sets up the store's own.
func tracedDBs(t *testing.T, path string) (write, read *sql.DB) {
	t.Helper()
	registerTracer(t)
	w, err := sql.Open("sqlite-knotreport", dsn(path))
	if err != nil {
		t.Fatal(err)
	}
	w.SetMaxOpenConns(1)
	r, err := sql.Open("sqlite-knotreport", dsn(path))
	if err != nil {
		t.Fatal(err)
	}
	r.SetMaxOpenConns(4)
	return w, r
}

// registerTracer registers the tracing driver as "sqlite-knotreport" once.
func registerTracer(t *testing.T) {
	traceOnce.Do(func() {
		db, err := sql.Open("sqlite", ":memory:")
		if err != nil {
			t.Fatal(err)
		}
		tracer.base = db.Driver()
		db.Close()
		sql.Register("sqlite-knotreport", tracer)
	})
}

// reportStore is what the report needs from a dnstap store, so the current
// Store and the v2 prototype run through the same steps.
type reportStore interface {
	SaveDnstap(ctx context.Context, userID, collectorID string, d wire.Dnstap, now int64) error
	Rollup(ctx context.Context, now int64) (int, error)
	Dnstap(ctx context.Context, q Query, dim string, limit int) (Breakdown, error)
	Prune(ctx context.Context, keep Retention, now int64, users []string) (PruneResult, error)
	Close() error
	// reportHourly writes a synthetic hour straight into the hourly tables.
	reportHourly(ctx context.Context, user, col string, d wire.Dnstap) error
	// reportOther counts (other) rows per minute table.
	reportOther(ctx context.Context, user string) (map[string]int64, error)
	// reportFocus returns the busiest zone, qname and network key since a time.
	reportFocus(ctx context.Context, user string, since int64) (zone, qname, netKey string)
	// reportTop is the rollup's rows kept per collector and hour.
	reportTop() int
}

func (s *Store) reportHourly(ctx context.Context, user, col string, d wire.Dnstap) error {
	return insertHourly(ctx, s, user, col, d)
}

func (s *Store) reportTop() int { return s.TopHourly }

func (s *Store) reportOther(ctx context.Context, user string) (map[string]int64, error) {
	out := map[string]int64{}
	for _, table := range []string{"dnstap_qtypes_minute", "dnstap_netnames_minute"} {
		var n int64
		if err := s.read.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+table+` WHERE user_id = ? AND qname = ?`, user, wire.Other).Scan(&n); err != nil {
			return nil, err
		}
		out[table] = n
	}
	return out, nil
}

func (s *Store) reportFocus(ctx context.Context, user string, since int64) (zone, qname, netKey string) {
	s.read.QueryRowContext(ctx, `SELECT zone FROM dnstap_totals_minute WHERE user_id = ? AND bucket >= ? GROUP BY zone ORDER BY SUM(count) DESC LIMIT 1`, user, since).Scan(&zone)
	s.read.QueryRowContext(ctx, `SELECT qname FROM dnstap_qnames_minute WHERE user_id = ? AND bucket >= ? AND qname <> ? GROUP BY qname ORDER BY SUM(count) DESC LIMIT 1`, user, since, wire.Other).Scan(&qname)
	s.read.QueryRowContext(ctx, `SELECT net FROM dnstap_nets_minute WHERE user_id = ? AND bucket >= ? AND net <> ? GROUP BY net ORDER BY SUM(count) DESC LIMIT 1`, user, since, wire.Other).Scan(&netKey)
	return zone, qname, netKey
}

// Traffic generator. Names and client networks follow Zipf distributions over
// fixed pools, so the same popular names return minute after minute; 8% of
// queries are one-off junk names answered NXDOMAIN, which fold into Other rows.

type weighted struct {
	v string
	w float64
}

var (
	genQTypes = []weighted{{"A", 45}, {"AAAA", 30}, {"HTTPS", 8}, {"MX", 4}, {"TXT", 4}, {"NS", 3}, {"SOA", 2}, {"DNSKEY", 2}, {"PTR", 1}, {"ANY", 1}}
	genRCodes = []weighted{{"NOERROR", 92}, {"NXDOMAIN", 5}, {"REFUSED", 2}, {"SERVFAIL", 1}}
	// genSizes weights the response-size buckets of wire.SizeBuckets, then "larger" (0).
	genSizes  = []float64{5, 35, 30, 20, 8, 1.5, 0.5}
	genLabels = []string{"www", "mail", "api", "cdn", "ns1", "ns2", "smtp", "imap", "git", "vpn", "static", "img"}
)

func pick(r *rand.Rand, list []weighted) string {
	var sum float64
	for _, w := range list {
		sum += w.w
	}
	x := r.Float64() * sum
	for _, w := range list {
		if x < w.w {
			return w.v
		}
		x -= w.w
	}
	return list[len(list)-1].v
}

type genName struct{ zone, qname string }

type generator struct {
	rng      *rand.Rand
	zones    []string
	names    []genName
	nets     []string
	nameZipf *rand.Zipf
	netZipf  *rand.Zipf
	// foldSubnet folds client rows below the cap per subnet only (KNOTREPORT_FOLD=subnet).
	foldSubnet bool
}

func newGenerator(seed int64, c reportCfg) *generator {
	r := rand.New(rand.NewSource(seed))
	g := &generator{rng: r, foldSubnet: c.Fold == "subnet"}
	for i := 0; i < c.Zones; i++ {
		g.zones = append(g.zones, fmt.Sprintf("zone%02d.example.net.", i))
	}
	// Pools come from a fixed seed so every collector serves the same names
	// and sees the same networks; only the traffic differs per collector.
	pool := rand.New(rand.NewSource(1))
	zoneZipf := rand.NewZipf(pool, 1.2, 1, uint64(len(g.zones)-1))
	for i := 0; i < c.Names; i++ {
		z := g.zones[zoneZipf.Uint64()]
		label := fmt.Sprintf("%s-%x", genLabels[i%len(genLabels)], i/len(genLabels))
		g.names = append(g.names, genName{zone: z, qname: label + "." + z})
	}
	seen := map[string]bool{}
	for len(g.nets) < c.Nets {
		var n string
		if pool.Float64() < 0.85 {
			n = fmt.Sprintf("%d.%d.%d.0/24", 1+pool.Intn(222), pool.Intn(256), pool.Intn(256))
		} else {
			n = fmt.Sprintf("2a01:%x:%x::/48", 1+pool.Intn(65535), pool.Intn(65536))
		}
		if !seen[n] {
			seen[n] = true
			g.nets = append(g.nets, n)
		}
	}
	g.nameZipf = rand.NewZipf(r, 1.07, 1, uint64(len(g.names)-1))
	g.netZipf = rand.NewZipf(r, 1.2, 1, uint64(len(g.nets)-1))
	return g
}

const junkChars = "abcdefghijklmnopqrstuvwxyz0123456789"

func (g *generator) junk() string {
	b := make([]byte, 12)
	for i := range b {
		b[i] = junkChars[g.rng.Intn(len(junkChars))]
	}
	return string(b)
}

type genMinute struct {
	totals  map[[3]string]uint64
	names   map[[4]string]uint64
	clients map[[5]string]uint64
	subnets map[string]uint64
	sizes   []uint64
	// foldSubnet: see generator.foldSubnet.
	foldSubnet bool
}

func (g *generator) sample(n int) *genMinute {
	m := &genMinute{
		totals:  map[[3]string]uint64{},
		names:   map[[4]string]uint64{},
		clients: map[[5]string]uint64{},
		subnets: map[string]uint64{},
		sizes:   make([]uint64, len(genSizes)),
		// copied so bucket folds the way the generator was configured
		foldSubnet: g.foldSubnet,
	}
	var sizeSum float64
	for _, w := range genSizes {
		sizeSum += w
	}
	for i := 0; i < n; i++ {
		qtype := pick(g.rng, genQTypes)
		var zone, qname, rcode string
		if g.rng.Float64() < 0.08 {
			zone = g.zones[g.rng.Intn(len(g.zones))]
			qname = g.junk() + "." + zone
			rcode = "NXDOMAIN"
		} else {
			nm := g.names[g.nameZipf.Uint64()]
			zone, qname = nm.zone, nm.qname
			rcode = pick(g.rng, genRCodes)
		}
		subnet := g.nets[g.netZipf.Uint64()]
		m.totals[[3]string{zone, qtype, rcode}]++
		m.names[[4]string{zone, qname, qtype, rcode}]++
		m.clients[[5]string{subnet, zone, qname, qtype, rcode}]++
		m.subnets[subnet]++
		x := g.rng.Float64() * sizeSum
		for b, w := range genSizes {
			if x < w || b == len(genSizes)-1 {
				m.sizes[b]++
				break
			}
			x -= w
		}
	}
	return m
}

// bucket folds a sampled minute the way the collector does (dnstap.go
// minute.bucket): names and client lists cut with the bound rule. Client
// lists are sent when maxClients and clientTop are above zero (maxClients is
// otherwise unused: the collector cuts networks at top). scale multiplies
// every count.
func (m *genMinute) bucket(start int64, top, maxClients, clientTop int, scale uint64) wire.Dnstap {
	d := wire.Dnstap{MinuteMs: start}
	for k, n := range m.totals {
		d.Totals = append(d.Totals, wire.Total{Zone: k[0], QType: k[1], RCode: k[2], Count: n * scale})
	}
	type nameRow struct {
		k  [4]string
		n  uint64
		id string
	}
	names := make([]nameRow, 0, len(m.names))
	for k, n := range m.names {
		names = append(names, nameRow{k, n, strings.Join(k[:], "\x00")})
	}
	sort.Slice(names, func(i, j int) bool {
		if names[i].n != names[j].n {
			return names[i].n > names[j].n
		}
		return names[i].id < names[j].id
	})
	// The bound rule, as the collector applies it (wire.Cut): rows above the
	// cut stay, ties at the cut fold together, and the cut is the error. The
	// same rule cuts per-name counts.
	other := map[[3]string]uint64{}
	counts := make([]uint64, len(names))
	byName := map[[2]string]uint64{}
	for i, r := range names {
		counts[i] = r.n
		byName[[2]string{r.k[0], r.k[1]}] += r.n
	}
	cut := wire.Cut(counts, top)
	for _, r := range names {
		if r.n > cut {
			d.Names = append(d.Names, wire.Name{Zone: r.k[0], QName: r.k[1], QType: r.k[2], RCode: r.k[3], Count: r.n * scale})
			continue
		}
		other[[3]string{r.k[0], r.k[2], r.k[3]}] += r.n
	}
	for k, n := range other {
		d.Names = append(d.Names, wire.Name{Zone: k[0], QName: wire.Other, QType: k[1], RCode: k[2], Count: n * scale})
	}
	type qn struct {
		k [2]string
		n uint64
	}
	qns := make([]qn, 0, len(byName))
	for k, n := range byName {
		qns = append(qns, qn{k, n})
	}
	sort.Slice(qns, func(i, j int) bool {
		if qns[i].n != qns[j].n {
			return qns[i].n > qns[j].n
		}
		return qns[i].k[0]+"\x00"+qns[i].k[1] < qns[j].k[0]+"\x00"+qns[j].k[1]
	})
	qcounts := make([]uint64, len(qns))
	for i, x := range qns {
		qcounts[i] = x.n
	}
	qcut := wire.Cut(qcounts, top)
	qother := map[string]uint64{}
	for _, x := range qns {
		if x.n > qcut {
			d.QNames = append(d.QNames, wire.QName{Zone: x.k[0], QName: x.k[1], Count: x.n * scale})
			continue
		}
		qother[x.k[0]] += x.n
	}
	for z, n := range qother {
		d.QNames = append(d.QNames, wire.QName{Zone: z, QName: wire.Other, Count: n * scale})
	}
	d.Bounded, d.NameErr, d.QNameErr = true, cut*scale, qcut*scale

	if maxClients > 0 && clientTop > 0 && len(m.subnets) > 0 {
		m.netLists(&d, top, clientTop, scale)
	}
	for i, n := range m.sizes {
		if n == 0 {
			continue
		}
		upTo := 0
		if i < len(wire.SizeBuckets) {
			upTo = wire.SizeBuckets[i]
		}
		d.Sizes = append(d.Sizes, wire.Size{UpTo: upTo, Count: n * scale})
	}
	return d
}

// netLists cuts the client networks the way the collector does (dnstap.go
// minute.netLists): networks at top with one Other network; network by name
// and network by qtype and rcode at clientTop, folding into one row per zone.
func (m *genMinute) netLists(d *wire.Dnstap, top, clientTop int, scale uint64) {
	type row struct {
		k  [4]string
		n  uint64
		id string
	}
	// cut sorts rows (ids precomputed, so ties sort cheaply) and returns the
	// bound rule's cut.
	cut := func(rows []row, top int) uint64 {
		sort.Slice(rows, func(i, j int) bool {
			if rows[i].n != rows[j].n {
				return rows[i].n > rows[j].n
			}
			return rows[i].id < rows[j].id
		})
		counts := make([]uint64, len(rows))
		for i, r := range rows {
			counts[i] = r.n
		}
		return wire.Cut(counts, top)
	}
	nets := make([]row, 0, len(m.subnets))
	for s, n := range m.subnets {
		nets = append(nets, row{k: [4]string{s}, n: n, id: s})
	}
	c := cut(nets, top)
	var otherNet uint64
	for _, r := range nets {
		if r.n > c {
			d.Nets = append(d.Nets, wire.Net{Subnet: r.k[0], Count: r.n * scale})
			continue
		}
		otherNet += r.n
	}
	if otherNet > 0 {
		d.Nets = append(d.Nets, wire.Net{Subnet: wire.Other, Count: otherNet * scale})
	}
	d.NetErr = c * scale

	byName := map[[3]string]uint64{}
	byCode := map[[4]string]uint64{}
	for k, n := range m.clients {
		byName[[3]string{k[0], k[1], k[2]}] += n
		byCode[[4]string{k[0], k[1], k[3], k[4]}] += n
	}
	names := make([]row, 0, len(byName))
	for k, n := range byName {
		names = append(names, row{k: [4]string{k[0], k[1], k[2]}, n: n, id: k[0] + "\x00" + k[1] + "\x00" + k[2]})
	}
	c = cut(names, clientTop)
	folded := map[string]uint64{}
	for _, r := range names {
		if r.n > c {
			d.NetNames = append(d.NetNames, wire.NetName{Subnet: r.k[0], Zone: r.k[1], QName: r.k[2], Count: r.n * scale})
			continue
		}
		folded[r.k[1]] += r.n
	}
	for z, n := range folded {
		d.NetNames = append(d.NetNames, wire.NetName{Subnet: wire.Other, Zone: z, QName: wire.Other, Count: n * scale})
	}
	d.NetNameErr = c * scale

	codes := make([]row, 0, len(byCode))
	for k, n := range byCode {
		codes = append(codes, row{k: k, n: n, id: strings.Join(k[:], "\x00")})
	}
	c = cut(codes, clientTop)
	folded = map[string]uint64{}
	for _, r := range codes {
		if r.n > c {
			d.NetCodes = append(d.NetCodes, wire.NetCode{Subnet: r.k[0], Zone: r.k[1], QType: r.k[2], RCode: r.k[3], Count: r.n * scale})
			continue
		}
		folded[r.k[1]] += r.n
	}
	for z, n := range folded {
		d.NetCodes = append(d.NetCodes, wire.NetCode{Subnet: wire.Other, Zone: z, QType: wire.Other, RCode: wire.Other, Count: n * scale})
	}
	d.NetCodeErr = c * scale
}

func rowCount(d wire.Dnstap) int {
	return len(d.Totals) + len(d.Names) + len(d.QNames) + len(d.Nets) + len(d.NetNames) + len(d.NetCodes) + len(d.Sizes)
}

// insertHourly writes a synthetic hour straight into the hourly tables. It
// stands in for history older than the generated minutes, so long windows
// have hourly rows to read without generating months of minutes.
func insertHourly(ctx context.Context, s *Store, user, col string, d wire.Dnstap) error {
	tx, err := s.write.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	rows := map[string][][]any{}
	for _, x := range d.Totals {
		rows["totals"] = append(rows["totals"], []any{x.Zone, x.QType, x.RCode, int64(x.Count)})
	}
	for _, x := range d.Names {
		rows["qtypes"] = append(rows["qtypes"], []any{x.Zone, x.QName, x.QType, x.RCode, int64(x.Count)})
	}
	for _, x := range qnamesOf(d) {
		rows["qnames"] = append(rows["qnames"], []any{x.Zone, x.QName, int64(x.Count)})
	}
	netKey := func(s string) string {
		if k, ok := netKeyOf(s); ok {
			return k
		}
		return wire.Other
	}
	nl := netsOf(d)
	for _, x := range nl.nets {
		rows["nets"] = append(rows["nets"], []any{netKey(x.Subnet), int64(x.Count)})
	}
	for _, x := range nl.names {
		rows["netnames"] = append(rows["netnames"], []any{netKey(x.Subnet), x.Zone, x.QName, int64(x.Count)})
	}
	for _, x := range nl.codes {
		rows["netcodes"] = append(rows["netcodes"], []any{netKey(x.Subnet), x.Zone, x.QType, x.RCode, int64(x.Count)})
	}
	for _, x := range d.Sizes {
		rows["sizes"] = append(rows["sizes"], []any{x.UpTo, int64(x.Count)})
	}
	for _, dim := range dims {
		if len(rows[dim.name]) == 0 {
			continue
		}
		marks := strings.TrimSuffix(strings.Repeat("?, ", len(dim.keys)), ", ")
		stmt, err := tx.PrepareContext(ctx, fmt.Sprintf(`INSERT INTO dnstap_%s_hourly (user_id, collector_id, bucket, %s, count) VALUES (?, ?, ?, %s, ?)`,
			dim.name, strings.Join(dim.keys, ", "), marks))
		if err != nil {
			return err
		}
		for _, r := range rows[dim.name] {
			if _, err := stmt.ExecContext(ctx, append([]any{user, col, d.MinuteMs}, r...)...); err != nil {
				stmt.Close()
				return err
			}
		}
		stmt.Close()
	}
	return tx.Commit()
}

// Report types.

type objSize struct {
	Name     string  `json:"name"`
	Table    string  `json:"table"`
	Index    bool    `json:"index"`
	Bytes    int64   `json:"bytes"`
	Pages    int64   `json:"pages"`
	Unused   int64   `json:"unused_bytes"`
	Rows     int64   `json:"rows,omitempty"`
	BytesRow float64 `json:"bytes_per_row,omitempty"`
}

type timing struct {
	Count       int     `json:"count"`
	TotalS      float64 `json:"total_s"`
	P50Ms       float64 `json:"p50_ms"`
	P95Ms       float64 `json:"p95_ms"`
	MaxMs       float64 `json:"max_ms"`
	FirstDayP50 float64 `json:"first_day_p50_ms,omitempty"`
	LastDayP50  float64 `json:"last_day_p50_ms,omitempty"`
}

type queryCase struct {
	Window   string  `json:"window"`
	Dim      string  `json:"dim"`
	Filter   string  `json:"filter"`
	FirstMs  float64 `json:"first_ms"`
	MedianMs float64 `json:"median_ms"`
	Rows     int     `json:"rows"`
	Total    int64   `json:"total"`
	Err      string  `json:"error,omitempty"`
}

type planEntry struct {
	SQL       string   `json:"sql"`
	Phases    []string `json:"phases"`
	Details   []string `json:"plan"`
	Indexes   []string `json:"indexes,omitempty"`
	FullScans []string `json:"full_scans,omitempty"`
}

type fileStats struct {
	Bytes    int64 `json:"bytes"`
	WALBytes int64 `json:"wal_bytes"`
	PageSize int64 `json:"page_size"`
	Pages    int64 `json:"pages"`
	Freelist int64 `json:"freelist_pages"`
}

type storageReport struct {
	Mode         string      `json:"mode"`
	Config       reportCfg   `json:"config"`
	File         fileStats   `json:"file"`
	Objects      []objSize   `json:"objects"`
	BeforeSynth  []objSize   `json:"objects_before_synthetic_hours,omitempty"`
	Writes       *timing     `json:"writes,omitempty"`
	RowsWritten  int64       `json:"rows_written,omitempty"`
	RowsPerMin   float64     `json:"rows_per_minute,omitempty"`
	RollupHours  int         `json:"rollup_hours,omitempty"`
	RollupS      float64     `json:"rollup_s,omitempty"`
	RolledHours  int64       `json:"rolled_hours"`
	MinuteDays   float64     `json:"minute_days"`
	Queries      []queryCase `json:"queries"`
	Plans        []planEntry `json:"plans"`
	Unused       []string    `json:"unused_indexes"`
	PruneMinuteS float64     `json:"prune_one_minute_day_s,omitempty"`
	PruneHourlyS float64     `json:"prune_one_hourly_day_s,omitempty"`
	PrunedMinute int64       `json:"pruned_minute_rows,omitempty"`
	PrunedHourly int64       `json:"pruned_hourly_rows,omitempty"`
	FileAfter    *fileStats  `json:"file_after_prune,omitempty"`
	VacuumS      float64     `json:"vacuum_s,omitempty"`
	VacuumBytes  int64       `json:"vacuum_bytes,omitempty"`
	Projection   projection  `json:"projection_per_collector"`
	// OtherRows counts the folded (other) rows per minute table.
	OtherRows map[string]int64 `json:"other_rows"`
	// topn stores only.
	TopN               *tblk.Stats `json:"topn_stats,omitempty"`
	HistoryRollupS     float64     `json:"history_rollup_s,omitempty"`
	HistoryRollupSteps int         `json:"history_rollup_steps,omitempty"`
	// InlineRollup is the hourly maintenance during generation (topn stores).
	InlineRollupS     float64 `json:"inline_rollup_s,omitempty"`
	InlineRollupSteps int     `json:"inline_rollup_steps,omitempty"`
	PeakRSSKB         int64   `json:"peak_rss_kb"`
}

// diskSize is a file's size, or everything under a directory.
func diskSize(path string) int64 {
	var n int64
	filepath.WalkDir(path, func(_ string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			if fi, err := d.Info(); err == nil {
				n += fi.Size()
			}
		}
		return nil
	})
	for _, sfx := range []string{"-wal"} {
		if fi, err := os.Stat(path + sfx); err == nil {
			n += fi.Size()
		}
	}
	return n
}

// peakRSSKB reads the process's peak resident memory (VmHWM) on Linux.
func peakRSSKB() int64 {
	raw, err := os.ReadFile("/proc/self/status")
	if err != nil {
		return 0
	}
	for _, line := range strings.Split(string(raw), "\n") {
		if v, ok := strings.CutPrefix(line, "VmHWM:"); ok {
			n, _ := strconv.ParseInt(strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(v), "kB")), 10, 64)
			return n
		}
	}
	return 0
}

type projection struct {
	MinuteBytesPerDay  int64 `json:"minute_bytes_per_day"`
	HourlyBytesPerHour int64 `json:"hourly_bytes_per_hour"`
	// Steady state with the given minute retention and 7 or 90 days of hourly rows.
	Week       int64 `json:"bytes_week"`
	NinetyDays int64 `json:"bytes_90_days"`
	// topn stores: the day tier, and all tiers at default retention (7d, 90d, 400d).
	DailyBytesPerDay  int64   `json:"daily_bytes_per_day,omitempty"`
	AllTiers          int64   `json:"bytes_all_tiers,omitempty"`
	MinuteBytesPerRow float64 `json:"minute_bytes_per_row,omitempty"`
}

func percentile(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	i := int(p * float64(len(sorted)-1))
	return sorted[i]
}

func summarize(ms []float64, perDay int) *timing {
	t := &timing{Count: len(ms)}
	for _, v := range ms {
		t.TotalS += v / 1000
	}
	s := append([]float64(nil), ms...)
	sort.Float64s(s)
	t.P50Ms, t.P95Ms, t.MaxMs = percentile(s, 0.5), percentile(s, 0.95), percentile(s, 1)
	if perDay > 0 && len(ms) >= 2*perDay {
		first := append([]float64(nil), ms[:perDay]...)
		last := append([]float64(nil), ms[len(ms)-perDay:]...)
		sort.Float64s(first)
		sort.Float64s(last)
		t.FirstDayP50, t.LastDayP50 = percentile(first, 0.5), percentile(last, 0.5)
	}
	return t
}

func fileOf(t *testing.T, db *sql.DB, path string) fileStats {
	t.Helper()
	var f fileStats
	if st, err := os.Stat(path); err == nil {
		f.Bytes = st.Size()
	}
	if st, err := os.Stat(path + "-wal"); err == nil {
		f.WALBytes = st.Size()
	}
	for _, p := range []struct {
		pragma string
		dst    *int64
	}{{"page_size", &f.PageSize}, {"page_count", &f.Pages}, {"freelist_count", &f.Freelist}} {
		if err := db.QueryRow("PRAGMA " + p.pragma).Scan(p.dst); err != nil {
			t.Fatal(err)
		}
	}
	return f
}

// objects reads per-b-tree sizes from dbstat and row counts per table.
func objects(t *testing.T, db *sql.DB) []objSize {
	t.Helper()
	kind := map[string][2]string{} // name → type, table
	rows, err := db.Query(`SELECT name, type, tbl_name FROM sqlite_schema WHERE type IN ('table', 'index')`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var n, ty, tbl string
		if err := rows.Scan(&n, &ty, &tbl); err != nil {
			t.Fatal(err)
		}
		kind[n] = [2]string{ty, tbl}
	}
	rows.Close()
	rows, err = db.Query(`SELECT name, pageno, pgsize, unused FROM dbstat WHERE aggregate = 1`)
	if err != nil {
		t.Fatalf("dbstat: %v", err)
	}
	var out []objSize
	for rows.Next() {
		var o objSize
		if err := rows.Scan(&o.Name, &o.Pages, &o.Bytes, &o.Unused); err != nil {
			t.Fatal(err)
		}
		k := kind[o.Name]
		o.Table, o.Index = k[1], k[0] == "index"
		if o.Table == "" {
			o.Table = o.Name
		}
		out = append(out, o)
	}
	rows.Close()
	for i := range out {
		if out[i].Index || strings.HasPrefix(out[i].Name, "sqlite_") {
			continue
		}
		if err := db.QueryRow(`SELECT COUNT(*) FROM "` + out[i].Name + `"`).Scan(&out[i].Rows); err != nil {
			t.Fatal(err)
		}
		if out[i].Rows > 0 {
			out[i].BytesRow = float64(out[i].Bytes) / float64(out[i].Rows)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Bytes > out[j].Bytes })
	return out
}

// grainBytes sums the tables (with their indexes) of one grain.
func grainBytes(objs []objSize, grain string) int64 {
	var n int64
	for _, o := range objs {
		if (strings.HasPrefix(o.Table, "dnstap_") || strings.HasPrefix(o.Table, "dnstap2_")) && strings.HasSuffix(o.Table, "_"+grain) {
			n += o.Bytes
		}
		// Per-minute bookkeeping and the v2 key dictionaries count with the minutes.
		if grain == "minute" && (o.Table == "dnstap_minutes" || o.Table == "dnstap2_minutes" || o.Table == "dnstap2_keys" || o.Table == "dnstap2_nets") {
			n += o.Bytes
		}
	}
	return n
}

func mb(b int64) string { return fmt.Sprintf("%.1f MB", float64(b)/1e6) }

func TestStorageReport(t *testing.T) {
	cfg := loadCfg(t)
	ctx := context.Background()
	rep := storageReport{Config: cfg}
	// save writes the report so far, so a run stopped by the test timeout keeps what it measured.
	save := func(phase string) {
		if cfg.Out == "" {
			return
		}
		b, err := json.MarshalIndent(rep, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(cfg.Out, b, 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("report saved after %s", phase)
	}

	path := cfg.DB
	generated := path == ""
	if generated {
		rep.Mode = "generated"
		if err := os.MkdirAll(cfg.Dir, 0o755); err != nil {
			t.Fatal(err)
		}
		path = filepath.Join(cfg.Dir, fmt.Sprintf("knot-%s-%d.db", cfg.Store, time.Now().Unix()))
		if strings.HasPrefix(cfg.Store, "topn-") {
			path = strings.TrimSuffix(path, ".db") // a directory
		}
		if !cfg.Keep {
			t.Cleanup(func() {
				os.RemoveAll(path)
				for _, suffix := range []string{"-wal", "-shm"} {
					os.Remove(path + suffix)
				}
			})
		}
		t.Logf("database: %s", path)
	} else {
		rep.Mode = "existing"
	}
	var s reportStore
	dbPath := path // the SQLite file dbstat, EXPLAIN and VACUUM look at; none for blocks
	switch cfg.Store {
	case "v2":
		s = openV2(t, path)
	case "topn-sql":
		s = openKnotTopN(t, path, tblk.SQL, 1000)
		dbPath = filepath.Join(path, "topn.db")
	case "topn-blocks":
		s = openKnotTopN(t, path, tblk.Blocks, 1000)
		dbPath = ""
	default:
		s = openTraced(t, path)
	}
	defer s.Close()
	var plain *sql.DB
	if dbPath != "" {
		var err error
		if plain, err = sql.Open("sqlite", dsn(dbPath)); err != nil {
			t.Fatal(err)
		}
		defer plain.Close()
	}
	var err error

	const user = "5b0c8a2e-3f6d-4c1b-9e7a-2d4f6a8c0e11"
	userID := user
	end := time.Date(2026, 11, 18, 0, 0, 0, 0, time.UTC).UnixMilli()
	oldest := end

	if generated {
		var cols []string
		var gens []*generator
		for c := 0; c < cfg.Collectors; c++ {
			cols = append(cols, fmt.Sprintf("9d3e7f10-%04x-4a5b-8c6d-0e1f2a3b4c5d", c))
			gens = append(gens, newGenerator(int64(c+1), cfg))
		}
		first := end - int64(cfg.Days)*dayMs
		tracer.setPhase("save")
		var writes []float64
		started := time.Now()
		for m := first; m < end; m += minuteMs {
			for c, col := range cols {
				d := gens[c].sample(cfg.QPM).bucket(m, cfg.Top, cfg.Clients, cfg.ClientTop, 1)
				t0 := time.Now()
				if err := s.SaveDnstap(ctx, userID, col, d, m+minuteMs); err != nil {
					t.Fatal(err)
				}
				writes = append(writes, float64(time.Since(t0).Microseconds())/1000)
				rep.RowsWritten += int64(rowCount(d))
			}
			// topn stores maintain every hour as the server would, so the
			// block engine's head stays a few hours long; timed apart from writes.
			if _, ok := s.(*knotTopN); ok && (m+minuteMs)%hourMs == 0 {
				tracer.setPhase("rollup")
				t0 := time.Now()
				for {
					n, err := s.Rollup(ctx, m+minuteMs)
					if err != nil {
						t.Fatal(err)
					}
					if n == 0 {
						break
					}
					rep.InlineRollupSteps += n
				}
				rep.InlineRollupS += time.Since(t0).Seconds()
				tracer.setPhase("save")
			}
			if (m-first+minuteMs)%(12*hourMs) == 0 {
				size := diskSize(path)
				t.Logf("generated %.1f of %d days, %d rows, file %s, %s elapsed",
					float64(m-first+minuteMs)/float64(dayMs), cfg.Days, rep.RowsWritten, mb(size), time.Since(started).Round(time.Second))
			}
		}
		rep.Writes = summarize(writes, 1440*cfg.Collectors)
		rep.RowsPerMin = float64(rep.RowsWritten) / float64(len(writes))

		tracer.setPhase("rollup")
		t0 := time.Now()
		for {
			n, err := s.Rollup(ctx, end)
			if err != nil {
				t.Fatal(err)
			}
			if n == 0 {
				break
			}
			rep.RollupHours += n
		}
		rep.RollupS = time.Since(t0).Seconds()
		t.Logf("rolled %d hours in %.1fs", rep.RollupHours, rep.RollupS)

		tracer.setPhase("setup")
		if plain != nil {
			rep.BeforeSynth = objects(t, plain)
		}

		// Hourly-only history before the minutes: one sampled quarter of each
		// hour, scaled by 4, folded to TopHourly rows like the rollup.
		synthFirst := first - int64(cfg.HourlyDays)*dayMs
		for h := synthFirst; h < first; h += hourMs {
			for c, col := range cols {
				d := gens[c].sample(cfg.QPM*15).bucket(h, s.reportTop(), cfg.Clients*4, s.reportTop(), 4)
				if err := s.reportHourly(ctx, userID, col, d); err != nil {
					t.Fatal(err)
				}
			}
			if done := h - synthFirst + hourMs; done%(5*dayMs) == 0 {
				t.Logf("hourly-only rows: %d of %d days", done/dayMs, cfg.HourlyDays)
			}
		}
		oldest = synthFirst

		// Daily-only history before that, for stores with a day tier: a
		// sampled half hour of each day, scaled by 48, folded to the day cap.
		if dd, ok := s.(interface {
			reportDaily(context.Context, string, string, wire.Dnstap) error
		}); ok && cfg.DailyDays > 0 {
			dailyFirst := synthFirst - int64(cfg.DailyDays)*dayMs
			for d := dailyFirst; d < synthFirst; d += dayMs {
				for c, col := range cols {
					day := gens[c].sample(cfg.QPM*30).bucket(d, s.reportTop(), cfg.Clients*8, s.reportTop(), 48)
					if err := dd.reportDaily(ctx, userID, col, day); err != nil {
						t.Fatal(err)
					}
				}
			}
			t.Logf("daily-only rows: %d days", cfg.DailyDays)
		} else if cfg.DailyDays > 0 {
			t.Logf("KNOTREPORT_DAILY_DAYS ignored: %s has no day tier", cfg.Store)
		}
		if fl, ok := s.(interface{ reportFlush(context.Context) error }); ok {
			if err := fl.reportFlush(ctx); err != nil {
				t.Fatal(err)
			}
		}
		// topn stores roll the imported hourly days into days.
		if _, ok := s.(*knotTopN); ok {
			tracer.setPhase("rollup")
			t0 := time.Now()
			for {
				n, err := s.Rollup(ctx, end)
				if err != nil {
					t.Fatal(err)
				}
				if n == 0 {
					break
				}
				rep.HistoryRollupSteps += n
			}
			rep.HistoryRollupS = time.Since(t0).Seconds()
			t.Logf("rolled imported history: %d steps in %.1fs", rep.HistoryRollupSteps, rep.HistoryRollupS)
			tracer.setPhase("setup")
		}
	} else {
		tracer.setPhase("setup")
		var last, firstMin sql.NullInt64
		if err := plain.QueryRow(`SELECT user_id FROM dnstap_totals_minute GROUP BY user_id ORDER BY COUNT(*) DESC LIMIT 1`).Scan(&userID); err != nil {
			t.Fatalf("no dnstap minutes to measure: %v", err)
		}
		if err := plain.QueryRow(`SELECT MIN(bucket), MAX(bucket) FROM dnstap_totals_minute WHERE user_id = ?`, userID).Scan(&firstMin, &last); err != nil {
			t.Fatal(err)
		}
		end = last.Int64 + minuteMs
		oldest = firstMin.Int64
		var firstHour sql.NullInt64
		if err := plain.QueryRow(`SELECT MIN(bucket) FROM dnstap_totals_hourly WHERE user_id = ?`, userID).Scan(&firstHour); err == nil && firstHour.Valid && firstHour.Int64 < oldest {
			oldest = firstHour.Int64
		}
		if cfg.Mutate {
			tracer.setPhase("rollup")
			t0 := time.Now()
			for {
				n, err := s.Rollup(ctx, end)
				if err != nil {
					t.Fatal(err)
				}
				if n == 0 {
					break
				}
				rep.RollupHours += n
			}
			rep.RollupS = time.Since(t0).Seconds()
		}
		tracer.setPhase("setup")
	}

	if plain != nil {
		rep.File = fileOf(t, plain, dbPath)
		rep.Objects = objects(t, plain)
	} else {
		rep.File.Bytes = diskSize(path)
	}
	base := rep.BeforeSynth
	if base == nil {
		base = rep.Objects
	}
	if generated {
		rep.MinuteDays = float64(cfg.Days)
		rep.RolledHours = int64(rep.RollupHours)
	} else {
		var minuteSpan, hourCount sql.NullInt64
		plain.QueryRow(`SELECT MAX(bucket) - MIN(bucket) + ? FROM dnstap_minutes`, minuteMs).Scan(&minuteSpan)
		plain.QueryRow(`SELECT COUNT(DISTINCT bucket) FROM dnstap_totals_hourly`).Scan(&hourCount)
		rep.MinuteDays = float64(minuteSpan.Int64) / float64(dayMs)
		rep.RolledHours = hourCount.Int64
	}
	collectors := int64(cfg.Collectors)
	if !generated {
		plain.QueryRow(`SELECT COUNT(DISTINCT collector_id) FROM dnstap_minutes`).Scan(&collectors)
		collectors = max(collectors, 1)
	}
	if rep.MinuteDays > 0 {
		rep.Projection.MinuteBytesPerDay = int64(float64(grainBytes(base, "minute")) / rep.MinuteDays / float64(collectors))
	}
	if rep.RolledHours > 0 {
		rep.Projection.HourlyBytesPerHour = grainBytes(base, "hourly") / rep.RolledHours / collectors
	}
	keepMin := int64(cfg.MinuteRetentionDays)
	rep.Projection.Week = rep.Projection.MinuteBytesPerDay*min(keepMin, 7) + rep.Projection.HourlyBytesPerHour*24*7
	rep.Projection.NinetyDays = rep.Projection.MinuteBytesPerDay*keepMin + rep.Projection.HourlyBytesPerHour*24*90
	if tk, ok := s.(*knotTopN); ok {
		st, err := tk.s.Stats(ctx)
		if err != nil {
			t.Fatal(err)
		}
		rep.TopN = &st
		units := func(g tblk.Grain) int64 {
			if st.Last[g] <= st.First[g] {
				return 0
			}
			return (st.Last[g] - st.First[g]) / g.Ms()
		}
		p := &rep.Projection
		p.MinuteBytesPerDay = int64(float64(st.Bytes[tblk.Minute]+st.HeadBytes+st.DictBytes) / rep.MinuteDays / float64(collectors))
		if n := units(tblk.Hour); n > 0 {
			p.HourlyBytesPerHour = st.Bytes[tblk.Hour] / n / collectors
		}
		if n := units(tblk.Day); n > 0 {
			p.DailyBytesPerDay = st.Bytes[tblk.Day] / n / collectors
		}
		p.Week = p.MinuteBytesPerDay*min(keepMin, 7) + p.HourlyBytesPerHour*24*7
		p.NinetyDays = p.MinuteBytesPerDay*keepMin + p.HourlyBytesPerHour*24*90
		p.AllTiers = p.NinetyDays + p.DailyBytesPerDay*400
		var samples int64
		for _, byGrain := range st.Rows {
			samples += byGrain[tblk.Minute]
		}
		if samples > 0 {
			p.MinuteBytesPerRow = float64(st.Bytes[tblk.Minute]+st.HeadBytes+st.DictBytes) / float64(samples)
		}
	}

	if rep.OtherRows, err = s.reportOther(ctx, userID); err != nil {
		t.Fatal(err)
	}

	// Focus values for filtered cases: the busiest zone, name and network of the last day.
	zone, qname, netKey := s.reportFocus(ctx, userID, end-dayMs)
	var client netip.Prefix
	if netKey != "" {
		if p, err := netip.ParsePrefix(netLabel(netKey)); err == nil {
			bits := 16
			if p.Addr().Is6() {
				bits = 32
			}
			client = netip.PrefixFrom(p.Addr(), bits).Masked()
		}
	}
	t.Logf("filters: zone=%s qname=%s client=%s", zone, qname, client)
	save("sizes")

	// The page's drill-downs, built the way http.go parseWindow builds them.
	tracer.setPhase("page")
	if cs, ok := s.(*Store); ok {
		if _, err := cs.LastDnstap(ctx, userID); err != nil {
			t.Fatal(err)
		}
	}
	minuteFrom := end - keepMin*dayMs
	windows := []struct {
		name string
		span int64
	}{{"1h", hourMs}, {"24h", dayMs}, {"7d", 7 * dayMs}, {"30d", 30 * dayMs}}
	filtersList := []string{"none", "zone", "qname", "client"}
	dimsList := []string{"qname", "zone", "qtype", "rcode", "client", "size"}
	for _, w := range windows {
		if w.span > end-oldest {
			t.Logf("skipping %s: only %.1f days of data", w.name, float64(end-oldest)/float64(dayMs))
			continue
		}
		// hourly routes windows of 24h or more to hourly rows, as a changed
		// http.go minuteFrom would: grain of an hour, MinuteFrom past the window.
		hourly := cfg.Route == "hourly" && w.span >= dayMs
		start, stop, step := pageWindow(end, w.span, minuteFrom, hourly)
		for _, f := range filtersList {
			for _, dim := range dimsList {
				q := Query{UserID: userID, Start: start, End: stop, Step: step, MinuteFrom: minuteFrom}
				if hourly {
					q.MinuteFrom = stop
				}
				switch f {
				case "zone":
					q.Zone = zone
				case "qname":
					q.QName = qname
				case "client":
					q.Client = client
				}
				c := queryCase{Window: w.name, Dim: dim, Filter: f}
				var times []float64
				limit := time.Duration(cfg.QueryTimeoutS) * time.Second
				for r := 0; r < cfg.Runs; r++ {
					qctx, cancel := context.WithTimeout(ctx, limit)
					t0 := time.Now()
					b, err := s.Dnstap(qctx, q, dim, 50)
					ms := float64(time.Since(t0).Microseconds()) / 1000
					timedOut := qctx.Err() != nil
					cancel()
					if timedOut {
						c.Err = fmt.Sprintf("timeout after %ds", cfg.QueryTimeoutS)
						c.FirstMs, c.MedianMs = ms, ms
						break
					}
					if err != nil {
						c.Err = err.Error()
						break
					}
					times = append(times, ms)
					c.Rows, c.Total = len(b.Rows), b.Total
					if ms > 10000 {
						break
					}
				}
				if len(times) > 0 {
					c.FirstMs = times[0]
					sort.Float64s(times)
					c.MedianMs = times[len(times)/2]
				}
				rep.Queries = append(rep.Queries, c)
				t.Logf("query %s %s %s: median %.0f ms, %d rows %s", c.Window, c.Filter, c.Dim, c.MedianMs, c.Rows, c.Err)
			}
		}
	}

	save("queries")

	// Maintenance timings mutate the file; a real database is touched only on request.
	if generated || cfg.Mutate {
		tracer.setPhase("prune")
		minuteKeep := int64(rep.MinuteDays*float64(dayMs)) - dayMs
		if minuteKeep > 0 {
			t0 := time.Now()
			res, err := s.Prune(ctx, Retention{MinuteMs: minuteKeep}, end, []string{userID})
			if err != nil {
				t.Fatal(err)
			}
			rep.PruneMinuteS, rep.PrunedMinute = time.Since(t0).Seconds(), res.Minute
		}
		if hourKeep := end - oldest - dayMs; hourKeep > 0 && rep.RolledHours > 0 {
			t0 := time.Now()
			res, err := s.Prune(ctx, Retention{HourlyMs: hourKeep}, end, []string{userID})
			if err != nil {
				t.Fatal(err)
			}
			rep.PruneHourlyS, rep.PrunedHourly = time.Since(t0).Seconds(), res.Hourly
		}
		tracer.setPhase("setup")
		var after fileStats
		if plain != nil {
			after = fileOf(t, plain, dbPath)
		} else {
			after.Bytes = diskSize(path)
		}
		rep.FileAfter = &after
		save("prune")
	}
	if cfg.Vacuum && plain != nil {
		dst := filepath.Join(filepath.Dir(path), "vacuumed-"+filepath.Base(path))
		os.Remove(dst)
		t0 := time.Now()
		if _, err := plain.Exec(`VACUUM INTO ?`, dst); err != nil {
			t.Fatalf("vacuum into: %v", err)
		}
		rep.VacuumS = time.Since(t0).Seconds()
		if st, err := os.Stat(dst); err == nil {
			rep.VacuumBytes = st.Size()
		}
		os.Remove(dst)
	}

	if plain != nil {
		rep.Plans, rep.Unused = plans(t, plain)
	}
	rep.PeakRSSKB = peakRSSKB()
	logReport(t, &rep)
	save("plans")
}

// pageWindow builds a window ending at end the way http.go parseWindow does:
// hour grain before the oldest minute (or when hourly), a chart step, and
// start and stop aligned to it.
func pageWindow(end, span, minuteFrom int64, hourly bool) (start, stop, step int64) {
	start, stop = end-span, end
	grain := minuteMs
	if start < minuteFrom || hourly {
		grain = hourMs
	}
	step = max(metrics.StepForRange(stop-start), grain)
	step = (step + grain - 1) / grain * grain
	start -= start % step
	if rem := stop % step; rem != 0 {
		stop += step - rem
	}
	return start, stop, step
}

var (
	indexRe = regexp.MustCompile(`USING (?:COVERING )?INDEX (\w+)`)
	scanRe  = regexp.MustCompile(`^SCAN (\w+)`)
)

// plans explains every statement the store ran (outside setup) and lists the
// indexes no plan used. Plain INSERT ... VALUES statements have no plan to show.
func plans(t *testing.T, db *sql.DB) ([]planEntry, []string) {
	t.Helper()
	used := map[string]bool{}
	var out []planEntry
	tracer.mu.Lock()
	seen := make(map[string]map[string]bool, len(tracer.seen))
	for q, p := range tracer.seen {
		seen[q] = p
	}
	tracer.mu.Unlock()
	for q, phases := range seen {
		var ph []string
		for p := range phases {
			if p != "setup" && p != "" {
				ph = append(ph, p)
			}
		}
		if len(ph) == 0 {
			continue
		}
		up := strings.ToUpper(strings.TrimSpace(q))
		if !strings.HasPrefix(up, "SELECT") && !strings.HasPrefix(up, "INSERT") && !strings.HasPrefix(up, "DELETE") {
			continue
		}
		if strings.HasPrefix(up, "INSERT") && !strings.Contains(up, "SELECT") {
			continue
		}
		sort.Strings(ph)
		e := planEntry{SQL: strings.Join(strings.Fields(q), " "), Phases: ph}
		rows, err := db.Query("EXPLAIN QUERY PLAN "+q, make([]any, strings.Count(q, "?"))...)
		if err != nil {
			e.Details = []string{"explain failed: " + err.Error()}
			out = append(out, e)
			continue
		}
		for rows.Next() {
			var id, parent, notused int
			var detail string
			if err := rows.Scan(&id, &parent, &notused, &detail); err != nil {
				t.Fatal(err)
			}
			e.Details = append(e.Details, detail)
			if m := indexRe.FindStringSubmatch(detail); m != nil {
				e.Indexes = append(e.Indexes, m[1])
				used[m[1]] = true
			} else if m := scanRe.FindStringSubmatch(detail); m != nil && !strings.Contains(detail, "USING") {
				e.FullScans = append(e.FullScans, m[1])
			}
		}
		rows.Close()
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].SQL < out[j].SQL })
	var unused []string
	rows, err := db.Query(`SELECT name FROM sqlite_schema WHERE type = 'index' AND name NOT LIKE 'sqlite_autoindex%' ORDER BY name`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			t.Fatal(err)
		}
		if !used[n] {
			unused = append(unused, n)
		}
	}
	return out, unused
}

func logReport(t *testing.T, r *storageReport) {
	var b strings.Builder
	w := tabwriter.NewWriter(&b, 0, 4, 2, ' ', tabwriter.AlignRight)
	fmt.Fprintf(&b, "\nfile %s (wal %s), %d pages of %d bytes, %d free\n", mb(r.File.Bytes), mb(r.File.WALBytes), r.File.Pages, r.File.PageSize, r.File.Freelist)
	if r.Writes != nil {
		fmt.Fprintf(&b, "writes: %d minutes, %.0f rows/minute, p50 %.1f ms, p95 %.1f ms, max %.1f ms, total %.0fs\n",
			r.Writes.Count, r.RowsPerMin, r.Writes.P50Ms, r.Writes.P95Ms, r.Writes.MaxMs, r.Writes.TotalS)
		if r.Writes.FirstDayP50 > 0 {
			fmt.Fprintf(&b, "writes: first day p50 %.1f ms, last day p50 %.1f ms\n", r.Writes.FirstDayP50, r.Writes.LastDayP50)
		}
	}
	for table, n := range r.OtherRows {
		fmt.Fprintf(&b, "(other) rows in %s: %d\n", table, n)
	}
	if r.RollupHours > 0 {
		fmt.Fprintf(&b, "rollup: %d hours in %.1fs (%.0f ms/hour)\n", r.RollupHours, r.RollupS, r.RollupS*1000/float64(r.RollupHours))
	}
	fmt.Fprintf(&b, "\nobject\tbytes\trows\tbytes/row\tunused\t\n")
	for _, o := range r.Objects {
		name := o.Name
		if o.Index {
			name = "  idx " + name
		}
		rows, per := "", ""
		if o.Rows > 0 {
			rows, per = strconv.FormatInt(o.Rows, 10), fmt.Sprintf("%.0f", o.BytesRow)
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t\n", name, mb(o.Bytes), rows, per, mb(o.Unused))
	}
	w.Flush()
	p := r.Projection
	fmt.Fprintf(&b, "\nper collector: minutes %s/day, hours %s/hour → week %s, steady 90 days %s\n",
		mb(p.MinuteBytesPerDay), mb(p.HourlyBytesPerHour), mb(p.Week), mb(p.NinetyDays))
	if r.TopN != nil {
		fmt.Fprintf(&b, "topn: days %s/day, all tiers (7d/90d/400d) %s, minute bytes/row %.1f, files %d, head %s, dictionary %s\n",
			mb(p.DailyBytesPerDay), mb(p.AllTiers), p.MinuteBytesPerRow, r.TopN.Files, mb(r.TopN.HeadBytes), mb(r.TopN.DictBytes))
		for _, g := range []tblk.Grain{tblk.Minute, tblk.Hour, tblk.Day} {
			fmt.Fprintf(&b, "topn %s tier: %s\n", g, mb(r.TopN.Bytes[g]))
		}
	}
	if r.InlineRollupSteps > 0 {
		fmt.Fprintf(&b, "maintenance during generation: %d steps in %.1fs (%.0f ms/step)\n", r.InlineRollupSteps, r.InlineRollupS, r.InlineRollupS*1000/float64(r.InlineRollupSteps))
	}
	fmt.Fprintf(&b, "peak RSS %d MB\n", r.PeakRSSKB/1024)

	fmt.Fprintf(&b, "\nqueries (median of %d runs, ms)\n", r.Config.Runs)
	w = tabwriter.NewWriter(&b, 0, 4, 2, ' ', tabwriter.AlignRight)
	fmt.Fprintf(w, "window\tfilter\tdim\tfirst\tmedian\trows\ttotal\t\n")
	for _, q := range r.Queries {
		flag := ""
		if q.MedianMs > 300 {
			flag = " slow"
		}
		if q.Err != "" {
			flag = " error: " + q.Err
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%.1f\t%.1f\t%d\t%d\t%s\n", q.Window, q.Filter, q.Dim, q.FirstMs, q.MedianMs, q.Rows, q.Total, flag)
	}
	w.Flush()
	if r.PrunedMinute > 0 {
		fmt.Fprintf(&b, "\nprune one day of minutes: %.1fs (%d rows)\n", r.PruneMinuteS, r.PrunedMinute)
	}
	if r.PrunedHourly > 0 {
		fmt.Fprintf(&b, "prune one day of hours: %.1fs (%d rows)\n", r.PruneHourlyS, r.PrunedHourly)
	}
	if r.FileAfter != nil {
		fmt.Fprintf(&b, "after prune: file %s, %d free pages\n", mb(r.FileAfter.Bytes), r.FileAfter.Freelist)
	}
	if r.VacuumBytes > 0 {
		fmt.Fprintf(&b, "vacuum into copy: %.1fs, %s\n", r.VacuumS, mb(r.VacuumBytes))
	}
	fmt.Fprintf(&b, "\nfull scans:\n")
	for _, e := range r.Plans {
		if len(e.FullScans) > 0 {
			fmt.Fprintf(&b, "  %v %v: %s\n", e.Phases, e.FullScans, e.SQL)
		}
	}
	fmt.Fprintf(&b, "\nindexes no plan used: %s\n", strings.Join(r.Unused, ", "))
	t.Log(b.String())
}
