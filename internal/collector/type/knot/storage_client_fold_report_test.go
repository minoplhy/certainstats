//go:build knotreport

// The client fold report: how far Knot's client network lists drift from the
// truth. Collectors cut networks, network by name and network by qtype with
// the bound rule each minute; the server cuts again per hour. This feeds
// generated traffic with planted networks through the production store,
// keeps an exact count beside it, and checks the three client views the page
// shows: the top networks, one network's names, and one name's networks.
//
//	FOLDREPORT_DIR=~/.cache/certainstats-knotreport/clients1 \
//	  GOFLAGS=-p=1 nice go test -tags knotreport -run TestClientFoldReport -timeout 0 -v ./internal/collector/type/knot/
//
// FOLDREPORT_DAYS (3), FOLDREPORT_QPM (3000), FOLDREPORT_COLLECTORS (4),
// FOLDREPORT_NETS (20000), FOLDREPORT_STORE (topn|sql) and
// FOLDREPORT_SCENARIOS (clients,clients-burst) tune it.
package knot

import (
	"context"
	"encoding/json"
	"fmt"
	"net/netip"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"certainstats/internal/collector/type/knot/wire"
	"certainstats/internal/tblk"
)

// clientLadder is the planted steady networks' rates, queries per hour on
// each collector. Each spreads its queries over clientLadderNames names.
var clientLadder = []int{30, 60, 120, 240, 480, 960, 1920}

const (
	clientMax         = 50   // only switches client lists on in the generator; networks are cut at foldTop
	clientTopRows     = 1000 // collector default KNOT_DNSTAP_CLIENT_TOP
	clientLadderNames = 20
	clientBursts      = 120 // burst networks per hour, each 30 a minute for 10 minutes
	clientSpreadPerM  = 30  // spread-name queries a minute, each from a random network
	clientSpreadName  = "spread-name.zone00.example.net."
)

func ladderNet(i int) string { return fmt.Sprintf("fd01:0:%x::/48", i+1) }

// canonNet makes generator subnets and stored labels comparable.
func canonNet(s string) string {
	p, err := netip.ParsePrefix(s)
	if err != nil {
		return s
	}
	return p.Masked().String()
}

type clientCount struct {
	total     int64
	net       map[string]int64            // truth by network
	sent      map[string]int64            // what the collectors sent, by network
	focusNet  map[string]map[string]int64 // network → qname → truth
	focusName map[string]map[string]int64 // qname → network → truth
}

type clientFocus struct {
	Kind       string   `json:"kind"` // "network" (its names) or "name" (its networks)
	Key        string   `json:"key"`
	Truth      int64    `json:"truth"`
	Listed     int64    `json:"listed"`
	StoreTotal int64    `json:"store_total"`
	Coverage   float64  `json:"coverage"` // listed / truth
	Recall10   float64  `json:"recall10"`
	MaxErr     float64  `json:"max_err"` // worst of the listed top 10
	Missed     []string `json:"missed,omitempty"`
	ErrBound   int64    `json:"err_bound"`
	Bounded    bool     `json:"bounded"`
	Outside    int      `json:"outside"` // listed keys whose truth is outside [count, count+err]
}

type clientResult struct {
	Scenario      string        `json:"scenario"`
	Window        string        `json:"window"`
	Reads         string        `json:"reads"`
	Store         string        `json:"store"`
	Days          int           `json:"days"`
	QPM           int           `json:"qpm"`
	Nets          int           `json:"nets"`
	Total         int64         `json:"total"`
	StoreTotal    int64         `json:"store_total"`
	Coverage      float64       `json:"coverage"`       // traffic in listed networks / total
	AgentCoverage float64       `json:"agent_coverage"` // traffic the collectors sent / total
	Recall10      float64       `json:"recall10"`
	Recall50      float64       `json:"recall50"`
	AgentRecall50 float64       `json:"agent_recall50"`
	MaxErr        float64       `json:"max_err"`
	MedianErr     float64       `json:"median_err"`
	Missed        []string      `json:"missed,omitempty"`
	Approximate   bool          `json:"approximate"`
	Ladder        []foldPlanted `json:"ladder"`
	Focus         []clientFocus `json:"focus"`
	ErrBound      int64         `json:"err_bound"`
	ErrLimit      int64         `json:"err_limit"` // stages·total/(cap+1)
	Bounded       bool          `json:"bounded"`
	Outside       int           `json:"outside"`
	Pass          bool          `json:"pass"`
	Why           []string      `json:"why,omitempty"`
}

