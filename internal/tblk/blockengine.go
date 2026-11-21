package tblk

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// blockEngine keeps unsealed minutes in a SQLite head and everything else in
// immutable block files, like a TSDB: small blocks written often, merged in
// levels, never a big block rewritten on every write.
//
//	minute  15-minute slices sealed from the head; merged into 1 h blocks
//	hour    1 h blocks, the per-hour fold made while merging minutes;
//	        merged into one block per day once the day is complete
//	day     1 day blocks, the per-day fold made while merging hours
//
// Each step reads and writes one (owner, family) group at a time, and folds
// one collector at a time, so memory follows the slice, not the store.
// Retention and the size cap delete whole files.
type blockEngine struct {
	sch  *schemaInfo
	dir  string
	head *sqlEngine
	c    *codec

	mu     sync.RWMutex
	blocks map[Grain][]*blockFile // sorted by start, end, seq
	bad    []string
	seq    int64
	sealed int64 // minutes before this are in blocks, not the head
	// dropped counts minutes that arrived for slices already sealed.
	dropped int64
	// late counts Add calls for minutes already sealed, written as blocks.
	late int64

	wmu     sync.Mutex
	pending map[Grain]*builder
}

// sliceMs is how much of the head one seal writes.
const sliceMs = 15 * minuteMs

// tierWindow is the window tier data appended directly (AppendTier) is
// gathered in before it is written as a block.
func tierWindow(g Grain) int64 {
	if g == Minute {
		return hourMs
	}
	return dayMs
}

// newPath names a new block: start-end-seq, with -m when it is a merge.
func (e *blockEngine) newPath(g Grain, start, end int64, merged bool) (string, int64) {
	e.mu.Lock()
	e.seq++
	seq := e.seq
	e.mu.Unlock()
	name := fmt.Sprintf("%d-%d-%d", start, end, seq)
	if merged {
		name += "-m"
	}
	return filepath.Join(e.dir, "blocks", g.String(), name+".blk"), seq
}

func (e *blockEngine) newWriter(g Grain, start, end int64, merged bool) *blockWriter {
	path, seq := e.newPath(g, start, end, merged)
	w := newBlockWriter(e.c, e.sch, path, blockHeader{grain: g, start: start, end: end, covered: end})
	w.seq, w.merged = seq, merged
	return w
}

// parseBlockName reads seq and merged from start-end-seq[-m].blk; older
// start-end.blk names have seq 0.
func parseBlockName(name string) (seq int64, merged bool) {
	parts := strings.Split(strings.TrimSuffix(name, ".blk"), "-")
	if len(parts) >= 3 {
		seq, _ = strconv.ParseInt(parts[2], 10, 64)
	}
	return seq, len(parts) == 4 && parts[3] == "m"
}

func openBlocks(dir string, si *schemaInfo, driver string) (*blockEngine, error) {
	head, err := openSQL(filepath.Join(dir, "head.db"), si, driver, []Grain{Minute})
	if err != nil {
		return nil, err
	}
	c, err := newCodec()
	if err != nil {
		head.close()
		return nil, err
	}
	e := &blockEngine{sch: si, dir: dir, head: head, c: c, blocks: map[Grain][]*blockFile{}, pending: map[Grain]*builder{}}
	if e.sealed, err = head.metaGet(context.Background(), head.read, "sealed_until"); err != nil {
		e.close()
		return nil, err
	}
	for _, g := range grains {
		gd := filepath.Join(dir, "blocks", g.String())
		if err := os.MkdirAll(gd, 0o755); err != nil {
			e.close()
			return nil, err
		}
		ents, err := os.ReadDir(gd)
		if err != nil {
			e.close()
			return nil, err
		}
		var list []*blockFile
		for _, de := range ents {
			p := filepath.Join(gd, de.Name())
			switch {
			case strings.HasSuffix(de.Name(), ".tmp"):
				// A write that never finished; the data is still in its source.
				os.Remove(p)
			case strings.HasSuffix(de.Name(), ".blk"):
				bf, err := openBlock(c, p)
				if err != nil || bf.h.grain != g {
					e.bad = append(e.bad, p)
					continue
				}
				bf.seq, bf.merged = parseBlockName(de.Name())
				e.seq = max(e.seq, bf.seq)
				list = append(list, bf)
			}
		}
		// A merge writes its block before deleting its inputs: inputs still
		// beside a merged block that covers them are left from a crash.
		kept := list[:0]
		for _, bf := range list {
			if supersededBy(bf, list) {
				os.Remove(bf.path)
				continue
			}
			kept = append(kept, bf)
		}
		e.blocks[g] = kept
		sortBlocks(e.blocks[g])
	}
	return e, nil
}

func supersededBy(bf *blockFile, list []*blockFile) bool {
	for _, m := range list {
		if m != bf && m.merged && m.seq > bf.seq && m.h.start <= bf.h.start && bf.h.end <= m.h.end {
			return true
		}
	}
	return false
}

