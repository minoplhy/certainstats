package rblk

import (
	"container/heap"
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

const (
	dayMs  = int64(24 * 3600 * 1000)
	hourMs = int64(3600 * 1000)
	// sealLag keeps the newest two hours in the head: collectors resend
	// unacknowledged records for a while, as tblk's roll lag. Older rows are
	// sealed by the hour into their day's block, so the head stays small and
	// recent windows read columns like older ones.
	sealLag = 2 * hourMs
	// seenKeep is how long sealed dedupe values are remembered.
	seenKeep = 7 * dayMs
)

// blockEngine keeps recent rows in a slim head and every settled day in one
// immutable block. A day's block is rewritten when late rows arrive for it:
// the new block (higher seq) is written before the old one and the head rows
// are deleted, and on open the highest seq of a day wins, so a crash never
// loses or doubles a row.
type blockEngine struct {
	si     *schemaInfo
	dir    string
	head   *slimEngine
	mu     sync.RWMutex
	blocks []*blockFile
	wmu    sync.Mutex
	seq    int64
	dmu    sync.Mutex
	dicts  map[string][]string
	cache  *secCache
}

// sectionCacheBytes bounds the decoded sections kept across queries.
const sectionCacheBytes = 64 << 20

func openBlocks(dir string, si *schemaInfo) (*blockEngine, error) {
	bdir := filepath.Join(dir, "blocks")
	if err := os.MkdirAll(bdir, 0o755); err != nil {
		return nil, err
	}
	head, err := openSlim(filepath.Join(dir, "head.db"), si, true)
	if err != nil {
		return nil, err
	}
	e := &blockEngine{si: si, dir: dir, head: head, dicts: map[string][]string{}, cache: newSecCache(sectionCacheBytes)}
	ents, err := os.ReadDir(bdir)
	if err != nil {
		head.close()
		return nil, err
	}
	byDay := map[int64][]*blockFile{}
	for _, ent := range ents {
		p := filepath.Join(bdir, ent.Name())
		switch {
		case strings.HasSuffix(ent.Name(), ".tmp"):
			os.Remove(p)
		case strings.HasSuffix(ent.Name(), ".rblk"):
			bf, err := openBlockFile(si, p)
			if err != nil {
				os.Rename(p, p+".bad")
				continue
			}
			byDay[bf.h.Start] = append(byDay[bf.h.Start], bf)
			e.seq = max(e.seq, bf.h.Seq)
		}
	}
	for _, list := range byDay {
		sort.Slice(list, func(i, j int) bool { return list[i].h.Seq > list[j].h.Seq })
		e.blocks = append(e.blocks, list[0])
		for _, old := range list[1:] {
			os.Remove(old.path) // superseded by a rewrite that finished
		}
	}
	sort.Slice(e.blocks, func(i, j int) bool { return e.blocks[i].h.Start < e.blocks[j].h.Start })
	// A crash after a block was written but before the head forgot its rows:
	// merge them again (rows already in the block are skipped by id).
	ctx := context.Background()
	days, err := head.days(ctx, math.MaxInt64)
	if err != nil {
		e.close()
		return nil, err
	}
	for _, d := range days {
		if bf := e.blockFor(d); bf != nil {
			// Merge again only what the block already covers; rows after
			// that were never sealed and stay in the head.
			if err := e.reseal(ctx, d, bf.sealed()); err != nil {
				e.close()
				return nil, err
			}
		}
	}
	return e, nil
}

func (e *blockEngine) close() error { return e.head.close() }

func (e *blockEngine) snapshot() []*blockFile {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return append([]*blockFile(nil), e.blocks...)
}

func (e *blockEngine) blockFor(d int64) *blockFile {
	for _, bf := range e.snapshot() {
		if bf.h.Start == d {
			return bf
		}
	}
	return nil
}

// swap puts add in place of drop (either may be nil) and deletes drop's file.
func (e *blockEngine) swap(add, drop *blockFile) {
	e.mu.Lock()
	var out []*blockFile
	for _, bf := range e.blocks {
		if bf != drop {
			out = append(out, bf)
		}
	}
	if add != nil {
		out = append(out, add)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].h.Start < out[j].h.Start })
	e.blocks = out
	e.mu.Unlock()
	if drop != nil {
		os.Remove(drop.path)
		e.cache.drop(drop.path)
		e.dmu.Lock()
		delete(e.dicts, drop.path)
		e.dmu.Unlock()
	}
}

