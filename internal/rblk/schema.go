package rblk

import (
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Kind is how a column is stored.
// ErrShapeChanged: the store was written with a schema whose stored bytes
// read differently (a column added, removed, renamed or retyped, a SameAs or
// template added or removed). Old blocks and head rows would read wrong.
var ErrShapeChanged = errors.New("rblk: schema shape changed since the store was written")

type Kind int

const (
	// Dict is a repeated string (a scenario, an IP): stored once, rows hold ids.
	Dict Kind = iota
	// Int is a number.
	Int
	// Bool is true or false, stored as 1 or 0 in N.
	Bool
	// Text is free text; a Template may rebuild it, so it is stored only when it differs.
	Text
	// ID is an identifier, often a UUID: 16 bytes when it is one, else its text.
	ID
)

// Column is one column of a record or of a child row.
type Column struct {
	Name string
	Kind Kind
	// SameAs names a Dict column this Dict column usually equals ("^name" for a
	// parent column, from a child); equal values cost nothing.
	SameAs string
	// Index keeps a lookup by this Dict column, for pages about one value.
	Index bool
	// Active marks the child Int column that holds an expiry time in ms.
	Active bool
}

// Schema is what a collector type declares.
type Schema struct {
	Columns []Column
	// Dedupe names an ID column: an insert whose value the owner already has
	// is a duplicate and changes nothing.
	Dedupe string
	// Templates rebuild Text columns from the rest of the row; when the text
	// matches, it is not stored.
	Templates map[string]func(r *Record) string
	Children  []Column
}

// Record is one row. S holds Dict, Text and ID values, N holds Int and Bool
// values, both indexed like Schema.Columns.
type Record struct {
	ID       int64
	Time     int64
	S        []string
	N        []int64
	Children []Child
}

// Child is one child row, indexed like Schema.Children.
type Child struct {
	S []string
	N []int64
}

// Filter keeps rows whose Dict column is one of Values, or with Not, none of them.
type Filter struct {
	Col    string
	Values []string
	Not    bool
}

// Query selects rows of one owner.
type Query struct {
	Owner string
	// Start and End bound Time, [Start, End); End 0 means no bound.
	Start, End int64
	Filters    []Filter
	// Contains keeps rows where any of In (Dict columns) holds it, ASCII
	// case-insensitive, as SQLite's LIKE.
	Contains string
	In       []string
	// Sort is a Dict or Int column, or "" for Time; ties break on ID.
	Sort     string
	Desc     bool
	Offset   int
	Limit    int // 0: every row
	Children bool
}

// Agg groups the rows a Query selects.
type Agg struct {
	By string // Dict column, or "" for one group
	// Width > 0 buckets Time: ((t + Offset) / Width) * Width - Offset.
	Width, Offset int64
	Sum           []string // Int columns, or "#children"
	Distinct      string   // Dict column: distinct non-empty values
	Max           []string // Dict columns: greatest value
	// Top > 0 keeps only the Top groups with the most rows (ties: key, then
	// bucket), ordered that way, and sets Group.Total on each.
	Top int
	// Times fills Group.MinTime and MaxTime (otherwise 0).
	Times bool
}

// Group is one aggregate row.
type Group struct {
	Key              string
	Bucket           int64
	Count            int64
	Sums             []int64
	Distinct         int64
	Max              []string
	MinTime, MaxTime int64
	// Total is the row count over every group, set when Agg.Top > 0.
	Total int64
}

// Stats measure a store on disk.
type Stats struct {
	FileBytes  int64
	HeadBytes  int64
	BlockBytes int64
	Files      int
	Blocks     int
	HeadRows   int64
	Rows       int64 // every stored row, head and blocks
}

// ErrNotFound is returned by Get for an unknown id.
var ErrNotFound = errors.New("rblk: no such record")

// Engine names an implementation.
type Engine string

const (
	Slim   Engine = "slim"
	Blocks Engine = "blocks"
)

type engine interface {
	insert(ctx context.Context, owner string, recs []Record) ([]int64, error)
	get(ctx context.Context, owner string, id int64) (*Record, error)
	find(ctx context.Context, q Query) ([]Record, int64, error)
	aggregate(ctx context.Context, q Query, a Agg) ([]Group, error)
	active(ctx context.Context, q Query, now int64) ([]Record, error)
	maintain(ctx context.Context, now int64) error
	prune(ctx context.Context, before int64, owners []string) error
	stats(ctx context.Context) (Stats, error)
	oldest(ctx context.Context, owner string) (int64, bool, error)
	close() error
}

// Store is a record store in one directory.
type Store struct {
	sch  *schemaInfo
	eng  engine
	lock *os.File
}

// ErrLocked: another process has the store open. A store has one owner at a
// time; it covers recovery on open, inserts, sealing, retention, owner
// removal and sequence numbers.
var ErrLocked = errors.New("rblk: store is open in another process")

// Open opens or creates a store in dir.
func Open(dir string, s Schema, e Engine) (*Store, error) {
	si, err := compile(s)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	if e != Slim && e != Blocks {
		return nil, fmt.Errorf("rblk: unknown engine %q", e)
	}
	lock, err := lockDir(dir)
	if err != nil {
		return nil, err
	}
	var eng engine
	if e == Slim {
		eng, err = openSlim(filepath.Join(dir, "records.db"), si, false)
	} else {
		eng, err = openBlocks(dir, si)
	}
	if err != nil {
		if lock != nil {
			lock.Close()
		}
		return nil, err
	}
	return &Store{sch: si, eng: eng, lock: lock}, nil
}

func (s *Store) Close() error {
	err := s.eng.close()
	if s.lock != nil {
		s.lock.Close()
	}
	return err
}

// Col returns a column's index in Record.S and Record.N, or -1.
func (s *Store) Col(name string) int { return s.sch.col(name) }

// ChildCol returns a child column's index, or -1.
func (s *Store) ChildCol(name string) int { return s.sch.child(name) }

// Insert stores records for an owner; IDs come back in order, 0 for duplicates.
func (s *Store) Insert(ctx context.Context, owner string, recs []Record) ([]int64, error) {
	if owner == "" {
		return nil, errors.New("rblk: empty owner")
	}
	for i := range recs {
		if err := s.sch.check(&recs[i]); err != nil {
			return nil, err
		}
	}
	return s.eng.insert(ctx, owner, recs)
}

// Get returns one record with its children, or ErrNotFound.
func (s *Store) Get(ctx context.Context, owner string, id int64) (*Record, error) {
	return s.eng.get(ctx, owner, id)
}

// Find returns one page of rows and how many rows match in all.
func (s *Store) Find(ctx context.Context, q Query) ([]Record, int64, error) {
	if err := s.sch.checkQuery(q); err != nil {
		return nil, 0, err
	}
	return s.eng.find(ctx, q)
}

// Aggregate groups the rows q selects (q's sort and page are ignored).
func (s *Store) Aggregate(ctx context.Context, q Query, a Agg) ([]Group, error) {
	if err := s.sch.checkQuery(q); err != nil {
		return nil, err
	}
	return s.eng.aggregate(ctx, q, a)
}

// Active returns the rows q selects (Time ignored) that have a child whose
// Active column is after now, with only those children.
func (s *Store) Active(ctx context.Context, q Query, now int64) ([]Record, error) {
	if err := s.sch.checkQuery(q); err != nil {
		return nil, err
	}
	q.Start, q.End = 0, 0
	return s.eng.active(ctx, q, now)
}

// Maintain moves settled rows to their final form (sealing, for Blocks).
func (s *Store) Maintain(ctx context.Context, now int64) error { return s.eng.maintain(ctx, now) }

// Prune deletes rows older than before (0: none) and rows of owners not listed (nil: keep all).
func (s *Store) Prune(ctx context.Context, before int64, owners []string) error {
	return s.eng.prune(ctx, before, owners)
}

func (s *Store) Stats(ctx context.Context) (Stats, error) { return s.eng.stats(ctx) }

// Oldest returns the earliest row time of an owner; ok is false when the
// owner has no rows. It reads an index or one block column, never a scan.
func (s *Store) Oldest(ctx context.Context, owner string) (t int64, ok bool, err error) {
	return s.eng.oldest(ctx, owner)
}

// ---- schema checks ----

var nameRe = regexp.MustCompile(`^[a-z][a-z0-9_]{0,31}$`)

type schemaInfo struct {
	Schema
	idx      map[string]int
	cidx     map[string]int
	same     []int // per column: SameAs column index, or -1
	csame    []int // per child column: parent column index, or -1
	dedupe   int
	activeCh int
}

func compile(s Schema) (*schemaInfo, error) {
	si := &schemaInfo{Schema: s, idx: map[string]int{}, cidx: map[string]int{}, dedupe: -1, activeCh: -1}
	for i, c := range s.Columns {
		if !nameRe.MatchString(c.Name) {
			return nil, fmt.Errorf("rblk: column name %q", c.Name)
		}
		if _, dup := si.idx[c.Name]; dup {
			return nil, fmt.Errorf("rblk: column %q declared twice", c.Name)
		}
		si.idx[c.Name] = i
	}
	for i, c := range s.Columns {
		si.same = append(si.same, -1)
		if c.SameAs != "" {
			j, ok := si.idx[c.SameAs]
			if !ok || c.Kind != Dict || s.Columns[j].Kind != Dict {
				return nil, fmt.Errorf("rblk: %q is the same as %q, but both must be Dict columns", c.Name, c.SameAs)
			}
			si.same[i] = j
		}
		if c.Index && c.Kind != Dict {
			return nil, fmt.Errorf("rblk: only Dict columns can be indexed (%q)", c.Name)
		}
	}
	// SameAs chains must end: a cycle would never resolve.
	for i := range s.Columns {
		j, steps := i, 0
		for si.same[j] >= 0 {
			j = si.same[j]
			if steps++; steps > len(s.Columns) {
				return nil, fmt.Errorf("rblk: SameAs of %q forms a cycle", s.Columns[i].Name)
			}
		}
	}
	for i, c := range s.Children {
		if !nameRe.MatchString(c.Name) {
			return nil, fmt.Errorf("rblk: child column name %q", c.Name)
		}
		if _, dup := si.cidx[c.Name]; dup {
			return nil, fmt.Errorf("rblk: child column %q declared twice", c.Name)
		}
		si.cidx[c.Name] = i
		si.csame = append(si.csame, -1)
		if c.SameAs != "" {
			j, ok := si.idx[strings.TrimPrefix(c.SameAs, "^")]
			if !strings.HasPrefix(c.SameAs, "^") || !ok || c.Kind != Dict || s.Columns[j].Kind != Dict {
				return nil, fmt.Errorf("rblk: child %q must be the same as a parent Dict column (^name)", c.Name)
			}
			si.csame[i] = j
		}
		if c.Active {
			if c.Kind != Int || si.activeCh >= 0 {
				return nil, fmt.Errorf("rblk: one child Int column can be Active (%q)", c.Name)
			}
			si.activeCh = i
		}
	}
	if s.Dedupe != "" {
		j, ok := si.idx[s.Dedupe]
		if !ok || s.Columns[j].Kind != ID {
			return nil, fmt.Errorf("rblk: dedupe column %q must be an ID column", s.Dedupe)
		}
		si.dedupe = j
	}
	for name := range s.Templates {
		if j, ok := si.idx[name]; !ok || s.Columns[j].Kind != Text {
			return nil, fmt.Errorf("rblk: template for %q, which is not a Text column", name)
		}
	}
	return si, nil
}

// shape is a fingerprint of everything in the schema that decides how block
// and head bytes read: column and child names, kinds, SameAs, which Text
// columns have a template, the Active child and the dedupe column. Indexes
// are left out; they change no stored byte. A template's output is not
// covered: changing it is the program's job (Section 12 of the spec).
func (si *schemaInfo) shape() int64 {
	h := fnv.New64a()
	w := func(parts ...string) {
		for _, p := range parts {
			h.Write([]byte(p))
			h.Write([]byte{0})
		}
		h.Write([]byte{1})
	}
	for _, c := range si.Columns {
		w("c", c.Name, strconv.Itoa(int(c.Kind)), c.SameAs, strconv.FormatBool(si.Templates[c.Name] != nil))
	}
	for _, c := range si.Children {
		w("k", c.Name, strconv.Itoa(int(c.Kind)), c.SameAs, strconv.FormatBool(c.Active))
	}
	w("d", si.Dedupe)
	return int64(h.Sum64())
}

func (si *schemaInfo) col(name string) int {
	if i, ok := si.idx[name]; ok {
		return i
	}
	return -1
}

func (si *schemaInfo) child(name string) int {
	if i, ok := si.cidx[name]; ok {
		return i
	}
	return -1
}

func (si *schemaInfo) check(r *Record) error {
	if len(r.S) != len(si.Columns) || len(r.N) != len(si.Columns) {
		return fmt.Errorf("rblk: a record needs %d values in S and N", len(si.Columns))
	}
	for _, c := range r.Children {
		if len(c.S) != len(si.Children) || len(c.N) != len(si.Children) {
			return fmt.Errorf("rblk: a child needs %d values in S and N", len(si.Children))
		}
	}
	return nil
}

func (si *schemaInfo) checkQuery(q Query) error {
	for _, f := range q.Filters {
		if j := si.col(f.Col); j < 0 || si.Columns[j].Kind != Dict {
			return fmt.Errorf("rblk: filter on %q, which is not a Dict column", f.Col)
		}
	}
	for _, c := range q.In {
		if j := si.col(c); j < 0 || si.Columns[j].Kind != Dict {
			return fmt.Errorf("rblk: search in %q, which is not a Dict column", c)
		}
	}
	if q.Sort != "" {
		if j := si.col(q.Sort); j < 0 || (si.Columns[j].Kind != Dict && si.Columns[j].Kind != Int) {
			return fmt.Errorf("rblk: sort by %q, which is not a Dict or Int column", q.Sort)
		}
	}
	return nil
}

// ---- matching and aggregating rows in memory (Blocks, and tests) ----

// matcher tests materialized rows against a query's filters.
type matcher struct {
	si       *schemaInfo
	q        Query
	filters  []int // column per filter
	in       []int
	contains string
}

func newMatcher(si *schemaInfo, q Query) *matcher {
	m := &matcher{si: si, q: q, contains: strings.ToLower(q.Contains)}
	for _, f := range q.Filters {
		m.filters = append(m.filters, si.col(f.Col))
	}
	for _, c := range q.In {
		m.in = append(m.in, si.col(c))
	}
	return m
}

func (m *matcher) timeOK(t int64) bool {
	return m.q.End == 0 || (t >= m.q.Start && t < m.q.End)
}

func (m *matcher) match(r *Record) bool {
	if !m.timeOK(r.Time) {
		return false
	}
	for i, f := range m.q.Filters {
		v := r.S[m.filters[i]]
		in := false
		for _, x := range f.Values {
			if x == v {
				in = true
				break
			}
		}
		if in == f.Not {
			return false
		}
	}
	if m.q.Contains != "" {
		hit := false
		for _, c := range m.in {
			if containsFold(r.S[c], m.contains) {
				hit = true
				break
			}
		}
		if !hit {
			return false
		}
	}
	return true
}

// containsFold is SQLite LIKE '%q%' for a lower-cased q: ASCII case-insensitive.
func containsFold(s, lowerQ string) bool {
	if lowerQ == "" {
		return true
	}
	return strings.Contains(asciiLower(s), lowerQ)
}

func asciiLower(s string) string {
	for i := 0; i < len(s); i++ {
		if c := s[i]; c >= 'A' && c <= 'Z' {
			b := []byte(s)
			for j := i; j < len(b); j++ {
				if b[j] >= 'A' && b[j] <= 'Z' {
					b[j] += 'a' - 'A'
				}
			}
			return string(b)
		}
	}
	return s
}

// less orders rows as ORDER BY sort dir, id dir.
func (si *schemaInfo) less(q Query) func(a, b *Record) bool {
	col := si.col(q.Sort)
	return func(a, b *Record) bool {
		c := 0
		switch {
		case q.Sort == "":
			c = cmp64(a.Time, b.Time)
		case si.Columns[col].Kind == Int:
			c = cmp64(a.N[col], b.N[col])
		default:
			c = strings.Compare(a.S[col], b.S[col])
		}
		if c == 0 {
			c = cmp64(a.ID, b.ID)
		}
		if q.Desc {
			return c > 0
		}
		return c < 0
	}
}

func cmp64(a, b int64) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

// aggregator sums matched rows into groups.
type aggregator struct {
	si     *schemaInfo
	a      Agg
	by     int
	sums   []int
	dist   int
	max    []int
	groups map[[2]string]*Group
	seen   map[[2]string]map[string]struct{}
}

func newAggregator(si *schemaInfo, a Agg) *aggregator {
	g := &aggregator{si: si, a: a, by: -1, dist: -1, groups: map[[2]string]*Group{}, seen: map[[2]string]map[string]struct{}{}}
	if a.By != "" {
		g.by = si.col(a.By)
	}
	for _, s := range a.Sum {
		g.sums = append(g.sums, si.col(s)) // -1 for "#children"
	}
	if a.Distinct != "" {
		g.dist = si.col(a.Distinct)
	}
	for _, m := range a.Max {
		g.max = append(g.max, si.col(m))
	}
	return g
}

func (g *aggregator) add(r *Record, nchild int64) {
	key := ""
	if g.by >= 0 {
		key = r.S[g.by]
	}
	var bucket int64
	if g.a.Width > 0 {
		bucket = ((r.Time+g.a.Offset)/g.a.Width)*g.a.Width - g.a.Offset
	}
	k := [2]string{key, fmt.Sprint(bucket)}
	gr := g.groups[k]
	if gr == nil {
		gr = &Group{Key: key, Bucket: bucket, Sums: make([]int64, len(g.sums)), Max: make([]string, len(g.max)), MinTime: math.MaxInt64, MaxTime: math.MinInt64}
		for i := range gr.Max {
			gr.Max[i] = "\x00unset"
		}
		g.groups[k] = gr
	}
	gr.Count++
	for i, c := range g.sums {
		if c < 0 {
			gr.Sums[i] += nchild
		} else {
			gr.Sums[i] += r.N[c]
		}
	}
	if g.dist >= 0 && r.S[g.dist] != "" {
		set := g.seen[k]
		if set == nil {
			set = map[string]struct{}{}
			g.seen[k] = set
		}
		set[r.S[g.dist]] = struct{}{}
	}
	for i, c := range g.max {
		if gr.Max[i] == "\x00unset" || r.S[c] > gr.Max[i] {
			gr.Max[i] = r.S[c]
		}
	}
	gr.MinTime, gr.MaxTime = min(gr.MinTime, r.Time), max(gr.MaxTime, r.Time)
}

func (g *aggregator) result() []Group {
	out := make([]Group, 0, len(g.groups))
	for k, gr := range g.groups {
		gr.Distinct = int64(len(g.seen[k]))
		for i := range gr.Max {
			if gr.Max[i] == "\x00unset" {
				gr.Max[i] = ""
			}
		}
		if !g.a.Times {
			gr.MinTime, gr.MaxTime = 0, 0
		}
		out = append(out, *gr)
	}
	return finishGroups(out, g.a)
}

// finishGroups orders groups (by key and bucket, or with Top by count) and
// applies Top.
func finishGroups(out []Group, a Agg) []Group {
	if a.Top <= 0 {
		sortGroups(out)
		return out
	}
	var total int64
	for _, g := range out {
		total += g.Count
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		if out[i].Key != out[j].Key {
			return out[i].Key < out[j].Key
		}
		return out[i].Bucket < out[j].Bucket
	})
	if len(out) > a.Top {
		out = out[:a.Top]
	}
	for i := range out {
		out[i].Total = total
	}
	return out
}

// sortGroups orders groups by key then bucket, so engines compare equal.
func sortGroups(gs []Group) {
	sort.Slice(gs, func(i, j int) bool {
		if gs[i].Key != gs[j].Key {
			return gs[i].Key < gs[j].Key
		}
		return gs[i].Bucket < gs[j].Bucket
	})
}

// dirBytes sums file sizes under dir.
func dirBytes(dir string) (int64, int) {
	var n int64
	files := 0
	filepath.Walk(dir, func(_ string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			n += info.Size()
			files++
		}
		return nil
	})
	return n, files
}
