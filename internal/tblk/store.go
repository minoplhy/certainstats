package tblk

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Engine picks the storage layout.
type Engine string

const (
	// SQL keeps every tier in one SQLite file with integer keys.
	SQL Engine = "sql"
	// Blocks keeps unsealed minutes in a SQLite head and everything else in
	// immutable compressed column blocks.
	Blocks Engine = "blocks"
)

const (
	// rollLag keeps the newest hours as minutes only: collectors resend
	// unacknowledged minutes for up to an hour, so an hour is rolled once settled.
	rollLag = 2 * hourMs
	// defaultSlots and defaultQueueWait match the collector TSDB's read limiter.
	defaultSlots     = 8
	defaultQueueWait = 2 * time.Second
	// maintainBatch caps the hours rolled per Maintain call, so the writer is
	// never held for long.
	maintainBatch = 72
)

// ErrBusy is returned when every query slot stays taken for the queue wait.
var ErrBusy = errors.New("top-N store is busy; try again")

// Options tune a store.
type Options struct {
	Engine Engine
	// Driver is the database/sql driver for SQLite files ("sqlite" when empty);
	// the storage report passes a tracing driver.
	Driver     string
	QuerySlots int
	QueueWait  time.Duration
}

// Row is one row to append: keys in family order (KeyNet as CIDR, KeyInt as a
// decimal number) and one value per measure.
type Row struct {
	Keys   []string
	Values []int64
}

// rec is a row inside the package: keys stored form (KeyNet as its sortable
// key), with collector and bucket.
type rec struct {
	collector string
	bucket    int64
	keys      []string
	vals      []int64
}

// MaintainResult counts one Maintain call's work.
type MaintainResult struct {
	Hours, Days int
	Pruned      PruneResult
}

// PruneResult counts removed rows per tier (and files, for the block engine).
type PruneResult struct {
	Rows  map[Grain]int64
	Files map[Grain]int
}

// Stats describe a store's size.
type Stats struct {
	// Bytes per tier, dictionaries and head excluded.
	Bytes map[Grain]int64
	// DictBytes is the SQL engine's string dictionaries.
	DictBytes int64
	// HeadBytes is the block engine's unsealed-minute head.
	HeadBytes int64
	// FileBytes is everything on disk.
	FileBytes int64
	Files     int
	// Rows and FoldedRows per family and tier.
	Rows       map[string]map[Grain]int64
	FoldedRows map[string]map[Grain]int64
	// First and Last bucket per tier (Last is exclusive).
	First, Last map[Grain]int64
	// BadBlocks lists block files that failed their checks and are skipped.
	BadBlocks []string
}

// engine is what both layouts implement.
type engine interface {
	append(ctx context.Context, owner, collector string, f *famInfo, g Grain, bucket int64, rows []rec) error
	// add sums minute rows onto what is stored; rows older than what is
	// already sealed or rolled also reach the tiers built from them.
	add(ctx context.Context, owner, collector string, f *famInfo, bucket int64, rows []rec) error
	flushTier(ctx context.Context) error
	scan(ctx context.Context, sp scanSpec, out *partial) error
	// rolled returns how far the hour and day tiers reach (exclusive bucket ends).
	rolled(ctx context.Context) (hour, day int64, err error)
	maintain(ctx context.Context, now int64, limit int) (MaintainResult, error)
	prune(ctx context.Context, now int64, keep map[Grain]time.Duration) (PruneResult, error)
	dropOwner(ctx context.Context, owner string) error
	ownerList(ctx context.Context) ([]string, error)
	stats(ctx context.Context) (Stats, error)
	close() error
}

// Store is a top-N store in one directory.
type Store struct {
	sch       *schemaInfo
	eng       engine
	slots     chan struct{}
	queueWait time.Duration
}