func (e *blockEngine) dict(bf *blockFile, r *reader) ([]string, error) {
	e.dmu.Lock()
	d, ok := e.dicts[bf.path]
	e.dmu.Unlock()
	if ok {
		return d, nil
	}
	raw, err := r.raw("dict")
	if err != nil {
		return nil, err
	}
	d, err = readDict(raw)
	if err != nil {
		return nil, err
	}
	e.dmu.Lock()
	if len(e.dicts) > 256 {
		e.dicts = map[string][]string{}
	}
	e.dicts[bf.path] = d
	e.dmu.Unlock()
	return d, nil
}

func (e *blockEngine) open(bf *blockFile) (*reader, error) {
	r, err := openReader(e.si, bf, nil)
	if err != nil {
		return nil, err
	}
	r.cache = e.cache
	if r.dict, err = e.dict(bf, r); err != nil {
		r.close()
		return nil, err
	}
	return r, nil
}

// readAll materializes every row of a block, per owner.
func (e *blockEngine) readAll(bf *blockFile) (map[string][]Record, error) {
	r, err := e.open(bf)
	if err != nil {
		return nil, err
	}
	defer r.close()
	out := map[string][]Record{}
	for _, or := range bf.h.Owners {
		for i := or.Lo; i < or.Hi; i++ {
			rec, err := r.record(i, nil, true, nil)
			if err != nil {
				return nil, err
			}
			out[or.Owner] = append(out[or.Owner], rec)
		}
	}
	return out, nil
}

// nextSeq takes the next block sequence number. It never wraps: a wrapped
// number would lose to every older block (Section 9.2 of the spec).
func (e *blockEngine) nextSeq() (int64, error) {
	if e.seq == math.MaxInt64 {
		return 0, errors.New("rblk: block sequence numbers used up")
	}
	e.seq++
	return e.seq, nil
}

func (e *blockEngine) path(start, seq int64) string {
	return filepath.Join(e.dir, "blocks", fmt.Sprintf("%d-%d.rblk", start, seq))
}

// reseal writes day d's head rows before upto into its block, merging any
// block already there. Today's block is rewritten as each hour settles.
func (e *blockEngine) reseal(ctx context.Context, d, upto int64) error {
	cut := min(d+dayMs, upto)
	rows, err := e.head.dump(ctx, d, cut)
	if err != nil {
		return err
	}
	old := e.blockFor(d)
	merged := map[string][]Record{}
	if old != nil {
		if merged, err = e.readAll(old); err != nil {
			return err
		}
	}
	have := map[int64]bool{}
	for _, list := range merged {
		for _, r := range list {
			have[r.ID] = true
		}
	}
	for o, list := range rows {
		for _, r := range list {
			if !have[r.ID] {
				merged[o] = append(merged[o], r)
			}
		}
	}
	sealed := cut
	if old != nil {
		sealed = max(sealed, old.sealed())
	}
	seq, err := e.nextSeq()
	if err != nil {
		return err
	}
	bf, err := writeBlock(e.si, e.path(d, seq), d, d+dayMs, sealed, seq, merged)
	if err != nil {
		return err
	}
	e.swap(bf, old)
	return e.head.forget(ctx, d, cut)
}

func (e *blockEngine) insert(ctx context.Context, owner string, recs []Record) ([]int64, error) {
	ids, err := e.head.insert(ctx, owner, recs)
	if err != nil || e.si.dedupe < 0 {
		return ids, err
	}
	return ids, nil
}

