package knot

import (
	"context"
	"database/sql"
	"encoding/json"
	"math"
	"net/netip"
	"path/filepath"
	"strings"
	"testing"
	"time"

	ctsdb "certainstats/internal/collector/tsdb"
	"certainstats/internal/collector/type/knot/wire"
)

func openTSDB(t *testing.T) *ctsdb.DB {
	t.Helper()
	db, err := ctsdb.Open(filepath.Join(t.TempDir(), "tsdb"), ctsdb.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func openStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "knot.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func udp(v uint64) []wire.Counter {
	return []wire.Counter{{Section: "mod-stats", Item: "request-protocol", ID: "udp4", Value: v}}
}

func near(a, b float64) bool { return math.Abs(a-b) < 1e-6 }

func TestIncreasesStartAtZeroSurviveRestartsAndSpreadGaps(t *testing.T) {
	db := openTSDB(t)
	ctx := context.Background()
	min := time.Minute.Milliseconds()
	t0 := time.Date(2026, 11, 16, 10, 0, 0, 0, time.UTC).UnixMilli()
	// Another owner and a per-zone series must not leak into u1's server-wide totals.
	writeCounters(ctx, db, "u2", "c9", udp(1), t0+min)
	writeCounters(ctx, db, "u2", "c9", udp(999_999), t0+2*min)
	writeCounters(ctx, db, "u1", "c1", []wire.Counter{{Section: "mod-stats", Item: "request-protocol", ID: "udp4", Zone: "example.com.", Value: 5}}, t0+min)
	writeCounters(ctx, db, "u1", "c1", []wire.Counter{{Section: "mod-stats", Item: "request-protocol", ID: "udp4", Zone: "example.com.", Value: 9}}, t0+2*min)

	// Knot had served 12M queries before the collector's first reading: that is a baseline.
	for _, s := range []struct {
		at int64
		v  uint64
	}{{t0, 12_000_000}, {t0 + min, 12_000_100}, {t0 + 2*min, 40}, {t0 + 62*min, 640}} {
		if err := writeCounters(ctx, db, "u1", "c1", udp(s.v), s.at); err != nil {
			t.Fatal(err)
		}
	}
	inc, err := readIncreases(ctx, db, counterQuery{UserID: "u1", Items: []string{"request-protocol"}, Start: t0, End: t0 + 62*min, Step: 10 * min})
	if err != nil {
		t.Fatal(err)
	}
	if got := inc.sumItem("request-protocol"); !near(got, 740) {
		t.Fatalf("total %v, want 100 + 40 after the restart + 600 over the gap", got)
	}
	if inc.Pairs != 3 || inc.From != t0 {
		t.Fatalf("pairs %d from %d", inc.Pairs, inc.From)
	}
	b := inc.Series[seriesKey{"request-protocol", "udp4"}]
	// 600 accrued evenly over 60 minutes: 10 per minute, so 80 lands in the first bucket (2..10 min)
	// beside the 140 from the first two readings, and 100 in each full bucket after.
	if !near(b[0], 140+80) || !near(b[1], 100) || !near(b[5], 100) {
		t.Fatalf("buckets %v", b)
	}

	zone, err := readIncreases(ctx, db, counterQuery{UserID: "u1", Zone: "example.com.", Items: []string{"request-protocol"}, Start: t0, End: t0 + 62*min, Step: 62 * min})
	if err != nil || !near(zone.sumItem("request-protocol"), 4) {
		t.Fatalf("zone total %v %v", zone.sumItem("request-protocol"), err)
	}

	// A fresh collector with one reading so far shows nothing, and says it is collecting.
	writeCounters(ctx, db, "u3", "c3", udp(5_000_000), t0)
	fresh, err := readIncreases(ctx, db, counterQuery{UserID: "u3", Start: t0 - min, End: t0 + min, Step: min})
	if err != nil || fresh.Pairs != 0 || fresh.sumItem("request-protocol") != 0 {
		t.Fatalf("fresh %+v %v", fresh, err)
	}
}

func TestSpreadClipsToRange(t *testing.T) {
	b := make([]float64, 3)
	spread(b, 100, 10, 130, 90, 120, 30) // 1 per ms-unit; 10 before the range is dropped
	if !near(b[0], 10) || !near(b[1], 10) || !near(b[2], 0) {
		t.Fatalf("%v", b)
	}
}

func TestZonesKeepSerialChangeTimes(t *testing.T) {
	s := openStore(t)
	ctx := context.Background()
	z := func(name string, serial uint32) wire.Zone {
		return wire.Zone{Name: name, Role: "primary", Serial: serial, HasSerial: true}
	}
	if err := s.SaveZones(ctx, "u1", "c1", []wire.Zone{z("a.", 1), z("b.", 1)}, 1000); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveZones(ctx, "u1", "c1", []wire.Zone{z("a.", 1), z("c.", 3)}, 2000); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveZones(ctx, "u1", "c1", []wire.Zone{z("a.", 2), z("c.", 3)}, 3000); err != nil {
		t.Fatal(err)
	}
	rows, err := s.Zones(ctx, "u1", "")
	if err != nil || len(rows) != 2 {
		t.Fatalf("rows %+v %v", rows, err)
	}
	if rows[0].Zone != "a." || rows[0].Serial != 2 || rows[0].SerialChangedAt != 3000 || rows[0].SeenAt != 3000 {
		t.Fatalf("a: %+v", rows[0])
	}
	if rows[1].Zone != "c." || rows[1].SerialChangedAt != 2000 {
		t.Fatalf("c: %+v", rows[1])
	}
	if other, _ := s.Zones(ctx, "u2", ""); len(other) != 0 {
		t.Fatal("another owner sees u1's zones")
	}
}

func sumRows(b Breakdown) int64 {
	var n int64
	for _, r := range b.Rows {
		n += r.Count
	}
	return n
}

func bucketAt(minute int64) wire.Dnstap {
	return wire.Dnstap{
		MinuteMs: minute,
		Totals: []wire.Total{
			{Zone: "example.com.", QType: "A", RCode: "NOERROR", Count: 70},
			{Zone: "example.com.", QType: "AAAA", RCode: "NXDOMAIN", Count: 30},
		},
		Names: []wire.Name{
			{Zone: "example.com.", QName: "www.example.com.", QType: "A", RCode: "NOERROR", Count: 60},
			{Zone: "example.com.", QName: "mail.example.com.", QType: "A", RCode: "NOERROR", Count: 10},
			{Zone: "example.com.", QName: "nope.example.com.", QType: "AAAA", RCode: "NXDOMAIN", Count: 20},
			{Zone: "example.com.", QName: wire.Other, QType: "AAAA", RCode: "NXDOMAIN", Count: 10},
		},
		Clients: []wire.Client{
			{Subnet: "192.0.2.0/24", Zone: "example.com.", QName: "www.example.com.", QType: "A", RCode: "NOERROR", Count: 50},
			{Subnet: "192.0.2.0/24", Zone: "example.com.", QName: "nope.example.com.", QType: "AAAA", RCode: "NXDOMAIN", Count: 20},
			{Subnet: "2001:db8:1::/48", Zone: "example.com.", QName: "www.example.com.", QType: "A", RCode: "NOERROR", Count: 10},
			{Subnet: "2001:db8:1::/48", Zone: "example.com.", QName: "mail.example.com.", QType: "A", RCode: "NOERROR", Count: 10},
			{Subnet: "2001:db8:1::/48", Zone: "example.com.", QName: wire.Other, QType: "AAAA", RCode: "NXDOMAIN", Count: 10},
			// A row from an older collector has no name and is skipped.
			{Subnet: "198.51.100.0/24", Count: 999},
		},
		Sizes: []wire.Size{{UpTo: 128, Count: 90}, {UpTo: 0, Count: 10}},
	}
}

func TestDnstapStoreQueriesRollupAndPrune(t *testing.T) {
	s := openStore(t)
	s.TopHourly = 1
	ctx := context.Background()
	now := time.Date(2026, 11, 16, 12, 30, 0, 0, time.UTC).UnixMilli()
	old := hourOf(now) - 5*hourMs
	for _, m := range []int64{old, old + minuteMs, hourOf(now) - minuteMs} {
		if err := s.SaveDnstap(ctx, "u1", "c1", bucketAt(m), now); err != nil {
			t.Fatal(err)
		}
	}
	// A resent part replaces rows instead of adding to them.
	if err := s.SaveDnstap(ctx, "u1", "c1", bucketAt(old), now); err != nil {
		t.Fatal(err)
	}
	q := Query{UserID: "u1", Start: old, End: now, Step: hourMs}
	b, err := s.Dnstap(ctx, q, "rcode", 10)
	if err != nil {
		t.Fatal(err)
	}
	if b.Total != 300 || !b.Exact || len(b.Rows) != 2 || b.Rows[0] != (Row{"NOERROR", 210}) {
		t.Fatalf("rcode: %+v", b)
	}
	b, err = s.Dnstap(ctx, Query{UserID: "u1", QType: "AAAA", Start: old, End: now}, "qname", 10)
	if err != nil {
		t.Fatal(err)
	}
	if b.Total != 90 || !b.Exact || len(b.Rows) != 1 || b.Rows[0] != (Row{"nope.example.com.", 60}) || b.Other != 30 || !b.Approximate {
		t.Fatalf("qname by qtype: %+v", b)
	}
	b, err = s.Dnstap(ctx, Query{UserID: "u1", QName: "www.example.com.", Start: old, End: now}, "qtype", 10)
	if err != nil || b.Total != 180 || b.Exact {
		t.Fatalf("filtered by name: %+v %v", b, err)
	}
	b, err = s.Dnstap(ctx, Query{UserID: "u1", Zone: "example.com.", Start: old, End: now}, "client", 10)
	if err != nil || len(b.Ignored) != 0 || len(b.Rows) != 2 || b.Rows[0] != (Row{"192.0.2.0/24", 210}) || b.Rows[1] != (Row{"2001:db8:1::/48", 90}) {
		t.Fatalf("clients: %+v %v", b, err)
	}
	if b, _ := s.Dnstap(ctx, Query{UserID: "u2", Start: old, End: now}, "rcode", 10); b.Total != 0 {
		t.Fatal("another owner sees u1's dnstap")
	}
	if _, err := s.Dnstap(ctx, q, "bogus", 10); err == nil || !strings.Contains(err.Error(), "unknown dimension") {
		t.Fatal("bogus dimension accepted")
	}

	// Rolling settles hours older than the lag; the newest hour stays in minutes.
	// TopHourly = 1 folds client names too, but each network's sum is kept.
	n, err := s.Rollup(ctx, now)
	if err != nil || n == 0 {
		t.Fatalf("rollup %d %v", n, err)
	}
	rolled, _ := s.rolledUntil(ctx)
	if rolled != hourOf(now-rollLag.Milliseconds()) {
		t.Fatalf("rolled until %d", rolled)
	}
	// A range starting before minute retention reads the hourly rows, which must match.
	hq := q
	hq.MinuteFrom = now
	hb, err := s.Dnstap(ctx, hq, "rcode", 10)
	if err != nil || hb.Total != 300 {
		t.Fatalf("hourly total %+v %v", hb, err)
	}
	names, err := s.Dnstap(ctx, hq, "qname", 10)
	if err != nil {
		t.Fatal(err)
	}
	var sum int64
	for _, r := range names.Rows {
		sum += r.Count
	}
	if sum+names.Other != 300 || len(names.Rows) > 3 {
		t.Fatalf("hourly names do not add up: %+v", names)
	}
	// The cap of 1 cuts networks with the bound rule: the busiest stays, the
	// rest fold into Other with the cut as the error, and the sums still match.
	// These minutes come from an older collector, so the list is unbounded.
	hc, err := s.Dnstap(ctx, hq, "client", 10)
	if err != nil || hc.Rows[0] != (Row{"192.0.2.0/24", 210}) || sumRows(hc)+hc.Other != 300 || hc.ErrBound == 0 || hc.Bounded || !hc.Approximate {
		t.Fatalf("hourly clients: %+v %v", hc, err)
	}

	// Retention drops old minutes; an unknown account loses everything.
	if _, err := s.Prune(ctx, Retention{MinuteMs: 2 * hourMs}, now, []string{"u1"}); err != nil {
		t.Fatal(err)
	}
	mb, _ := s.Dnstap(ctx, Query{UserID: "u1", Start: old, End: now}, "rcode", 10)
	if mb.Total != 100 {
		t.Fatalf("after minute retention %d", mb.Total)
	}
	if cb, _ := s.Dnstap(ctx, Query{UserID: "u1", Start: old, End: now}, "client", 10); len(cb.Rows) != 2 || cb.Rows[0].Count+cb.Rows[1].Count != 100 {
		t.Fatalf("client rows after minute retention: %+v", cb)
	}
	// Hourly retention drops the rolled client rows too.
	if _, err := s.Prune(ctx, Retention{HourlyMs: 1}, now, []string{"u1"}); err != nil {
		t.Fatal(err)
	}
	var hourly int
	if err := s.read.QueryRow(`SELECT COUNT(*) FROM dnstap_clients_hourly`).Scan(&hourly); err != nil || hourly != 0 {
		t.Fatalf("hourly client rows left: %d %v", hourly, err)
	}
	s.SaveZones(ctx, "gone", "c2", []wire.Zone{{Name: "x."}}, now)
	s.SaveDnstap(ctx, "gone", "c2", bucketAt(old), now)
	res, err := s.Prune(ctx, Retention{}, now, []string{"u1"})
	if err != nil || res.Zones != 1 || res.Minute == 0 {
		t.Fatalf("prune removed account: %+v %v", res, err)
	}
}

func TestSerialArithmetic(t *testing.T) {
	if !serialLess(1, 2) || serialLess(2, 1) || serialLess(5, 5) {
		t.Fatal("plain order")
	}
	if !serialLess(4294967295, 1) || serialLess(1, 4294967295) {
		t.Fatal("wraparound")
	}
}

func TestDriftNeedsTimeToSettle(t *testing.T) {
	now := int64(10 * 3600 * 1000)
	rows := []ZoneRow{
		{CollectorID: "p", Zone: "a.", Role: "primary", Serial: 10, HasSerial: true, SerialChangedAt: now - 20*minuteMs},
		{CollectorID: "s", Zone: "a.", Role: "secondary", Serial: 9, HasSerial: true, ExpireAt: now + hourMs},
		{CollectorID: "p", Zone: "b.", Role: "primary", Serial: 7, HasSerial: true, SerialChangedAt: now - 5*minuteMs},
		{CollectorID: "s", Zone: "b.", Role: "secondary", Serial: 6, HasSerial: true},
		{CollectorID: "gone", Zone: "c.", Role: "primary", Serial: 1, HasSerial: true},
	}
	views := zoneViews(rows, map[string]string{"p": "ns1", "s": "ns2"}, now)
	if len(views) != 2 {
		t.Fatalf("views %+v", views)
	}
	if !views[0].Drift || !views[0].Rows[1].Behind || *views[0].PrimarySerial != 10 || views[0].ExpireAt != now+hourMs {
		t.Fatalf("a: %+v", views[0])
	}
	if views[1].Drift {
		t.Fatal("b drifted before the primary's change could reach the secondary")
	}
}

func TestChecks(t *testing.T) {
	db := openTSDB(t)
	ctx := context.Background()
	now := time.Now()
	nowMs := now.UnixMilli()
	rc := func(nx, fail uint64) []wire.Counter {
		return []wire.Counter{
			{Section: "mod-stats", Item: "response-code", ID: "NOERROR", Value: nx},
			{Section: "mod-stats", Item: "response-code", ID: "SERVFAIL", Value: fail},
		}
	}
	writeCounters(ctx, db, "u1", "c1", rc(0, 0), nowMs-10*minuteMs)
	writeCounters(ctx, db, "u1", "c1", rc(950, 50), nowMs-5*minuteMs)
	typ := &Type{TSDB: db}
	servers := []Server{
		{CollectorID: "c1", Name: "ns1", Enabled: true, Connected: true, ConnectedSince: nowMs - hourMs, FirstSeen: 1, ReportedOn: true, Status: "ok", ModStats: true, LastStatsMs: nowMs},
		{CollectorID: "c2", Name: "ns2", Enabled: true, FirstSeen: 1, LastSeen: nowMs - hourMs},
		{CollectorID: "c3", Name: "ns3", Enabled: true, Connected: true, ReportedOn: true, FirstSeen: 1, Status: "error", Error: "cannot open Knot's control socket"},
		{CollectorID: "c4", Name: "ns4", Enabled: true, Connected: true, ConnectedSince: nowMs - hourMs, FirstSeen: 1, ReportedOn: true, Status: "ok",
			LastStatsMs: nowMs, Dnstap: &wire.DnstapState{Socket: "/run/x.sock", Listening: true}},
	}
	zones := []ZoneView{{Zone: "a.", Rows: []ZoneSide{
		{ZoneRow: ZoneRow{CollectorID: "c1", Role: "secondary", ExpireAt: nowMs + hourMs}, Server: "ns1"},
		{ZoneRow: ZoneRow{CollectorID: "c4", Role: "secondary", ExpireAt: nowMs - minuteMs}, Server: "ns4"},
	}}}
	got := map[string]string{}
	for _, c := range typ.checks(ctx, "u1", servers, zones, now) {
		got[c.Kind+"/"+c.Server] = c.Level
	}
	want := map[string]string{
		"servfail/ns1": "warn", "offline/ns2": "bad", "unreachable/ns3": "bad",
		"no_mod_stats/ns4": "warn", "dnstap_quiet/ns4": "warn", "expiring/ns1": "warn", "expired/ns4": "bad",
	}
	for k, lvl := range want {
		if got[k] != lvl {
			t.Errorf("%s: got %q, want %q (all: %v)", k, got[k], lvl, got)
		}
	}
	if len(got) != len(want) {
		t.Errorf("unexpected checks: %v", got)
	}
}

// The page reads dnstap state by these names (dnstap.js); Go's default
// capitalised keys hid the Client networks card.
func TestDnstapStateJSONNames(t *testing.T) {
	raw, err := json.Marshal(Server{Dnstap: &wire.DnstapState{Socket: "/s", Listening: true, LastFrameMs: 5, Clients: true}})
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Dnstap map[string]any `json:"dnstap"`
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"socket", "listening", "connected", "last_frame_ms", "dropped", "clients"} {
		if _, ok := got.Dnstap[k]; !ok {
			t.Errorf("dnstap has no %q: %s", k, raw)
		}
	}
	if got.Dnstap["clients"] != true {
		t.Errorf("clients: %s", raw)
	}
}

