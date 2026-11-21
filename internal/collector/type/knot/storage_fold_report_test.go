//go:build knotreport

// The fold report: how far Knot's long-range name lists drift from the truth
// because names are folded into (other) three times (by the agent per minute,
// then per hour and per day on the server). It feeds generated traffic with
// planted patterns through the production TopN (SaveDnstap, then Rollup),
// keeps an exact count beside it, and compares the qname breakdown the page
// reads. Nothing is changed in production code.
//
//	FOLDREPORT_DIR=~/.cache/certainstats-knotreport/fold1 \
//	  GOFLAGS=-p=1 nice go test -tags knotreport -run TestFoldReport -timeout 0 -v ./internal/collector/type/knot/
package knot

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"certainstats/internal/collector/type/knot/wire"
	"certainstats/internal/tblk"
)

// foldLadder is the steady planted names' rates, queries per hour on each
// collector, from far below the hourly cap to clearly above it.
var foldLadder = []int{20, 40, 60, 90, 120, 180, 240, 360, 600, 1200, 2000, 3000}

const (
	foldTop      = 1000 // agent KNOT_DNSTAP_TOP and server TopHourly defaults
	foldK        = 50
	foldBursts   = 300  // burst names per hour, 1,200 queries each per collector
	foldLocals   = 300  // collector-local names, 600 an hour each
	foldEnum     = 1500 // names hammered in a busy hour, 300 an hour each
	foldFloodMul = 1.5  // extra random subdomains per query in a flood hour
)

var foldScenarios = []string{"baseline", "burst", "local", "busy", "flood"}

type foldWindow struct {
	Name       string `json:"name"`
	Start, End int64  `json:"-"`
	Reads      string `json:"reads"`
}

// foldCount is an exact count by qname for one window: truth counts what was
// queried, sent counts what the agent kept after its per-minute fold.
type foldCount struct {
	truth, sent map[string]int64
	total       int64
	junk        int64
}

type foldPlanted struct {
	Name      string  `json:"name"`
	PerHour   int     `json:"per_hour"`
	Truth     int64   `json:"truth"`
	TruthRank int     `json:"truth_rank"`
	Got       int64   `json:"got"`
	GotRank   int     `json:"got_rank"` // 0: not listed at all
	Err       float64 `json:"err"`
}

type foldResult struct {
	Scenario      string        `json:"scenario"`
	Window        string        `json:"window"`
	Reads         string        `json:"reads"`
	Days          int           `json:"days"`
	QPM           int           `json:"qpm"`
	Total         int64         `json:"total"`
	StoreTotal    int64         `json:"store_total"`
	OtherShare    float64       `json:"other_share"`
	Approximate   bool          `json:"approximate"`
	Recall10      float64       `json:"recall10"`
	Recall50      float64       `json:"recall50"`
	AgentRecall50 float64       `json:"agent_recall50"`  // what the agent sent, against truth
	ServerRecall  float64       `json:"server_recall50"` // the store, against what the agent sent
	MaxErr        float64       `json:"max_err"`
	MedianErr     float64       `json:"median_err"`
	Missed        []string      `json:"missed"` // true top-50 names not in the store's top 50
	Planted       []foldPlanted `json:"planted"`
	Store         string        `json:"store"`
	ErrBound      int64         `json:"err_bound"`
	ErrLimit      int64         `json:"err_limit"` // stages·total/(cap+1)
	Bounded       bool          `json:"bounded"`
	Outside       int           `json:"outside"` // listed names whose truth is outside [count, count+err]
	Pass          bool          `json:"pass"`
	Why           []string      `json:"why,omitempty"`
}

// foldStore is either engine, as the report drives it.
type foldStore interface {
	SaveDnstap(ctx context.Context, userID, collectorID string, d wire.Dnstap, now int64) error
	Dnstap(ctx context.Context, q Query, dim string, limit int) (Breakdown, error)
	Rollup(ctx context.Context, now int64) (int, error)
	Close() error
}

type kv struct {
	k string
	n int64
}

func ranked(m map[string]int64) []kv {
	out := make([]kv, 0, len(m))
	for k, n := range m {
		out = append(out, kv{k, n})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].n != out[j].n {
			return out[i].n > out[j].n
		}
		return out[i].k < out[j].k
	})
	return out
}

// recall is the share of got's first k names whose true count reaches the
// k-th true count, so ties at the cut don't count as misses.
func recall(got []string, truth []kv, tm map[string]int64, k int) float64 {
	k = min(k, len(truth))
	if k == 0 {
		return 1
	}
	cut := truth[k-1].n
	hit := 0
	for _, name := range got[:min(k, len(got))] {
		if tm[name] >= cut {
			hit++
		}
	}
	return float64(hit) / float64(k)
}

