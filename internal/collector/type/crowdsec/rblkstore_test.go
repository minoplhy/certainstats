package crowdsec

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"certainstats/internal/collector/type/crowdsec/wire"
)

// rgFeed writes one day of every collector to each store, minute by minute,
// sealing on the hour as the hourly maintenance does. With check set, every
// store must report the same stored, duplicate and newest counts. It returns
// each store's write time in seconds.
func rgFeed(ctx context.Context, day [][][]wire.Alert, start int64, stores []*Store, check bool) ([]float64, error) {
	secs := make([]float64, len(stores))
	for m := range int64(1440) {
		minute := start + m*rgMinuteMs
		recv := minute + rgMinuteMs + 5_000
		for c := range day {
			alerts := day[c][m]
			if len(alerts) == 0 {
				continue
			}
			col := fmt.Sprintf("c%d", c)
			var first [3]int64
			for i, st := range stores {
				t0 := time.Now()
				stored, dup, newest, err := st.Insert(ctx, rgUser, col, alerts, recv)
				if err != nil {
					return secs, err
				}
				secs[i] += time.Since(t0).Seconds()
				got := [3]int64{int64(stored), int64(dup), newest}
				if i == 0 {
					first = got
				} else if check && got != first {
					return secs, fmt.Errorf("insert at %d: store %d gave %v, store 0 %v", minute, i, got, first)
				}
			}
		}
		if (minute+rgMinuteMs)%hourMs == 0 {
			for i, st := range stores {
				t0 := time.Now()
				if err := st.Seal(ctx, minute+rgMinuteMs); err != nil {
					return secs, err
				}
				secs[i] += time.Since(t0).Seconds()
			}
		}
	}
	return secs, nil
}

func rgDay(g *rgGen, collectors int, d int64) [][][]wire.Alert {
	out := make([][][]wire.Alert, collectors)
	for c := range collectors {
		out[c] = g.day(c, d)
	}
	return out
}

func rgFilter(start, end int64, inc, exc map[string][]string, q string) Filter {
	f := Filter{Start: start, End: end, Include: map[string][]string{}, Exclude: map[string][]string{}, Q: q}
	for k, v := range inc {
		f.Include[k] = v
	}
	for k, v := range exc {
		f.Exclude[k] = v
	}
	return f
}

type rgCase struct {
	name string
	f    Filter
}

// rgCases crosses time windows (those that fit in the data) with filters on
// every field, exclusions and searches.
func rgCases(g *rgGen, end, oldest int64, collectors int) []rgCase {
	type win struct {
		name       string
		start, end int64
	}
	var wins []win
	for _, w := range []win{{"1h", end - hourMs, end}, {"90m off", end - 97*rgMinuteMs, end - 7*rgMinuteMs}, {"24h", end - rgDayMs, end}, {"3d", end - 3*rgDayMs, end}, {"7d", end - 7*rgDayMs, end}} {
		if w.start >= oldest {
			wins = append(wins, w)
		}
	}
	// Longer than the rollup switch: crowdsec.db answers from rollup_hourly,
	// rblk from its records.
	wins = append(wins, win{"8d", end - rollupAfterMs - rgDayMs, end})
	topIP, topRange, topASN := g.ips[0], g.ipRange[0], strconv.Itoa(g.ipASN[0])
	filters := []struct {
		name     string
		inc, exc map[string][]string
		q        string
	}{
		{name: "none"},
		{name: "scenario", inc: map[string][]string{"scenario": {g.scenarios[0], g.scenarios[3]}}},
		{name: "not country", exc: map[string][]string{"country": {g.ipCountry[0]}}},
		{name: "machine+action", inc: map[string][]string{"machine": {"machine-0-1"}, "action": {"ban"}}},
		{name: "collector", inc: map[string][]string{"collector": {fmt.Sprintf("c%d", collectors-1)}}},
		{name: "ip", inc: map[string][]string{"ip": {topIP}}},
		{name: "range+asn", inc: map[string][]string{"range": {topRange}, "asn": {topASN}}},
		{name: "search ip part", q: topIP[:strings.LastIndex(topIP, ".")]},
		{name: "search scenario", q: "SCENARIO-0"},
		{name: "search name", q: "net 12"},
		{name: "search nothing", q: "zz%_z"},
	}
	var out []rgCase
	for _, w := range wins {
		for _, fl := range filters {
			out = append(out, rgCase{w.name + " / " + fl.name, rgFilter(w.start, w.end, fl.inc, fl.exc, fl.q)})
		}
	}
	return out
}