func (e *blockEngine) maintain(ctx context.Context, now int64) error {
	e.wmu.Lock()
	defer e.wmu.Unlock()
	target := (now - sealLag) / hourMs * hourMs
	days, err := e.head.days(ctx, target)
	if err != nil {
		return err
	}
	for _, d := range days {
		if err := e.reseal(ctx, d, target); err != nil {
			return err
		}
	}
	if e.head.head {
		e.head.wmu.Lock()
		_, err = e.head.write.ExecContext(ctx, `DELETE FROM seen WHERE t < ?`, now-seenKeep)
		e.head.wmu.Unlock()
	}
	return err
}

// match lists the rows of a block's owner that a query selects.
func (e *blockEngine) match(r *reader, q Query, withTime bool) ([]int, error) {
	or, ok := r.bf.owner(q.Owner)
	if !ok {
		return nil, nil
	}
	n := r.bf.h.Rows
	// Rows of a block lie in its day, so a day inside the window needs no time check.
	var ts []int64
	if withTime && q.End != 0 && !inside(r.bf, q) {
		var err error
		if ts, err = r.ints("t", n); err != nil {
			return nil, err
		}
	}
	si := e.si
	code := func(v string) (uint32, bool) {
		i := sort.SearchStrings(r.dict, v)
		if i < len(r.dict) && r.dict[i] == v {
			return uint32(i) + 1, true
		}
		return 0, false
	}
	// Each filter becomes a lookup by code over the column's resolved codes.
	type fset struct {
		codes []uint32
		set   []bool
		not   bool
	}
	var fs []fset
	for _, f := range q.Filters {
		set := make([]bool, len(r.dict)+1)
		have := false
		for _, v := range f.Values {
			if c, ok := code(v); ok {
				set[c] = true
				have = true
			}
		}
		switch {
		case !have && f.Not:
			continue
		case !have:
			return nil, nil
		}
		codes, err := r.dictCodes(si.col(f.Col))
		if err != nil {
			return nil, err
		}
		fs = append(fs, fset{codes, set, f.Not})
	}
	var hit []bool
	var in [][]uint32
	if q.Contains != "" {
		lq := strings.ToLower(q.Contains)
		hit = make([]bool, len(r.dict)+1)
		anyHit := false
		for i, s := range r.dict {
			if containsFold(s, lq) {
				hit[i+1] = true
				anyHit = true
			}
		}
		if !anyHit {
			return nil, nil
		}
		for _, c := range q.In {
			codes, err := r.dictCodes(si.col(c))
			if err != nil {
				return nil, err
			}
			in = append(in, codes)
		}
	}
	var out []int
rows:
	for i := or.Lo; i < or.Hi; i++ {
		if ts != nil && (ts[i] < q.Start || ts[i] >= q.End) {
			continue
		}
		for _, f := range fs {
			if f.set[f.codes[i]] == f.not {
				continue rows
			}
		}
		if hit != nil {
			found := false
			for _, codes := range in {
				if hit[codes[i]] {
					found = true
					break
				}
			}
			if !found {
				continue
			}
		}
		out = append(out, i)
	}
	return out, nil
}

func overlaps(bf *blockFile, q Query) bool {
	return q.End == 0 || (bf.h.Start < q.End && bf.h.End > q.Start)
}

// inside reports whether a block's whole day lies in q's window.
func inside(bf *blockFile, q Query) bool {
	return q.End == 0 || (bf.h.Start >= q.Start && bf.h.End <= q.End)
}

type cand struct {
	s   string
	n   int64
	id  int64
	r   *reader // nil: a head row
	row int
}

// topK keeps the first k candidates in result order (k 0: all of them). With
// k set it is a heap whose root is the worst candidate kept.
type topK struct {
	k      int
	before func(x, y *cand) bool
	items  []cand
}

func (t *topK) Len() int           { return len(t.items) }
func (t *topK) Less(i, j int) bool { return t.before(&t.items[j], &t.items[i]) }
func (t *topK) Swap(i, j int)      { t.items[i], t.items[j] = t.items[j], t.items[i] }
func (t *topK) Push(x any)         { t.items = append(t.items, x.(cand)) }
func (t *topK) Pop() any {
	x := t.items[len(t.items)-1]
	t.items = t.items[:len(t.items)-1]
	return x
}

func (t *topK) full() bool { return t.k > 0 && len(t.items) >= t.k }