func isJunk(qname string) bool {
	first, _, _ := strings.Cut(qname, ".")
	return !strings.Contains(first, "-")
}

// plant adds n queries for qname, split between A and AAAA, to every table
// of a sampled minute so totals, names and sizes agree.
func (m *genMinute) plant(zone, qname, subnet string, n uint64) {
	if n == 0 {
		return
	}
	a := (n + 1) / 2
	for _, p := range []struct {
		qtype string
		n     uint64
	}{{"A", a}, {"AAAA", n - a}} {
		if p.n == 0 {
			continue
		}
		m.totals[[3]string{zone, p.qtype, "NOERROR"}] += p.n
		m.names[[4]string{zone, qname, p.qtype, "NOERROR"}] += p.n
		m.clients[[5]string{subnet, zone, qname, p.qtype, "NOERROR"}] += p.n
	}
	m.subnets[subnet] += n
	m.sizes[1] += n
}

// perMinute is how many of perHour queries fall in minute i of the hour.
func perMinute(perHour, i int) uint64 {
	return uint64((i+1)*perHour/60 - i*perHour/60)
}

func (g *generator) plantScenario(m *genMinute, scenario string, col int, t int64, qpm int) {
	zone, subnet := g.zones[0], g.nets[0]
	minute := int((t % hourMs) / minuteMs)
	hour := t / hourMs
	hourOfDay := int(hour % 24)
	day := t / dayMs
	for _, r := range foldLadder {
		m.plant(zone, fmt.Sprintf("steady-%d.%s", r, zone), subnet, perMinute(r, minute))
	}
	switch scenario {
	case "burst":
		for i := range foldBursts {
			m.plant(zone, fmt.Sprintf("burst-h%d-%d.%s", hour, i, zone), subnet, perMinute(1200, minute))
		}
	case "local":
		for i := range foldLocals {
			m.plant(zone, fmt.Sprintf("local-c%d-%d.%s", col, i, zone), subnet, perMinute(600, minute))
		}
	case "busy":
		if hourOfDay == 3 || hourOfDay == 15 {
			for i := range foldEnum {
				m.plant(zone, fmt.Sprintf("enum-d%d-h%d-%d.%s", day, hourOfDay, i, zone), subnet, perMinute(300, minute))
			}
		}
	case "flood":
		if hourOfDay >= 9 && hourOfDay < 15 {
			for range int(float64(qpm) * foldFloodMul) {
				m.plant(zone, g.junk()+"."+zone, subnet, 1)
			}
		}
	}
}

