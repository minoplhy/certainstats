package rblk

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"
)

var testEnd = time.Date(2026, 11, 18, 0, 0, 0, 0, time.UTC).UnixMilli()

func testSchema() Schema {
	var s Schema
	s = Schema{
		Columns: []Column{
			{Name: "src", Kind: Dict, Index: true},
			{Name: "cat", Kind: Dict},
			{Name: "host", Kind: Dict},
			{Name: "val", Kind: Dict, SameAs: "src"},
			{Name: "n", Kind: Int},
			{Name: "flag", Kind: Bool},
			{Name: "msg", Kind: Text},
			{Name: "uid", Kind: ID},
		},
		Dedupe: "uid",
		Children: []Column{
			{Name: "typ", Kind: Dict},
			{Name: "v", Kind: Dict, SameAs: "^src"},
			{Name: "until", Kind: Int, Active: true},
		},
	}
	s.Templates = map[string]func(r *Record) string{
		"msg": func(r *Record) string { return fmt.Sprintf("%s did %s (%d)", r.S[0], r.S[1], r.N[4]) },
	}
	return s
}

// gen makes records; a few have a message the template does not give, a
// value other than src, an ID that is not a UUID, or a child with another value.
type gen struct{ r *rand.Rand }

func (g *gen) rec(t int64) Record {
	r := g.r
	src := fmt.Sprintf("10.%d.%d.%d", r.Intn(4), r.Intn(8), r.Intn(50))
	if r.Intn(3) == 0 {
		src = "10.0.0.1" // a heavy hitter
	}
	rec := Record{Time: t, S: make([]string, 8), N: make([]int64, 8)}
	rec.S[0], rec.S[1], rec.S[2] = src, fmt.Sprintf("Cat-%d", r.Intn(6)), fmt.Sprintf("host%d", r.Intn(3))
	rec.S[3] = src
	if r.Intn(10) == 0 {
		rec.S[3] = "192.0.2.0/24"
	}
	rec.N[4], rec.N[5] = int64(r.Intn(40)), int64(r.Intn(2))
	rec.S[6] = fmt.Sprintf("%s did %s (%d)", rec.S[0], rec.S[1], rec.N[4])
	if r.Intn(20) == 0 {
		rec.S[6] = "custom message " + fmt.Sprint(r.Intn(1000))
	}
	rec.S[7] = fmt.Sprintf("%08x-%04x-4%03x-a%03x-%012x", r.Uint32(), r.Intn(1<<16), r.Intn(1<<12), r.Intn(1<<12), r.Int63n(1<<48))
	if r.Intn(25) == 0 {
		rec.S[7] = "h:" + fmt.Sprint(r.Int63())
	}
	for k := range r.Intn(3) {
		ch := Child{S: []string{"ban", src, ""}, N: []int64{0, 0, t + int64(1+r.Intn(6))*3600_000}}
		if k == 1 {
			ch.S[0] = "captcha"
		}
		if r.Intn(8) == 0 {
			ch.S[1] = "198.51.100.7"
		}
		rec.Children = append(rec.Children, ch)
	}
	return rec
}

func openBoth(t *testing.T) (*Store, *Store) {
	t.Helper()
	a, err := Open(filepath.Join(t.TempDir(), "slim"), testSchema(), Slim)
	if err != nil {
		t.Fatal(err)
	}
	b, err := Open(filepath.Join(t.TempDir(), "blocks"), testSchema(), Blocks)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.Close(); b.Close() })
	return a, b
}

// truth is every stored record, for brute-force answers.
type truth struct {
	si   *schemaInfo
	rows map[string][]Record
	uids map[string]bool
}

func (tr *truth) add(owner string, recs []Record, ids []int64) {
	for i, r := range recs {
		if ids[i] == 0 {
			continue
		}
		r.ID = ids[i]
		tr.rows[owner] = append(tr.rows[owner], r)
	}
}