// Open opens or creates a store in dir.
func Open(dir string, s Schema, o Options) (*Store, error) {
	si, err := compile(s)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	if o.Driver == "" {
		o.Driver = "sqlite"
	}
	if o.QuerySlots <= 0 {
		o.QuerySlots = defaultSlots
	}
	if o.QueueWait <= 0 {
		o.QueueWait = defaultQueueWait
	}
	var eng engine
	switch o.Engine {
	case SQL, "":
		eng, err = openSQL(dir+"/topn.db", si, o.Driver, grains)
	case Blocks:
		eng, err = openBlocks(dir, si, o.Driver)
	default:
		return nil, fmt.Errorf("tblk: unknown engine %q", o.Engine)
	}
	if err != nil {
		return nil, err
	}
	return &Store{sch: si, eng: eng, slots: make(chan struct{}, o.QuerySlots), queueWait: o.QueueWait}, nil
}

func (s *Store) Close() error { return s.eng.close() }

// acquire takes a query slot, waiting at most the queue wait.
func (s *Store) acquire(ctx context.Context) (func(), error) {
	wait, cancel := context.WithTimeout(ctx, s.queueWait)
	defer cancel()
	select {
	case s.slots <- struct{}{}:
		return func() { <-s.slots }, nil
	case <-wait.Done():
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, ErrBusy
	}
}

func (s *Store) family(name string) (*famInfo, error) {
	f := s.sch.byName[name]
	if f == nil {
		return nil, fmt.Errorf("tblk: unknown family %q", name)
	}
	return f, nil
}

// toRecs checks rows and turns them into stored form; rows with a bad network
// or number are skipped, as a collector's bad row should not fail the minute.
func toRecs(f *famInfo, collector string, bucket int64, rows []Row) []rec {
	out := make([]rec, 0, len(rows))
	for _, r := range rows {
		if len(r.Keys) != len(f.Keys) || len(r.Values) != len(f.Measures) {
			continue
		}
		keys := make([]string, len(r.Keys))
		ok := true
		for i, k := range f.Keys {
			v := r.Keys[i]
			switch k.Kind {
			case KeyNet:
				v, ok = netKeyOf(v)
			case KeyInt:
				if _, err := strconv.ParseInt(v, 10, 64); err != nil {
					ok = false
				}
			}
			if !ok {
				break
			}
			keys[i] = v
		}
		if ok {
			out = append(out, rec{collector: collector, bucket: bucket, keys: keys, vals: append([]int64(nil), r.Values...)})
		}
	}
	return out
}

// Append stores one minute of one family for an owner's collector. Rows
// replace earlier rows with the same keys, so a resent minute changes nothing.
func (s *Store) Append(ctx context.Context, owner, collector, family string, bucketMs int64, rows []Row) error {
	f, err := s.family(family)
	if err != nil {
		return err
	}
	bucket := bucketMs - bucketMs%minuteMs
	return s.eng.append(ctx, owner, collector, f, Minute, bucket, toRecs(f, collector, bucket, rows))
}

// Add sums one minute of one family onto what is stored, for sources whose
// events arrive a few at a time (alerts, log lines). Unlike Append, a resent
// row counts twice, and a minute older than what maintenance has already
// sealed or rolled is not dropped: it is counted in the minute tier, and in
// the hour and day tiers that were already built from that minute's hour and
// day, so every tier keeps adding up.
func (s *Store) Add(ctx context.Context, owner, collector, family string, bucketMs int64, rows []Row) error {
	f, err := s.family(family)
	if err != nil {
		return err
	}
	bucket := bucketMs - bucketMs%minuteMs
	return s.eng.add(ctx, owner, collector, f, bucket, toRecs(f, collector, bucket, rows))
}

// AppendTier writes rows straight into the hour or day tier, for imports and
// the storage report's synthetic history. Call FlushTier when done.
func (s *Store) AppendTier(ctx context.Context, g Grain, owner, collector, family string, bucketMs int64, rows []Row) error {
	if g == Minute {
		return s.Append(ctx, owner, collector, family, bucketMs, rows)
	}
	f, err := s.family(family)
	if err != nil {
		return err
	}
	bucket := bucketMs - bucketMs%g.Ms()
	return s.eng.append(ctx, owner, collector, f, g, bucket, toRecs(f, collector, bucket, rows))
}

