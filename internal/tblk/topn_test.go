package tblk

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"net/netip"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

var testEnd = time.Date(2026, 11, 18, 0, 0, 0, 0, time.UTC).UnixMilli()

// dnsSchema mirrors Knot's families with small caps, so the folds have work.
func dnsSchema() Schema {
	return Schema{Families: []Family{
		{Name: "totals", Keys: []Key{{Name: "zone"}, {Name: "qtype"}, {Name: "rcode"}}, Measures: []string{"count"}},
		{Name: "names", Keys: []Key{{Name: "zone"}, {Name: "qname", Fold: true}, {Name: "qtype"}, {Name: "rcode"}}, Measures: []string{"count"},
			Cap: map[Grain]int{Hour: 25, Day: 25}},
		{Name: "clients", Keys: []Key{{Name: "net", Kind: KeyNet}, {Name: "zone", Fold: true}, {Name: "qname", Fold: true}, {Name: "qtype", Fold: true}, {Name: "rcode", Fold: true}},
			Measures: []string{"count"}, Cap: map[Grain]int{Hour: 20, Day: 20}},
		{Name: "sizes", Keys: []Key{{Name: "up_to", Kind: KeyInt}}, Measures: []string{"count"}},
	}}
}

type dnsGen struct {
	r     *rand.Rand
	zones []string
	names []string
	nets  []string
}

func newDNSGen(seed int64) *dnsGen {
	g := &dnsGen{r: rand.New(rand.NewSource(seed))}
	for i := 0; i < 4; i++ {
		g.zones = append(g.zones, fmt.Sprintf("z%d.example.", i))
	}
	for i := 0; i < 300; i++ {
		g.names = append(g.names, fmt.Sprintf("n%d.%s", i, g.zones[i%4]))
	}
	for i := 0; i < 40; i++ {
		if i%5 == 0 {
			g.nets = append(g.nets, fmt.Sprintf("2001:db8:%x::/48", i))
		} else {
			g.nets = append(g.nets, fmt.Sprintf("10.%d.%d.0/24", i%3, i))
		}
	}
	return g
}

// minute returns one minute's rows per family, with the most popular names and
// networks picked more often so caps cut a real tail.
func (g *dnsGen) minute() map[string][]Row {
	pick := func(n int) int { return int(float64(n) * g.r.Float64() * g.r.Float64()) }
	qtypes, rcodes := []string{"A", "AAAA", "MX", "TXT"}, []string{"NOERROR", "NXDOMAIN"}
	totals, names, clients, sizes := map[string]int64{}, map[string]int64{}, map[string]int64{}, map[string]int64{}
	for i := 0; i < 400; i++ {
		name := g.names[pick(len(g.names))]
		zone := name[strings.Index(name, ".")+1:]
		qt, rc := qtypes[pick(len(qtypes))], rcodes[pick(len(rcodes))]
		net := g.nets[pick(len(g.nets))]
		totals[zone+"|"+qt+"|"+rc]++
		names[zone+"|"+name+"|"+qt+"|"+rc]++
		clients[net+"|"+zone+"|"+name+"|"+qt+"|"+rc]++
		sizes[strconv.Itoa([]int{64, 128, 512, 0}[pick(4)])]++
	}
	out := map[string][]Row{}
	add := func(fam string, m map[string]int64, top int) {
		var rows []Row
		other := map[string]int64{}
		for k, v := range m {
			parts := strings.Split(k, "|")
			if top > 0 && v < 2 && fam == "names" {
				parts[1] = Other
				other[strings.Join(parts, "|")] += v
				continue
			}
			if top > 0 && v < 2 && fam == "clients" {
				other[parts[0]+"|"+Other+"|"+Other+"|"+Other+"|"+Other] += v
				continue
			}
			rows = append(rows, Row{Keys: parts, Values: []int64{v}})
		}
		for k, v := range other {
			rows = append(rows, Row{Keys: strings.Split(k, "|"), Values: []int64{v}})
		}
		out[fam] = rows
	}
	add("totals", totals, 0)
	add("names", names, 1)
	add("clients", clients, 1)
	add("sizes", sizes, 0)
	return out
}

func openBoth(t *testing.T, s Schema) (*Store, *Store) {
	t.Helper()
	a, err := Open(filepath.Join(t.TempDir(), "sql"), s, Options{Engine: SQL})
	if err != nil {
		t.Fatal(err)
	}
	b, err := Open(filepath.Join(t.TempDir(), "blocks"), s, Options{Engine: Blocks})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.Close(); b.Close() })
	return a, b
}