func (tr *truth) find(q Query) ([]Record, int64) {
	m := newMatcher(tr.si, q)
	var hit []Record
	for _, r := range tr.rows[q.Owner] {
		if m.match(&r) {
			hit = append(hit, r)
		}
	}
	less := tr.si.less(q)
	sort.Slice(hit, func(i, j int) bool { return less(&hit[i], &hit[j]) })
	total := int64(len(hit))
	lo := min(q.Offset, len(hit))
	hi := len(hit)
	if q.Limit > 0 {
		hi = min(lo+q.Limit, len(hit))
	}
	out := []Record{}
	for _, r := range hit[lo:hi] {
		if !q.Children {
			r.Children = nil
		}
		out = append(out, r)
	}
	return out, total
}

func (tr *truth) aggregate(q Query, a Agg) []Group {
	m := newMatcher(tr.si, q)
	ag := newAggregator(tr.si, a)
	for _, r := range tr.rows[q.Owner] {
		if m.match(&r) {
			ag.add(&r, int64(len(r.Children)))
		}
	}
	return ag.result()
}

func norm(recs []Record) []Record {
	if len(recs) == 0 {
		return nil
	}
	for i := range recs {
		if len(recs[i].Children) == 0 {
			recs[i].Children = nil
		}
	}
	return recs
}

func queries(owner string, now int64) []Query {
	var out []Query
	// Whole days (a block is counted from its header), a partial window, and none.
	windows := [][2]int64{{0, 0}, {testEnd - dayMs, testEnd}, {testEnd - 3*dayMs, testEnd}, {testEnd - 3*dayMs - 7*3600_000, testEnd - 3600_000}}
	filters := [][]Filter{
		nil,
		{{Col: "src", Values: []string{"10.0.0.1"}}},
		{{Col: "cat", Values: []string{"Cat-1", "Cat-2", "Cat-9"}}},
		{{Col: "host", Values: []string{"host0"}, Not: true}},
		{{Col: "val", Values: []string{"192.0.2.0/24"}}},
		{{Col: "src", Values: []string{"nowhere"}}},
		{{Col: "val", Values: []string{"10.0.0.1"}, Not: true}, {Col: "cat", Values: []string{"Cat-3"}}},
	}
	for _, w := range windows {
		for fi, f := range filters {
			for _, s := range []string{"", "src", "n", "cat"} {
				q := Query{Owner: owner, Start: w[0], End: w[1], Filters: f, Sort: s, Desc: fi%2 == 1, Limit: 25, Offset: (fi % 3) * 10, Children: fi%2 == 0}
				out = append(out, q)
			}
		}
		for _, c := range []string{"10.0.", "cat-4", "HOST", "192.0.2", "zzz"} {
			out = append(out, Query{Owner: owner, Start: w[0], End: w[1], Contains: c, In: []string{"src", "cat", "host", "val"}, Limit: 50})
		}
	}
	return out
}

var aggs = []Agg{
	{},
	{By: "cat", Sum: []string{"n", "#children"}, Distinct: "src"},
	{By: "src", Sum: []string{"n"}, Max: []string{"host"}},
	{Width: 3600_000, Sum: []string{"#children"}},
	{Width: dayMs, Offset: 7 * 3600_000, Distinct: "val"},
	{By: "val", Max: []string{"cat", "host"}},
	{Sum: []string{"n"}, Times: true},
	{By: "src", Sum: []string{"n"}, Top: 3},
	{By: "cat", Distinct: "src", Top: 2, Times: true},
	{By: "host", Max: []string{"cat"}, Top: 10},
	{Width: 3600_000, Sum: []string{"#children"}, Top: 5},
}