// FlushTier writes out AppendTier rows still buffered.
func (s *Store) FlushTier(ctx context.Context) error { return s.eng.flushTier(ctx) }

// Maintain rolls settled minutes into hours and complete days into days (at
// most a batch of hours per call), then applies retention and the size cap.
// Call it until Hours and Days are zero to catch up.
func (s *Store) Maintain(ctx context.Context, now int64) (MaintainResult, error) {
	res, err := s.eng.maintain(ctx, now, maintainBatch)
	if err != nil {
		return res, err
	}
	p, err := s.eng.prune(ctx, now, s.sch.Retention)
	res.Pruned = p
	return res, err
}

// Prune applies the given retention now, outside Maintain.
func (s *Store) Prune(ctx context.Context, now int64, keep map[Grain]time.Duration) (PruneResult, error) {
	return s.eng.prune(ctx, now, keep)
}

// DropOwner removes every row of an owner.
func (s *Store) DropOwner(ctx context.Context, owner string) error {
	return s.eng.dropOwner(ctx, owner)
}

// Owners lists every owner with rows in any tier.
func (s *Store) Owners(ctx context.Context) ([]string, error) { return s.eng.ownerList(ctx) }

// Stats measure the store.
func (s *Store) Stats(ctx context.Context) (Stats, error) { return s.eng.stats(ctx) }