// worst is the last candidate kept; only meaningful when full.
func (t *topK) worst() *cand { return &t.items[0] }

func (t *topK) add(c cand) {
	switch {
	case t.k == 0:
		t.items = append(t.items, c)
	case len(t.items) < t.k:
		heap.Push(t, c)
	case t.before(&c, &t.items[0]):
		t.items[0] = c
		heap.Fix(t, 0)
	}
}

// sorted returns the kept candidates in result order.
func (t *topK) sorted() []cand {
	sort.Slice(t.items, func(a, b int) bool { return t.before(&t.items[a], &t.items[b]) })
	return t.items
}

func (e *blockEngine) find(ctx context.Context, q Query) ([]Record, int64, error) {
	k := 0
	if q.Limit > 0 {
		k = q.Offset + q.Limit
	}
	// The head answers its own first k keys and its count in SQL; only the
	// head rows that land on the page are read in full, at the end.
	head, total, err := e.head.findKeys(ctx, q, k)
	if err != nil {
		return nil, 0, err
	}
	si := e.si
	sortCol := si.col(q.Sort)
	isInt := q.Sort != "" && si.Columns[sortCol].Kind == Int
	strKey := q.Sort != "" && !isInt
	top := &topK{k: k, before: func(x, y *cand) bool {
		c := 0
		if strKey {
			c = strings.Compare(x.s, y.s)
		} else {
			c = cmp64(x.n, y.n)
		}
		if c == 0 {
			c = cmp64(x.id, y.id)
		}
		if q.Desc {
			return c > 0
		}
		return c < 0
	}}
	for _, h := range head {
		c := cand{id: h.id, row: -1, s: h.s}
		if q.Sort == "" {
			c.n = h.t
		} else if isInt {
			c.n = h.n
		}
		top.add(c)
	}
	var readers []*reader
	defer func() {
		for _, r := range readers {
			r.close()
		}
	}()
	var blocks []*blockFile
	for _, bf := range e.snapshot() {
		if _, ok := bf.owner(q.Owner); ok && overlaps(bf, q) {
			blocks = append(blocks, bf)
		}
	}
	if q.Sort == "" && q.Desc {
		// Newest day first, so a time-sorted page fills early.
		for i, j := 0, len(blocks)-1; i < j; i, j = i+1, j-1 {
			blocks[i], blocks[j] = blocks[j], blocks[i]
		}
	}
	plain := len(q.Filters) == 0 && q.Contains == ""
	// past says no row of a block can enter a full time-sorted page.
	past := func(bf *blockFile) bool {
		if q.Sort != "" || !top.full() {
			return false
		}
		w := top.worst().n
		if q.Desc {
			return bf.h.End <= w
		}
		return bf.h.Start > w
	}
	for _, bf := range blocks {
		or, _ := bf.owner(q.Owner)
		if plain && inside(bf, q) && past(bf) {
			total += int64(or.Hi - or.Lo) // counted from the header, nothing read
			continue
		}
		r, err := e.open(bf)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return nil, 0, err
		}
		readers = append(readers, r)
		rows, err := e.match(r, q, true)
		if err != nil {
			return nil, 0, err
		}
		total += int64(len(rows))
		if len(rows) == 0 || past(bf) {
			continue
		}
		n := bf.h.Rows
		ids, err := r.ints("id", n)
		if err != nil {
			return nil, 0, err
		}
		var keyN []int64
		var keyC []uint32
		switch {
		case q.Sort == "":
			keyN, err = r.ints("t", n)
		case isInt:
			keyN, err = r.ints("c."+q.Sort, n)
		default:
			keyC, err = r.dictCodes(sortCol)
		}
		if err != nil {
			return nil, 0, err
		}
		for _, i := range rows {
			c := cand{id: ids[i], r: r, row: i}
			if keyN != nil {
				c.n = keyN[i]
			} else {
				c.s = r.dictStr(keyC[i])
			}
			top.add(c)
		}
	}
	page := top.sorted()
	page = page[min(q.Offset, len(page)):]
	out := make([]Record, len(page))
	var headIDs []int64
	var headAt []int
	for i, c := range page {
		if c.r == nil {
			headIDs = append(headIDs, c.id)
			headAt = append(headAt, i)
			continue
		}
		rec, err := c.r.record(c.row, nil, q.Children, nil)
		if err != nil {
			return nil, 0, err
		}
		out[i] = rec
	}
	if len(headIDs) > 0 {
		recs, err := e.head.getMany(ctx, q.Owner, headIDs, q.Children)
		if err != nil {
			return nil, 0, err
		}
		if len(recs) != len(headIDs) {
			// Sealed between the two reads: answer again from the new state.
			return e.find(ctx, q)
		}
		for i, at := range headAt {
			out[at] = recs[i]
		}
	}
	return out, total, nil
}