func appendBoth(t *testing.T, stores []*Store, owner, col string, bucket int64, rows map[string][]Row) {
	t.Helper()
	for _, s := range stores {
		for fam, rs := range rows {
			if err := s.Append(context.Background(), owner, col, fam, bucket, rs); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func maintainAll(t *testing.T, s *Store, now int64) {
	t.Helper()
	for {
		r, err := s.Maintain(context.Background(), now)
		if err != nil {
			t.Fatal(err)
		}
		if r.Hours == 0 && r.Days == 0 {
			return
		}
	}
}

// dnsQueries are the reads compared between engines.
func dnsQueries(owner string) []Query {
	var out []Query
	windows := []struct {
		span, step int64
	}{{hourMs, 5 * minuteMs}, {6 * hourMs, 15 * minuteMs}, {dayMs, 30 * minuteMs}, {2 * dayMs, hourMs}, {8 * dayMs, hourMs}}
	filters := [][]Filter{
		nil,
		{{Key: "zone", Value: "z1.example."}},
		{{Key: "qname", Value: "n1.z1.example."}},
		{{Key: "net", Prefix: netip.MustParsePrefix("10.1.0.0/16")}},
		{{Key: "net", Prefix: netip.MustParsePrefix("2001:db8::/32")}},
		{{Key: "net", Value: "10.2.2.0/24"}},
		{{Key: "up_to", Value: "128"}},
		{{Key: "zone", Value: "nowhere."}},
		{{Key: "rcode", Value: "NXDOMAIN"}, {Key: "zone", Value: "z0.example."}},
		// Value lists, exclusion and the collector, as CrowdSec filters.
		{{Key: "zone", Values: []string{"z1.example.", "z2.example.", "nowhere."}}},
		{{Key: "rcode", Value: "NOERROR", Not: true}},
		{{Key: "collector", Value: "c1"}},
		{{Key: "collector", Value: "c1", Not: true}, {Key: "qtype", Values: []string{"A", "AAAA"}}},
		{{Key: "zone", Values: []string{"nowhere."}, Not: true}},
		{{Key: "up_to", Values: []string{"128", "512"}, Not: true}},
	}
	byKey := map[string][]string{
		"totals": {"", "zone", "qtype", "rcode", "collector"}, "names": {"", "qname", "zone", "rcode", "collector"},
		"clients": {"", "net", "zone", "qname", "collector"}, "sizes": {"", "up_to", "collector"},
	}
	for _, w := range windows {
		for _, fam := range []string{"totals", "names", "clients", "sizes"} {
			for fi, fl := range filters {
				for _, by := range byKey[fam] {
					q := Query{Family: fam, Owner: owner, Filters: fl, Start: testEnd - w.span, End: testEnd, Step: w.step,
						By: by, Limit: 10, MinuteFrom: testEnd - 7*dayMs, CheckFolded: true, KeepOther: fi%2 == 1}
					out = append(out, q)
					if fi == 0 && by == "" {
						q.Collector = "c2"
						out = append(out, q)
					}
				}
			}
		}
	}
	return out
}

// compareEngines runs every query on both stores and fails on the first
// (in list order) whose answers differ. Reads run on a few workers, since
// both engines serve concurrent queries; the full DNS set is thousands of
// reads per checkpoint.
func compareEngines(t *testing.T, label string, a, b *Store, qs []Query) (withRows, folded int) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	fails := make([]string, len(qs))
	rows := make([]bool, len(qs))
	fold := make([]bool, len(qs))
	next := make(chan int)
	var wg sync.WaitGroup
	for w := 0; w < min(4, runtime.GOMAXPROCS(0)); w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range next {
				q := qs[i]
				ra, err := a.Query(ctx, q)
				if err != nil {
					fails[i] = fmt.Sprintf("%s sql %+v: %v", label, q, err)
					cancel()
					continue
				}
				rb, err := b.Query(ctx, q)
				if err != nil {
					fails[i] = fmt.Sprintf("%s blocks %+v: %v", label, q, err)
					cancel()
					continue
				}
				if !reflect.DeepEqual(ra, rb) {
					fails[i] = fmt.Sprintf("%s: %s by %q filters %v window %dh differ:\nsql    total %v other %v rows %v folded %v\nblocks total %v other %v rows %v folded %v",
						label, q.Family, q.By, q.Filters, (q.End-q.Start)/hourMs, ra.Total, ra.Other, ra.Rows, ra.Folded, rb.Total, rb.Other, rb.Rows, rb.Folded)
					cancel()
					continue
				}
				rows[i], fold[i] = len(ra.Rows) > 0, ra.Folded
			}
		}()
	}
feed:
	for i := range qs {
		select {
		case next <- i:
		case <-ctx.Done():
			break feed
		}
	}
	close(next)
	wg.Wait()
	for i := range qs {
		// A query cut short by another's failure reports context.Canceled;
		// the first real failure in list order is the one to show.
		if fails[i] != "" && !strings.Contains(fails[i], context.Canceled.Error()) {
			t.Fatal(fails[i])
		}
	}
	for i := range qs {
		if fails[i] != "" {
			t.Fatal(fails[i])
		}
		if rows[i] {
			withRows++
		}
		if fold[i] {
			folded++
		}
	}
	return withRows, folded
}

func TestEnginesAgree(t *testing.T) {
	ctx := context.Background()
	a, b := openBoth(t, dnsSchema())
	both := []*Store{a, b}
	g := newDNSGen(7)
	first := testEnd - 50*hourMs
	for m := first; m < testEnd; m += minuteMs {
		for _, col := range []string{"c1", "c2"} {
			appendBoth(t, both, "alice", col, m, g.minute())
		}
		if m%(10*minuteMs) == 0 {
			appendBoth(t, both, "bob", "c9", m, g.minute())
		}
	}
	// A resent minute replaces its rows; nothing doubles.
	resend := g.minute()
	appendBoth(t, both, "alice", "c1", testEnd-30*minuteMs, resend)
	appendBoth(t, both, "alice", "c1", testEnd-30*minuteMs, resend)

	qs := dnsQueries("alice")
	check := func(label string) {
		rows, folded := compareEngines(t, label, a, b, qs)
		t.Logf("%s: %d reads agree, %d with rows, %d with folded rows", label, len(qs), rows, folded)
		if rows == 0 || folded == 0 {
			t.Fatalf("%s compared no data", label)
		}
	}
	check("head only")

	// Mid-day: some hours folded, the latest hour still in 15-minute slices,
	// today's day not folded yet.
	mid := testEnd - 9*hourMs + 7*minuteMs
	maintainAll(t, a, mid)
	maintainAll(t, b, mid)
	check("mid-day, slices and partial day")

	maintainAll(t, a, testEnd)
	maintainAll(t, b, testEnd)
	for _, s := range both {
		h, d, err := s.eng.rolled(ctx)
		if err != nil || h != testEnd-2*hourMs || d != testEnd-dayMs {
			t.Fatalf("rolled hour %d day %d (%v), want %d %d", h, d, err, testEnd-2*hourMs, testEnd-dayMs)
		}
	}
	check("after sealing and rollup")

	// The other owner's rows never show in alice's reads, and back.
	for _, s := range both {
		r, err := s.Query(ctx, Query{Family: "totals", Owner: "bob", Start: first, End: testEnd})
		if err != nil || r.Total[0] == 0 {
			t.Fatalf("bob's total %v %v", r.Total, err)
		}
	}

	keep := map[Grain]time.Duration{Minute: 24 * time.Hour, Hour: 48 * time.Hour}
	pa, err := a.Prune(ctx, testEnd, keep)
	if err != nil {
		t.Fatal(err)
	}
	pb, err := b.Prune(ctx, testEnd, keep)
	if err != nil {
		t.Fatal(err)
	}
	if pa.Rows[Minute] == 0 || pa.Rows[Minute] != pb.Rows[Minute] || pa.Rows[Hour] != pb.Rows[Hour] {
		t.Fatalf("pruned rows differ: sql %v, blocks %v (files %v)", pa.Rows, pb.Rows, pb.Files)
	}
	check("after retention")

	for _, s := range both {
		if err := s.DropOwner(ctx, "bob"); err != nil {
			t.Fatal(err)
		}
		r, err := s.Query(ctx, Query{Family: "totals", Owner: "bob", Start: first - 8*dayMs, End: testEnd + dayMs, MinuteFrom: testEnd})
		if err != nil || r.Total[0] != 0 {
			t.Fatalf("bob after drop: %v %v", r.Total, err)
		}
	}
	check("after dropping another owner")
}

