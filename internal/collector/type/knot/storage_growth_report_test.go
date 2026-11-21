//go:build knotreport

// Growth report: grow one top-N store a step at a time and measure memory per
// phase at every checkpoint. The store directory is kept, so a later run with
// a larger KNOTGROW_UNTIL_DAYS continues where the last one stopped.
//
//	KNOTGROW_DIR=/path KNOTGROW_STORE=topn-blocks KNOTGROW_COLLECTORS=4 \
//	KNOTGROW_HISTORY_DAYS=23,60 KNOTGROW_UNTIL_DAYS=3 \
//	KNOTREPORT_TOP=200 KNOTREPORT_CLIENT_TOP=200 KNOTREPORT_FOLD=subnet \
//	go test -tags knotreport -run TestStorageGrowth -v -timeout 6h ./internal/collector/type/knot/
//
// Traffic knobs (top, client top, QPM, fold, zones, names, nets) come from the
// KNOTREPORT_* settings of TestStorageReport.
package knot

import (
	"context"
	"encoding/gob"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/netip"
	"os"
	"path/filepath"
	"runtime"
	"runtime/metrics"
	"runtime/pprof"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"certainstats/internal/collector/type/knot/wire"
	"certainstats/internal/tblk"

	"github.com/klauspost/compress/zstd"
)

// growTop is the store's hourly and daily cap; history traffic is folded to it.
const growTop = 1000

// growFirst is the first generated minute; time runs forward from here.
var growFirst = time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC).UnixMilli()

// traffic caches generated dnstap buckets on disk, one zstd gob stream per
// (collector, day) and one per collector for history, so the generator runs
// once and every store run replays the same traffic. A store run then
// measures the store, not the generator's maps.
type traffic struct {
	dir string
	cfg reportCfg
}

func newTraffic(t *testing.T, cfg reportCfg, growDir string) *traffic {
	root := os.Getenv("KNOTGROW_TRAFFIC")
	if root == "" {
		root = filepath.Join(filepath.Dir(filepath.Clean(growDir)), "knotgrow-traffic")
	}
	key := fmt.Sprintf("top%d-ct%d-cl%d-q%d-%s-z%d-n%d-nt%d", cfg.Top, cfg.ClientTop, cfg.Clients, cfg.QPM, cfg.Fold, cfg.Zones, cfg.Names, cfg.Nets)
	dir := filepath.Join(root, key)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	return &traffic{dir: dir, cfg: cfg}
}

// dayGen gives the same traffic for a (collector, day) however the growth is
// split into runs; day -1 is the history.
func (tr *traffic) dayGen(c int, day int64) *generator {
	return newGenerator(int64(c)*100000+day+1, tr.cfg)
}

// trafficStream delivers one file's buckets in order; err is set before ch closes.
type trafficStream struct {
	ch  chan wire.Dnstap
	err error
}

// next returns the next bucket or fails the test.
func (s *trafficStream) next(t *testing.T) wire.Dnstap {
	d, ok := <-s.ch
	if !ok {
		if s.err == nil {
			s.err = errors.New("traffic stream ended early")
		}
		t.Fatal(s.err)
	}
	return d
}

