package tblk

import (
	"context"
	"fmt"
	"math/rand"
	"net/netip"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
)

func boundSchema(top int) Schema {
	return Schema{Families: []Family{{
		Name:     "names",
		Keys:     []Key{{Name: "zone"}, {Name: "name", Fold: true}},
		Measures: []string{"count", "err"},
		Cap:      map[Grain]int{Hour: top, Day: top},
		Bound:    "err",
	}}}
}

// TestBoundFoldTop: the cut keeps rows strictly above the first row past the
// cap, drops ties at the cut together, keeps sums, and adds the cut to the
// bound row on top of the errors carried in.
func TestBoundFoldTop(t *testing.T) {
	si, err := compile(boundSchema(2))
	if err != nil {
		t.Fatal(err)
	}
	f := si.byName["names"]
	row := func(zone, name string, n, e int64) rec {
		return rec{collector: "c1", keys: []string{zone, name}, vals: []int64{n, e}}
	}
	O := Other
	got := recSet(foldTop(f, []rec{
		row("z", "a", 10, 0), row("z", "b", 8, 0), row("z", "c", 5, 0), row("y", "d", 5, 0), row("z", "e", 3, 0),
		row("z", O, 4, 0), // folded earlier
		row(O, O, 0, 7),   // errors of earlier cuts
	}, 2))
	want := map[string][]int64{
		"c1|z|a": {10, 0}, "c1|z|b": {8, 0},
		"c1|z|" + O: {12, 0}, "c1|y|" + O: {5, 0},
		"c1|" + O + "|" + O: {0, 12},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("cut:\n got %v\nwant %v", got, want)
	}
	// Ties at the cut drop together, even inside the cap.
	got = recSet(foldTop(f, []rec{row("z", "a", 10, 0), row("z", "b", 5, 0), row("z", "c", 5, 0)}, 2))
	want = map[string][]int64{"c1|z|a": {10, 0}, "c1|z|" + O: {10, 0}, "c1|" + O + "|" + O: {0, 5}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ties:\n got %v\nwant %v", got, want)
	}
	// Nothing cut: no bound row appears, and a carried one passes through.
	got = recSet(foldTop(f, []rec{row("z", "a", 10, 0), row("z", "b", 5, 0)}, 2))
	if want = map[string][]int64{"c1|z|a": {10, 0}, "c1|z|b": {5, 0}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("under the cap:\n got %v\nwant %v", got, want)
	}
	got = recSet(foldTop(f, []rec{row("z", "a", 10, 0), row(O, O, 0, 3)}, 2))
	if want = map[string][]int64{"c1|z|a": {10, 0}, "c1|" + O + "|" + O: {0, 3}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("carried bound:\n got %v\nwant %v", got, want)
	}
}