func TestSizeCapDropsOldestDetailFirst(t *testing.T) {
	ctx := context.Background()
	s := dnsSchema()
	s.MaxBytes = 1
	a, b := openBoth(t, s)
	g := newDNSGen(3)
	for m := testEnd - 30*hourMs; m < testEnd; m += minuteMs {
		appendBoth(t, []*Store{a, b}, "alice", "c1", m, g.minute())
	}
	for _, st := range []*Store{a, b} {
		maintainAll(t, st, testEnd)
		// Everything rolled may go; the newest, unrolled minutes stay.
		r, err := st.Query(ctx, Query{Family: "totals", Owner: "alice", Start: testEnd - hourMs, End: testEnd, MinuteFrom: testEnd - 7*dayMs})
		if err != nil || r.Total[0] == 0 {
			t.Fatalf("unrolled minutes gone: %v %v", r.Total, err)
		}
		old, err := st.Query(ctx, Query{Family: "totals", Owner: "alice", Start: testEnd - 30*hourMs, End: testEnd - 10*hourMs, MinuteFrom: testEnd - 7*dayMs})
		if err != nil || old.Total[0] != 0 {
			t.Fatalf("rolled minutes kept over budget: %v %v", old.Total, err)
		}
	}
}

func TestNginxFamily(t *testing.T) {
	ctx := context.Background()
	s := Schema{Families: []Family{{
		Name:     "requests",
		Keys:     []Key{{Name: "host"}, {Name: "path", Fold: true}, {Name: "status", Kind: KeyInt}, {Name: "method"}, {Name: "client", Kind: KeyNet}},
		Measures: []string{"count", "bytes", "ms_sum", "le_50ms", "le_250ms", "le_inf"},
		Cap:      map[Grain]int{Hour: 5, Day: 5},
	}}}
	a, b := openBoth(t, s)
	r := rand.New(rand.NewSource(1))
	hosts := []string{"a.example", "b.example"}
	statuses := []string{"200", "404", "500"}
	type acc struct{ count, bytes, le50, le250, inf int64 }
	want := map[string]*acc{} // path -> sums for host a.example, status 500
	var total acc
	first := testEnd - 4*hourMs
	for m := first; m < testEnd; m += minuteMs {
		var rows []Row
		seen := map[string]int{}
		for i := 0; i < 40; i++ {
			host, st := hosts[r.Intn(2)], statuses[r.Intn(3)]
			path := fmt.Sprintf("/p/%d", r.Intn(12))
			client := fmt.Sprintf("192.0.2.%d/32", r.Intn(4))
			ms := int64(r.Intn(400))
			bytes := int64(100 + r.Intn(5000))
			le50, le250, inf := b2i(ms <= 50), b2i(ms <= 250), int64(1)
			key := host + path + st + client
			if j, ok := seen[key]; ok {
				v := rows[j].Values
				v[0]++
				v[1] += bytes
				v[2] += ms
				v[3] += le50
				v[4] += le250
				v[5]++
			} else {
				seen[key] = len(rows)
				rows = append(rows, Row{Keys: []string{host, path, st, "GET", client}, Values: []int64{1, bytes, ms, le50, le250, inf}})
			}
			total.count++
			total.le50 += le50
			total.le250 += le250
			if host == "a.example" && st == "500" {
				w := want[path]
				if w == nil {
					w = &acc{}
					want[path] = w
				}
				w.count++
				w.bytes += bytes
			}
		}
		for _, st := range []*Store{a, b} {
			if err := st.Append(ctx, "o", "web1", "requests", m, rows); err != nil {
				t.Fatal(err)
			}
		}
	}
	topBytes := Query{Family: "requests", Owner: "o", Start: first, End: testEnd, MinuteFrom: testEnd - 7*dayMs,
		Filters: []Filter{{Key: "host", Value: "a.example"}, {Key: "status", Value: "500"}}, By: "path", Measures: []string{"bytes", "count"}}
	hist := Query{Family: "requests", Owner: "o", Start: first, End: testEnd, MinuteFrom: testEnd - 7*dayMs,
		Measures: []string{"count", "le_50ms", "le_250ms"}, By: "status"}
	for _, st := range []*Store{a, b} {
		res, err := st.Query(ctx, topBytes)
		if err != nil {
			t.Fatal(err)
		}
		if len(res.Rows) != len(want) {
			t.Fatalf("top paths: %d rows, want %d", len(res.Rows), len(want))
		}
		for i, row := range res.Rows {
			w := want[row.Key]
			if w == nil || row.Values[0] != w.bytes || row.Values[1] != w.count {
				t.Fatalf("path %s: %v, want %+v", row.Key, row.Values, w)
			}
			if i > 0 && row.Values[0] > res.Rows[i-1].Values[0] {
				t.Fatal("rows not ranked by the first measure")
			}
		}
	}
	// After rollup the hour tier caps paths at 5 per hour, but every sum,
	// histogram buckets included, still adds up, and folded paths keep their
	// host, status and method.
	maintainAll(t, a, testEnd)
	maintainAll(t, b, testEnd)
	hist.MinuteFrom = testEnd // force the hour tier where rolled
	for _, st := range []*Store{a, b} {
		res, err := st.Query(ctx, hist)
		if err != nil {
			t.Fatal(err)
		}
		if res.Total[0] != total.count || res.Total[1] != total.le50 || res.Total[2] != total.le250 {
			t.Fatalf("histogram totals %v, want %d %d %d", res.Total, total.count, total.le50, total.le250)
		}
		byPath := topBytes
		byPath.MinuteFrom, byPath.KeepOther, byPath.CheckFolded = testEnd, true, true
		pr, err := st.Query(ctx, byPath)
		if err != nil {
			t.Fatal(err)
		}
		var sum int64
		hasOther := false
		for _, row := range pr.Rows {
			sum += row.Values[1]
			hasOther = hasOther || row.Key == Other
		}
		var wantCount int64
		for _, w := range want {
			wantCount += w.count
		}
		if sum != wantCount || !hasOther || !pr.Folded {
			t.Fatalf("hour tier paths for host and status: sum %d want %d, other row %v, folded %v", sum, wantCount, hasOther, pr.Folded)
		}
	}
	compareEngines(t, "nginx", a, b, []Query{topBytes, hist})
}