// stream reads name from the cache, or runs gen to produce, cache and deliver
// its n buckets. Each stream has its own goroutine, so collectors generate in
// parallel.
func (tr *traffic) stream(name string, n int, gen func(emit func(wire.Dnstap))) *trafficStream {
	s := &trafficStream{ch: make(chan wire.Dnstap, 8)}
	path := filepath.Join(tr.dir, name+".gob.zst")
	go func() {
		defer close(s.ch)
		if fh, err := os.Open(path); err == nil {
			defer fh.Close()
			zr, err := zstd.NewReader(fh, zstd.WithDecoderConcurrency(1))
			if err != nil {
				s.err = err
				return
			}
			defer zr.Close()
			dec := gob.NewDecoder(zr)
			for i := 0; i < n; i++ {
				var d wire.Dnstap
				if err := dec.Decode(&d); err != nil {
					s.err = fmt.Errorf("%s: bucket %d: %w", path, i, err)
					return
				}
				s.ch <- d
			}
			return
		}
		tmp := path + ".tmp"
		fh, err := os.Create(tmp)
		if err != nil {
			s.err = err
			return
		}
		zw, err := zstd.NewWriter(fh, zstd.WithEncoderConcurrency(1))
		if err != nil {
			fh.Close()
			s.err = err
			return
		}
		enc := gob.NewEncoder(zw)
		count := 0
		gen(func(d wire.Dnstap) {
			if s.err == nil {
				s.err = enc.Encode(d)
			}
			count++
			s.ch <- d
		})
		if err := zw.Close(); err != nil && s.err == nil {
			s.err = err
		}
		if err := fh.Close(); err != nil && s.err == nil {
			s.err = err
		}
		if s.err == nil && count != n {
			s.err = fmt.Errorf("%s: generated %d buckets, want %d", name, count, n)
		}
		if s.err != nil {
			os.Remove(tmp)
			return
		}
		s.err = os.Rename(tmp, path)
	}()
	return s
}

// minutes streams day's 1440 minute buckets for collector c.
func (tr *traffic) minutes(c int, day int64) *trafficStream {
	return tr.stream(fmt.Sprintf("min-c%d-d%d", c, day), 1440, func(emit func(wire.Dnstap)) {
		g := tr.dayGen(c, day)
		start := growFirst + day*dayMs
		for i := int64(0); i < 1440; i++ {
			emit(g.sample(tr.cfg.QPM).bucket(start+i*minuteMs, tr.cfg.Top, tr.cfg.Clients, tr.cfg.ClientTop, 1))
		}
	})
}

// history streams collector c's hourly-only hours, then its daily-only days.
func (tr *traffic) history(c, hourly, daily int) *trafficStream {
	return tr.stream(fmt.Sprintf("hist-c%d-h%d-d%d", c, hourly, daily), hourly*24+daily, func(emit func(wire.Dnstap)) {
		g := tr.dayGen(c, -1)
		hourFirst := growFirst - int64(hourly)*dayMs
		for h := hourFirst; h < growFirst; h += hourMs {
			emit(g.sample(tr.cfg.QPM*15).bucket(h, growTop, tr.cfg.Clients*4, growTop, 4))
		}
		for d := hourFirst - int64(daily)*dayMs; d < hourFirst; d += dayMs {
			emit(g.sample(tr.cfg.QPM*30).bucket(d, growTop, tr.cfg.Clients*8, growTop, 48))
		}
	})
}

// growState is kept in <dir>/grow.json between runs.
type growState struct {
	Store         string `json:"store"`
	Collectors    int    `json:"collectors"`
	Top           int    `json:"top"`
	ClientTop     int    `json:"client_top"`
	Clients       int    `json:"clients"`
	QPM           int    `json:"qpm"`
	Fold          string `json:"fold"`
	Zones         int    `json:"zones"`
	Names         int    `json:"names"`
	Nets          int    `json:"nets"`
	HistoryHourly int    `json:"history_hourly_days"`
	HistoryDaily  int    `json:"history_daily_days"`
	First         int64  `json:"first"`
	Next          int64  `json:"next"`
	HistoryDone   bool   `json:"history_done"`
}

// same reports whether two states describe the same store and traffic.
func (a growState) same(b growState) bool {
	a.Next, b.Next, a.HistoryDone, b.HistoryDone = 0, 0, false, false
	return a == b
}

type growQuery struct {
	Window   string  `json:"window"`
	Filter   string  `json:"filter"`
	Dim      string  `json:"dim"`
	MedianMs float64 `json:"median_ms"`
	Rows     int     `json:"rows"`
	Err      string  `json:"err,omitempty"`
}