func compareAll(t *testing.T, stage string, stores []*Store, tr *truth, owner string, now int64) {
	t.Helper()
	ctx := context.Background()
	for _, q := range queries(owner, now) {
		want, wantN := tr.find(q)
		for si, s := range stores {
			got, n, err := s.Find(ctx, q)
			if err != nil {
				t.Fatal(err)
			}
			if n != wantN || !reflect.DeepEqual(norm(got), norm(want)) {
				t.Fatalf("%s engine %d find %+v: %d rows of %d, want %d of %d\n got %v\nwant %v", stage, si, q, len(got), n, len(want), wantN, firstIDs(got), firstIDs(want))
			}
		}
		for _, a := range aggs {
			want := tr.aggregate(q, a)
			for si, s := range stores {
				got, err := s.Aggregate(ctx, q, a)
				if err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(got, want) && !(len(got) == 0 && len(want) == 0) {
					t.Fatalf("%s engine %d aggregate %+v over %+v:\n got %v\nwant %v", stage, si, a, q, got, want)
				}
			}
		}
	}
	// Active children now, under a filter or none.
	for _, f := range [][]Filter{nil, {{Col: "src", Values: []string{"10.0.0.1"}}}} {
		var want []Record
		m := newMatcher(tr.si, Query{Owner: owner, Filters: f})
		for _, r := range tr.rows[owner] {
			if !m.match(&r) {
				continue
			}
			var kids []Child
			for _, c := range r.Children {
				if c.N[2] > now {
					kids = append(kids, c)
				}
			}
			if len(kids) > 0 {
				r.Children = kids
				want = append(want, r)
			}
		}
		sort.Slice(want, func(i, j int) bool { return want[i].ID < want[j].ID })
		for si, s := range stores {
			got, err := s.Active(ctx, Query{Owner: owner, Filters: f}, now)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(norm(got), norm(want)) {
				t.Fatalf("%s engine %d active: %d rows, want %d", stage, si, len(got), len(want))
			}
		}
	}
	// Detail for a sample of ids, and an unknown one.
	list := tr.rows[owner]
	for k := 0; k < len(list); k += max(1, len(list)/40) {
		want := list[k]
		for si, s := range stores {
			got, err := s.Get(ctx, owner, want.ID)
			if err != nil {
				t.Fatalf("%s engine %d get %d: %v", stage, si, want.ID, err)
			}
			if !reflect.DeepEqual(norm([]Record{*got}), norm([]Record{want})) {
				t.Fatalf("%s engine %d get %d:\n got %+v\nwant %+v", stage, si, want.ID, *got, want)
			}
		}
	}
	// Oldest row time and the stored row count.
	var oldest int64
	for i, r := range tr.rows[owner] {
		if i == 0 || r.Time < oldest {
			oldest = r.Time
		}
	}
	var rows int64
	for _, list := range tr.rows {
		rows += int64(len(list))
	}
	for si, s := range stores {
		got, ok, err := s.Oldest(ctx, owner)
		if err != nil || ok != (len(tr.rows[owner]) > 0) || (ok && got != oldest) {
			t.Fatalf("%s engine %d oldest: %d %v %v, want %d", stage, si, got, ok, err, oldest)
		}
		if _, ok, _ := s.Oldest(ctx, "nobody"); ok {
			t.Fatalf("%s engine %d: oldest for an unknown owner", stage, si)
		}
		st, err := s.Stats(ctx)
		if err != nil || st.Rows != rows {
			t.Fatalf("%s engine %d rows: %d (%v), want %d", stage, si, st.Rows, err, rows)
		}
	}
	for _, s := range stores {
		if _, err := s.Get(ctx, owner, 1<<40); err != ErrNotFound {
			t.Fatalf("%s: unknown id: %v", stage, err)
		}
		if _, err := s.Get(ctx, "someone-else", list[0].ID); err != ErrNotFound {
			t.Fatalf("%s: another owner's id: %v", stage, err)
		}
	}
}

func firstIDs(recs []Record) []int64 {
	var out []int64
	for i, r := range recs {
		if i >= 6 {
			break
		}
		out = append(out, r.ID)
	}
	return out
}

func insertAll(t *testing.T, stores []*Store, tr *truth, owner string, recs []Record) {
	t.Helper()
	var first []int64
	for i, s := range stores {
		cp := make([]Record, len(recs))
		copy(cp, recs)
		ids, err := s.Insert(context.Background(), owner, cp)
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			first = ids
		} else if !reflect.DeepEqual(ids, first) {
			t.Fatalf("engines gave different ids: %v vs %v", ids[:min(5, len(ids))], first[:min(5, len(first))])
		}
	}
	tr.add(owner, recs, first)
}