func sortBlocks(list []*blockFile) {
	sort.Slice(list, func(i, j int) bool {
		a, b := list[i], list[j]
		if a.h.start != b.h.start {
			return a.h.start < b.h.start
		}
		if a.h.end != b.h.end {
			return a.h.end < b.h.end
		}
		return a.seq < b.seq
	})
}

func (e *blockEngine) close() error {
	e.c.close()
	return e.head.close()
}

// write writes w's block without recording it.
func (e *blockEngine) write(w *blockWriter) (*blockFile, error) {
	bf, err := w.finish()
	if err != nil {
		return nil, err
	}
	bf.seq, bf.merged = w.seq, w.merged
	return bf, nil
}

// finish writes w's block and records it.
func (e *blockEngine) finish(w *blockWriter) (*blockFile, error) {
	bf, err := e.write(w)
	if err != nil {
		return nil, err
	}
	e.swap([]*blockFile{bf}, nil)
	return bf, nil
}

// swap records add and forgets drop in one step, so a read sees either the
// inputs or their merge, never both; then it deletes drop's files.
func (e *blockEngine) swap(add, drop []*blockFile) error {
	e.mu.Lock()
	for _, bf := range add {
		e.blocks[bf.h.grain] = append(e.blocks[bf.h.grain], bf)
	}
	for _, bf := range drop {
		list := e.blocks[bf.h.grain]
		for i, x := range list {
			if x == bf {
				e.blocks[bf.h.grain] = append(list[:i:i], list[i+1:]...)
				break
			}
		}
	}
	for _, g := range grains {
		sortBlocks(e.blocks[g])
	}
	e.mu.Unlock()
	for _, bf := range drop {
		if err := os.Remove(bf.path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
	}
	return nil
}

func (e *blockEngine) removeBlock(bf *blockFile) error {
	if err := os.Remove(bf.path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	list := e.blocks[bf.h.grain]
	for i, x := range list {
		if x == bf {
			e.blocks[bf.h.grain] = append(list[:i:i], list[i+1:]...)
			break
		}
	}
	return nil
}

func (e *blockEngine) snapshot(g Grain) []*blockFile {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return append([]*blockFile(nil), e.blocks[g]...)
}

// within lists g's blocks inside [a, b).
func (e *blockEngine) within(g Grain, a, b int64) []*blockFile {
	var out []*blockFile
	for _, bf := range e.snapshot(g) {
		if bf.h.start >= a && bf.h.end <= b {
			out = append(out, bf)
		}
	}
	return out
}

func coversLocked(list []*blockFile, a, b int64) bool {
	for _, bf := range list {
		if bf.h.start <= a && bf.h.end >= b {
			return true
		}
	}
	return false
}

// covers reports whether one of g's blocks spans all of [a, b).
func (e *blockEngine) covers(g Grain, a, b int64) bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return coversLocked(e.blocks[g], a, b)
}

func (e *blockEngine) overlaps(g Grain, a, b int64) bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	for _, bf := range e.blocks[g] {
		if bf.h.start < b && bf.h.end > a {
			return true
		}
	}
	return false
}

// builder gathers tier rows appended directly, for one window.
type builder struct {
	g          Grain
	start, end int64
	// rows may repeat a key; the writer sums them once sorted.
	rows blockRows
}

func newBuilder(g Grain, start int64) *builder {
	return &builder{g: g, start: start, end: start + tierWindow(g), rows: blockRows{}}
}

func (b *builder) add(owner, family string, r rec) {
	gk := [2]string{owner, family}
	b.rows[gk] = append(b.rows[gk], r)
}

// blockReader is an open block with its dictionary.
type blockReader struct {
	fh   *os.File
	dict []string
	bf   *blockFile
}

func (e *blockEngine) openReader(bf *blockFile) (*blockReader, error) {
	fh, err := os.Open(bf.path)
	if err != nil {
		return nil, err
	}
	dict, err := readDict(e.c, fh, bf)
	if err != nil {
		fh.Close()
		return nil, err
	}
	return &blockReader{fh: fh, dict: dict, bf: bf}, nil
}

func (r *blockReader) close() { r.fh.Close() }

// groupData is one group's decoded columns: compact until recs builds rows.
type groupData struct {
	f          *famInfo
	dict       []string
	start, gms int64
	col, bk    []uint32
	keys       [][]uint32
	vals       [][]int64
}