// normIP orders what SQL leaves unordered: groups tied on alerts, and
// decisions of one alert.
func normIP(v *IPView) {
	sortByAlerts(v.Scenarios)
	sortByAlerts(v.Machines)
	sort.SliceStable(v.Decisions, func(i, j int) bool {
		a, b := v.Decisions[i], v.Decisions[j]
		if a.StartedAt != b.StartedAt {
			return a.StartedAt > b.StartedAt
		}
		if a.AlertID != b.AlertID {
			return a.AlertID > b.AlertID
		}
		return fmt.Sprint(a.Decision) < fmt.Sprint(b.Decision)
	})
}

// rgCompare checks every screen of got against want (the crowdsec.db store),
// coverage included; it returns how many answers were compared.
func rgCompare(ctx context.Context, t *testing.T, label string, want, got *Store, g *rgGen, end, oldest int64, collectors int) int {
	t.Helper()
	n := 0
	fail := func(what string, g, w any) {
		t.Helper()
		gj, _ := json.Marshal(g)
		wj, _ := json.Marshal(w)
		if len(gj) > 600 {
			gj = gj[:600]
		}
		if len(wj) > 600 {
			wj = wj[:600]
		}
		t.Fatalf("%s %s:\n got %s\nwant %s", label, what, gj, wj)
	}
	same := func(what string, gv, gerr, wv, werr any) {
		t.Helper()
		if gerr != nil || werr != nil {
			t.Fatalf("%s %s: errors %v / %v", label, what, gerr, werr)
		}
		if !reflect.DeepEqual(gv, wv) {
			fail(what, gv, wv)
		}
		n++
	}
	// cover checks that rblk reads its records alone (no rollup, nothing marked
	// expired) and reaches as far back as crowdsec.db's raw alerts. It returns
	// whether the rest can be compared: crowdsec.db's rollup outlives pruned
	// alerts, which rblk deliberately does not count.
	cover := func(what string, gc, wc *Coverage) bool {
		t.Helper()
		if gc.Source != "raw" || gc.RollupSince != 0 || gc.DetailExpiredBefore != 0 || gc.RawSince != wc.RawSince {
			fail(what+" coverage", gc, wc)
		}
		rollup := wc.Source == "rollup"
		pruned := wc.RollupSince > 0 && (wc.RawSince == 0 || wc.RollupSince < hourOf(wc.RawSince))
		*gc, *wc = Coverage{}, Coverage{}
		return !rollup || !pruned
	}
	for _, c := range rgCases(g, end, oldest, collectors) {
		for _, sk := range []string{"time", "ip", "scenario", "events", "asn"} {
			for _, desc := range []bool{true, false} {
				for _, page := range []int{1, 7} {
					w, werr := want.Alerts(ctx, rgUser, c.f, sk, desc, page, 25)
					gv, gerr := got.Alerts(ctx, rgUser, c.f, sk, desc, page, 25)
					what := fmt.Sprintf("%s list by %s desc=%v page %d", c.name, sk, desc, page)
					if werr == nil && gerr == nil && !cover(what, &gv.Coverage, &w.Coverage) {
						continue
					}
					same(what, gv, gerr, w, werr)
				}
			}
		}
		ws, werr := want.Summary(ctx, rgUser, c.f, end)
		gs, gerr := got.Summary(ctx, rgUser, c.f, end)
		if werr == nil && gerr == nil {
			rollup := ws.Source == "rollup"
			if cover(c.name+" summary", &gs.Coverage, &ws.Coverage) {
				if rollup {
					gs.UniqueIPs = nil // the rollup has no addresses to count
				}
				same(c.name+" summary", gs, gerr, ws, werr)
			}
		}
		wt, werr := want.Timeline(ctx, rgUser, c.f)
		gt, gerr := got.Timeline(ctx, rgUser, c.f)
		if werr == nil && gerr == nil && cover(c.name+" timeline", &gt.Coverage, &wt.Coverage) {
			same(c.name+" timeline", gt, gerr, wt, werr)
		}
		for _, fd := range fields {
			wg, werr := want.Groups(ctx, rgUser, c.f, fd, 10)
			gg, gerr := got.Groups(ctx, rgUser, c.f, fd, 10)
			if werr != nil || gerr != nil {
				same(c.name+" groups by "+fd.Param, gg, gerr, wg, werr)
				continue
			}
			rollup := wg.Source == "rollup"
			if !cover(c.name+" groups by "+fd.Param, &gg.Coverage, &wg.Coverage) {
				continue
			}
			if rollup {
				for i := range gg.Items {
					gg.Items[i].Label = "" // the rollup keeps no AS names
				}
			}
			same(c.name+" groups by "+fd.Param, gg, gerr, wg, werr)
		}
	}
	// Detail for sampled ids, and IP pages for heavy, rare and unknown addresses.
	var maxID int64
	want.read.QueryRow(`SELECT COALESCE(MAX(id), 0) FROM alerts`).Scan(&maxID)
	for id := int64(1); id <= maxID+1; id += max(1, maxID/60) {
		w, werr := want.Alert(ctx, rgUser, id)
		gv, gerr := got.Alert(ctx, rgUser, id)
		if (werr != nil) != (gerr != nil) {
			t.Fatalf("%s alert %d: errors %v vs %v", label, id, gerr, werr)
		}
		if werr == nil && !reflect.DeepEqual(gv, w) {
			fail(fmt.Sprintf("alert %d", id), gv, w)
		}
		n++
	}
	for _, ip := range []string{g.ips[0], g.ips[1], g.ips[100], g.ips[40000], "203.0.113.9"} {
		w, werr := want.IP(ctx, rgUser, ip)
		gv, gerr := got.IP(ctx, rgUser, ip)
		if werr != nil || gerr != nil {
			t.Fatalf("%s ip %s: errors %v / %v", label, ip, gerr, werr)
		}
		normIP(w)
		normIP(gv)
		same("ip "+ip, gv, nil, w, nil)
	}
	return n
}