func TestDnstapClientFilter(t *testing.T) {
	t.Run(EngineSQL, func(t *testing.T) { testDnstapClientFilter(t, openStore(t)) })
	t.Run(EngineTopN, func(t *testing.T) { k, _ := openTopNStore(t, 1000); testDnstapClientFilter(t, k) })
}

func testDnstapClientFilter(t *testing.T, s dnstapStore) {
	ctx := context.Background()
	now := time.Date(2026, 11, 16, 12, 30, 0, 0, time.UTC).UnixMilli()
	start := hourOf(now) - hourMs
	for _, m := range []int64{start, start + minuteMs, start + 2*minuteMs} {
		if err := s.SaveDnstap(ctx, "u1", "c1", bucketAt(m), now); err != nil {
			t.Fatal(err)
		}
	}
	pfx := func(raw string) netip.Prefix { return netip.MustParsePrefix(raw) }
	q := Query{UserID: "u1", Start: start, End: now, Client: pfx("192.0.0.0/16")}
	b, err := s.Dnstap(ctx, q, "qname", 10)
	if err != nil || b.Total != 210 || b.Exact || len(b.Rows) != 2 || b.Rows[0] != (Row{"www.example.com.", 150}) || b.Rows[1] != (Row{"nope.example.com.", 60}) || b.Other != 0 || b.Bounded || !b.Approximate {
		// An older collector's client rows carry no cut, so they are unbounded.
		t.Fatalf("names of 192.0/16: %+v %v", b, err)
	}
	// An IPv6 /48 inside a /32; its Other row is counted apart and marks the list approximate.
	q.Client = pfx("2001:db8::/32")
	b, err = s.Dnstap(ctx, q, "qname", 10)
	if err != nil || b.Total != 90 || len(b.Rows) != 2 || b.Other != 30 || !b.Approximate {
		t.Fatalf("names of 2001:db8::/32: %+v %v", b, err)
	}
	q.Client = pfx("192.0.2.0/24")
	if b, err = s.Dnstap(ctx, q, "rcode", 10); err != nil || len(b.Rows) != 2 || b.Rows[0] != (Row{"NOERROR", 150}) {
		t.Fatalf("rcodes of one /24: %+v %v", b, err)
	}
	if b, err = s.Dnstap(ctx, q, "size", 10); err != nil || len(b.Ignored) != 1 || b.Ignored[0] != "client" {
		t.Fatalf("sizes cannot split by client: %+v %v", b, err)
	}
	q.Client = pfx("10.0.0.0/8")
	if b, err = s.Dnstap(ctx, q, "qname", 10); err != nil || b.Total != 0 || len(b.Rows) != 0 {
		t.Fatalf("no traffic from 10/8: %+v %v", b, err)
	}
	// The reverse: which networks asked one name.
	b, err = s.Dnstap(ctx, Query{UserID: "u1", Start: start, End: now, QName: "www.example.com."}, "client", 10)
	if err != nil || len(b.Rows) != 2 || b.Rows[0] != (Row{"192.0.2.0/24", 150}) || b.Rows[1] != (Row{"2001:db8:1::/48", 30}) {
		t.Fatalf("networks asking www: %+v %v", b, err)
	}
}