// groupAcc merges partial groups from the head and from blocks.
type groupAcc struct {
	a      Agg
	groups map[groupKey]*Group
	dist   map[groupKey]map[string]struct{}
}

type groupKey struct {
	key    string
	bucket int64
}

func newGroupAcc(a Agg) *groupAcc {
	return &groupAcc{a: a, groups: map[groupKey]*Group{}, dist: map[groupKey]map[string]struct{}{}}
}

func (g *groupAcc) group(k groupKey) *Group {
	gr := g.groups[k]
	if gr == nil {
		gr = &Group{Key: k.key, Bucket: k.bucket, Sums: make([]int64, len(g.a.Sum)), Max: make([]string, len(g.a.Max)), MinTime: math.MaxInt64, MaxTime: math.MinInt64}
		g.groups[k] = gr
	}
	return gr
}

// merge adds a partial group (its Distinct is ignored; see distinct).
func (g *groupAcc) merge(p *Group) {
	gr := g.group(groupKey{p.Key, p.Bucket})
	gr.Count += p.Count
	for i := range gr.Sums {
		gr.Sums[i] += p.Sums[i]
	}
	for i := range gr.Max {
		gr.Max[i] = max(gr.Max[i], p.Max[i])
	}
	gr.MinTime, gr.MaxTime = min(gr.MinTime, p.MinTime), max(gr.MaxTime, p.MaxTime)
}

func (g *groupAcc) distinct(k groupKey, v string) {
	if v == "" {
		return
	}
	set := g.dist[k]
	if set == nil {
		set = map[string]struct{}{}
		g.dist[k] = set
	}
	set[v] = struct{}{}
}

func (g *groupAcc) result() []Group {
	out := make([]Group, 0, len(g.groups))
	for k, gr := range g.groups {
		if gr.Count == 0 {
			continue
		}
		gr.Distinct = int64(len(g.dist[k]))
		if !g.a.Times {
			gr.MinTime, gr.MaxTime = 0, 0
		}
		out = append(out, *gr)
	}
	return finishGroups(out, g.a)
}