func openPair(t *testing.T) (sqlStore, recStore *Store) {
	t.Helper()
	dir := t.TempDir()
	a, err := Open(filepath.Join(dir, "sql.db"))
	if err != nil {
		t.Fatal(err)
	}
	b, err := Open(filepath.Join(dir, "rblk.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := b.UseRecords(filepath.Join(dir, "crowdsec-rblk")); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.Close(); b.Close() })
	return a, b
}

// TestRblkMatchesSQL feeds the same alerts to a Store on crowdsec.db and one
// on rblk, and checks every screen through sealing, duplicates, late alerts,
// retention and a removed account.
func TestRblkMatchesSQL(t *testing.T) {
	ctx := context.Background()
	sqlS, recS := openPair(t)
	stores := []*Store{sqlS, recS}
	g := newRGGen(1500)
	const collectors, days = 2, 2
	var resend []wire.Alert
	for d := range int64(days) {
		day := rgDay(g, collectors, d)
		if _, err := rgFeed(ctx, day, rgFirst+d*rgDayMs, stores, true); err != nil {
			t.Fatal(err)
		}
		resend = append(resend, day[0][600]...)
	}
	end := rgFirst + days*rgDayMs
	// Duplicates of alerts already sealed: both stores count them the same.
	for i, st := range stores {
		stored, dup, _, err := st.Insert(ctx, rgUser, "c0", resend, end)
		if err != nil || stored != 0 || dup != len(resend) {
			t.Fatalf("store %d resend: stored %d dup %d of %d (%v)", i, stored, dup, len(resend), err)
		}
	}
	n := rgCompare(ctx, t, "fed", sqlS, recS, g, end, rgFirst, collectors)
	t.Logf("%d answers identical", n)
	// Late alerts for a sealed day, then sealing them.
	var late []wire.Alert
	for _, m := range g.day(5, 0)[800:820] {
		late = append(late, m...)
	}
	for _, st := range stores {
		if _, _, _, err := st.Insert(ctx, rgUser, "c1", late, end); err != nil {
			t.Fatal(err)
		}
	}
	rgCompare(ctx, t, "late in the head", sqlS, recS, g, end, rgFirst, collectors)
	for _, st := range stores {
		if err := st.Seal(ctx, end+3*hourMs); err != nil {
			t.Fatal(err)
		}
	}
	rgCompare(ctx, t, "late sealed", sqlS, recS, g, end, rgFirst, collectors)
	// Another account's alerts, then retention in the middle of a day with
	// that account gone.
	other := g.day(1, 1)[100:110]
	for _, st := range stores {
		for _, m := range other {
			if _, _, _, err := st.Insert(ctx, "someone-else", "c9", m, end); err != nil {
				t.Fatal(err)
			}
		}
	}
	cut := rgFirst + rgDayMs + 7*hourMs
	var results []PruneResult
	for _, st := range stores {
		res, err := st.Prune(ctx, Retention{RawMs: end - cut}, end, []string{rgUser})
		if err != nil {
			t.Fatal(err)
		}
		results = append(results, res)
	}
	// rblk keeps no rollup rows; the alert counts must agree.
	if results[0].Alerts != results[1].Alerts || results[0].AlertRows != results[1].AlertRows || results[1].RollupRows != 0 {
		t.Fatalf("prune: rblk %+v, sql %+v", results[1], results[0])
	}
	rgCompare(ctx, t, "pruned", sqlS, recS, g, end, cut, collectors)
	// Past retention, rblk shows only what it kept, even on long ranges where
	// crowdsec.db's rollup still counts the pruned alerts.
	kept, err := sqlS.Summary(ctx, rgUser, rgFilter(cut, end, nil, nil, ""), end)
	if err != nil {
		t.Fatal(err)
	}
	long, err := recS.Summary(ctx, rgUser, rgFilter(end-rollupAfterMs-rgDayMs, end, nil, nil, ""), end)
	if err != nil || long.Source != "raw" || long.Alerts != kept.Alerts || long.Events != kept.Events {
		t.Fatalf("long range after retention: %+v, kept %+v (%v)", long, kept, err)
	}
	for i, st := range stores {
		page, err := st.Alerts(ctx, "someone-else", rgFilter(rgFirst, end, nil, nil, ""), "time", true, 1, 10)
		if err != nil || page.Total != 0 {
			t.Fatalf("store %d: removed account still has %d alerts (%v)", i, page.Total, err)
		}
	}
}

// TestRblkIndependent opens an existing crowdsec.db with the record store:
// nothing from before the switch shows, even on ranges long enough for the
// rollup, rblk writes no rollup rows, and resends are duplicates.
func TestRblkIndependent(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	path := filepath.Join(dir, "crowdsec.db")
	st, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	g := newRGGen(400)
	for d := range int64(2) {
		if _, err := rgFeed(ctx, rgDay(g, 1, d), rgFirst+d*rgDayMs, []*Store{st}, false); err != nil {
			t.Fatal(err)
		}
	}
	st.Close()
	end := rgFirst + 2*rgDayMs
	long := rgFilter(end-rollupAfterMs-rgDayMs, end, nil, nil, "")
	sw, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer sw.Close()
	if err := sw.UseRecords(filepath.Join(dir, "crowdsec-rblk")); err != nil {
		t.Fatal(err)
	}
	var rollupRows int64
	if err := sw.read.QueryRow(`SELECT COUNT(*) FROM rollup_hourly`).Scan(&rollupRows); err != nil || rollupRows == 0 {
		t.Fatalf("crowdsec.db should hold the old rollup: %d rows (%v)", rollupRows, err)
	}
	sum, err := sw.Summary(ctx, rgUser, long, end)
	if err != nil {
		t.Fatal(err)
	}
	if sum.Source != "raw" || sum.Alerts != 0 || sum.RollupSince != 0 || sum.DetailExpiredBefore != 0 {
		t.Fatalf("long range after switching: %+v", sum)
	}
	page, err := sw.Alerts(ctx, rgUser, rgFilter(end-rgDayMs, end, nil, nil, ""), "time", true, 1, 25)
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 0 || page.DetailExpiredBefore != 0 {
		t.Fatalf("alert list after switching: %d rows, detail expired before %d", page.Total, page.DetailExpiredBefore)
	}
	// New alerts land in rblk only and show on the long range.
	batch := g.day(0, 3)[0]
	stored, dup, _, err := sw.Insert(ctx, rgUser, "c0", batch, end)
	if err != nil || stored != len(batch) || dup != 0 {
		t.Fatalf("insert: stored %d dup %d of %d (%v)", stored, dup, len(batch), err)
	}
	var after int64
	if err := sw.read.QueryRow(`SELECT COUNT(*) FROM rollup_hourly`).Scan(&after); err != nil || after != rollupRows {
		t.Fatalf("rblk wrote rollup rows: %d before, %d after (%v)", rollupRows, after, err)
	}
	stored, dup, _, err = sw.Insert(ctx, rgUser, "c0", batch, end)
	if err != nil || stored != 0 || dup != len(batch) {
		t.Fatalf("resend: stored %d dup %d of %d (%v)", stored, dup, len(batch), err)
	}
	wide := rgFilter(rgFirst-rollupAfterMs, rgFirst+5*rgDayMs, nil, nil, "")
	if sum, err = sw.Summary(ctx, rgUser, wide, end); err != nil || sum.Source != "raw" || sum.Alerts != int64(len(batch)) {
		t.Fatalf("long range after inserting %d: %+v (%v)", len(batch), sum, err)
	}
}