// foldTop applies a tier's cap to the summed rows of one owner and bucket,
// per collector and one fold level at a time: the top n rows by the first
// measure stay, the rest lose their level-1 keys (set to Other) and are
// summed; of those, the top n stay and the rest lose level 2, and so on.
// Rows that lost every level are all kept, so sums still add up. Rows
// already folded join the stage of the levels they have lost. With one
// level this is the plain top n plus Other rows. Ties break on the keys, so
// every engine keeps the same rows.
func foldTop(f *famInfo, rows []rec, n int) []rec {
	if n <= 0 || f.rank < 0 {
		return rows
	}
	if f.bound >= 0 {
		return boundTop(f, rows, n)
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
	levels := len(f.levels)
	out := make([]rec, 0, len(rows))
	for _, c := range cols {
		// stage[i] holds the rows that have lost levels 1..i, summed by key.
		stage := make([]map[string]*rec, levels+1)
		order := make([][]string, levels+1)
		put := func(i int, r rec) {
			if stage[i] == nil {
				stage[i] = map[string]*rec{}
			}
			id := strings.Join(r.keys, "\x00")
			o := stage[i][id]
			if o == nil {
				o = &rec{collector: r.collector, bucket: r.bucket, keys: r.keys, vals: make([]int64, len(r.vals))}
				stage[i][id] = o
				order[i] = append(order[i], id)
			}
			for j, v := range r.vals {
				o.vals[j] += v
			}
		}
		for _, r := range byCol[c] {
			put(f.foldedLevels(r.keys), r)
		}
		for i := 0; i < levels; i++ {
			list := make([]rec, 0, len(order[i]))
			for _, id := range order[i] {
				list = append(list, *stage[i][id])
			}
			sort.Slice(list, func(a, b int) bool {
				if list[a].vals[0] != list[b].vals[0] {
					return list[a].vals[0] > list[b].vals[0]
				}
				return strings.Join(list[a].keys, "\x00") < strings.Join(list[b].keys, "\x00")
			})
			keep := min(n, len(list))
			out = append(out, list[:keep]...)
			for _, r := range list[keep:] {
				put(i+1, rec{collector: r.collector, bucket: r.bucket, keys: f.foldTo(r.keys, i+1), vals: r.vals})
			}
		}
		last := order[levels]
		sort.Strings(last)
		for _, id := range last {
			out = append(out, *stage[levels][id])
		}
	}
	return out
}

// boundTop is foldTop for a bounded family, per collector: of the rows not
// yet folded, those ranked strictly above the first row past the cap stay;
// the rest, ties at the cut included, fold to Other and are summed with the
// rows already folded. The cut (that first row's rank measure, 0 when
// nothing is cut) is added to the bound row, which also sums the bound rows
// of the input, so the error of every earlier cut carries over.
func boundTop(f *famInfo, rows []rec, n int) []rec {
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
		var live []rec
		folded := map[string]*rec{}
		var order []string
		var bound *rec
		toBound := func(r rec) {
			if bound == nil {
				bound = &rec{collector: r.collector, bucket: r.bucket, keys: BoundKeys(f.Family), vals: make([]int64, len(r.vals))}
			}
			for j, v := range r.vals {
				bound.vals[j] += v
			}
		}
		add := func(r rec) {
			// In a family whose every key folds, a folded row is the bound
			// row: it carries the folded count and the error together.
			if f.isBound(r.keys) {
				toBound(r)
				return
			}
			id := strings.Join(r.keys, "\x00")
			o := folded[id]
			if o == nil {
				o = &rec{collector: r.collector, bucket: r.bucket, keys: r.keys, vals: make([]int64, len(r.vals))}
				folded[id] = o
				order = append(order, id)
			}
			for j, v := range r.vals {
				o.vals[j] += v
			}
		}
		for _, r := range byCol[c] {
			switch {
			case f.isBound(r.keys):
				toBound(r)
			case f.folded(r.keys):
				add(r)
			default:
				live = append(live, r)
			}
		}
		var cut int64
		if len(live) > n {
			sort.Slice(live, func(a, b int) bool { return live[a].vals[0] > live[b].vals[0] })
			cut = live[n].vals[0]
			keep := live[:0:0]
			for _, r := range live {
				if r.vals[0] > cut {
					keep = append(keep, r)
					continue
				}
				add(rec{collector: r.collector, bucket: r.bucket, keys: f.foldTo(r.keys, 1), vals: r.vals})
			}
			live = keep
		}
		sort.Slice(live, func(a, b int) bool {
			if live[a].vals[0] != live[b].vals[0] {
				return live[a].vals[0] > live[b].vals[0]
			}
			return strings.Join(live[a].keys, "\x00") < strings.Join(live[b].keys, "\x00")
		})
		out = append(out, live...)
		sort.Strings(order)
		for _, id := range order {
			out = append(out, *folded[id])
		}
		if cut > 0 && bound == nil {
			r0 := byCol[c][0]
			bound = &rec{collector: c, bucket: r0.bucket, keys: BoundKeys(f.Family), vals: make([]int64, len(f.Measures))}
		}
		if bound != nil {
			bound.vals[f.bound] += cut
			out = append(out, *bound)
		}
	}
	return out
}

// sumRecs adds rows with the same collector and keys, setting every bucket to
// bucket: the step from a finer tier to one bucket of a coarser one.
func sumRecs(rows []rec, bucket int64) []rec {
	idx := map[string]int{}
	var out []rec
	for _, r := range rows {
		id := r.collector + "\x00" + strings.Join(r.keys, "\x00")
		if i, ok := idx[id]; ok {
			for j, v := range r.vals {
				out[i].vals[j] += v
			}
			continue
		}
		idx[id] = len(out)
		out = append(out, rec{collector: r.collector, bucket: bucket, keys: r.keys, vals: append([]int64(nil), r.vals...)})
	}
	return out
}

// rollRows turns one owner's rows of a finer tier, covering one bucket of g,
// into that bucket's rows: summed, then capped by the family's cap for g.
func rollRows(f *famInfo, g Grain, bucket int64, rows []rec) []rec {
	return foldTop(f, sumRecs(rows, bucket), f.cap(g))
}
