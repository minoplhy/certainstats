package tblk

import (
	"context"
	"fmt"
	"net/netip"
	"sort"
	"strconv"
)

// Filter limits rows by one key: Value and Values match exactly, any of them
// (a CIDR for a network key); Prefix, for network keys, matches every network
// inside it. Not excludes the values instead. The key "collector", in a family
// without such a key, filters the collector rows were sent by.
type Filter struct {
	Key    string
	Value  string
	Values []string
	Not    bool
	Prefix netip.Prefix
}

// CollectorKey filters or groups by the collector rows were sent by.
const CollectorKey = "collector"

// byCollector is scanSpec.by when rows group by collector.
const byCollector = -2

// Query selects rows of one family.
type Query struct {
	Family    string
	Owner     string
	Collector string // empty: every collector of the owner
	Filters   []Filter
	// [Start, End) in Unix ms; Step is the series bucket width (0: no series).
	Start, End, Step int64
	// By groups rows by this key; empty returns totals only.
	By string
	// Limit keeps this many rows; the rest add to Other. Zero keeps all.
	Limit int
	// Measures to sum; empty sums the first. The series uses the first listed.
	Measures []string
	// MinuteFrom is the oldest minute still kept; earlier ranges read hours.
	MinuteFrom int64
	// KeepOther lists folded rows (key Other) as rows instead of adding them to Other.
	KeepOther bool
	// CheckFolded sets Result.Folded.
	CheckFolded bool
	// Finest reads minutes wherever they are kept (from MinuteFrom), then
	// hours, never days: exact counts and fine series over multi-day windows.
	Finest bool
}

// ResultRow is one group: its key and one sum per measure.
type ResultRow struct {
	Key    string
	Values []int64
}

// Result answers a Query.
type Result struct {
	Rows []ResultRow
	// Other sums folded rows and rows past Limit, per measure.
	Other []int64
	// Total sums every matching row, per measure.
	Total  []int64
	Series [][2]int64
	// Folded is set (with CheckFolded) when any folded row is in range, so
	// rows below the caps are missing and per-key sums are lower bounds.
	Folded bool
	// Ignored lists filters on keys this family does not have.
	Ignored []string
}

// eqFilter keeps rows whose key is one of values (stored form), or with not,
// none of them. key -1 is the collector.
type eqFilter struct {
	key    int
	values []string
	not    bool
}

type rangeFilter struct {
	key    int
	lo, hi string
}

// scanSpec is one read of one tier over [start, end).
type scanSpec struct {
	fam              *famInfo
	grain            Grain
	owner, collector string
	start, end       int64
	eq               []eqFilter
	rng              []rangeFilter
	by               int   // key index, -1 for none, or byCollector
	step             int64 // series width, or 0
	msr              []int
}

// partial collects what scans find, labels in stored form.
type partial struct {
	groups map[string][]int64
	series map[int64]int64
	total  []int64
}

func newPartial(n int) *partial {
	return &partial{groups: map[string][]int64{}, series: map[int64]int64{}, total: make([]int64, n)}
}

func (p *partial) addGroup(label string, vals []int64) {
	g := p.groups[label]
	if g == nil {
		g = make([]int64, len(vals))
		p.groups[label] = g
	}
	for i, v := range vals {
		g[i] += v
	}
}

// span is one tier's share of a window.
type span struct {
	g    Grain
	a, b int64
}

// route splits [start, end) between tiers: days for whole days (useDay) up to
// the day tier's reach, hours (useHour) up to the hour tier's reach, minutes
// for the rest.
func route(start, end int64, useHour, useDay bool, hourRolled, dayRolled int64) []span {
	var out []span
	pieces := [][2]int64{{start, end}}
	if useDay {
		d0 := start + (dayMs-start%dayMs)%dayMs
		d1 := min(end-end%dayMs, dayRolled)
		if d1 > d0 {
			out = append(out, span{Day, d0, d1})
			pieces = [][2]int64{{start, d0}, {d1, end}}
		}
	}
	for _, p := range pieces {
		if p[1] <= p[0] {
			continue
		}
		if useHour && p[0] < hourRolled {
			cut := min(p[1], hourRolled)
			out = append(out, span{Hour, p[0] - p[0]%hourMs, cut})
			if cut < p[1] {
				out = append(out, span{Minute, cut, p[1]})
			}
			continue
		}
		out = append(out, span{Minute, p[0], p[1]})
	}
	return out
}