func (e *blockEngine) readGroup(r *blockReader, g *groupRef) (*groupData, error) {
	f := e.sch.byName[g.family]
	if f == nil || len(g.cols) != 2+len(f.Keys)+len(f.Measures) {
		return nil, errBadBlock
	}
	gd := &groupData{f: f, dict: r.dict, start: r.bf.h.start, gms: r.bf.h.grain.Ms(),
		keys: make([][]uint32, len(f.Keys)), vals: make([][]int64, len(f.Measures))}
	var err error
	if gd.col, err = readRLE(e.c, r.fh, g.cols[0], g.rows); err != nil {
		return nil, err
	}
	if gd.bk, err = readUvarints(e.c, r.fh, g.cols[1], g.rows); err != nil {
		return nil, err
	}
	for k := range gd.keys {
		if gd.keys[k], err = readUvarints(e.c, r.fh, g.cols[2+k], g.rows); err != nil {
			return nil, err
		}
	}
	for m := range gd.vals {
		if gd.vals[m], err = readVarints(e.c, r.fh, g.cols[2+len(f.Keys)+m], g.rows); err != nil {
			return nil, err
		}
	}
	return gd, nil
}

// colRun is the rows [lo, hi) of one collector; rows are sorted by collector.
type colRun struct {
	collector string
	lo, hi    int
}

func (gd *groupData) runs() []colRun {
	var out []colRun
	for i := 0; i < len(gd.col); {
		j := i
		for j < len(gd.col) && gd.col[j] == gd.col[i] {
			j++
		}
		out = append(out, colRun{gd.dict[gd.col[i]], i, j})
		i = j
	}
	return out
}

// recs builds rows [lo, hi), with their keys and values in one slab each.
func (gd *groupData) recs(lo, hi int) []rec {
	nk, nm := len(gd.keys), len(gd.vals)
	keySlab := make([]string, (hi-lo)*nk)
	valSlab := make([]int64, (hi-lo)*nm)
	out := make([]rec, 0, hi-lo)
	for i := lo; i < hi; i++ {
		r := rec{collector: gd.dict[gd.col[i]], bucket: gd.start + int64(gd.bk[i])*gd.gms,
			keys: keySlab[:nk:nk], vals: valSlab[:nm:nm]}
		keySlab, valSlab = keySlab[nk:], valSlab[nm:]
		for k := range gd.keys {
			r.keys[k] = gd.dict[gd.keys[k][i]]
		}
		for m := range gd.vals {
			r.vals[m] = gd.vals[m][i]
		}
		out = append(out, r)
	}
	return out
}

// readAll calls fn for every row of a block.
func (e *blockEngine) readAll(bf *blockFile, fn func(owner, family string, r rec)) error {
	r, err := e.openReader(bf)
	if err != nil {
		return err
	}
	defer r.close()
	for i := range bf.group {
		g := &bf.group[i]
		gd, err := e.readGroup(r, g)
		if err != nil {
			return err
		}
		for _, x := range gd.recs(0, len(gd.col)) {
			fn(g.owner, g.family, x)
		}
	}
	return nil
}

// compactOut is one block a compaction writes. A fold output sums each
// collector's rows into bucket at and keeps that grain's top-N; otherwise
// rows are merged as they are.
type compactOut struct {
	w    *blockWriter
	fold bool
	g    Grain
	at   int64
}