type growPhase struct {
	Phase   string  `json:"phase"`
	Seconds float64 `json:"seconds"`
	// PeakRSSKB is this phase's own peak when VmHWM could be reset, else the
	// whole process's (PeakIsProcess).
	PeakRSSKB     int64 `json:"peak_rss_kb"`
	PeakIsProcess bool  `json:"peak_is_process,omitempty"`
	RSSKB         int64 `json:"rss_kb_after"`
	// Go memory sampled every 100 ms during the phase.
	PeakHeapBytes    uint64 `json:"peak_heap_bytes"`
	PeakGoTotalBytes uint64 `json:"peak_go_total_bytes"`
	LiveHeapBytes    uint64 `json:"live_heap_bytes_after_gc"`
	GCCycles         uint64 `json:"gc_cycles"`
	// generate only
	WriteS     float64 `json:"write_s,omitempty"`
	MaintS     float64 `json:"maint_s,omitempty"`
	MaintSteps int     `json:"maint_steps,omitempty"`
	// query only
	Queries []growQuery `json:"queries,omitempty"`
	Profile string      `json:"profile,omitempty"`
}

type growCheckpoint struct {
	Days       float64     `json:"minute_days"`
	Collectors int         `json:"collectors"`
	Store      string      `json:"store"`
	Phases     []growPhase `json:"phases"`
	DiskBytes  int64       `json:"disk_bytes"`
	Stats      tblk.Stats  `json:"stats"`
	At         string      `json:"at"`
}

// resetPeakRSS resets VmHWM to the current RSS; false when not allowed.
func resetPeakRSS() bool {
	return os.WriteFile("/proc/self/clear_refs", []byte("5"), 0) == nil
}

func procStatusKB(field string) int64 {
	raw, err := os.ReadFile("/proc/self/status")
	if err != nil {
		return 0
	}
	for _, line := range strings.Split(string(raw), "\n") {
		if v, ok := strings.CutPrefix(line, field+":"); ok {
			n, _ := strconv.ParseInt(strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(v), "kB")), 10, 64)
			return n
		}
	}
	return 0
}

// memSampler records peak Go heap and total Go memory while running.
type memSampler struct {
	stop    chan struct{}
	done    sync.WaitGroup
	heap    uint64
	total   uint64
	gcStart uint64
}

var memSamples = []string{"/memory/classes/heap/objects:bytes", "/memory/classes/total:bytes", "/gc/cycles/total:gc-cycles"}

func readMem() (heap, total, gcs uint64) {
	s := make([]metrics.Sample, len(memSamples))
	for i, n := range memSamples {
		s[i].Name = n
	}
	metrics.Read(s)
	return s[0].Value.Uint64(), s[1].Value.Uint64(), s[2].Value.Uint64()
}

func startSampler() *memSampler {
	m := &memSampler{stop: make(chan struct{})}
	m.heap, m.total, m.gcStart = readMem()
	m.done.Add(1)
	go func() {
		defer m.done.Done()
		tk := time.NewTicker(100 * time.Millisecond)
		defer tk.Stop()
		for {
			select {
			case <-m.stop:
				return
			case <-tk.C:
				h, tot, _ := readMem()
				m.heap, m.total = max(m.heap, h), max(m.total, tot)
			}
		}
	}()
	return m
}

func (m *memSampler) finish() (heap, total, gcs uint64) {
	close(m.stop)
	m.done.Wait()
	h, tot, g := readMem()
	return max(m.heap, h), max(m.total, tot), g - m.gcStart
}