func (e *blockEngine) aggregate(ctx context.Context, q Query, a Agg) ([]Group, error) {
	acc := newGroupAcc(a)
	// The head aggregates in SQL; distinct values come back as values, since
	// distinct counts do not add up.
	ha := a
	ha.Top, ha.Distinct = 0, ""
	hg, err := e.head.aggregate(ctx, q, ha)
	if err != nil {
		return nil, err
	}
	for i := range hg {
		acc.merge(&hg[i])
	}
	if a.Distinct != "" {
		if err := e.head.distinctValues(ctx, q, a, acc.distinct); err != nil {
			return nil, err
		}
	}
	si := e.si
	by, dist := -1, -1
	if a.By != "" {
		by = si.col(a.By)
	}
	if a.Distinct != "" {
		dist = si.col(a.Distinct)
	}
	maxCols := make([]int, len(a.Max))
	for i, m := range a.Max {
		maxCols[i] = si.col(m)
	}
	type lkey struct {
		code   uint32
		bucket int64
	}
	type lacc struct {
		count  int64
		sums   []int64
		max    []uint32
		mn, mx int64
		dist   map[uint32]struct{}
	}
	for _, bf := range e.snapshot() {
		if _, ok := bf.owner(q.Owner); !ok || !overlaps(bf, q) {
			continue
		}
		r, err := e.open(bf)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return nil, err
		}
		rows, err := e.match(r, q, true)
		if err != nil {
			r.close()
			return nil, err
		}
		if len(rows) == 0 {
			r.close()
			continue
		}
		n := bf.h.Rows
		ts, err := r.ints("t", n)
		if err != nil {
			r.close()
			return nil, err
		}
		// Each sum reads one column: an Int column, or the child counts.
		sums := make([][]int64, len(a.Sum))
		var nch []uint32
		for i, sname := range a.Sum {
			if sname == "#children" {
				if nch, err = r.nchild(); err != nil {
					r.close()
					return nil, err
				}
				continue
			}
			if sums[i], err = r.ints("c."+sname, n); err != nil {
				r.close()
				return nil, err
			}
		}
		// Resolve every Dict column the groups read once per block.
		var byC, distC []uint32
		maxC := make([][]uint32, len(maxCols))
		if by >= 0 {
			byC, err = r.dictCodes(by)
		}
		if err == nil && dist >= 0 {
			distC, err = r.dictCodes(dist)
		}
		for j, c := range maxCols {
			if err == nil {
				maxC[j], err = r.dictCodes(c)
			}
		}
		if err != nil {
			r.close()
			return nil, err
		}
		local := map[lkey]*lacc{}
		for _, i := range rows {
			var k lkey
			if byC != nil {
				k.code = byC[i]
			}
			if a.Width > 0 {
				k.bucket = ((ts[i]+a.Offset)/a.Width)*a.Width - a.Offset
			}
			l := local[k]
			if l == nil {
				l = &lacc{sums: make([]int64, len(a.Sum)), max: make([]uint32, len(a.Max)), mn: math.MaxInt64, mx: math.MinInt64}
				local[k] = l
			}
			l.count++
			for j := range a.Sum {
				if sums[j] != nil {
					l.sums[j] += sums[j][i]
				} else {
					l.sums[j] += int64(nch[i])
				}
			}
			for j, codes := range maxC {
				// Dictionaries are sorted, so the greatest code is the greatest string.
				l.max[j] = max(l.max[j], codes[i])
			}
			if distC != nil {
				if l.dist == nil {
					l.dist = map[uint32]struct{}{}
				}
				l.dist[distC[i]] = struct{}{}
			}
			l.mn, l.mx = min(l.mn, ts[i]), max(l.mx, ts[i])
		}
		for k, l := range local {
			key := ""
			if by >= 0 {
				key = r.dictStr(k.code)
			}
			p := Group{Key: key, Bucket: k.bucket, Count: l.count, Sums: l.sums, Max: make([]string, len(a.Max)), MinTime: l.mn, MaxTime: l.mx}
			for j, code := range l.max {
				p.Max[j] = r.dictStr(code)
			}
			acc.merge(&p)
			gk := groupKey{key, k.bucket}
			for code := range l.dist {
				acc.distinct(gk, r.dictStr(code))
			}
		}
		r.close()
	}
	return acc.result(), nil
}

func (e *blockEngine) get(ctx context.Context, owner string, id int64) (*Record, error) {
	rec, err := e.head.get(ctx, owner, id)
	if err != ErrNotFound {
		return rec, err
	}
	for _, bf := range e.snapshot() {
		or, ok := bf.owner(owner)
		if !ok || id < or.MinID || id > or.MaxID {
			continue
		}
		r, err := e.open(bf)
		if err != nil {
			return nil, err
		}
		ids, err := r.ints("id", bf.h.Rows)
		if err != nil {
			r.close()
			return nil, err
		}
		for i := or.Lo; i < or.Hi; i++ {
			if ids[i] == id {
				rec, err := r.record(i, nil, true, nil)
				r.close()
				return &rec, err
			}
		}
		r.close()
	}
	return nil, ErrNotFound
}