// TestEnginesAgree runs both engines and a brute-force reference through
// inserts, duplicates, sealing, late rows, retention and owner removal.
// The block engine's section cache stays warm from one stage to the next, so
// the later stages also check that a reseal or prune never serves stale data.
func TestEnginesAgree(t *testing.T) {
	ctx := context.Background()
	a, b := openBoth(t)
	stores := []*Store{a, b}
	si, _ := compile(testSchema())
	tr := &truth{si: si, rows: map[string][]Record{}}
	g := &gen{r: rand.New(rand.NewSource(1))}
	first := testEnd - 4*dayMs
	var batch []Record
	var resend []Record
	for ts := first; ts < testEnd; ts += 7 * 60_000 {
		r := g.rec(ts)
		batch = append(batch, r)
		if len(batch) == 40 {
			insertAll(t, stores, tr, "alice", batch)
			insertAll(t, stores, tr, "bob", batch[:5])
			resend = append(resend, batch[0], batch[7])
			batch = nil
		}
	}
	insertAll(t, stores, tr, "alice", batch)
	insertAll(t, stores, tr, "alice", resend) // duplicates: all skipped
	if n := len(tr.rows["alice"]); n == 0 {
		t.Fatal("nothing stored")
	}
	now := testEnd + 3600_000
	compareAll(t, "head only", stores, tr, "alice", now)
	for _, s := range stores {
		if err := s.Maintain(ctx, now); err != nil {
			t.Fatal(err)
		}
	}
	st, _ := b.Stats(ctx)
	if st.Blocks < 3 {
		t.Fatalf("blocks after sealing: %d", st.Blocks)
	}
	compareAll(t, "sealed", stores, tr, "alice", now)
	insertAll(t, stores, tr, "alice", resend) // duplicates of sealed rows
	// Late rows for sealed days.
	var late []Record
	for k := range 30 {
		late = append(late, g.rec(first+int64(k)*2*3600_000+123))
	}
	insertAll(t, stores, tr, "alice", late)
	compareAll(t, "late in the head", stores, tr, "alice", now)
	for _, s := range stores {
		if err := s.Maintain(ctx, now); err != nil {
			t.Fatal(err)
		}
	}
	compareAll(t, "late sealed", stores, tr, "alice", now)
	compareAll(t, "other owner", stores, tr, "bob", now)
	// Retention in the middle of a day, and bob removed.
	cut := first + dayMs + 5*3600_000
	for _, s := range stores {
		if err := s.Prune(ctx, cut, []string{"alice"}); err != nil {
			t.Fatal(err)
		}
	}
	var kept []Record
	for _, r := range tr.rows["alice"] {
		if r.Time >= cut {
			kept = append(kept, r)
		}
	}
	tr.rows["alice"], tr.rows["bob"] = kept, nil
	compareAll(t, "pruned", stores, tr, "alice", now)
	for _, s := range stores {
		got, n, err := s.Find(ctx, Query{Owner: "bob"})
		if err != nil || n != 0 || len(got) != 0 {
			t.Fatalf("bob's rows left: %d %v", n, err)
		}
	}
}