func TestBoundSchemaChecks(t *testing.T) {
	bad := map[string]func(*Schema){
		"unknown measure": func(s *Schema) { s.Families[0].Bound = "nope" },
		"rank measure":    func(s *Schema) { s.Families[0].Bound = "count" },
		"two levels": func(s *Schema) {
			s.Families[0].Keys = []Key{{Name: "zone", Fold: true, Level: 2}, {Name: "name", Fold: true}}
		},
		"int key": func(s *Schema) { s.Families[0].Keys = append(s.Families[0].Keys, Key{Name: "n", Kind: KeyInt}) },
	}
	for name, mod := range bad {
		s := boundSchema(2)
		mod(&s)
		if _, err := compile(s); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
	if _, err := compile(boundSchema(2)); err != nil {
		t.Fatal(err)
	}
}

// cutMinute applies the bound rule to one minute the way a collector would
// before sending: rows strictly above the first row past top stay, the rest
// fold into one Other row per zone, and the cut rides on the bound row.
func cutMinute(counts map[[2]string]int64, top int) []Row {
	type kv struct {
		k [2]string
		n int64
	}
	var list []kv
	for k, n := range counts {
		list = append(list, kv{k, n})
	}
	sort.Slice(list, func(i, j int) bool { return list[i].n > list[j].n })
	var cut int64
	if len(list) > top {
		cut = list[top].n
	}
	other := map[string]int64{}
	var rows []Row
	for _, x := range list {
		if x.n > cut {
			rows = append(rows, Row{Keys: []string{x.k[0], x.k[1]}, Values: []int64{x.n, 0}})
			continue
		}
		other[x.k[0]] += x.n
	}
	for z, n := range other {
		rows = append(rows, Row{Keys: []string{z, Other}, Values: []int64{n, 0}})
	}
	rows = append(rows, Row{Keys: []string{Other, Other}, Values: []int64{0, cut}})
	return rows
}

// TestBoundGuarantee feeds skewed names through a collector-side cut, then
// hours and days, on both engines, and checks that every listed name's true
// sum lies between its stored sum and that plus the window's error, that the
// error stays within stages·N/(k+1), and that the engines agree.
func TestBoundGuarantee(t *testing.T) {
	ctx := context.Background()
	const top = 20
	a, b := openBoth(t, boundSchema(top))
	both := []*Store{a, b}
	r := rand.New(rand.NewSource(11))
	zipf := rand.NewZipf(r, 1.1, 2, 400)
	type obs struct {
		bucket int64
		col    string
		key    [2]string
		n      int64
	}
	var truth []obs
	first := testEnd - 9*dayMs
	cols := []string{"c1", "c2"}
	for m := first; m < testEnd; m += 10 * minuteMs {
		for _, c := range cols {
			counts := map[[2]string]int64{}
			for range 120 {
				i := zipf.Uint64()
				// A name that bursts for one hour a day, to move the cut around.
				if (m/hourMs)%24 == 5 && r.Intn(3) == 0 {
					i = 1000 + uint64(r.Intn(40))
				}
				k := [2]string{fmt.Sprintf("z%d", i%3), fmt.Sprintf("n%d", i)}
				counts[k]++
			}
			for k, n := range counts {
				truth = append(truth, obs{m, c, k, n})
			}
			for _, s := range both {
				if err := s.Append(ctx, "alice", c, "names", m, cutMinute(counts, top)); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
	for _, s := range both {
		maintainAll(t, s, testEnd+3*hourMs)
	}
	windows := []struct {
		name              string
		start, minuteFrom int64
		stages            int64
	}{
		{"minutes", testEnd - 2*hourMs, testEnd - 2*hourMs, 1},
		{"hours", testEnd - 3*dayMs, testEnd, 2},
		{"days", first, testEnd, 3},
	}
	for _, w := range windows {
		for _, col := range []string{"", "c2"} {
			label := fmt.Sprintf("%s collector %q", w.name, col)
			sums := map[string]int64{}
			var n int64
			for _, o := range truth {
				if o.bucket < w.start || o.bucket >= testEnd || (col != "" && o.col != col) {
					continue
				}
				sums[o.key[1]] += o.n
				n += o.n
			}
			var results [2]Result
			var errs [2]int64
			for i, s := range both {
				res, err := s.Query(ctx, Query{Family: "names", Owner: "alice", Collector: col, Start: w.start, End: testEnd, By: "name", MinuteFrom: w.minuteFrom})
				if err != nil {
					t.Fatal(err)
				}
				e, err := s.Query(ctx, Query{Family: "names", Owner: "alice", Collector: col, Start: w.start, End: testEnd, Measures: []string{"err"}, MinuteFrom: w.minuteFrom})
				if err != nil {
					t.Fatal(err)
				}
				results[i], errs[i] = res, e.Total[0]
			}
			if !reflect.DeepEqual(results[0].Rows, results[1].Rows) || errs[0] != errs[1] {
				t.Fatalf("%s: engines differ: errors %d / %d", label, errs[0], errs[1])
			}
			res, e := results[0], errs[0]
			if res.Total[0] != n {
				t.Fatalf("%s: stored total %d, sent %d", label, res.Total[0], n)
			}
			if limit := w.stages * n / (top + 1); e > limit {
				t.Fatalf("%s: error %d over the limit %d", label, e, limit)
			}
			for _, row := range res.Rows {
				if got, want := row.Values[0], sums[row.Key]; want < got || want > got+e {
					t.Fatalf("%s: %s stored %d, true %d, error %d", label, row.Key, got, want, e)
				}
			}
			listed := map[string]bool{}
			var smallest int64 = -1
			for _, row := range res.Rows {
				listed[row.Key] = true
				if smallest < 0 || row.Values[0] < smallest {
					smallest = row.Values[0]
				}
			}
			for name, want := range sums {
				if !listed[name] && want > e+max(smallest, 0) {
					t.Fatalf("%s: %s (true %d) missing although above error %d plus the smallest listed %d", label, name, want, e, smallest)
				}
			}
			t.Logf("%s: %d listed, error %d of %d (%.2f%%)", label, len(res.Rows), e, n, 100*float64(e)/float64(n))
		}
	}
}

// TestQuietHourRolls: an hour whose minutes fit one slice has nothing to
// merge; once its hour block exists, reads use it and later days still fold.
func TestQuietHourRolls(t *testing.T) {
	ctx := context.Background()
	s, err := Open(filepath.Join(t.TempDir(), "blocks"), boundSchema(2), Options{Engine: Blocks})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	h := testEnd - 3*dayMs
	h -= h % dayMs
	row := func(name string, n int64) Row { return Row{Keys: []string{"z", name}, Values: []int64{n, 0}} }
	if err := s.Append(ctx, "u", "c", "names", h, []Row{row("a", 10), row("b", 8), row("c", 3)}); err != nil {
		t.Fatal(err)
	}
	maintainAll(t, s, testEnd)
	hour, day, err := s.eng.rolled(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if hour <= h+hourMs || day < h+dayMs {
		t.Fatalf("rolled: hour %d, day %d, quiet hour at %d", hour, day, h)
	}
	res, err := s.Query(ctx, Query{Family: "names", Owner: "u", Start: h, End: h + hourMs, MinuteFrom: testEnd, By: "name"})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Rows) != 2 || res.Rows[0].Key != "a" || res.Rows[1].Key != "b" {
		t.Fatalf("hour read: %+v", res.Rows)
	}
}

// TestBoundNetKeys: a bounded family keyed by network alone. Folded
// networks and the error share the Other row; a prefix filter never matches
// it; both engines give the same answers before and after the hour cut.
func TestBoundNetKeys(t *testing.T) {
	ctx := context.Background()
	s := Schema{Families: []Family{{
		Name:     "nets",
		Keys:     []Key{{Name: "net", Kind: KeyNet, Fold: true}},
		Measures: []string{"count", "err"},
		Cap:      map[Grain]int{Hour: 2, Day: 2},
		Bound:    "err",
	}}}
	a, b := openBoth(t, s)
	h := testEnd - 3*dayMs
	h -= h % dayMs
	row := func(net string, n, e int64) Row { return Row{Keys: []string{net}, Values: []int64{n, e}} }
	appendBoth(t, []*Store{a, b}, "u", "c1", h, map[string][]Row{"nets": {
		row("10.0.0.0/24", 10, 0), row("10.0.1.0/24", 8, 0), row("192.0.2.0/24", 6, 0), row("2001:db8::/48", 6, 0),
		row(Other, 4, 3), // the collector folded 4 queries and cut at 3
	}})
	appendBoth(t, []*Store{a, b}, "u", "c1", h+minuteMs, map[string][]Row{"nets": {row("192.0.2.0/24", 7, 0)}})
	ten := netip.MustParsePrefix("10.0.0.0/8")
	type answer struct {
		rows        []ResultRow
		other, errs int64
		inTen       []ResultRow
	}
	read := func(st *Store, minuteFrom int64) answer {
		q := Query{Family: "nets", Owner: "u", Start: h, End: h + hourMs, MinuteFrom: minuteFrom, By: "net"}
		r, err := st.Query(ctx, q)
		if err != nil {
			t.Fatal(err)
		}
		eq := q
		eq.By, eq.Measures = "", []string{"err"}
		e, err := st.Query(ctx, eq)
		if err != nil {
			t.Fatal(err)
		}
		fq := q
		fq.Filters = []Filter{{Key: "net", Prefix: ten}}
		f, err := st.Query(ctx, fq)
		if err != nil {
			t.Fatal(err)
		}
		return answer{r.Rows, r.Other[0], e.Total[0], f.Rows}
	}
	keys := func(rows []ResultRow) string {
		var out []string
		for _, r := range rows {
			out = append(out, fmt.Sprintf("%s=%d", r.Key, r.Values[0]))
		}
		return fmt.Sprint(out)
	}
	for _, st := range []*Store{a, b} {
		m := read(st, h)
		if got := keys(m.rows); got != "[192.0.2.0/24=13 10.0.0.0/24=10 10.0.1.0/24=8 2001:db8::/48=6]" || m.other != 4 || m.errs != 3 {
			t.Fatalf("minutes: %s other %d err %d", got, m.other, m.errs)
		}
		if got := keys(m.inTen); got != "[10.0.0.0/24=10 10.0.1.0/24=8]" {
			t.Fatalf("minutes in 10/8: %s", got)
		}
		maintainAll(t, st, testEnd)
		// The hour: 13, 10, 8, 6 with a cap of 2 cuts at 8, so 8 and 6 fold.
		hr := read(st, testEnd)
		if got := keys(hr.rows); got != "[192.0.2.0/24=13 10.0.0.0/24=10]" || hr.other != 18 || hr.errs != 11 {
			t.Fatalf("hour: %s other %d err %d", got, hr.other, hr.errs)
		}
		if got := keys(hr.inTen); got != "[10.0.0.0/24=10]" {
			t.Fatalf("hour in 10/8: %s", got)
		}
	}
}
