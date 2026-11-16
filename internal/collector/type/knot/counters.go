package knot

import (
	"context"
	"errors"
	"math"
	"regexp"
	"sort"
	"time"

	ctsdb "certainstats/internal/collector/tsdb"
	"certainstats/internal/collector/type/knot/wire"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/storage"
	"github.com/prometheus/prometheus/tsdb/chunkenc"
)

// metricName is the one TSDB metric every Knot counter is stored under; the
// labels say which counter it is.
const metricName = "knot_counter"

// lookback is how far before a range a series' previous reading is searched.
// A reading with no earlier one within lookback is a baseline: it counts as
// zero, so a fresh install starts at 0 instead of Knot's lifetime total.
const lookback = 24 * time.Hour

// writeCounters appends one reading at the server's time, which is monotonic
// per series; the collector's clock is not trusted for ordering.
func writeCounters(ctx context.Context, db *ctsdb.DB, userID, collectorID string, cs []wire.Counter, at int64) error {
	app := db.Appender(ctx)
	for _, c := range cs {
		b := labels.NewScratchBuilder(7)
		b.Add("__name__", metricName)
		b.Add("user_id", userID)
		b.Add("collector_id", collectorID)
		b.Add("section", c.Section)
		b.Add("item", c.Item)
		if c.ID != "" {
			b.Add("id", c.ID)
		}
		if c.Zone != "" {
			b.Add("zone", c.Zone)
		}
		b.Sort()
		if _, err := app.Append(0, b.Labels(), at, float64(c.Value)); err != nil {
			if errors.Is(err, storage.ErrOutOfOrderSample) || errors.Is(err, storage.ErrDuplicateSampleForTimestamp) || errors.Is(err, storage.ErrTooOldSample) || errors.Is(err, storage.ErrOutOfBounds) {
				continue
			}
			_ = app.Rollback()
			return err
		}
	}
	return app.Commit()
}

// counterQuery selects counters. Zone empty means server-wide counters only.
type counterQuery struct {
	UserID      string
	CollectorID string // empty: every collector of the owner
	Zone        string
	Items       []string // mod-stats items
	Start, End  int64
	Step        int64
}

// seriesKey groups increases: one item and one id.
type seriesKey struct{ item, id string }

// increases is the result: per item and id, the increase in each step bucket.
type increases struct {
	Start, Step int64
	Buckets     int
	Series      map[seriesKey][]float64
	// From is the earliest time covered by an increase in range; zero when none.
	From int64
	// Pairs counts consecutive readings seen; zero means only baselines so far.
	Pairs int
}

func (inc *increases) total(k seriesKey) float64 {
	var t float64
	for _, v := range inc.Series[k] {
		t += v
	}
	return t
}

// sumItem adds every id of an item.
func (inc *increases) sumItem(item string) float64 {
	var t float64
	for k := range inc.Series {
		if k.item == item {
			t += inc.total(k)
		}
	}
	return t
}

// readIncreases turns cumulative readings into increases. Between two readings
// the increase is the difference, or the newer value when it dropped (Knot
// restarted). Each increase is spread evenly over the time between its two
// readings, so a gap after an outage fills the buckets it covers instead of
// spiking one; only the part inside [Start, End) counts.
func readIncreases(ctx context.Context, db *ctsdb.DB, q counterQuery) (*increases, error) {
	step := max(q.Step, int64(time.Minute/time.Millisecond))
	n := int((q.End - q.Start + step - 1) / step)
	inc := &increases{Start: q.Start, Step: step, Buckets: n, Series: map[seriesKey][]float64{}}
	if db == nil || n <= 0 {
		return inc, nil
	}
	ms := []*labels.Matcher{
		labels.MustNewMatcher(labels.MatchEqual, "__name__", metricName),
		labels.MustNewMatcher(labels.MatchEqual, "user_id", q.UserID),
		labels.MustNewMatcher(labels.MatchEqual, "section", "mod-stats"),
		labels.MustNewMatcher(labels.MatchEqual, "zone", q.Zone),
	}
	if q.CollectorID != "" {
		ms = append(ms, labels.MustNewMatcher(labels.MatchEqual, "collector_id", q.CollectorID))
	}
	if len(q.Items) > 0 {
		re := ""
		for i, it := range q.Items {
			if i > 0 {
				re += "|"
			}
			re += regexp.QuoteMeta(it)
		}
		ms = append(ms, labels.MustNewMatcher(labels.MatchRegexp, "item", re))
	}
	from := q.Start - lookback.Milliseconds()
	err := db.Query(ctx, from, q.End, func(qr storage.Querier) error {
		set := qr.Select(ctx, false, nil, ms...)
		for set.Next() {
			lb := set.At().Labels()
			key := seriesKey{lb.Get("item"), lb.Get("id")}
			buckets := inc.Series[key]
			if buckets == nil {
				buckets = make([]float64, n)
				inc.Series[key] = buckets
			}
			it := set.At().Iterator(nil)
			var pt int64
			var pv float64
			have := false
			for it.Next() != chunkenc.ValNone {
				t, v := it.At()
				if math.IsNaN(v) || math.IsInf(v, 0) {
					continue
				}
				if have && t > pt {
					inc.Pairs++
					d := v - pv
					if v < pv {
						d = v // Knot restarted: everything since then is new
					}
					if d > 0 && t > q.Start {
						spread(buckets, q.Start, step, q.End, pt, t, d)
						if lo := max(pt, q.Start); inc.From == 0 || lo < inc.From {
							inc.From = lo
						}
					}
				}
				pt, pv, have = t, v, true
			}
			if err := it.Err(); err != nil {
				return err
			}
		}
		return set.Err()
	})
	return inc, err
}

// spread adds d, which accrued evenly over (t0, t1], to the buckets of
// [start, end) it overlaps.
func spread(buckets []float64, start, step, end, t0, t1 int64, d float64) {
	lo, hi := max(t0, start), min(t1, end)
	if hi <= lo {
		return
	}
	rate := d / float64(t1-t0)
	for b := (lo - start) / step; b < int64(len(buckets)); b++ {
		bs := start + b*step
		be := bs + step
		if bs >= hi {
			break
		}
		ov := min(be, hi) - max(bs, lo)
		if ov > 0 {
			buckets[b] += rate * float64(ov)
		}
	}
}

// topIDs returns the ids of an item ordered by total, largest first.
func (inc *increases) topIDs(item string) []string {
	type kv struct {
		id string
		n  float64
	}
	var list []kv
	for k := range inc.Series {
		if k.item == item {
			list = append(list, kv{k.id, inc.total(k)})
		}
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].n != list[j].n {
			return list[i].n > list[j].n
		}
		return list[i].id < list[j].id
	})
	out := make([]string, len(list))
	for i, x := range list {
		out[i] = x.id
	}
	return out
}