func b2i(v bool) int64 {
	if v {
		return 1
	}
	return 0
}

func TestBlockCrashSafety(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	s, err := Open(dir, dnsSchema(), Options{Engine: Blocks})
	if err != nil {
		t.Fatal(err)
	}
	g := newDNSGen(5)
	for m := testEnd - 6*hourMs; m < testEnd; m += minuteMs {
		for fam, rows := range g.minute() {
			if err := s.Append(ctx, "alice", "c1", fam, m, rows); err != nil {
				t.Fatal(err)
			}
		}
	}
	maintainAll(t, s, testEnd)
	s.Close()

	minutes := filepath.Join(dir, "blocks", "minute")
	ents, err := os.ReadDir(minutes)
	if err != nil || len(ents) < 2 {
		t.Fatalf("minute blocks: %d %v", len(ents), err)
	}
	victim := filepath.Join(minutes, ents[0].Name())
	raw, err := os.ReadFile(victim)
	if err != nil {
		t.Fatal(err)
	}
	raw[len(raw)-trailerLen-3] ^= 0xff // inside the index section
	if err := os.WriteFile(victim, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	tmp := filepath.Join(minutes, "123-456.blk.tmp")
	if err := os.WriteFile(tmp, []byte("half a block"), 0o644); err != nil {
		t.Fatal(err)
	}

	s, err = Open(dir, dnsSchema(), Options{Engine: Blocks})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err := os.Stat(tmp); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("leftover .tmp still there: %v", err)
	}
	st, err := s.Stats(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(st.BadBlocks) != 1 || st.BadBlocks[0] != victim {
		t.Fatalf("bad blocks %v, want %s", st.BadBlocks, victim)
	}
	if _, err := s.Query(ctx, Query{Family: "names", Owner: "alice", Start: testEnd - 6*hourMs, End: testEnd, By: "qname", MinuteFrom: testEnd - dayMs}); err != nil {
		t.Fatalf("reads fail with a bad block skipped: %v", err)
	}
}

func TestQuerySlotsBusy(t *testing.T) {
	s, err := Open(t.TempDir(), dnsSchema(), Options{Engine: SQL, QuerySlots: 1, QueueWait: 50 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	release, err := s.acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	if _, err := s.Query(context.Background(), Query{Family: "totals", Owner: "x", Start: 0, End: hourMs}); !errors.Is(err, ErrBusy) {
		t.Fatalf("query with every slot taken: %v, want ErrBusy", err)
	}
}

func TestSchemaChecks(t *testing.T) {
	bad := []Schema{
		{},
		{Families: []Family{{Name: "Bad", Measures: []string{"n"}}}},
		{Families: []Family{{Name: "x"}}},
		{Families: []Family{{Name: "x", Keys: []Key{{Name: "n", Kind: KeyInt, Fold: true}}, Measures: []string{"n"}}}},
		{Families: []Family{{Name: "x", Keys: []Key{{Name: "a"}}, Measures: []string{"n"}, Cap: map[Grain]int{Hour: 3}}}},
	}
	for i, s := range bad {
		if _, err := compile(s); err == nil {
			t.Errorf("schema %d accepted", i)
		}
	}
	if _, err := compile(dnsSchema()); err != nil {
		t.Fatal(err)
	}
}

// recsFor builds block rows for one owner from generated minutes.
func recsFor(t *testing.T, si *schemaInfo, owner string, cols []string, start int64, minutes int, seed int64) blockRows {
	t.Helper()
	g := newDNSGen(seed)
	out := blockRows{}
	for i := 0; i < minutes; i++ {
		for _, col := range cols {
			for fam, rows := range g.minute() {
				gk := [2]string{owner, fam}
				out[gk] = append(out[gk], toRecs(si.byName[fam], col, start+int64(i)*minuteMs, rows)...)
			}
		}
	}
	return out
}

func copyRows(in blockRows) blockRows {
	out := blockRows{}
	for gk, list := range in {
		for _, r := range list {
			r.keys = append([]string(nil), r.keys...)
			r.vals = append([]int64(nil), r.vals...)
			out[gk] = append(out[gk], r)
		}
	}
	return out
}

func TestBlockWriterSameBytes(t *testing.T) {
	si, err := compile(dnsSchema())
	if err != nil {
		t.Fatal(err)
	}
	c, err := newCodec()
	if err != nil {
		t.Fatal(err)
	}
	defer c.close()
	dir := t.TempDir()
	h := blockHeader{grain: Minute, start: testEnd, end: testEnd + hourMs, covered: testEnd + hourMs}
	cols := []string{"c1", "c2", "c3"}
	rows := recsFor(t, si, "alice", cols, testEnd, 20, 3)
	for gk, list := range recsFor(t, si, "bob", cols[:1], testEnd, 5, 4) {
		rows[gk] = list
	}
	whole, err := writeBlock(c, si, filepath.Join(dir, "whole.blk"), h, copyRows(rows))
	if err != nil {
		t.Fatal(err)
	}

	// The same rows group by group in shuffled order, each group split into
	// one add per collector, plus a resent copy of every row.
	w := newBlockWriter(c, si, filepath.Join(dir, "parts.blk"), h)
	gkeys := make([][2]string, 0, len(rows))
	for gk := range rows {
		gkeys = append(gkeys, gk)
	}
	rand.New(rand.NewSource(1)).Shuffle(len(gkeys), func(i, j int) { gkeys[i], gkeys[j] = gkeys[j], gkeys[i] })
	parts := copyRows(rows)
	for _, gk := range gkeys {
		byCol := map[string][]rec{}
		for _, r := range parts[gk] {
			byCol[r.collector] = append(byCol[r.collector], r)
		}
		for _, col := range cols {
			if err := w.add(gk[0], gk[1], byCol[col]); err != nil {
				t.Fatal(err)
			}
		}
	}
	if _, err := w.finish(); err != nil {
		t.Fatal(err)
	}
	a, _ := os.ReadFile(whole.path)
	b, _ := os.ReadFile(filepath.Join(dir, "parts.blk"))
	if !reflect.DeepEqual(a, b) {
		t.Fatalf("block written group by group differs from writeBlock: %d vs %d bytes", len(b), len(a))
	}

	// A collector added again after a later one is refused.
	w = newBlockWriter(c, si, filepath.Join(dir, "order.blk"), h)
	r := func(col string) []rec {
		return []rec{{collector: col, bucket: testEnd, keys: []string{"z.", "A", "NOERROR"}, vals: []int64{1}}}
	}
	if err := w.add("alice", "totals", r("c2")); err != nil {
		t.Fatal(err)
	}
	if err := w.add("alice", "totals", r("c1")); err == nil {
		t.Fatal("adding c1 after c2 to one group should fail")
	}

	// Duplicate rows sum.
	w = newBlockWriter(c, si, filepath.Join(dir, "dup.blk"), h)
	if err := w.add("alice", "totals", append(r("c1"), r("c1")...)); err != nil {
		t.Fatal(err)
	}
	bf, err := w.finish()
	if err != nil {
		t.Fatal(err)
	}
	e := &blockEngine{c: c, sch: si}
	var got []rec
	if err := e.readAll(bf, func(_, _ string, x rec) { got = append(got, x) }); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].vals[0] != 2 {
		t.Fatalf("duplicate rows read back as %+v, want one row of 2", got)
	}
}

func TestCompactionCrash(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	s, err := Open(dir, dnsSchema(), Options{Engine: Blocks})
	if err != nil {
		t.Fatal(err)
	}
	g := newDNSGen(11)
	first := testEnd - 30*hourMs
	for m := first; m < testEnd; m += minuteMs {
		for fam, rows := range g.minute() {
			if err := s.Append(ctx, "alice", "c1", fam, m, rows); err != nil {
				t.Fatal(err)
			}
		}
	}
	maintainAll(t, s, testEnd)
	q := func(st *Store, grain string) []int64 {
		t.Helper()
		var r Result
		var err error
		switch grain {
		case "minute":
			r, err = st.Query(ctx, Query{Family: "totals", Owner: "alice", Start: testEnd - 6*hourMs, End: testEnd - 3*hourMs, MinuteFrom: first})
		default:
			r, err = st.Query(ctx, Query{Family: "totals", Owner: "alice", Start: first, End: testEnd, MinuteFrom: testEnd})
		}
		if err != nil {
			t.Fatal(err)
		}
		return r.Total
	}
	wantMin, wantAll := q(s, "minute"), q(s, "all")
	s.Close()

	// Leave copies of merged blocks behind under lower seqs, as a crash
	// between writing a merge and deleting its inputs would.
	var copies []string
	for _, tier := range []string{"minute", "hour"} {
		ents, err := os.ReadDir(filepath.Join(dir, "blocks", tier))
		if err != nil {
			t.Fatal(err)
		}
		for _, de := range ents {
			seq, merged := parseBlockName(de.Name())
			if !merged {
				continue
			}
			parts := strings.Split(de.Name(), "-")
			cp := filepath.Join(dir, "blocks", tier, fmt.Sprintf("%s-%s-%d.blk", parts[0], parts[1], seq-1))
			raw, err := os.ReadFile(filepath.Join(dir, "blocks", tier, de.Name()))
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(cp, raw, 0o644); err != nil {
				t.Fatal(err)
			}
			copies = append(copies, cp)
			break
		}
	}
	if len(copies) != 2 {
		t.Fatalf("expected a merged minute and hour block to copy, got %d", len(copies))
	}

	s, err = Open(dir, dnsSchema(), Options{Engine: Blocks})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for _, cp := range copies {
		if _, err := os.Stat(cp); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("leftover input %s still there: %v", cp, err)
		}
	}
	if got := q(s, "minute"); !reflect.DeepEqual(got, wantMin) {
		t.Fatalf("minute total after reopen %v, want %v", got, wantMin)
	}
	if got := q(s, "all"); !reflect.DeepEqual(got, wantAll) {
		t.Fatalf("hour total after reopen %v, want %v", got, wantAll)
	}
}

// Value lists, exclusion and the collector give known totals, on minutes and
// after the rows are sealed and rolled.
func TestValueListsAndCollector(t *testing.T) {
	ctx := context.Background()
	a, b := openBoth(t, dnsSchema())
	both := []*Store{a, b}
	start := testEnd - 6*hourMs
	row := func(zone, qtype, rcode string, n int64) Row {
		return Row{Keys: []string{zone, qtype, rcode}, Values: []int64{n}}
	}
	appendBoth(t, both, "alice", "c1", start, map[string][]Row{"totals": {row("a.", "A", "NOERROR", 10), row("b.", "AAAA", "NXDOMAIN", 20)}})
	appendBoth(t, both, "alice", "c2", start, map[string][]Row{"totals": {row("a.", "MX", "NOERROR", 40), row("c.", "A", "NOERROR", 80)}})
	cases := []struct {
		name    string
		filters []Filter
		want    int64
	}{
		{"none", nil, 150},
		{"zone a or b", []Filter{{Key: "zone", Values: []string{"a.", "b.", "zz."}}}, 70},
		{"zone a plus list", []Filter{{Key: "zone", Value: "a.", Values: []string{"c."}}}, 130},
		{"not NOERROR", []Filter{{Key: "rcode", Value: "NOERROR", Not: true}}, 20},
		{"not an unknown zone", []Filter{{Key: "zone", Value: "zz.", Not: true}}, 150},
		{"only unknown zones", []Filter{{Key: "zone", Values: []string{"zz.", "yy."}}}, 0},
		{"collector c2", []Filter{{Key: "collector", Value: "c2"}}, 120},
		{"not c2, A only", []Filter{{Key: "collector", Value: "c2", Not: true}, {Key: "qtype", Values: []string{"A"}}}, 10},
		{"unknown collector", []Filter{{Key: "collector", Value: "c9"}}, 0},
	}
	check := func(stage string, minuteFrom int64) {
		for _, s := range both {
			for _, c := range cases {
				r, err := s.Query(ctx, Query{Family: "totals", Owner: "alice", Filters: c.filters, Start: start, End: testEnd, MinuteFrom: minuteFrom})
				if err != nil {
					t.Fatal(err)
				}
				if r.Total[0] != c.want {
					t.Errorf("%s %s: total %d, want %d", stage, c.name, r.Total[0], c.want)
				}
			}
			r, err := s.Query(ctx, Query{Family: "totals", Owner: "alice", Start: start, End: testEnd, By: "collector", MinuteFrom: minuteFrom,
				Filters: []Filter{{Key: "qtype", Values: []string{"A", "MX"}}}})
			if err != nil {
				t.Fatal(err)
			}
			if len(r.Rows) != 2 || r.Rows[0].Key != "c2" || r.Rows[0].Values[0] != 120 || r.Rows[1].Key != "c1" || r.Rows[1].Values[0] != 10 {
				t.Errorf("%s by collector: %+v", stage, r.Rows)
			}
		}
	}
	check("minutes", start)
	for _, s := range both {
		maintainAll(t, s, testEnd)
	}
	check("hours", testEnd)
}

// TestLateAdds sums rows in with Add and sends minutes late at every stage:
// in the head, in a sealed slice whose hour is not compacted yet, in a merged
// hour whose day is still open, and in a day already folded. Every tier must
// keep adding up to what was sent, and both engines must agree.
func TestLateAdds(t *testing.T) {
	ctx := context.Background()
	sch := dnsSchema()
	sch.Retention = map[Grain]time.Duration{} // keep every tier, so each can be checked
	a, b := openBoth(t, sch)
	both := []*Store{a, b}
	first := testEnd - 9*dayMs
	truth := map[string]int64{} // zone → count
	var total int64
	add := func(m int64, zone string, n int64) {
		t.Helper()
		rows := []Row{{Keys: []string{zone, "A", "NOERROR"}, Values: []int64{n}}}
		for _, s := range both {
			if err := s.Add(ctx, "alice", "c1", "totals", m, rows); err != nil {
				t.Fatal(err)
			}
			if err := s.Add(ctx, "alice", "c1", "names", m, []Row{{Keys: []string{zone, "n." + zone, "A", "NOERROR"}, Values: []int64{n}}}); err != nil {
				t.Fatal(err)
			}
		}
		truth[zone] += n
		total += n
	}
	check := func(stage string) {
		t.Helper()
		for _, s := range both {
			for _, w := range []struct {
				name       string
				minuteFrom int64
			}{{"minutes", first}, {"hours", testEnd}} {
				// Nine days: with hours allowed, whole days come from the day tier.
				r, err := s.Query(ctx, Query{Family: "totals", Owner: "alice", Start: first, End: testEnd, By: "zone", MinuteFrom: w.minuteFrom})
				if err != nil {
					t.Fatal(err)
				}
				if r.Total[0] != total {
					t.Errorf("%s %s: total %d, want %d", stage, w.name, r.Total[0], total)
				}
				for _, row := range r.Rows {
					if row.Values[0] != truth[row.Key] {
						t.Errorf("%s %s: zone %s %d, want %d", stage, w.name, row.Key, row.Values[0], truth[row.Key])
					}
				}
			}
		}
		// Both engines agree on every read, the capped names family included.
		for _, q := range dnsQueries("alice") {
			ra, err := a.Query(ctx, q)
			if err != nil {
				t.Fatal(err)
			}
			rb, err := b.Query(ctx, q)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(ra.Total, rb.Total) || !reflect.DeepEqual(ra.Rows, rb.Rows) || !reflect.DeepEqual(ra.Other, rb.Other) {
				t.Errorf("%s: engines differ on %s by %q %v: %v/%v vs %v/%v", stage, q.Family, q.By, q.Filters, ra.Total, ra.Rows, rb.Total, rb.Rows)
				return
			}
		}
	}
	// Nine days, every 20 minutes, with a second Add to the same minute and key.
	for m := first; m < testEnd-dayMs; m += 20 * minuteMs {
		add(m, fmt.Sprintf("z%d.", (m/minuteMs)%7), 3)
		add(m, fmt.Sprintf("z%d.", (m/minuteMs)%7), 2)
	}
	check("head only")
	// Seal and roll up to the middle of an hour of the last day: 14:30 is
	// sealed, hour 14 is not compacted, days before the last are folded.
	now := testEnd - dayMs - 10*hourMs + 30*minuteMs + rollLag
	for _, s := range both {
		maintainAll(t, s, now)
	}
	check("rolled")
	last := testEnd - 2*dayMs // a day already folded
	add(last+10*hourMs+5*minuteMs, "late-day.", 7)
	check("late into a folded day")
	open := testEnd - dayMs - 10*hourMs            // hour 14 of the open day
	add(open-hourMs+20*minuteMs, "late-hour.", 11) // hour 13: merged, day open
	check("late into a merged hour")
	add(open+10*minuteMs, "late-slice.", 13) // hour 14: sealed slice, not compacted
	check("late into a sealed slice")
	add(now-30*minuteMs, "head.", 17) // still in the head
	add(now-30*minuteMs, "head.", 1)
	check("into the head")
	// Roll everything: the late rows reach hours and days exactly once, and
	// stray blocks are merged away.
	for _, s := range both {
		maintainAll(t, s, testEnd+3*hourMs)
	}
	check("all rolled")
	// Three late minutes, two families each, took the late path.
	if n := b.eng.(*blockEngine).late; n != 6 {
		t.Errorf("late block writes %d, want 6", n)
	}
	st, err := b.Stats(ctx)
	if err != nil {
		t.Fatal(err)
	}
	h := last + 10*hourMs
	if n := len(b.eng.(*blockEngine).within(Minute, h, h+hourMs)); n != 1 {
		t.Errorf("hour with a late minute keeps %d minute blocks, want 1 after merging", n)
	}
	t.Logf("blocks after late adds: %v", st.Files)
}

// A late Add racing maintenance loses nothing and counts nothing twice.
func TestLateAddsRaceMaintenance(t *testing.T) {
	ctx := context.Background()
	sch := dnsSchema()
	sch.Retention = map[Grain]time.Duration{}
	s, err := Open(filepath.Join(t.TempDir(), "blocks"), sch, Options{Engine: Blocks})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	first := testEnd - 3*dayMs
	var want int64
	for m := first; m < testEnd; m += 10 * minuteMs {
		if err := s.Add(ctx, "alice", "c1", "totals", m, []Row{{Keys: []string{"z.", "A", "NOERROR"}, Values: []int64{1}}}); err != nil {
			t.Fatal(err)
		}
		want++
	}
	done := make(chan error, 1)
	go func() {
		for now := first + 4*hourMs; now <= testEnd+3*hourMs; now += 30 * minuteMs {
			if _, err := s.Maintain(ctx, now); err != nil {
				done <- err
				return
			}
		}
		done <- nil
	}()
	// Late rows spread over the whole range while maintenance moves through it.
	r := rand.New(rand.NewSource(3))
	for range 400 {
		m := first + r.Int63n(3*dayMs)
		if err := s.Add(ctx, "alice", "c1", "totals", m, []Row{{Keys: []string{"z.", "A", "NOERROR"}, Values: []int64{1}}}); err != nil {
			t.Fatal(err)
		}
		want++
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	maintainAll(t, s, testEnd+3*hourMs)
	for _, mf := range []int64{first, testEnd} {
		res, err := s.Query(ctx, Query{Family: "totals", Owner: "alice", Start: first, End: testEnd, MinuteFrom: mf})
		if err != nil {
			t.Fatal(err)
		}
		if res.Total[0] != want {
			t.Errorf("minuteFrom %d: total %d, want %d", mf, res.Total[0], want)
		}
	}
	if s.eng.(*blockEngine).late == 0 {
		t.Error("no Add took the late path; the race was not exercised")
	}
}

func levelSchema(top int) Schema {
	caps := map[Grain]int{Hour: top, Day: top}
	return Schema{
		Families: []Family{{Name: "sources", Keys: []Key{
			{Name: "ip", Fold: true, Level: 1}, {Name: "range", Fold: true, Level: 2}, {Name: "asn", Fold: true, Level: 3}, {Name: "scenario"},
		}, Measures: []string{"alerts", "events"}, Cap: caps}},
		Retention: map[Grain]time.Duration{},
	}
}

// oldFoldTop is foldTop before fold levels, kept to show single-level
// families fold exactly as they did.
func oldFoldTop(f *famInfo, rows []rec, n int) []rec {
	if n <= 0 || f.rank < 0 {
		return rows
	}
	byCol := map[string][]rec{}
	var cols []string
	for _, r := range rows {
		if _, ok := byCol[r.collector]; !ok {
			cols = append(cols, r.collector)
		}
		byCol[r.collector] = append(byCol[r.collector], r)
	}
	sort.Strings(cols)
	out := make([]rec, 0, len(rows))
	for _, c := range cols {
		named := []rec{}
		other := map[string]*rec{}
		var otherOrder []string
		addOther := func(r rec) {
			fk := f.foldKeys(r.keys)
			id := strings.Join(fk, "\x00")
			o := other[id]
			if o == nil {
				o = &rec{collector: r.collector, bucket: r.bucket, keys: fk, vals: make([]int64, len(r.vals))}
				other[id] = o
				otherOrder = append(otherOrder, id)
			}
			for i, v := range r.vals {
				o.vals[i] += v
			}
		}
		for _, r := range byCol[c] {
			if f.folded(r.keys) {
				addOther(r)
				continue
			}
			named = append(named, r)
		}
		sort.Slice(named, func(i, j int) bool {
			if named[i].vals[0] != named[j].vals[0] {
				return named[i].vals[0] > named[j].vals[0]
			}
			return strings.Join(named[i].keys, "\x00") < strings.Join(named[j].keys, "\x00")
		})
		if len(named) > n {
			for _, r := range named[n:] {
				addOther(r)
			}
			named = named[:n]
		}
		out = append(out, named...)
		sort.Strings(otherOrder)
		for _, id := range otherOrder {
			out = append(out, *other[id])
		}
	}
	return out
}

func recSet(rows []rec) map[string][]int64 {
	out := map[string][]int64{}
	for _, r := range rows {
		out[r.collector+"|"+strings.Join(r.keys, "|")] = r.vals
	}
	return out
}

// TestFoldLevels: staged folding on known rows, and single-level families
// folding exactly as before.
func TestFoldLevels(t *testing.T) {
	si, err := compile(levelSchema(2))
	if err != nil {
		t.Fatal(err)
	}
	f := si.byName["sources"]
	row := func(ip, rg, asn string, n int64) rec {
		return rec{collector: "c1", keys: []string{ip, rg, asn, "s"}, vals: []int64{n, n}}
	}
	got := recSet(foldTop(f, []rec{
		row("ip1", "r1", "a1", 10), row("ip2", "r1", "a1", 8), row("ip3", "r1", "a1", 5), row("ip4", "r2", "a1", 4),
		row("ip5", "r3", "a2", 3), row("ip6", "r4", "a3", 2), row("ip7", "r5", "a4", 1),
		// A row already folded at level 1 joins that stage.
		row(Other, "r2", "a1", 6),
	}, 2))
	O := Other
	want := map[string][]int64{
		"c1|ip1|r1|a1|s": {10, 10}, "c1|ip2|r1|a1|s": {8, 8}, // level 0: top 2
		"c1|" + O + "|r2|a1|s": {10, 10}, "c1|" + O + "|r1|a1|s": {5, 5}, // level 1: top 2 (6+4 joined)
		"c1|" + O + "|" + O + "|a2|s": {3, 3}, "c1|" + O + "|" + O + "|a3|s": {2, 2}, // level 2: top 2
		"c1|" + O + "|" + O + "|" + O + "|s": {1, 1}, // everything left
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("levels:\n got %v\nwant %v", got, want)
	}
	// Bad declarations are refused.
	for _, bad := range []Key{{Name: "x", Level: 2}, {Name: "x", Fold: true, Level: -1}} {
		s := levelSchema(2)
		s.Families[0].Keys = append(s.Families[0].Keys, bad)
		if _, err := compile(s); err == nil {
			t.Errorf("key %+v accepted", bad)
		}
	}
	// Single level: the same rows as before fold levels, on generated data.
	dsi, err := compile(dnsSchema())
	if err != nil {
		t.Fatal(err)
	}
	g := newDNSGen(11)
	for _, fam := range []string{"names", "clients"} {
		df := dsi.byName[fam]
		var rows []rec
		for range 30 {
			for _, col := range []string{"c1", "c2"} {
				rows = append(rows, toRecs(df, col, testEnd, g.minute()[fam])...)
			}
		}
		rows = sumRecs(rows, testEnd)
		for _, n := range []int{1, 5, 25, 1000} {
			if a, b := recSet(foldTop(df, rows, n)), recSet(oldFoldTop(df, rows, n)); !reflect.DeepEqual(a, b) {
				t.Fatalf("%s cap %d: single-level fold changed (%d vs %d rows)", fam, n, len(a), len(b))
			}
		}
	}
}

// TestFoldLevelsEnginesAgree rolls a three-level family on both engines and
// checks every read against the rows sent: totals always match, and any read
// not flagged Folded matches key by key.
func TestFoldLevelsEnginesAgree(t *testing.T) {
	ctx := context.Background()
	a, b := openBoth(t, levelSchema(5))
	both := []*Store{a, b}
	r := rand.New(rand.NewSource(5))
	iz := rand.NewZipf(r, 1.2, 2, 199)
	ipOf := func(i int) (ip, rg, asn string) {
		return fmt.Sprintf("10.%d.%d.%d", i%7, i/7%5, i), fmt.Sprintf("10.%d.%d.0/24", i%7, i/7%5), fmt.Sprintf("AS%d", i%7)
	}
	type sent struct {
		bucket int64
		keys   []string
		n      int64
	}
	var all []sent
	first := testEnd - 3*dayMs
	for m := first; m < testEnd; m += 5 * minuteMs {
		var rows []Row
		for range 4 {
			ip, rg, asn := ipOf(int(iz.Uint64()))
			k := []string{ip, rg, asn, fmt.Sprintf("s%d", r.Intn(3))}
			n := int64(1 + r.Intn(5))
			rows = append(rows, Row{Keys: k, Values: []int64{n, 2 * n}})
			all = append(all, sent{m, k, n})
		}
		for _, s := range both {
			if err := s.Add(ctx, "alice", "c1", "sources", m, rows); err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, s := range both {
		maintainAll(t, s, testEnd+3*hourMs)
	}
	keyIdx := map[string]int{"ip": 0, "range": 1, "asn": 2, "scenario": 3}
	ip0, rg0, asn0 := ipOf(0)
	filters := [][]Filter{
		nil,
		{{Key: "ip", Value: ip0}},
		{{Key: "range", Value: rg0}},
		{{Key: "asn", Value: asn0}},
		{{Key: "range", Value: rg0}, {Key: "asn", Value: asn0}},
		{{Key: "scenario", Value: "s1"}},
		{{Key: "asn", Value: asn0, Not: true}},
	}
	exact, folded := 0, 0
	for _, w := range []struct{ start, minuteFrom int64 }{{testEnd - 2*hourMs, testEnd - 2*hourMs}, {first, testEnd}, {first, first}} {
		for _, fl := range filters {
			for _, by := range []string{"ip", "range", "asn", "scenario"} {
				q := Query{Family: "sources", Owner: "alice", Start: w.start, End: testEnd, By: by, MinuteFrom: w.minuteFrom, CheckFolded: true, KeepOther: true}
				q.Filters = fl
				ra, err := a.Query(ctx, q)
				if err != nil {
					t.Fatal(err)
				}
				rb, err := b.Query(ctx, q)
				if err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(ra.Rows, rb.Rows) || !reflect.DeepEqual(ra.Total, rb.Total) || ra.Folded != rb.Folded {
					t.Fatalf("engines differ by %s %v: %v %v vs %v %v", by, fl, ra.Rows, ra.Folded, rb.Rows, rb.Folded)
				}
				// The truth for this read.
				truth := map[string]int64{}
				var total int64
				for _, s := range all {
					if s.bucket < q.Start || s.bucket >= q.End {
						continue
					}
					ok := true
					for _, f := range fl {
						match := s.keys[keyIdx[f.Key]] == f.Value
						if match == f.Not {
							ok = false
						}
					}
					if !ok {
						continue
					}
					truth[s.keys[keyIdx[by]]] += s.n
					total += s.n
				}
				got := map[string]int64{}
				for _, row := range ra.Rows {
					got[row.Key] = row.Values[0]
				}
				if !ra.Folded {
					if !reflect.DeepEqual(got, truth) || ra.Total[0] != total {
						t.Errorf("not flagged folded, but by %s %v (from %d) differs:\n got %v\nwant %v", by, fl, (testEnd-q.Start)/hourMs, got, truth)
					}
					exact++
				} else {
					folded++
				}
				if len(fl) == 0 && ra.Total[0] != total {
					t.Errorf("by %s: total %d, want %d", by, ra.Total[0], total)
				}
			}
		}
	}
	if exact == 0 || folded == 0 {
		t.Errorf("reads should be both exact and folded: %d exact, %d folded", exact, folded)
	}
	t.Logf("%d reads exact, %d flagged folded", exact, folded)
}