func (g *generator) plantClients(m *genMinute, scenario string, t int64) {
	zone := g.zones[0]
	minute := int((t % hourMs) / minuteMs)
	hour := t / hourMs
	for i, r := range clientLadder {
		n := int(perMinute(r, minute))
		for j := range n {
			m.plant(zone, fmt.Sprintf("net%d-%d.%s", r, (minute+j)%clientLadderNames, zone), ladderNet(i), 1)
		}
	}
	for range clientSpreadPerM {
		m.plant(zone, clientSpreadName, g.nets[g.rng.Intn(len(g.nets))], 1)
	}
	if scenario == "clients-burst" {
		// Burst network i is busy for 10 minutes from minute i/2 of its hour.
		for i := range clientBursts {
			if s := i / 2; minute >= s && minute < s+10 {
				m.plant(zone, fmt.Sprintf("burst%d.%s", i%40, zone), fmt.Sprintf("fd02:%x:%x::/48", hour%65536, i+1), 30)
			}
		}
	}
}

func TestClientFoldReport(t *testing.T) {
	dir := os.Getenv("FOLDREPORT_DIR")
	if dir == "" {
		t.Skip("set FOLDREPORT_DIR to run the client fold report")
	}
	ctx := context.Background()
	days := envNum(t, "FOLDREPORT_DAYS", 3, 2)
	qpm := envNum(t, "FOLDREPORT_QPM", 3000, 50)
	collectors := envNum(t, "FOLDREPORT_COLLECTORS", 4, 1)
	nets := envNum(t, "FOLDREPORT_NETS", 20000, 100)
	engine := envChoice(t, "FOLDREPORT_STORE", "topn", "sql")
	scenarios := []string{"clients", "clients-burst"}
	if s := os.Getenv("FOLDREPORT_SCENARIOS"); s != "" {
		scenarios = strings.Split(s, ",")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	out, err := os.Create(filepath.Join(dir, "clients-"+engine+".jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer out.Close()

	end := time.Date(2026, 11, 18, 0, 0, 0, 0, time.UTC).UnixMilli()
	first := end - int64(days)*dayMs
	minuteFrom := end - dayMs // older windows read hours
	wins := []foldWindow{
		{Name: "24h recent", Start: end - dayMs, End: end, Reads: "minutes"},
		{Name: fmt.Sprintf("%dh before", 24*(days-1)), Start: first, End: end - dayMs, Reads: "hours"},
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
			gens[c] = newGenerator(int64(c+1), reportCfg{Zones: 5, Names: 2000, Nets: nets})
		}
		pool := map[string]bool{}
		for _, n := range gens[0].nets {
			pool[canonNet(n)] = true
		}
		for i := range clientLadder {
			if pool[canonNet(ladderNet(i))] {
				t.Fatalf("ladder network %s is also in the pool", ladderNet(i))
			}
		}
		// Focus: the busiest pool network, two ladder networks, the busiest
		// pool name and the spread name.
		focusNets := []string{canonNet(gens[0].nets[0]), canonNet(ladderNet(3)), canonNet(ladderNet(6))}
		focusNames := []string{gens[0].names[0].qname, clientSpreadName}
		counts := make([]*clientCount, len(wins))
		for i := range counts {
			c := &clientCount{net: map[string]int64{}, sent: map[string]int64{}, focusNet: map[string]map[string]int64{}, focusName: map[string]map[string]int64{}}
			for _, n := range focusNets {
				c.focusNet[n] = map[string]int64{}
			}
			for _, n := range focusNames {
				c.focusName[n] = map[string]int64{}
			}
			counts[i] = c
		}
		for m := first; m < end; m += minuteMs {
			var in []*clientCount
			for i, w := range wins {
				if m >= w.Start && m < w.End {
					in = append(in, counts[i])
				}
			}
			for c, g := range gens {
				gm := g.sample(qpm)
				g.plantClients(gm, sc, m)
				d := gm.bucket(m, foldTop, clientMax, clientTopRows, 1)
				for _, cc := range in {
					for s, n := range gm.subnets {
						cc.total += int64(n)
						cc.net[canonNet(s)] += int64(n)
					}
					for key, n := range gm.clients {
						s := canonNet(key[0])
						if f := cc.focusNet[s]; f != nil {
							f[key[2]] += int64(n)
						}
						if f := cc.focusName[key[2]]; f != nil {
							f[s] += int64(n)
						}
					}
					for _, r := range d.Nets {
						if r.Subnet != wire.Other {
							cc.sent[canonNet(r.Subnet)] += int64(r.Count)
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
			res := clientCompare(ctx, t, k, user, w, counts[i], minuteFrom, focusNets, focusNames)
			res.Scenario, res.Store, res.Days, res.QPM, res.Nets = sc, engine, days, qpm, nets
			if !res.Pass {
				failed++
			}
			b, _ := json.Marshal(res)
			fmt.Fprintln(out, string(b))
			t.Logf("%s %-13s %-12s (%s) networks: covered %.1f%% (collectors sent %.1f%%) | recall@10 %.2f @50 %.2f (sent %.2f) | max err %.1f%% median %.1f%% | bound %d (%.3f%%, limit %d) outside %d | %s",
				engine, sc, w.Name, w.Reads, 100*res.Coverage, 100*res.AgentCoverage, res.Recall10, res.Recall50, res.AgentRecall50, 100*res.MaxErr, 100*res.MedianErr,
				res.ErrBound, 100*float64(res.ErrBound)/float64(max(res.Total, 1)), res.ErrLimit, res.Outside, passWord(foldResult{Pass: res.Pass}))
			for _, p := range res.Ladder {
				t.Logf("    %-14s %5d/h  truth rank %5d  got rank %5d  err %5.1f%%", p.Name, p.PerHour, p.TruthRank, p.GotRank, 100*p.Err)
			}
			for _, f := range res.Focus {
				what := "names of network"
				if f.Kind == "name" {
					what = "networks of name"
				}
				t.Logf("    %s %-34s covered %5.1f%%  recall@10 %.2f  max err %5.1f%%  (truth %d, listed %d, bound %d, outside %d)", what, f.Key, 100*f.Coverage, f.Recall10, 100*f.MaxErr, f.Truth, f.Listed, f.ErrBound, f.Outside)
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

// listCheck compares a stored list with exact counts: coverage of the
// truth's total, recall of the top k, and the worst error in the listed top k.
func listCheck(rows []Row, truth map[string]int64, canon func(string) string, skip func(string) bool, k int) (listed int64, recallK, maxErr float64, missed []string, gotN map[string]int64, gotRank map[string]int) {
	gotN, gotRank = map[string]int64{}, map[string]int{}
	var got []string
	for i, r := range rows {
		key := canon(r.Key)
		got = append(got, key)
		gotN[key] = r.Count
		gotRank[key] = i + 1
		listed += r.Count
	}
	filtered := map[string]int64{}
	for key, n := range truth {
		if skip == nil || !skip(key) {
			filtered[key] = n
		}
	}
	tr := ranked(filtered)
	recallK = recall(got, tr, filtered, k)
	for _, key := range got[:min(k, len(got))] {
		if tn := filtered[key]; tn > 0 {
			maxErr = max(maxErr, float64(tn-gotN[key])/float64(tn))
		}
	}
	if kk := min(k, len(tr)); kk > 0 {
		inTop := map[string]bool{}
		for _, key := range got[:min(k, len(got))] {
			inTop[key] = true
		}
		for _, x := range tr[:kk] {
			if !inTop[x.k] && x.n > tr[kk-1].n {
				missed = append(missed, x.k)
			}
		}
	}
	return listed, recallK, maxErr, missed, gotN, gotRank
}

// outside counts listed keys whose truth is not within [count, count+err].
func outside(rows []Row, truth map[string]int64, canon func(string) string, err int64) int {
	n := 0
	for _, r := range rows {
		if tn := truth[canon(r.Key)]; tn < r.Count || tn > r.Count+err {
			n++
		}
	}
	return n
}

func clientCompare(ctx context.Context, t *testing.T, k foldStore, user string, w foldWindow, cc *clientCount, minuteFrom int64, focusNets, focusNames []string) clientResult {
	t.Helper()
	base := Query{UserID: user, Start: w.Start, End: w.End, MinuteFrom: minuteFrom}
	b, err := k.Dnstap(ctx, base, "client", 0)
	if err != nil {
		t.Fatal(err)
	}
	stages := int64(map[string]int{"minutes": 1, "hours": 2}[w.Reads])
	res := clientResult{Window: w.Name, Reads: w.Reads, Total: cc.total, StoreTotal: b.Total, Approximate: b.Approximate,
		ErrBound: b.ErrBound, Bounded: b.Bounded, ErrLimit: stages * cc.total / (foldTop + 1), Outside: outside(b.Rows, cc.net, canonNet, b.ErrBound)}
	listed, r50, _, missed, gotN, gotRank := listCheck(b.Rows, cc.net, canonNet, nil, foldK)
	res.Recall50, res.Missed = r50, missed
	_, res.Recall10, _, _, _, _ = listCheck(b.Rows, cc.net, canonNet, nil, 10)
	res.Coverage = float64(listed) / float64(max(cc.total, 1))
	var sentSum int64
	for _, n := range cc.sent {
		sentSum += n
	}
	res.AgentCoverage = float64(sentSum) / float64(max(cc.total, 1))
	sentNames := make([]string, 0, len(cc.sent))
	for _, x := range ranked(cc.sent) {
		sentNames = append(sentNames, x.k)
	}
	res.AgentRecall50 = recall(sentNames, ranked(cc.net), cc.net, foldK)
	var errs []float64
	for i, r := range b.Rows {
		if i >= foldK {
			break
		}
		if tn := cc.net[canonNet(r.Key)]; tn > 0 {
			errs = append(errs, float64(tn-r.Count)/float64(tn))
		}
	}
	sort.Float64s(errs)
	if len(errs) > 0 {
		res.MaxErr, res.MedianErr = errs[len(errs)-1], errs[len(errs)/2]
	}
	truthRank := map[string]int{}
	for i, x := range ranked(cc.net) {
		truthRank[x.k] = i + 1
	}
	for i, r := range clientLadder {
		n := canonNet(ladderNet(i))
		p := foldPlanted{Name: fmt.Sprintf("steady-net-%d", r), PerHour: r, Truth: cc.net[n], TruthRank: truthRank[n], Got: gotN[n], GotRank: gotRank[n]}
		if p.Truth > 0 {
			p.Err = float64(p.Truth-p.Got) / float64(p.Truth)
		}
		res.Ladder = append(res.Ladder, p)
	}
	sumOf := func(m map[string]int64) (s int64) {
		for _, n := range m {
			s += n
		}
		return s
	}
	for _, n := range focusNets {
		q := base
		q.Client = netip.MustParsePrefix(n)
		fb, err := k.Dnstap(ctx, q, "qname", 0)
		if err != nil {
			t.Fatal(err)
		}
		truth := cc.focusNet[n]
		l, r10, me, miss, _, _ := listCheck(fb.Rows, truth, func(s string) string { return s }, isJunk, 10)
		tot := sumOf(truth)
		res.Focus = append(res.Focus, clientFocus{Kind: "network", Key: n, Truth: tot, Listed: l, StoreTotal: fb.Total, Coverage: float64(l) / float64(max(tot, 1)), Recall10: r10, MaxErr: me, Missed: miss,
			ErrBound: fb.ErrBound, Bounded: fb.Bounded, Outside: outside(fb.Rows, truth, func(s string) string { return s }, fb.ErrBound)})
	}
	for _, n := range focusNames {
		q := base
		q.QName = n
		fb, err := k.Dnstap(ctx, q, "client", 0)
		if err != nil {
			t.Fatal(err)
		}
		truth := cc.focusName[n]
		l, r10, me, miss, _, _ := listCheck(fb.Rows, truth, canonNet, nil, 10)
		tot := sumOf(truth)
		res.Focus = append(res.Focus, clientFocus{Kind: "name", Key: n, Truth: tot, Listed: l, StoreTotal: fb.Total, Coverage: float64(l) / float64(max(tot, 1)), Recall10: r10, MaxErr: me, Missed: miss,
			ErrBound: fb.ErrBound, Bounded: fb.Bounded, Outside: outside(fb.Rows, truth, canonNet, fb.ErrBound)})
	}

	// The gate: the same bar as the name report. Coverage (traffic in listed
	// keys) is reported but not gated, as the name report's "(other)" share
	// is not: a long tail folds under any cap.
	res.Pass = true
	fail := func(f string, a ...any) { res.Pass = false; res.Why = append(res.Why, fmt.Sprintf(f, a...)) }
	if res.StoreTotal != res.Total {
		fail("store total %d differs from truth %d", res.StoreTotal, res.Total)
	}
	if res.Recall10 < 1 {
		fail("top-10 network recall %.2f", res.Recall10)
	}
	if res.Recall50 < 0.98 {
		fail("top-50 network recall %.2f, missing %d", res.Recall50, len(res.Missed))
	}
	if res.MaxErr > 0.02 {
		fail("a top-50 network count is %.1f%% low", 100*res.MaxErr)
	}
	for _, p := range res.Ladder {
		if p.TruthRank > 0 && p.TruthRank <= foldK && (p.GotRank == 0 || p.GotRank > foldK) {
			fail("%s is rank %d but not in the top %d", p.Name, p.TruthRank, foldK)
		}
	}
	for _, f := range res.Focus {
		// A name asked by thousands of networks a few times each cannot be
		// listed network by network under any cap; it is held to the
		// guarantee only.
		if f.Key != clientSpreadName && (f.Recall10 < 1 || f.MaxErr > 0.02) {
			fail("%s %s: covered %.1f%%, recall@10 %.2f, max err %.1f%%", f.Kind, f.Key, 100*f.Coverage, f.Recall10, 100*f.MaxErr)
		}
		if !f.Bounded || f.Outside > 0 {
			fail("%s %s: bounded %v, %d listed outside [count, count+%d]", f.Kind, f.Key, f.Bounded, f.Outside, f.ErrBound)
		}
	}
	// The bound on the network list.
	if !res.Bounded {
		fail("network list not bounded")
	}
	if res.Outside > 0 {
		fail("%d listed networks outside [count, count+%d]", res.Outside, res.ErrBound)
	}
	if res.ErrBound > res.ErrLimit {
		fail("network error %d over the limit %d", res.ErrBound, res.ErrLimit)
	}
	return res
}