func (e *blockEngine) active(ctx context.Context, q Query, now int64) ([]Record, error) {
	if e.si.activeCh < 0 {
		return []Record{}, nil
	}
	out, err := e.head.active(ctx, q, now)
	if err != nil {
		return nil, err
	}
	ac := e.si.activeCh
	keep := func(c *Child) bool { return c.N[ac] > now }
	for _, bf := range e.snapshot() {
		if bf.h.MaxActive <= now {
			continue
		}
		if _, ok := bf.owner(q.Owner); !ok {
			continue
		}
		r, err := e.open(bf)
		if err != nil {
			return nil, err
		}
		rows, err := e.match(r, q, false)
		if err != nil {
			r.close()
			return nil, err
		}
		for _, i := range rows {
			rec, err := r.record(i, nil, true, keep)
			if err != nil {
				r.close()
				return nil, err
			}
			if len(rec.Children) > 0 {
				out = append(out, rec)
			}
		}
		r.close()
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (e *blockEngine) prune(ctx context.Context, before int64, owners []string) (err error) {
	e.wmu.Lock()
	defer e.wmu.Unlock()
	// A block deleted with no replacement must stay deleted after a power
	// cut: no higher seq would hide it if its directory entry came back. One
	// directory sync covers every deletion of the pass.
	deleted := false
	defer func() {
		if deleted {
			if serr := syncDir(filepath.Join(e.dir, "blocks")); err == nil {
				err = serr
			}
		}
	}()
	if err := e.head.prune(ctx, before, owners); err != nil {
		return err
	}
	var keep map[string]bool
	if owners != nil {
		keep = map[string]bool{}
		for _, o := range owners {
			keep[o] = true
		}
	}
	for _, bf := range e.snapshot() {
		if before > 0 && bf.h.End <= before {
			e.swap(nil, bf)
			deleted = true
			continue
		}
		rewrite := before > bf.h.Start && before > 0
		for _, or := range bf.h.Owners {
			rewrite = rewrite || (keep != nil && !keep[or.Owner])
		}
		if !rewrite {
			continue
		}
		all, err := e.readAll(bf)
		if err != nil {
			return err
		}
		kept := map[string][]Record{}
		rows := 0
		for o, list := range all {
			if keep != nil && !keep[o] {
				continue
			}
			for _, r := range list {
				if before == 0 || r.Time >= before {
					kept[o] = append(kept[o], r)
					rows++
				}
			}
		}
		if rows == 0 {
			e.swap(nil, bf)
			deleted = true
			continue
		}
		seq, err := e.nextSeq()
		if err != nil {
			return err
		}
		nb, err := writeBlock(e.si, e.path(bf.h.Start, seq), bf.h.Start, bf.h.End, bf.h.Upto, seq, kept)
		if err != nil {
			return err
		}
		e.swap(nb, bf)
	}
	return nil
}

func (e *blockEngine) stats(ctx context.Context) (Stats, error) {
	hs, err := e.head.stats(ctx)
	if err != nil {
		return Stats{}, err
	}
	st := Stats{HeadBytes: hs.FileBytes, HeadRows: hs.HeadRows, Rows: hs.HeadRows}
	for _, bf := range e.snapshot() {
		st.Rows += int64(bf.h.Rows)
		if n, ok := fileSize(bf.path); ok {
			st.BlockBytes += n
			st.Blocks++
		}
	}
	st.FileBytes, st.Files = dirBytes(e.dir)
	return st, nil
}

// oldest is the earlier of the head's oldest row and the first row of the
// owner's earliest block (rows in a block are sorted by owner, then time).
func (e *blockEngine) oldest(ctx context.Context, owner string) (int64, bool, error) {
	t, ok, err := e.head.oldest(ctx, owner)
	if err != nil {
		return 0, false, err
	}
	for _, bf := range e.snapshot() { // sorted by day
		or, has := bf.owner(owner)
		if !has || or.Hi <= or.Lo {
			continue
		}
		r, err := e.open(bf)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return 0, false, err
		}
		ts, err := r.ints("t", bf.h.Rows)
		r.close()
		if err != nil {
			return 0, false, err
		}
		if !ok || ts[or.Lo] < t {
			t, ok = ts[or.Lo], true
		}
		break
	}
	return t, ok, nil
}