func TestOpenDropsVersionOneDnstap(t *testing.T) {
	path := filepath.Join(t.TempDir(), "knot.db")
	db, err := sql.Open("sqlite", dsn(path))
	if err != nil {
		t.Fatal(err)
	}
	for _, stmt := range []string{
		`CREATE TABLE zones (user_id TEXT NOT NULL, collector_id TEXT NOT NULL, zone TEXT NOT NULL, role TEXT NOT NULL DEFAULT '', serial INTEGER NOT NULL DEFAULT 0,
			has_serial INTEGER NOT NULL DEFAULT 0, serial_changed_at INTEGER NOT NULL DEFAULT 0, refresh_at INTEGER NOT NULL DEFAULT 0, expire_at INTEGER NOT NULL DEFAULT 0,
			resign_at INTEGER NOT NULL DEFAULT 0, frozen INTEGER NOT NULL DEFAULT 0, error TEXT NOT NULL DEFAULT '', seen_at INTEGER NOT NULL, PRIMARY KEY (user_id, collector_id, zone)) WITHOUT ROWID`,
		`INSERT INTO zones (user_id, collector_id, zone, seen_at) VALUES ('u1', 'c1', 'example.com.', 1)`,
		`CREATE TABLE meta (key TEXT PRIMARY KEY, value INTEGER NOT NULL) WITHOUT ROWID`,
		`INSERT INTO meta VALUES ('rolled_until', 123)`,
		`CREATE TABLE dnstap_clients_minute (user_id TEXT NOT NULL, collector_id TEXT NOT NULL, bucket INTEGER NOT NULL, subnet TEXT NOT NULL, count INTEGER NOT NULL, PRIMARY KEY (user_id, collector_id, bucket, subnet)) WITHOUT ROWID`,
		`INSERT INTO dnstap_clients_minute VALUES ('u1', 'c1', 60000, '192.0.2.0/24', 5)`,
		`PRAGMA user_version = 1`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatal(err)
		}
	}
	db.Close()
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if zones, _ := s.Zones(context.Background(), "u1", ""); len(zones) != 1 {
		t.Fatalf("zones lost: %+v", zones)
	}
	if rolled, _ := s.rolledUntil(context.Background()); rolled != 0 {
		t.Fatalf("rolled_until kept: %d", rolled)
	}
	var n, v int
	if err := s.read.QueryRow(`SELECT COUNT(*) FROM dnstap_clients_minute WHERE net != ''`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("old client rows: %d %v", n, err)
	}
	if err := s.read.QueryRow(`PRAGMA user_version`).Scan(&v); err != nil || v != schemaVersion {
		t.Fatalf("user_version %d %v", v, err)
	}
}