// measure runs fn as one phase and fills the phase's memory figures.
func measure(t *testing.T, name, profDir, profTag string, fn func(p *growPhase)) growPhase {
	t.Helper()
	runtime.GC()
	p := growPhase{Phase: name}
	reset := resetPeakRSS()
	smp := startSampler()
	t0 := time.Now()
	fn(&p)
	p.Seconds = time.Since(t0).Seconds()
	p.PeakHeapBytes, p.PeakGoTotalBytes, p.GCCycles = smp.finish()
	p.PeakRSSKB, p.PeakIsProcess = procStatusKB("VmHWM"), !reset
	p.RSSKB = procStatusKB("VmRSS")
	runtime.GC()
	p.LiveHeapBytes, _, _ = readMem()
	if profDir != "" {
		path := filepath.Join(profDir, fmt.Sprintf("heap-%s-%s.pb.gz", profTag, name))
		if fh, err := os.Create(path); err == nil {
			if err := pprof.WriteHeapProfile(fh); err == nil {
				p.Profile = path
			}
			fh.Close()
		}
	}
	peak := "phase"
	if p.PeakIsProcess {
		peak = "process"
	}
	t.Logf("%s %s: %.1fs, peak RSS %s (%s), RSS after %s, peak heap %s, peak Go total %s, live heap %s, %d GCs",
		profTag, name, p.Seconds, mb(p.PeakRSSKB*1024), peak, mb(p.RSSKB*1024),
		mb(int64(p.PeakHeapBytes)), mb(int64(p.PeakGoTotalBytes)), mb(int64(p.LiveHeapBytes)), p.GCCycles)
	return p
}