// compact merges inputs into outs one (owner, family) group and one
// collector at a time.
func (e *blockEngine) compact(inputs []*blockFile, outs []*compactOut) error {
	// Folds read the rows before a merge output sorts and shortens them.
	sort.SliceStable(outs, func(i, j int) bool { return outs[i].fold && !outs[j].fold })
	readers := make([]*blockReader, len(inputs))
	for i, bf := range inputs {
		r, err := e.openReader(bf)
		if err != nil {
			for _, x := range readers[:i] {
				x.close()
			}
			return err
		}
		readers[i] = r
	}
	defer func() {
		for _, r := range readers {
			r.close()
		}
	}()
	seen := map[[2]string]bool{}
	var gkeys [][2]string
	for _, bf := range inputs {
		for _, g := range bf.group {
			gk := [2]string{g.owner, g.family}
			if !seen[gk] {
				seen[gk] = true
				gkeys = append(gkeys, gk)
			}
		}
	}
	sort.Slice(gkeys, func(i, j int) bool {
		if gkeys[i][0] != gkeys[j][0] {
			return gkeys[i][0] < gkeys[j][0]
		}
		return gkeys[i][1] < gkeys[j][1]
	})
	type part struct {
		gd   *groupData
		runs []colRun
	}
	for _, gk := range gkeys {
		f := e.sch.byName[gk[1]]
		if f == nil {
			return errBadBlock
		}
		var parts []part
		colSet := map[string]bool{}
		for i, bf := range inputs {
			g := bf.find(gk[0], gk[1])
			if g == nil || g.rows == 0 {
				continue
			}
			gd, err := e.readGroup(readers[i], g)
			if err != nil {
				return err
			}
			p := part{gd: gd, runs: gd.runs()}
			for _, r := range p.runs {
				colSet[r.collector] = true
			}
			parts = append(parts, p)
		}
		cols := make([]string, 0, len(colSet))
		for c := range colSet {
			cols = append(cols, c)
		}
		sort.Strings(cols)
		for _, c := range cols {
			var rows []rec
			for _, p := range parts {
				for _, r := range p.runs {
					if r.collector == c {
						rows = append(rows, p.gd.recs(r.lo, r.hi)...)
					}
				}
			}
			for _, o := range outs {
				out := rows
				if o.fold {
					out = rollRows(f, o.g, o.at, rows)
				}
				if err := o.w.add(gk[0], gk[1], out); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func (e *blockEngine) append(ctx context.Context, owner, collector string, f *famInfo, g Grain, bucket int64, rows []rec) error {
	if g == Minute {
		e.mu.RLock()
		sealed := e.sealed
		e.mu.RUnlock()
		if bucket < sealed {
			e.mu.Lock()
			e.dropped++
			e.mu.Unlock()
			return nil
		}
		return e.head.append(ctx, owner, collector, f, Minute, bucket, rows)
	}
	e.wmu.Lock()
	defer e.wmu.Unlock()
	start := bucket - bucket%tierWindow(g)
	b := e.pending[g]
	if b != nil && b.start != start {
		if err := e.flush(b); err != nil {
			return err
		}
		b = nil
	}
	if b == nil {
		b = newBuilder(g, start)
		e.pending[g] = b
	}
	for _, r := range rows {
		b.add(owner, f.Name, r)
	}
	return nil
}

// add sums minute rows into the head, under the write lock so a seal cannot
// slip in between the check and the write. A minute already sealed goes into
// an hour-wide minute block of its own: an hour not yet compacted takes it in
// when it is; for an hour already folded, the hour rows (and the day rows, if
// the day is folded too) are written beside it. Maintenance later merges such
// stray minute and hour blocks into their hour and day.
func (e *blockEngine) add(ctx context.Context, owner, collector string, f *famInfo, bucket int64, rows []rec) error {
	e.wmu.Lock()
	defer e.wmu.Unlock()
	e.mu.RLock()
	sealed := e.sealed
	e.mu.RUnlock()
	if bucket >= sealed {
		return e.head.add(ctx, owner, collector, f, bucket, rows)
	}
	h, d := bucket-bucket%hourMs, bucket-bucket%dayMs
	one := func(g Grain, start, end int64, rs []rec) error {
		w := e.newWriter(g, start, end, false)
		if err := w.add(owner, f.Name, rs); err != nil {
			return err
		}
		_, err := e.finish(w)
		return err
	}
	// Coarser tiers are checked before the minute block exists, so the
	// minute block cannot make its own hour look folded.
	hourDone, dayDone := e.covers(Hour, h, h+hourMs), e.overlaps(Day, d, d+dayMs)
	if err := one(Minute, h, h+hourMs, rows); err != nil {
		return err
	}
	if hourDone {
		if err := one(Hour, h, h+hourMs, sumRecs(rows, h)); err != nil {
			return err
		}
	}
	if dayDone {
		if err := one(Day, d, d+dayMs, sumRecs(rows, d)); err != nil {
			return err
		}
	}
	e.mu.Lock()
	e.late++
	e.mu.Unlock()
	return nil
}

// flush writes a pending tier window as its own block; maintenance merges it
// with other blocks of the same window later.
func (e *blockEngine) flush(b *builder) error {
	delete(e.pending, b.g)
	if len(b.rows) == 0 {
		return nil
	}
	w := e.newWriter(b.g, b.start, b.end, false)
	for gk, list := range b.rows {
		if err := w.add(gk[0], gk[1], list); err != nil {
			return err
		}
		delete(b.rows, gk)
	}
	_, err := e.finish(w)
	return err
}

func (e *blockEngine) flushTier(ctx context.Context) error {
	e.wmu.Lock()
	defer e.wmu.Unlock()
	for _, g := range []Grain{Hour, Day} {
		if b := e.pending[g]; b != nil {
			if err := e.flush(b); err != nil {
				return err
			}
		}
	}
	return nil
}

// minutePending reports whether hour h still has minute data not yet in an
// hour block: slices not merged, or a merged hour without its fold.
func minutePendingLocked(minutes, hours []*blockFile, h int64) bool {
	n := 0
	for _, bf := range minutes {
		if bf.h.start >= h && bf.h.end <= h+hourMs {
			n++
		}
	}
	// One block is folded once the hour tier covers the hour, even when it
	// is a single slice (a quiet hour has nothing to merge it with); more
	// than one means rows not merged yet.
	return n > 1 || (n == 1 && !coversLocked(hours, h, h+hourMs))
}

// strayLocked reports whether hour h holds more than one minute block, as
// late rows leave beside a merged hour; compactHour merges them.
func strayLocked(minutes []*blockFile, h int64) bool {
	n := 0
	for _, bf := range minutes {
		if bf.h.start >= h && bf.h.end <= h+hourMs {
			n++
		}
	}
	return n > 1
}

// rolled is how far the hour and day tiers reach. The hour mark stops at the
// first hour whose minutes are not folded yet, so reads take minutes there.
func (e *blockEngine) rolled(ctx context.Context) (int64, int64, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	hour, day := e.sealed-e.sealed%hourMs, int64(0)
	for _, bf := range e.blocks[Hour] {
		hour = max(hour, bf.h.covered)
	}
	for _, bf := range e.blocks[Minute] {
		h := bf.h.start - bf.h.start%hourMs
		if h < hour && minutePendingLocked(e.blocks[Minute], e.blocks[Hour], h) {
			hour = h
		}
	}
	for _, bf := range e.blocks[Day] {
		day = max(day, bf.h.covered)
	}
	return hour, day, nil
}

// seal moves slice [s, s+sliceMs) from the head into a minute block, one
// family at a time. The file is written before the head forgets the slice;
// after a crash in between, the head still has the rows, so a block already
// there for the slice is a partial copy and is replaced.
func (e *blockEngine) seal(ctx context.Context, s int64) error {
	owners, err := e.head.owners(ctx, e.head.read)
	if err != nil {
		return err
	}
	var stale []*blockFile
	for _, bf := range e.within(Minute, s, s+sliceMs) {
		if bf.h.start == s && bf.h.end == s+sliceMs {
			stale = append(stale, bf)
		}
	}
	w := e.newWriter(Minute, s, s+sliceMs, false)
	for _, o := range owners {
		for _, f := range e.sch.fams {
			rows, err := e.head.dump(ctx, e.head.read, f, Minute, o.id, s, s+sliceMs, true)
			if err != nil {
				return err
			}
			if err := w.add(o.label, f.Name, rows); err != nil {
				return err
			}
		}
	}
	var add []*blockFile
	if !w.empty() {
		bf, err := e.write(w)
		if err != nil {
			return err
		}
		add = append(add, bf)
	}
	if err := e.swap(add, stale); err != nil {
		return err
	}
	// The head forgets the slice and moves its seal in one transaction.
	e.head.wmu.Lock()
	defer e.head.wmu.Unlock()
	tx, err := e.head.write.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, o := range owners {
		for _, f := range e.sch.fams {
			if _, err := tx.ExecContext(ctx, `DELETE FROM `+tableOf(f, Minute)+` WHERE owner = ? AND bucket >= ? AND bucket < ?`, o.id, s, s+sliceMs); err != nil {
				return err
			}
		}
	}
	if err := metaSet(ctx, tx, "sealed_until", s+sliceMs); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	e.mu.Lock()
	e.sealed = s + sliceMs
	e.mu.Unlock()
	return nil
}

// compactHour merges hour h's minute slices into one minute block and writes
// the hour's fold, whichever of the two is still missing.
func (e *blockEngine) compactHour(h int64) (bool, error) {
	inputs := e.within(Minute, h, h+hourMs)
	if len(inputs) == 0 {
		return false, nil
	}
	var outs []*compactOut
	needFold := !e.covers(Hour, h, h+hourMs)
	if needFold {
		outs = append(outs, &compactOut{w: e.newWriter(Hour, h, h+hourMs, false), fold: true, g: Hour, at: h})
	}
	merge := len(inputs) > 1
	if merge {
		outs = append(outs, &compactOut{w: e.newWriter(Minute, h, h+hourMs, true)})
	}
	if len(outs) == 0 {
		return false, nil
	}
	return true, e.runCompaction(inputs, outs, merge)
}

// compactDay merges day d's hour blocks into one, and writes the day's fold
// when the day tier has nothing for d yet.
func (e *blockEngine) compactDay(d int64) (bool, error) {
	inputs := e.within(Hour, d, d+dayMs)
	if len(inputs) == 0 {
		return false, nil
	}
	var outs []*compactOut
	if !e.overlaps(Day, d, d+dayMs) {
		outs = append(outs, &compactOut{w: e.newWriter(Day, d, d+dayMs, false), fold: true, g: Day, at: d})
	}
	merge := len(inputs) > 1
	if merge {
		outs = append(outs, &compactOut{w: e.newWriter(Hour, d, d+dayMs, true)})
	}
	if len(outs) == 0 {
		return false, nil
	}
	return true, e.runCompaction(inputs, outs, merge)
}

// runCompaction writes every output before it deletes the inputs: a crash
// leaves inputs beside a merged block, and open removes them.
func (e *blockEngine) runCompaction(inputs []*blockFile, outs []*compactOut, dropInputs bool) error {
	if err := e.compact(inputs, outs); err != nil {
		return err
	}
	// Folds are written first, so a crash before the merge is written redoes
	// only the merge.
	var add []*blockFile
	for _, o := range outs {
		if o.w.empty() {
			continue
		}
		bf, err := e.write(o.w)
		if err != nil {
			return err
		}
		add = append(add, bf)
	}
	var drop []*blockFile
	if dropInputs {
		drop = inputs
	}
	return e.swap(add, drop)
}

func (e *blockEngine) maintain(ctx context.Context, now int64, limit int) (MaintainResult, error) {
	var res MaintainResult
	e.wmu.Lock()
	defer e.wmu.Unlock()
	e.mu.RLock()
	sealed := e.sealed
	e.mu.RUnlock()
	if sealed == 0 {
		oldest, ok, err := e.head.minBucket(ctx, e.head.read, Minute)
		if err != nil {
			return res, err
		}
		if ok {
			sealed = oldest - oldest%hourMs
		}
	}
	target := now - rollLag
	target -= target % sliceMs
	for s := sealed; sealed > 0 && s < target && ctx.Err() == nil; s += sliceMs {
		if err := e.seal(ctx, s); err != nil {
			return res, err
		}
		if (s+sliceMs)%hourMs == 0 {
			if res.Hours >= limit {
				break
			}
			if _, err := e.compactHour(s + sliceMs - hourMs); err != nil {
				return res, err
			}
			res.Hours++
		}
	}
	// Hours left from before (a crash, or tier data): any complete hour with
	// minute work not yet folded.
	e.mu.RLock()
	sealed = e.sealed
	var hours []int64
	for _, bf := range e.blocks[Minute] {
		h := bf.h.start - bf.h.start%hourMs
		if h+hourMs <= sealed && (minutePendingLocked(e.blocks[Minute], e.blocks[Hour], h) || strayLocked(e.blocks[Minute], h)) && (len(hours) == 0 || hours[len(hours)-1] != h) {
			hours = append(hours, h)
		}
	}
	e.mu.RUnlock()
	for _, h := range hours {
		if res.Hours >= limit || ctx.Err() != nil {
			break
		}
		did, err := e.compactHour(h)
		if err != nil {
			return res, err
		}
		if did {
			res.Hours++
		}
	}
	// Complete days: merge their hour blocks and fold the day.
	hourRolled, _, _ := e.rolled(ctx)
	var days []int64
	for _, bf := range e.snapshot(Hour) {
		d := bf.h.start - bf.h.start%dayMs
		if d+dayMs > hourRolled || (len(days) > 0 && days[len(days)-1] == d) {
			continue
		}
		if len(e.within(Hour, d, d+dayMs)) > 1 || !e.overlaps(Day, d, d+dayMs) {
			days = append(days, d)
		}
	}
	for _, d := range days {
		if res.Days >= limit || ctx.Err() != nil {
			break
		}
		did, err := e.compactDay(d)
		if err != nil {
			return res, err
		}
		if did {
			res.Days++
		}
	}
	// Day blocks of the same day written apart (tier data): merge them.
	byDay := map[int64][]*blockFile{}
	for _, bf := range e.snapshot(Day) {
		byDay[bf.h.start] = append(byDay[bf.h.start], bf)
	}
	for d, list := range byDay {
		if len(list) < 2 || res.Days >= limit || ctx.Err() != nil {
			continue
		}
		if err := e.runCompaction(list, []*compactOut{{w: e.newWriter(Day, d, d+dayMs, true)}}, true); err != nil {
			return res, err
		}
		res.Days++
	}
	last, err := e.head.metaGet(ctx, e.head.read, "last_sweep")
	if err != nil {
		return res, err
	}
	if now-last >= dayMs {
		if err := e.head.sweep(ctx, now); err != nil {
			return res, err
		}
	}
	return res, nil
}

func (e *blockEngine) usedBytes() int64 {
	var n int64
	e.mu.RLock()
	for _, list := range e.blocks {
		for _, bf := range list {
			n += bf.size
		}
	}
	e.mu.RUnlock()
	// The head counts what it uses: a SQLite file keeps its size after the
	// sealed minutes are deleted, and reuses the free pages.
	if used, err := e.head.usedBytes(context.Background()); err == nil {
		n += used
	}
	return n
}

func (e *blockEngine) prune(ctx context.Context, now int64, keep map[Grain]time.Duration) (PruneResult, error) {
	res := PruneResult{Rows: map[Grain]int64{}, Files: map[Grain]int{}}
	e.wmu.Lock()
	defer e.wmu.Unlock()
	drop := func(bf *blockFile) error {
		n, _ := bf.rows()
		if err := e.removeBlock(bf); err != nil {
			return err
		}
		res.Rows[bf.h.grain] += n
		res.Files[bf.h.grain]++
		return nil
	}
	for _, g := range grains {
		d := keep[g]
		if d <= 0 {
			continue
		}
		cut := now - d.Milliseconds()
		for _, bf := range e.snapshot(g) {
			if bf.h.end <= cut {
				if err := drop(bf); err != nil {
					return res, err
				}
			}
		}
	}
	if e.sch.MaxBytes <= 0 {
		return res, nil
	}
	// Over budget: drop the oldest, most detailed blocks first: minutes once
	// their hour is folded, hours once their day is folded, then days.
	for ctx.Err() == nil && e.usedBytes() > e.sch.MaxBytes {
		var victim *blockFile
		for _, bf := range e.snapshot(Minute) {
			h := bf.h.start - bf.h.start%hourMs
			if e.covers(Hour, h, h+hourMs) {
				victim = bf
				break
			}
		}
		if victim == nil {
			for _, bf := range e.snapshot(Hour) {
				if e.covers(Day, bf.h.start-bf.h.start%dayMs, bf.h.start-bf.h.start%dayMs+dayMs) {
					victim = bf
					break
				}
			}
		}
		if victim == nil {
			if list := e.snapshot(Day); len(list) > 0 {
				victim = list[0]
			}
		}
		if victim == nil {
			break
		}
		if err := drop(victim); err != nil {
			return res, err
		}
	}
	return res, ctx.Err()
}

func (e *blockEngine) ownerList(ctx context.Context) ([]string, error) {
	list, err := e.head.ownerList(ctx)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for _, o := range list {
		seen[o] = true
	}
	for _, g := range grains {
		for _, bf := range e.snapshot(g) {
			for i := range bf.group {
				if o := bf.group[i].owner; !seen[o] {
					seen[o] = true
					list = append(list, o)
				}
			}
		}
	}
	sort.Strings(list)
	return list, nil
}

func (e *blockEngine) dropOwner(ctx context.Context, owner string) error {
	if err := e.head.dropOwner(ctx, owner); err != nil {
		return err
	}
	e.wmu.Lock()
	defer e.wmu.Unlock()
	for _, g := range grains {
		for _, bf := range e.snapshot(g) {
			has := false
			for _, gr := range bf.group {
				has = has || gr.owner == owner
			}
			if !has {
				continue
			}
			// The rewrite is a merge of one block: it supersedes the old file.
			w := e.newWriter(g, bf.h.start, bf.h.end, true)
			w.h.covered = bf.h.covered
			r, err := e.openReader(bf)
			if err != nil {
				return err
			}
			for i := range bf.group {
				gr := &bf.group[i]
				if gr.owner == owner {
					continue
				}
				gd, err := e.readGroup(r, gr)
				if err == nil {
					err = w.add(gr.owner, gr.family, gd.recs(0, len(gd.col)))
				}
				if err != nil {
					r.close()
					return err
				}
			}
			r.close()
			var add []*blockFile
			if !w.empty() {
				nb, err := e.write(w)
				if err != nil {
					return err
				}
				add = append(add, nb)
			}
			if err := e.swap(add, []*blockFile{bf}); err != nil {
				return err
			}
		}
	}
	return nil
}

func (e *blockEngine) scan(ctx context.Context, sp scanSpec, out *partial) error {
	if sp.end <= sp.start {
		return nil
	}
	end := sp.end
	if sp.grain == Minute {
		e.mu.RLock()
		sealed := e.sealed
		e.mu.RUnlock()
		if sp.end > sealed {
			hs := sp
			hs.start = max(sp.start, sealed)
			if err := e.head.scan(ctx, hs, out); err != nil {
				return err
			}
		}
		end = min(sp.end, sealed)
	}
	for _, bf := range e.snapshot(sp.grain) {
		if bf.h.end <= sp.start || bf.h.start >= end {
			continue
		}
		bs := sp
		bs.end = end
		if err := e.scanBlock(bf, bs, out); err != nil {
			return fmt.Errorf("%s: %w", bf.path, err)
		}
	}
	return nil
}

// scanBlock reads one block's (owner, family) group: only the columns the
// scan needs, filtering on dictionary indexes before any string is built.
func (e *blockEngine) scanBlock(bf *blockFile, sp scanSpec, out *partial) error {
	g := bf.find(sp.owner, sp.fam.Name)
	if g == nil || g.rows == 0 {
		return nil
	}
	f := sp.fam
	if len(g.cols) != 2+len(f.Keys)+len(f.Measures) {
		return errBadBlock
	}
	fh, err := os.Open(bf.path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil // pruned or replaced since the snapshot
	}
	if err != nil {
		return err
	}
	defer fh.Close()
	dict, err := readDict(e.c, fh, bf)
	if err != nil {
		return err
	}
	lookup := func(v string) (uint32, bool) {
		i := sort.SearchStrings(dict, v)
		return uint32(i), i < len(dict) && dict[i] == v
	}
	colWant, colOK := uint32(0), true
	if sp.collector != "" {
		colWant, colOK = lookup(sp.collector)
	}
	// eqIdx is a set filter on dictionary indexes; key -1 is the collector.
	type eqIdx struct {
		key int
		set []uint32
		not bool
	}
	type rngIdx struct {
		key    int
		lo, hi uint32
	}
	var eqs []eqIdx
	var rngs []rngIdx
	needCol := sp.collector != "" || sp.by == byCollector
	for _, f := range sp.eq {
		var set []uint32
		for _, v := range f.values {
			if i, ok := lookup(v); ok {
				set = append(set, i)
			}
		}
		switch {
		case len(set) == 0 && f.not:
			continue // nothing here to exclude
		case len(set) == 0:
			return nil // nothing here can match
		}
		needCol = needCol || f.key == -1
		eqs = append(eqs, eqIdx{f.key, set, f.not})
	}
	for _, r := range sp.rng {
		lo, hi := uint32(sort.SearchStrings(dict, r.lo)), uint32(sort.SearchStrings(dict, r.hi))
		if lo >= hi {
			return nil
		}
		rngs = append(rngs, rngIdx{r.key, lo, hi})
	}
	if !colOK {
		return nil
	}
	keyCache := map[int][]uint32{}
	keyCol := func(k int) ([]uint32, error) {
		if c, ok := keyCache[k]; ok {
			return c, nil
		}
		c, err := readUvarints(e.c, fh, g.cols[2+k], g.rows)
		keyCache[k] = c
		return c, err
	}
	var colIdx []uint32
	if needCol {
		if colIdx, err = readRLE(e.c, fh, g.cols[0], g.rows); err != nil {
			return err
		}
	}
	bk, err := readUvarints(e.c, fh, g.cols[1], g.rows)
	if err != nil {
		return err
	}
	eqCols := make([][]uint32, len(eqs))
	for i, q := range eqs {
		if q.key == -1 {
			eqCols[i] = colIdx
			continue
		}
		if eqCols[i], err = keyCol(q.key); err != nil {
			return err
		}
	}
	rngCols := make([][]uint32, len(rngs))
	for i, q := range rngs {
		if rngCols[i], err = keyCol(q.key); err != nil {
			return err
		}
	}
	var byCol []uint32
	switch {
	case sp.by == byCollector:
		byCol = colIdx
	case sp.by >= 0:
		if byCol, err = keyCol(sp.by); err != nil {
			return err
		}
	}
	msr := make([][]int64, len(sp.msr))
	for i, m := range sp.msr {
		if msr[i], err = readVarints(e.c, fh, g.cols[2+len(f.Keys)+m], g.rows); err != nil {
			return err
		}
	}
	gms := bf.h.grain.Ms()
	groups := map[uint32][]int64{}
	vals := make([]int64, len(sp.msr))
rows:
	for i := int64(0); i < g.rows; i++ {
		b := bf.h.start + int64(bk[i])*gms
		if b < sp.start || b >= sp.end {
			continue
		}
		if sp.collector != "" && colIdx[i] != colWant {
			continue
		}
		for j, q := range eqs {
			v, in := eqCols[j][i], false
			for _, x := range q.set {
				if x == v {
					in = true
					break
				}
			}
			if in == q.not {
				continue rows
			}
		}
		for j, q := range rngs {
			if v := rngCols[j][i]; v < q.lo || v >= q.hi {
				continue rows
			}
		}
		for j := range msr {
			vals[j] = msr[j][i]
			out.total[j] += vals[j]
		}
		if sp.step > 0 {
			out.series[(b/sp.step)*sp.step] += vals[0]
		}
		if byCol != nil {
			acc := groups[byCol[i]]
			if acc == nil {
				acc = make([]int64, len(vals))
				groups[byCol[i]] = acc
			}
			for j, v := range vals {
				acc[j] += v
			}
		}
	}
	for idx, v := range groups {
		out.addGroup(dict[idx], v)
	}
	return nil
}

func (e *blockEngine) stats(ctx context.Context) (Stats, error) {
	hs, err := e.head.stats(ctx)
	if err != nil {
		return Stats{}, err
	}
	st := Stats{Bytes: map[Grain]int64{}, Rows: map[string]map[Grain]int64{}, FoldedRows: map[string]map[Grain]int64{},
		First: map[Grain]int64{}, Last: map[Grain]int64{}}
	for _, f := range e.sch.fams {
		st.Rows[f.Name] = map[Grain]int64{Minute: hs.Rows[f.Name][Minute]}
		st.FoldedRows[f.Name] = map[Grain]int64{Minute: hs.FoldedRows[f.Name][Minute]}
	}
	// The head counts what it uses, not its file: SQLite keeps the file's size
	// after sealed minutes are deleted and reuses those pages.
	if used, err := e.head.usedBytes(ctx); err == nil {
		st.HeadBytes = used
	}
	st.FileBytes = hs.FileBytes
	if v, ok := hs.First[Minute]; ok {
		st.First[Minute], st.Last[Minute] = v, hs.Last[Minute]
	}
	e.mu.RLock()
	defer e.mu.RUnlock()
	for g, list := range e.blocks {
		for _, bf := range list {
			st.Bytes[g] += bf.size
			st.FileBytes += bf.size
			st.Files++
			for _, gr := range bf.group {
				st.Rows[gr.family][g] += gr.rows
				st.FoldedRows[gr.family][g] += gr.folded
			}
			if v, ok := st.First[g]; !ok || bf.h.start < v {
				st.First[g] = bf.h.start
			}
			st.Last[g] = max(st.Last[g], bf.h.covered)
		}
	}
	st.BadBlocks = append([]string(nil), e.bad...)
	return st, nil
}