// Query answers one read, holding a query slot.
func (s *Store) Query(ctx context.Context, q Query) (Result, error) {
	f, err := s.family(q.Family)
	if err != nil {
		return Result{}, err
	}
	var res Result
	base := scanSpec{fam: f, owner: q.Owner, collector: q.Collector, by: -1}
	empty := false
	for _, fl := range q.Filters {
		vals := fl.Values
		if fl.Value != "" {
			vals = append([]string{fl.Value}, vals...)
		}
		ki, ok := f.keyIdx[fl.Key]
		if !ok && fl.Key == CollectorKey && !fl.Prefix.IsValid() {
			if len(vals) > 0 {
				base.eq = append(base.eq, eqFilter{-1, vals, fl.Not})
			}
			continue
		}
		if !ok {
			res.Ignored = append(res.Ignored, fl.Key)
			continue
		}
		k := f.Keys[ki]
		switch {
		case fl.Prefix.IsValid() && k.Kind == KeyNet && !fl.Not:
			lo, hi := netRange(fl.Prefix)
			base.rng = append(base.rng, rangeFilter{ki, lo, hi})
		case fl.Prefix.IsValid():
			res.Ignored = append(res.Ignored, fl.Key)
		case len(vals) == 0:
		default:
			stored := make([]string, 0, len(vals))
			for _, v := range vals {
				switch k.Kind {
				case KeyNet:
					if n, ok := netKeyOf(v); ok {
						stored = append(stored, n)
					}
				case KeyInt:
					if n, err := strconv.ParseInt(v, 10, 64); err == nil {
						stored = append(stored, strconv.FormatInt(n, 10))
					}
				default:
					stored = append(stored, v)
				}
			}
			switch {
			case len(stored) > 0:
				base.eq = append(base.eq, eqFilter{ki, stored, fl.Not})
			case !fl.Not:
				empty = true // no value can match
			}
		}
	}
	if q.By != "" {
		ki, ok := f.keyIdx[q.By]
		switch {
		case ok:
			base.by = ki
		case q.By == CollectorKey:
			base.by = byCollector
		default:
			return res, fmt.Errorf("tblk: family %q has no key %q", f.Name, q.By)
		}
	}
	names := q.Measures
	if len(names) == 0 {
		names = f.Measures[:1]
	}
	for _, m := range names {
		mi, ok := f.msrIdx[m]
		if !ok {
			return res, fmt.Errorf("tblk: family %q has no measure %q", f.Name, m)
		}
		base.msr = append(base.msr, mi)
	}
	res.Rows, res.Other, res.Total, res.Series = []ResultRow{}, make([]int64, len(names)), make([]int64, len(names)), [][2]int64{}
	step := int64(0)
	if q.Step > 0 {
		step = max(q.Step, minuteMs)
	}
	if empty || q.End <= q.Start {
		res.Series = zeroFill(res.Series, q.Start, q.End, step)
		return res, nil
	}

	release, err := s.acquire(ctx)
	if err != nil {
		return res, err
	}
	defer release()
	hourRolled, dayRolled, err := s.eng.rolled(ctx)
	if err != nil {
		return res, err
	}
	win := q.End - q.Start
	useHour := win > dayMs || q.Start < q.MinuteFrom
	useDay := win > 7*dayMs
	if q.Finest {
		useHour, useDay = q.Start < q.MinuteFrom, false
	}
	run := func(spans []span, sp scanSpec, p *partial) error {
		for _, x := range spans {
			sp.grain, sp.start, sp.end = x.g, x.a, x.b
			if err := s.eng.scan(ctx, sp, p); err != nil {
				return err
			}
		}
		return nil
	}
	p := newPartial(len(names))
	sp := base
	// Day buckets are too coarse for a chart: long windows chart from hours.
	if !useDay {
		sp.step = step
	}
	if err := run(route(q.Start, q.End, useHour, useDay, hourRolled, dayRolled), sp, p); err != nil {
		return res, err
	}
	series := p.series
	if useDay && step > 0 {
		ps := newPartial(len(names))
		ss := base
		ss.by, ss.step = -1, step
		if err := run(route(q.Start, q.End, useHour, false, hourRolled, dayRolled), ss, ps); err != nil {
			return res, err
		}
		series = ps.series
	}
	if q.CheckFolded && f.rank >= 0 {
		pf := newPartial(len(names))
		fs := base
		fs.by = -1
		// The query hangs on the folding keys it groups or filters by. Rows
		// that lost the shallowest of those levels may hide what it asked
		// for, so they are looked for with the filters on keys of that level
		// and deeper left out: a folded row no longer says which value it was.
		lv := 0
		depend := func(k int) {
			if k >= 0 && f.level[k] > 0 && (lv == 0 || f.level[k] < lv) {
				lv = f.level[k]
			}
		}
		depend(base.by)
		for _, e := range base.eq {
			depend(e.key)
		}
		check := f.rank
		if lv > 0 {
			check = f.levels[lv-1][0]
		}
		fs.eq = nil
		for _, e := range base.eq {
			if lv > 0 && e.key >= 0 && f.level[e.key] >= lv {
				continue
			}
			fs.eq = append(fs.eq, e)
		}
		fs.eq = append(fs.eq, eqFilter{check, []string{Other}, false})
		if err := run(route(q.Start, q.End, useHour, useDay, hourRolled, dayRolled), fs, pf); err != nil {
			return res, err
		}
		res.Folded = pf.total[0] > 0
	}

	copy(res.Total, p.total)
	foldBy := base.by >= 0 && f.Keys[base.by].Fold
	for label, vals := range p.groups {
		if foldBy && label == Other && !q.KeepOther {
			for i, v := range vals {
				res.Other[i] += v
			}
			continue
		}
		key := label
		if base.by >= 0 && f.Keys[base.by].Kind == KeyNet {
			key = netLabel(label)
		}
		res.Rows = append(res.Rows, ResultRow{Key: key, Values: vals})
	}
	sort.Slice(res.Rows, func(i, j int) bool {
		if res.Rows[i].Values[0] != res.Rows[j].Values[0] {
			return res.Rows[i].Values[0] > res.Rows[j].Values[0]
		}
		return res.Rows[i].Key < res.Rows[j].Key
	})
	if q.Limit > 0 && len(res.Rows) > q.Limit {
		for _, r := range res.Rows[q.Limit:] {
			for i, v := range r.Values {
				res.Other[i] += v
			}
		}
		res.Rows = res.Rows[:q.Limit]
	}
	for t, n := range series {
		res.Series = append(res.Series, [2]int64{t, n})
	}
	sort.Slice(res.Series, func(i, j int) bool { return res.Series[i][0] < res.Series[j][0] })
	res.Series = zeroFill(res.Series, q.Start, q.End, step)
	return res, nil
}

// maxFill bounds how many buckets zeroFill writes.
const maxFill = 5000

// zeroFill gives every bucket of [start, end) a point; only buckets with
// traffic have rows, and a quiet bucket means zero, not missing data.
func zeroFill(series [][2]int64, start, end, step int64) [][2]int64 {
	if step <= 0 || end <= start || (end-start)/step > maxFill {
		return series
	}
	first := start - start%step
	out := make([][2]int64, 0, (end-first)/step+1)
	i := 0
	for t := first; t < end; t += step {
		for i < len(series) && series[i][0] < t {
			out = append(out, series[i])
			i++
		}
		if i < len(series) && series[i][0] == t {
			out = append(out, series[i])
			i++
			continue
		}
		out = append(out, [2]int64{t, 0})
	}
	return append(out, series[i:]...)
}