func TestFoldReport(t *testing.T) {
	dir := os.Getenv("FOLDREPORT_DIR")
	if dir == "" {
		t.Skip("set FOLDREPORT_DIR to run the fold report")
	}
	ctx := context.Background()
	days := envNum(t, "FOLDREPORT_DAYS", 30, 1)
	qpm := envNum(t, "FOLDREPORT_QPM", 3000, 50)
	collectors := envNum(t, "FOLDREPORT_COLLECTORS", 4, 1)
	engine := envChoice(t, "FOLDREPORT_STORE", "topn", "sql")
	scenarios := foldScenarios
	if s := os.Getenv("FOLDREPORT_SCENARIOS"); s != "" {
		scenarios = strings.Split(s, ",")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	out, err := os.Create(filepath.Join(dir, "fold-"+engine+".jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer out.Close()

	end := time.Date(2026, 11, 18, 0, 0, 0, 0, time.UTC).UnixMilli()
	first := end - int64(days)*dayMs
	minuteFrom := end - 7*dayMs
	var wins []foldWindow
	for _, w := range []foldWindow{
		{Name: "24h recent", Start: end - dayMs, End: end, Reads: "minutes"},
		{Name: "24h 8 days ago", Start: end - 9*dayMs, End: end - 8*dayMs, Reads: "hours"},
		{Name: "7d", Start: end - 7*dayMs, End: end, Reads: "hours"},
		{Name: "30d", Start: end - 30*dayMs, End: end, Reads: "days"},
	} {
		if w.Start >= first {
			wins = append(wins, w)
		}
	}
	const user = "5b0c8a2e-3f6d-4c1b-9e7a-2d4f6a8c0e11"
	cols := make([]string, collectors)
	for c := range cols {
		cols[c] = fmt.Sprintf("9d3e7f10-0000-4a5b-8c6d-%012d", c)
	}

	failed := 0
	for _, sc := range scenarios {
		started := time.Now()
		run := filepath.Join(dir, sc)
		os.RemoveAll(run)
		var k foldStore
		if engine == "sql" {
			if err := os.MkdirAll(run, 0o755); err != nil {
				t.Fatal(err)
			}
			st, err := Open(filepath.Join(run, "knot.db"))
			if err != nil {
				t.Fatal(err)
			}
			st.TopHourly = foldTop
			k = st
		} else {
			k = openKnotTopN(t, run, tblk.Blocks, foldTop)
		}
		gens := make([]*generator, collectors)
		for c := range gens {
			gens[c] = newGenerator(int64(c+1), reportCfg{Zones: 5, Names: 2000, Nets: 400})
		}
		counts := make([]*foldCount, len(wins))
		for i := range counts {
			counts[i] = &foldCount{truth: map[string]int64{}, sent: map[string]int64{}}
		}
		for m := first; m < end; m += minuteMs {
			var in []*foldCount
			for i, w := range wins {
				if m >= w.Start && m < w.End {
					in = append(in, counts[i])
				}
			}
			for c, g := range gens {
				gm := g.sample(qpm)
				g.plantScenario(gm, sc, c, m, qpm)
				d := gm.bucket(m, foldTop, 0, 0, 1) // client rows don't touch the names family
				for _, fc := range in {
					for key, n := range gm.names {
						fc.total += int64(n)
						if isJunk(key[1]) {
							fc.junk += int64(n)
							continue
						}
						fc.truth[key[1]] += int64(n)
					}
					// The name list reads the per-name counts.
					for _, r := range qnamesOf(d) {
						if r.QName != wire.Other && !isJunk(r.QName) {
							fc.sent[r.QName] += int64(r.Count)
						}
					}
				}
				if err := k.SaveDnstap(ctx, user, cols[c], d, m+minuteMs); err != nil {
					t.Fatal(err)
				}
			}
			if (m+minuteMs)%hourMs == 0 {
				if _, err := k.Rollup(ctx, m+minuteMs); err != nil {
					t.Fatal(err)
				}
			}
			if (m+minuteMs)%dayMs == 0 {
				t.Logf("%s: day %d of %d fed (%s)", sc, (m+minuteMs-first)/dayMs, days, time.Since(started).Round(time.Second))
			}
		}
		// Seal the last hours and days.
		for {
			n, err := k.Rollup(ctx, end+3*hourMs)
			if err != nil {
				t.Fatal(err)
			}
			if n == 0 {
				break
			}
		}
		for i, w := range wins {
			stages := int64(map[string]int{"minutes": 1, "hours": 2, "days": 3}[w.Reads])
			if engine == "sql" && stages > 2 {
				stages = 2 // knot.db has no day tier
			}
			res := foldCompare(ctx, t, k, user, sc, w, counts[i], minuteFrom, stages)
			res.Store = engine
			res.Days, res.QPM = days, qpm
			if !res.Pass {
				failed++
			}
			b, _ := json.Marshal(res)
			fmt.Fprintln(out, string(b))
			t.Logf("%s %-8s %-15s (%s) recall@10 %.2f @50 %.2f | agent %.2f server %.2f | max err %.1f%% median %.1f%% | other %.1f%% | bound %d (%.3f%%, limit %d) outside %d | %s",
				engine, sc, w.Name, w.Reads, res.Recall10, res.Recall50, res.AgentRecall50, res.ServerRecall, 100*res.MaxErr, 100*res.MedianErr, 100*res.OtherShare,
				res.ErrBound, 100*float64(res.ErrBound)/float64(max(res.Total, 1)), res.ErrLimit, res.Outside, passWord(res))
			for _, p := range res.Planted {
				if p.TruthRank <= 200 || p.GotRank > 0 {
					t.Logf("    %-22s %5d/h  truth rank %4d  got rank %4d  err %5.1f%%", p.Name, p.PerHour, p.TruthRank, p.GotRank, 100*p.Err)
				}
			}
			for _, why := range res.Why {
				t.Logf("    fails: %s", why)
			}
		}
		k.Close()
		t.Logf("%s done in %s", sc, time.Since(started).Round(time.Second))
	}
	t.Logf("gate: %d window(s) failed", failed)
}

func passWord(r foldResult) string {
	if r.Pass {
		return "pass"
	}
	return "FAIL"
}

func foldCompare(ctx context.Context, t *testing.T, k foldStore, user, sc string, w foldWindow, fc *foldCount, minuteFrom, stages int64) foldResult {
	t.Helper()
	b, err := k.Dnstap(ctx, Query{UserID: user, Start: w.Start, End: w.End, MinuteFrom: minuteFrom}, "qname", 0)
	if err != nil {
		t.Fatal(err)
	}
	tot, err := k.Dnstap(ctx, Query{UserID: user, Start: w.Start, End: w.End, MinuteFrom: minuteFrom}, "zone", 0)
	if err != nil {
		t.Fatal(err)
	}
	res := foldResult{Scenario: sc, Window: w.Name, Reads: w.Reads, Total: fc.total, StoreTotal: tot.Total, Approximate: b.Approximate,
		ErrBound: b.ErrBound, Bounded: b.Bounded, ErrLimit: stages * tot.Total / (foldTop + 1)}
	// The guarantee: every listed name's truth lies in [count, count+err].
	// Junk names are counted only in aggregate, so they are skipped.
	for _, r := range b.Rows {
		if isJunk(r.Key) {
			continue
		}
		if tn := fc.truth[r.Key]; tn < r.Count || tn > r.Count+b.ErrBound {
			res.Outside++
		}
	}
	got := make([]string, 0, len(b.Rows))
	gotN := map[string]int64{}
	gotRank := map[string]int{}
	var listed int64
	for i, r := range b.Rows {
		got = append(got, r.Key)
		gotN[r.Key] = r.Count
		gotRank[r.Key] = i + 1
		listed += r.Count
	}
	if tot.Total > 0 {
		res.OtherShare = float64(tot.Total-listed) / float64(tot.Total)
	}
	truth, sent := ranked(fc.truth), ranked(fc.sent)
	sentNames := make([]string, len(sent))
	for i, s := range sent {
		sentNames[i] = s.k
	}
	res.Recall10 = recall(got, truth, fc.truth, 10)
	res.Recall50 = recall(got, truth, fc.truth, foldK)
	res.AgentRecall50 = recall(sentNames, truth, fc.truth, foldK)
	res.ServerRecall = recall(got, sent, fc.sent, foldK)
	var errs []float64
	for _, name := range got[:min(foldK, len(got))] {
		if tn := fc.truth[name]; tn > 0 {
			errs = append(errs, float64(tn-gotN[name])/float64(tn))
		}
	}
	sort.Float64s(errs)
	if len(errs) > 0 {
		res.MaxErr, res.MedianErr = errs[len(errs)-1], errs[len(errs)/2]
	}
	inTop := map[string]bool{}
	for _, name := range got[:min(foldK, len(got))] {
		inTop[name] = true
	}
	truthRank := map[string]int{}
	for i, x := range truth {
		truthRank[x.k] = i + 1
	}
	for i := range min(foldK, len(truth)) {
		if !inTop[truth[i].k] && truth[i].n > truth[min(foldK, len(truth))-1].n {
			res.Missed = append(res.Missed, truth[i].k)
		}
	}
	for _, r := range foldLadder {
		name := fmt.Sprintf("steady-%d.zone00.example.net.", r)
		p := foldPlanted{Name: strings.TrimSuffix(name, ".zone00.example.net."), PerHour: r, Truth: fc.truth[name], TruthRank: truthRank[name], Got: gotN[name], GotRank: gotRank[name]}
		if p.Truth > 0 {
			p.Err = float64(p.Truth-p.Got) / float64(p.Truth)
		}
		res.Planted = append(res.Planted, p)
	}

	// The gate, set before the first run.
	res.Pass = true
	fail := func(f string, a ...any) { res.Pass = false; res.Why = append(res.Why, fmt.Sprintf(f, a...)) }
	if res.StoreTotal != res.Total {
		fail("store total %d differs from truth %d", res.StoreTotal, res.Total)
	}
	if res.Recall10 < 1 {
		fail("top-10 recall %.2f", res.Recall10)
	}
	if res.Recall50 < 0.98 {
		fail("top-50 recall %.2f, missing %v", res.Recall50, res.Missed)
	}
	for _, p := range res.Planted {
		if p.TruthRank > 0 && p.TruthRank <= foldK && (p.GotRank == 0 || p.GotRank > foldK) {
			fail("%s is rank %d but not in the top %d", p.Name, p.TruthRank, foldK)
		}
	}
	if res.MaxErr > 0.02 {
		fail("a top-50 count is %.1f%% low", 100*res.MaxErr)
	}
	if !res.Pass && !res.Approximate {
		fail("answer was wrong but not marked approximate")
	}
	// The bound itself, checked whatever the gate above says.
	if !res.Bounded {
		fail("answer not bounded")
	}
	if res.Outside > 0 {
		fail("%d listed names outside [count, count+%d]", res.Outside, res.ErrBound)
	}
	if res.ErrBound > res.ErrLimit {
		fail("error %d over the limit %d", res.ErrBound, res.ErrLimit)
	}
	return res
}