// A crash after a day's block was written but before the head forgot those
// rows: reopening merges them once, so nothing is counted twice.
func TestCrashBetweenSealAndForget(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	s, err := Open(dir, testSchema(), Blocks)
	if err != nil {
		t.Fatal(err)
	}
	g := &gen{r: rand.New(rand.NewSource(2))}
	var recs []Record
	for ts := testEnd - 2*dayMs; ts < testEnd; ts += 10 * 60_000 {
		recs = append(recs, g.rec(ts))
	}
	if _, err := s.Insert(ctx, "alice", recs); err != nil {
		t.Fatal(err)
	}
	e := s.eng.(*blockEngine)
	d := testEnd - 2*dayMs
	rows, err := e.head.dump(ctx, d, d+dayMs)
	if err != nil {
		t.Fatal(err)
	}
	e.seq++
	if _, err := writeBlock(e.si, e.path(d, e.seq), d, d+dayMs, d+dayMs, e.seq, rows); err != nil {
		t.Fatal(err)
	}
	// The next day crashed after sealing its first ten hours only.
	d2, cut := d+dayMs, d+dayMs+10*hourMs
	rows2, err := e.head.dump(ctx, d2, cut)
	if err != nil {
		t.Fatal(err)
	}
	e.seq++
	if _, err := writeBlock(e.si, e.path(d2, e.seq), d2, d2+dayMs, cut, e.seq, rows2); err != nil {
		t.Fatal(err)
	}
	s.Close() // the head still has both days' rows
	s, err = Open(dir, testSchema(), Blocks)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	_, n, err := s.Find(ctx, Query{Owner: "alice"})
	if err != nil || n != int64(len(recs)) {
		t.Fatalf("after reopening: %d rows, want %d (%v)", n, len(recs), err)
	}
	// The head keeps only the second day's rows after its ten sealed hours.
	if st, _ := s.Stats(ctx); st.HeadRows != int64(len(recs))-int64(len(rows["alice"]))-int64(len(rows2["alice"])) {
		t.Fatalf("head still holds %d rows", st.HeadRows)
	}
	all, _, err := s.Find(ctx, Query{Owner: "alice", Sort: "", Limit: 0})
	if err != nil {
		t.Fatal(err)
	}
	seen := map[int64]bool{}
	for _, r := range all {
		if seen[r.ID] {
			t.Fatalf("row %d twice", r.ID)
		}
		seen[r.ID] = true
	}
}

// A damaged block is set aside on open; the rest still answers.
func TestDamagedBlockSkipped(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	s, err := Open(dir, testSchema(), Blocks)
	if err != nil {
		t.Fatal(err)
	}
	g := &gen{r: rand.New(rand.NewSource(3))}
	var recs []Record
	for ts := testEnd - 3*dayMs; ts < testEnd; ts += 10 * 60_000 {
		recs = append(recs, g.rec(ts))
	}
	if _, err := s.Insert(ctx, "alice", recs); err != nil {
		t.Fatal(err)
	}
	if err := s.Maintain(ctx, testEnd+3*3600_000); err != nil {
		t.Fatal(err)
	}
	s.Close()
	ents, _ := os.ReadDir(filepath.Join(dir, "blocks"))
	var victim string
	for _, ent := range ents {
		if strings.HasSuffix(ent.Name(), ".rblk") {
			victim = filepath.Join(dir, "blocks", ent.Name())
			break
		}
	}
	raw, _ := os.ReadFile(victim)
	raw[len(raw)-1] ^= 0xff
	os.WriteFile(victim, raw, 0o644)
	s, err = Open(dir, testSchema(), Blocks)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	_, n, err := s.Find(ctx, Query{Owner: "alice"})
	if err != nil || n == 0 || n >= int64(len(recs)) {
		t.Fatalf("with one block set aside: %d rows of %d (%v)", n, len(recs), err)
	}
	if _, err := os.Stat(victim + ".bad"); err != nil {
		t.Fatalf("damaged block not set aside: %v", err)
	}
}

// IDs round-trip exactly: a canonical UUID takes 16 bytes, and no other text
// may take a form a reader would mistake for one (15 characters used to).
func TestIDRoundTrip(t *testing.T) {
	cases := []string{
		"", "a", "h:12345", "123456789012345", "\x00bcdefghijklmno", "1234567890123456",
		"6f1d2c3b-4a5e-4f60-8b7a-9c0d1e2f3a4b", "6F1D2C3B-4A5E-4F60-8B7A-9C0D1E2F3A4B",
		"6f1d2c3b-4a5e-4f60-8b7a-9c0d1e2f3a4", strings.Repeat("x", 300),
	}
	seen := map[string]string{}
	for _, s := range cases {
		b := encodeID(s)
		if got := decodeID(b); got != s {
			t.Errorf("%q: decoded %q from %x", s, got, b)
		}
		if prev, ok := seen[string(b)]; ok {
			t.Errorf("%q and %q encode alike (%x)", s, prev, b)
		}
		seen[string(b)] = s
	}
	if n := len(encodeID("6f1d2c3b-4a5e-4f60-8b7a-9c0d1e2f3a4b")); n != 16 {
		t.Errorf("a UUID takes %d bytes, want 16", n)
	}
}