func TestNetKeys(t *testing.T) {
	for _, c := range []string{"10.1.2.0/24", "2001:db8:1::/48", "0.0.0.0/24", "ffff:ffff:ffff::/48"} {
		key, ok := netKeyOf(c)
		if !ok || len(key) != 36 || netLabel(key) != c {
			t.Errorf("%s -> %q -> %q", c, key, netLabel(key))
		}
	}
	k := func(c string) string { key, _ := netKeyOf(c); return key }
	if !(k("9.255.255.0/24") < k("10.0.0.0/24") && k("10.0.0.0/24") < k("10.0.1.0/24") && k("10.0.1.0/24") < k("255.0.0.0/24") && k("255.0.0.0/24") < k("2001:db8::/48")) {
		t.Error("keys do not sort in address order")
	}
	in := func(cidr, subnet string) bool {
		lo, hi := netRange(netip.MustParsePrefix(cidr))
		key := k(subnet)
		return key >= lo && key < hi
	}
	for _, c := range []struct {
		cidr, subnet string
		want         bool
	}{
		{"10.0.0.0/8", "10.0.0.0/24", true},
		{"10.0.0.0/8", "10.255.255.0/24", true},
		{"10.0.0.0/8", "11.0.0.0/24", false},
		{"10.0.0.0/8", "9.255.255.0/24", false},
		{"10.1.2.0/24", "10.1.2.0/24", true},
		{"10.1.2.0/24", "10.1.3.0/24", false},
		{"0.0.0.0/0", "255.255.255.0/24", true},
		{"0.0.0.0/0", "2001:db8::/48", false},
		{"2001:db8::/32", "2001:db8:ffff::/48", true},
		{"2001:db8::/32", "2001:db9::/48", false},
		{"::/0", "ffff:ffff:ffff::/48", true},
	} {
		if got := in(c.cidr, c.subnet); got != c.want {
			t.Errorf("%s in %s = %v, want %v", c.subnet, c.cidr, got, c.want)
		}
	}
	for raw, want := range map[string]string{"10.1.2.3": "10.1.2.0/24", "10.0.0.0/8": "10.0.0.0/8", "10.1.2.3/30": "10.1.2.0/24",
		"2001:db8::1": "2001:db8::/48", "::ffff:10.1.2.3": "10.1.2.0/24", "10.9.9.9/8": "10.0.0.0/8"} {
		p, _, err := clientFilter(raw)
		if err != nil || p.String() != want {
			t.Errorf("clientFilter(%q) = %s %v, want %s", raw, p, err, want)
		}
	}
	if _, wide, _ := clientFilter("10.0.0.0/16"); wide {
		t.Error("a /16 is not widened")
	}
	for _, bad := range []string{"", "nope", "10.0.0.0/33", "10.0.0"} {
		if _, _, err := clientFilter(bad); err == nil {
			t.Errorf("clientFilter(%q) accepted", bad)
		}
	}
}