func TestStorageGrowth(t *testing.T) {
	dir := os.Getenv("KNOTGROW_DIR")
	if dir == "" {
		t.Skip("set KNOTGROW_DIR to run the growth report")
	}
	cfg := loadCfg(t)
	store := envChoice(t, "KNOTGROW_STORE", "topn-blocks", "topn-sql")
	collectors := envNum(t, "KNOTGROW_COLLECTORS", 1, 1)
	stepDays := envNum(t, "KNOTGROW_STEP_DAYS", 1, 1)
	untilDays := envNum(t, "KNOTGROW_UNTIL_DAYS", 1, 1)
	histHourly, histDaily := 0, 0
	if v := os.Getenv("KNOTGROW_HISTORY_DAYS"); v != "" {
		a, b, _ := strings.Cut(v, ",")
		var err1, err2 error
		histHourly, err1 = strconv.Atoi(strings.TrimSpace(a))
		if b != "" {
			histDaily, err2 = strconv.Atoi(strings.TrimSpace(b))
		}
		if err1 != nil || err2 != nil || histHourly < 0 || histDaily < 0 {
			t.Fatalf("KNOTGROW_HISTORY_DAYS=%q: want hourly,daily days, e.g. 23,60", v)
		}
	}
	profDir := os.Getenv("KNOTGROW_HEAPPROF")
	if profDir != "" {
		if err := os.MkdirAll(profDir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	out := os.Getenv("KNOTGROW_OUT")

	want := growState{
		Store: store, Collectors: collectors, Top: cfg.Top, ClientTop: cfg.ClientTop, Clients: cfg.Clients,
		QPM: cfg.QPM, Fold: cfg.Fold, Zones: cfg.Zones, Names: cfg.Names, Nets: cfg.Nets,
		HistoryHourly: histHourly, HistoryDaily: histDaily,
		First: growFirst,
	}
	want.Next = want.First
	statePath := filepath.Join(dir, "grow.json")
	st := want
	if raw, err := os.ReadFile(statePath); err == nil {
		if err := json.Unmarshal(raw, &st); err != nil {
			t.Fatalf("%s: %v", statePath, err)
		}
		if !st.same(want) {
			t.Fatalf("%s was grown with other settings:\n have %+v\n want %+v\nuse a new KNOTGROW_DIR", statePath, st, want)
		}
		t.Logf("continuing %s at day %.1f", dir, float64(st.Next-st.First)/float64(dayMs))
	} else if errors.Is(err, fs.ErrNotExist) {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		t.Logf("new store in %s", dir)
	} else {
		t.Fatal(err)
	}
	saveState := func() {
		b, _ := json.MarshalIndent(st, "", "  ")
		if err := os.WriteFile(statePath+".tmp", b, 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(statePath+".tmp", statePath); err != nil {
			t.Fatal(err)
		}
	}

	tr := newTraffic(t, cfg, dir)
	if envFlag("KNOTGROW_TRAFFIC_ONLY") {
		// Fill the traffic cache only: every collector generates in parallel.
		t0 := time.Now()
		var streams []*trafficStream
		for c := 0; c < collectors; c++ {
			if histHourly+histDaily > 0 {
				streams = append(streams, tr.history(c, histHourly, histDaily))
			}
		}
		drain := func(ss []*trafficStream) {
			var wg sync.WaitGroup
			for _, s := range ss {
				wg.Add(1)
				go func() {
					defer wg.Done()
					for range s.ch {
					}
				}()
			}
			wg.Wait()
			for _, s := range ss {
				if s.err != nil {
					t.Fatal(s.err)
				}
			}
		}
		drain(streams)
		for day := int64(0); day < int64(untilDays); day++ {
			streams = streams[:0]
			for c := 0; c < collectors; c++ {
				streams = append(streams, tr.minutes(c, day))
			}
			drain(streams)
			t.Logf("traffic: day %d ready, %s elapsed", day+1, time.Since(t0).Round(time.Second))
		}
		t.Logf("traffic cache %s: %s", tr.dir, mb(diskSize(tr.dir)))
		return
	}

	engine := tblk.Blocks
	if store == "topn-sql" {
		engine = tblk.SQL
	}
	k := openKnotTopN(t, filepath.Join(dir, "store"), engine, growTop)
	defer k.Close()
	ctx := context.Background()

	const user = "5b0c8a2e-3f6d-4c1b-9e7a-2d4f6a8c0e11"
	cols := make([]string, collectors)
	for c := range cols {
		cols[c] = fmt.Sprintf("9d3e7f10-%04x-4a5b-8c6d-0e1f2a3b4c5d", c)
	}
	maintain := func(now int64) (int, error) {
		steps := 0
		for {
			n, err := k.Rollup(ctx, now)
			if err != nil {
				return steps, err
			}
			if n == 0 {
				return steps, nil
			}
			steps += n
		}
	}
	oldest := st.First - int64(histHourly+histDaily)*dayMs

	emit := func(cp growCheckpoint) {
		cp.Days = float64(st.Next-st.First) / float64(dayMs)
		cp.Collectors, cp.Store, cp.At = collectors, store, time.Now().UTC().Format(time.RFC3339)
		cp.DiskBytes = diskSize(filepath.Join(dir, "store"))
		s, err := k.s.Stats(ctx)
		if err != nil {
			t.Fatal(err)
		}
		cp.Stats = s
		t.Logf("checkpoint day %.1f: disk %s, minute %s, hour %s, day %s, head %s, %d files", cp.Days, mb(cp.DiskBytes),
			mb(s.Bytes[tblk.Minute]), mb(s.Bytes[tblk.Hour]), mb(s.Bytes[tblk.Day]), mb(s.HeadBytes), s.Files)
		if out == "" {
			return
		}
		b, err := json.Marshal(cp)
		if err != nil {
			t.Fatal(err)
		}
		fh, err := os.OpenFile(out, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			t.Fatal(err)
		}
		defer fh.Close()
		fh.Write(append(b, '\n'))
	}

	if !st.HistoryDone && histHourly+histDaily > 0 {
		cp := growCheckpoint{}
		cp.Phases = append(cp.Phases, measure(t, "history", profDir, "d0", func(p *growPhase) {
			for c, col := range cols {
				s := tr.history(c, histHourly, histDaily)
				for i := 0; i < histHourly*24; i++ {
					if err := k.reportHourly(ctx, user, col, s.next(t)); err != nil {
						t.Fatal(err)
					}
				}
				for i := 0; i < histDaily; i++ {
					if err := k.reportDaily(ctx, user, col, s.next(t)); err != nil {
						t.Fatal(err)
					}
				}
			}
			if err := k.reportFlush(ctx); err != nil {
				t.Fatal(err)
			}
			t0 := time.Now()
			n, err := maintain(st.First)
			if err != nil {
				t.Fatal(err)
			}
			p.MaintS, p.MaintSteps = time.Since(t0).Seconds(), n
		}))
		st.HistoryDone = true
		saveState()
		emit(cp)
	}

	limit := st.First + int64(untilDays)*dayMs
	if st.Next >= limit {
		t.Logf("store already holds %.1f days; raise KNOTGROW_UNTIL_DAYS to grow it", float64(st.Next-st.First)/float64(dayMs))
	}
	for st.Next < limit {
		stop := min(st.Next+int64(stepDays)*dayMs, limit)
		tag := fmt.Sprintf("d%d", (stop-st.First)/dayMs)
		cp := growCheckpoint{}
		cp.Phases = append(cp.Phases, measure(t, "generate", profDir, tag, func(p *growPhase) {
			streams := make([]*trafficStream, len(cols))
			for m := st.Next; m < stop; m += minuteMs {
				if (m-st.First)%dayMs == 0 {
					for c := range cols {
						streams[c] = tr.minutes(c, (m-st.First)/dayMs)
					}
				}
				t0 := time.Now()
				for c, col := range cols {
					d := streams[c].next(t)
					if d.MinuteMs != m {
						t.Fatalf("traffic for collector %d: minute %d, want %d", c, d.MinuteMs, m)
					}
					if err := k.SaveDnstap(ctx, user, col, d, m+minuteMs); err != nil {
						t.Fatal(err)
					}
				}
				p.WriteS += time.Since(t0).Seconds()
				if (m+minuteMs)%hourMs == 0 {
					t0 := time.Now()
					n, err := maintain(m + minuteMs)
					if err != nil {
						t.Fatal(err)
					}
					p.MaintS += time.Since(t0).Seconds()
					p.MaintSteps += n
				}
			}
		}))
		st.Next = stop
		saveState()

		end := st.Next
		cp.Phases = append(cp.Phases, measure(t, "query", profDir, tag, func(p *growPhase) {
			p.Queries = growQueries(t, ctx, k, user, end, oldest, cfg.MinuteRetentionDays)
		}))
		emit(cp)
	}
}

// growQueries runs a small fixed set of the page's slowest cases, 2 runs each.
func growQueries(t *testing.T, ctx context.Context, k *knotTopN, user string, end, oldest int64, keepDays int) []growQuery {
	minuteFrom := end - int64(keepDays)*dayMs
	_, _, netKey := k.reportFocus(ctx, user, end-dayMs)
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
	span := min(30*dayMs, end-oldest)
	spanName := fmt.Sprintf("%.0fd", float64(span)/float64(dayMs))
	cases := []struct {
		name        string
		span        int64
		filter, dim string
	}{
		{"1h", hourMs, "none", "qname"},
		{"24h", dayMs, "none", "qname"},
		{"24h", dayMs, "none", "client"},
		{"24h", dayMs, "client", "qname"},
		{spanName, span, "none", "qname"},
		{spanName, span, "client", "qname"},
	}
	var out []growQuery
	for _, c := range cases {
		if c.span > end-oldest {
			continue
		}
		start, stop, step := pageWindow(end, c.span, minuteFrom, false)
		q := Query{UserID: user, Start: start, End: stop, Step: step, MinuteFrom: minuteFrom}
		if c.filter == "client" {
			q.Client = client
		}
		r := growQuery{Window: c.name, Filter: c.filter, Dim: c.dim}
		var times []float64
		for run := 0; run < 2; run++ {
			t0 := time.Now()
			b, err := k.Dnstap(ctx, q, c.dim, 50)
			if err != nil {
				r.Err = err.Error()
				break
			}
			times = append(times, float64(time.Since(t0).Microseconds())/1000)
			r.Rows = len(b.Rows)
		}
		for _, ms := range times {
			r.MedianMs += ms / float64(len(times)) // median of two runs is their mean
		}
		out = append(out, r)
	}
	for _, r := range out {
		t.Logf("  query %s %s %s: %.0f ms, %d rows %s", r.Window, r.Filter, r.Dim, r.MedianMs, r.Rows, r.Err)
	}
	return out
}
