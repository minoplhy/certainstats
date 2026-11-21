package knot

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"time"

	"certainstats/internal/collector/type/knot/wire"
	"certainstats/internal/tblk"
)

// DefaultDailyRetention keeps the day tier (KNOT_ENGINE=topn only) for 400 days.
const DefaultDailyRetention = 400 * 24 * time.Hour

// rollupRounds caps the Maintain calls of one hourly run; each rolls up to
// 72 hours, so a long outage catches up over a few runs.
const rollupRounds = 8

// knotSchema is Knot's declaration.
func knotSchema(top int, keep Retention, maxBytes int64) tblk.Schema {
	caps := map[tblk.Grain]int{tblk.Hour: top, tblk.Day: top}
	return tblk.Schema{
		Families: []tblk.Family{
			{Name: "totals", Keys: []tblk.Key{{Name: "zone"}, {Name: "qtype"}, {Name: "rcode"}}, Measures: []string{"count"}},
			// names held rows by qname, qtype and rcode, cut by rows; it is no
			// longer written and stays only so older blocks still read until
			// retention removes them. Name lists start with qnames and qtypes.
			{Name: "names", Keys: []tblk.Key{{Name: "zone"}, {Name: "qname", Fold: true}, {Name: "qtype"}, {Name: "rcode"}}, Measures: []string{"count"}, Cap: caps},
			// qnames counts each name over every qtype and rcode; qtypes splits
			// names by qtype and rcode. Both are cut with the bound rule, so err
			// sums how far any count can be off; unbounded counts minutes from
			// collectors that do not report their own cut.
			{Name: "qnames", Keys: []tblk.Key{{Name: "zone"}, {Name: "qname", Fold: true}}, Measures: boundMeasures, Cap: caps, Bound: "err"},
			{Name: "qtypes", Keys: []tblk.Key{{Name: "zone"}, {Name: "qname", Fold: true}, {Name: "qtype"}, {Name: "rcode"}}, Measures: boundMeasures, Cap: caps, Bound: "err"},
			// clients held rows by network, zone, name, qtype and rcode, cut
			// by rows; like names it is no longer written and stays for older
			// blocks. Client lists start with nets, netnames and netcodes.
			{Name: "clients", Keys: []tblk.Key{{Name: "net", Kind: tblk.KeyNet}, {Name: "zone", Fold: true}, {Name: "qname", Fold: true}, {Name: "qtype", Fold: true}, {Name: "rcode", Fold: true}},
				Measures: []string{"count"}, Cap: caps},
			// nets counts each client network; netnames splits networks by
			// zone and name, netcodes by zone, qtype and rcode. All three are
			// cut with the bound rule. Rows below the cut keep only their
			// zone (a row per network would grow with every network). In nets
			// the folded network and the error share the Other row.
			{Name: "nets", Keys: []tblk.Key{{Name: "net", Kind: tblk.KeyNet, Fold: true}}, Measures: boundMeasures, Cap: caps, Bound: "err"},
			{Name: "netnames", Keys: []tblk.Key{{Name: "net", Kind: tblk.KeyNet, Fold: true}, {Name: "zone"}, {Name: "qname", Fold: true}}, Measures: boundMeasures, Cap: caps, Bound: "err"},
			{Name: "netcodes", Keys: []tblk.Key{{Name: "net", Kind: tblk.KeyNet, Fold: true}, {Name: "zone"}, {Name: "qtype", Fold: true}, {Name: "rcode", Fold: true}}, Measures: boundMeasures, Cap: caps, Bound: "err"},
			{Name: "sizes", Keys: []tblk.Key{{Name: "up_to", Kind: tblk.KeyInt}}, Measures: []string{"count"}},
			// minutes flags minutes whose rcodes are unknown (queries only).
			{Name: "minutes", Measures: []string{"queries_only"}},
		},
		Retention: keep.tiers(),
		MaxBytes:  maxBytes,
	}
}

// boundMeasures are the measures of the bounded name families.
var boundMeasures = []string{"count", "err", "unbounded"}

// boundRow is a bounded family's row carrying a minute's cut error.
func boundRow(n int, err uint64, bounded bool) tblk.Row {
	keys := make([]string, n)
	for i := range keys {
		keys[i] = tblk.Other
	}
	return tblk.Row{Keys: keys, Values: []int64{0, int64(err), int64(b2i(!bounded))}}
}

// withBound adds a minute's cut error to rows as the bound row (every key
// Other). In a family whose every key folds (nets) that row is also the
// folded row, which only one part of a split minute carries: the error then
// rides on it, and other parts add nothing, so a bare bound row never
// replaces it. A collector's error is above zero only when something folded,
// so that row exists whenever there is an error to carry.
func withBound(rows []tblk.Row, n int, err uint64, bounded, allFold bool) []tblk.Row {
	b := boundRow(n, err, bounded)
	if allFold {
		for i, r := range rows {
			if r.Keys[0] == tblk.Other {
				rows[i].Values[1] += b.Values[1]
				rows[i].Values[2] += b.Values[2]
				return rows
			}
		}
		if bounded {
			return rows
		}
	}
	return append(rows, b)
}

// netLists is a part's client lists: as sent by a bounded collector, or
// summed from an older collector's Clients rows, which are then unbounded
// (and, when one network's rows span parts, may count only one part's share:
// rows replace by key). Subnets that do not parse are skipped.
type netLists struct {
	nets                     []wire.Net
	names                    []wire.NetName
	codes                    []wire.NetCode
	netErr, nameErr, codeErr uint64
	bounded                  bool
}

func netsOf(d wire.Dnstap) netLists {
	ok := func(s string) bool {
		if s == wire.Other {
			return true
		}
		_, ok := netKeyOf(s)
		return ok
	}
	l := netLists{netErr: d.NetErr, nameErr: d.NetNameErr, codeErr: d.NetCodeErr, bounded: d.Bounded}
	if len(d.Nets) > 0 || len(d.NetNames) > 0 || len(d.NetCodes) > 0 || len(d.Clients) == 0 {
		for _, r := range d.Nets {
			if ok(r.Subnet) {
				l.nets = append(l.nets, r)
			}
		}
		for _, r := range d.NetNames {
			if ok(r.Subnet) {
				l.names = append(l.names, r)
			}
		}
		for _, r := range d.NetCodes {
			if ok(r.Subnet) {
				l.codes = append(l.codes, r)
			}
		}
		return l
	}
	l = netLists{}
	nets := map[string]uint64{}
	names := map[wire.NetName]uint64{}
	codes := map[wire.NetCode]uint64{}
	var netOrder []string
	var nameOrder []wire.NetName
	var codeOrder []wire.NetCode
	for _, r := range d.Clients {
		// Rows of the oldest collectors carry no name and cannot be split.
		if r.QName == "" || !ok(r.Subnet) {
			continue
		}
		if _, seen := nets[r.Subnet]; !seen {
			netOrder = append(netOrder, r.Subnet)
		}
		nets[r.Subnet] += r.Count
		nk := wire.NetName{Subnet: r.Subnet, Zone: r.Zone, QName: r.QName}
		if r.QName == wire.Other {
			// Folded rows keep only their zone, as a bounded collector sends them.
			nk.Subnet = wire.Other
		}
		if _, seen := names[nk]; !seen {
			nameOrder = append(nameOrder, nk)
		}
		names[nk] += r.Count
		ck := wire.NetCode{Subnet: r.Subnet, Zone: r.Zone, QType: r.QType, RCode: r.RCode}
		if _, seen := codes[ck]; !seen {
			codeOrder = append(codeOrder, ck)
		}
		codes[ck] += r.Count
	}
	for _, k := range netOrder {
		l.nets = append(l.nets, wire.Net{Subnet: k, Count: nets[k]})
	}
	for _, k := range nameOrder {
		k2 := k
		k2.Count = names[k]
		l.names = append(l.names, k2)
	}
	for _, k := range codeOrder {
		k2 := k
		k2.Count = codes[k]
		l.codes = append(l.codes, k2)
	}
	return l
}

// tiers is the retention per topn tier; zero keeps a tier forever.
func (r Retention) tiers() map[tblk.Grain]time.Duration {
	ms := func(v int64) time.Duration { return time.Duration(v) * time.Millisecond }
	return map[tblk.Grain]time.Duration{tblk.Minute: ms(r.MinuteMs), tblk.Hour: ms(r.HourlyMs), tblk.Day: ms(r.DailyMs)}
}

// TopN keeps dnstap rows in the block engine (KNOT_ENGINE=topn). knot.db
// still holds zones and dnstap_minutes, which carry freshness for checks and
// alerts with either engine.
type TopN struct {
	s *tblk.Store
	// meta is knot.db; the storage report runs without it.
	meta *Store
	keep Retention
}

// OpenTopN opens or creates the block store in dir. top is how many name and
// client rows each collector keeps per hour and per day.
func OpenTopN(dir string, meta *Store, top int, keep Retention, maxBytes int64) (*TopN, error) {
	return openTopN(dir, meta, top, keep, maxBytes, tblk.Options{Engine: tblk.Blocks})
}

// openTopN also takes the engine and driver, for the storage report.
func openTopN(dir string, meta *Store, top int, keep Retention, maxBytes int64, o tblk.Options) (*TopN, error) {
	s, err := tblk.Open(dir, knotSchema(top, keep, maxBytes), o)
	if err != nil {
		return nil, fmt.Errorf("knot topn: %w", err)
	}
	return &TopN{s: s, meta: meta, keep: keep}, nil
}

func (k *TopN) Close() error { return k.s.Close() }

// qnamesOf is a part's per-name counts: as sent by a collector that cuts by
// name, or summed from its name rows for an older collector (which is then
// unbounded, so its counts carry no error figure).
func qnamesOf(d wire.Dnstap) []wire.QName {
	if d.Bounded || len(d.QNames) > 0 {
		return d.QNames
	}
	sum := map[[2]string]uint64{}
	var keys [][2]string
	for _, r := range d.Names {
		k := [2]string{r.Zone, r.QName}
		if _, ok := sum[k]; !ok {
			keys = append(keys, k)
		}
		sum[k] += r.Count
	}
	out := make([]wire.QName, 0, len(keys))
	for _, k := range keys {
		out = append(out, wire.QName{Zone: k[0], QName: k[1], Count: sum[k]})
	}
	return out
}

// knotRows turns a dnstap part into rows per family, as the collector sent
// them, with each bounded family's cut error on its bound row.
func knotRows(d wire.Dnstap) map[string][]tblk.Row {
	out := map[string][]tblk.Row{}
	for _, r := range d.Totals {
		out["totals"] = append(out["totals"], tblk.Row{Keys: []string{r.Zone, r.QType, r.RCode}, Values: []int64{int64(r.Count)}})
	}
	for _, r := range d.Names {
		out["qtypes"] = append(out["qtypes"], tblk.Row{Keys: []string{r.Zone, r.QName, r.QType, r.RCode}, Values: []int64{int64(r.Count), 0, 0}})
	}
	for _, r := range qnamesOf(d) {
		out["qnames"] = append(out["qnames"], tblk.Row{Keys: []string{r.Zone, r.QName}, Values: []int64{int64(r.Count), 0, 0}})
	}
	out["qtypes"] = append(out["qtypes"], boundRow(4, d.NameErr, d.Bounded))
	out["qnames"] = append(out["qnames"], boundRow(2, d.QNameErr, d.Bounded))
	if l := netsOf(d); len(l.nets)+len(l.names)+len(l.codes) > 0 {
		var nets, names, codes []tblk.Row
		for _, r := range l.nets {
			nets = append(nets, tblk.Row{Keys: []string{r.Subnet}, Values: []int64{int64(r.Count), 0, 0}})
		}
		for _, r := range l.names {
			names = append(names, tblk.Row{Keys: []string{r.Subnet, r.Zone, r.QName}, Values: []int64{int64(r.Count), 0, 0}})
		}
		for _, r := range l.codes {
			codes = append(codes, tblk.Row{Keys: []string{r.Subnet, r.Zone, r.QType, r.RCode}, Values: []int64{int64(r.Count), 0, 0}})
		}
		out["nets"] = withBound(nets, 1, l.netErr, l.bounded, true)
		out["netnames"] = withBound(names, 3, l.nameErr, l.bounded, false)
		out["netcodes"] = withBound(codes, 4, l.codeErr, l.bounded, false)
	}
	for _, r := range d.Sizes {
		out["sizes"] = append(out["sizes"], tblk.Row{Keys: []string{strconv.Itoa(r.UpTo)}, Values: []int64{int64(r.Count)}})
	}
	out["minutes"] = []tblk.Row{{Values: []int64{int64(b2i(d.QueriesOnly))}}}
	return out
}

// SaveDnstap stores one dnstap part, replacing rows by key, so a resent part
// changes nothing; the minute is recorded in knot.db after the rows.
func (k *TopN) SaveDnstap(ctx context.Context, userID, collectorID string, d wire.Dnstap, now int64) error {
	bucket := d.MinuteMs - d.MinuteMs%minuteMs
	for fam, rows := range knotRows(d) {
		if err := k.s.Append(ctx, userID, collectorID, fam, bucket, rows); err != nil {
			return err
		}
	}
	if k.meta == nil {
		return nil
	}
	return k.meta.SaveMinute(ctx, userID, collectorID, bucket, d.QueriesOnly, now)
}

// Rollup seals settled minutes and merges them into hour and day blocks,
// with retention and the size cap; it returns the hours and days rolled.
func (k *TopN) Rollup(ctx context.Context, now int64) (int, error) {
	total := 0
	for range rollupRounds {
		r, err := k.s.Maintain(ctx, now)
		if err != nil {
			return total, err
		}
		total += r.Hours + r.Days
		if r.Hours+r.Days == 0 {
			break
		}
	}
	return total, nil
}

// Prune applies retention to the block store and knot.db, and drops the
// rows of accounts that no longer exist.
func (k *TopN) Prune(ctx context.Context, keep Retention, now int64, users []string) (PruneResult, error) {
	var res PruneResult
	if k.meta != nil {
		r, err := k.meta.Prune(ctx, keep, now, users)
		if err != nil {
			return r, err
		}
		res = r
	}
	known := map[string]bool{}
	for _, u := range users {
		known[u] = true
	}
	owners, err := k.s.Owners(ctx)
	if err != nil {
		return res, err
	}
	for _, o := range owners {
		if !known[o] {
			if err := k.s.DropOwner(ctx, o); err != nil {
				return res, err
			}
		}
	}
	p, err := k.s.Prune(ctx, now, keep.tiers())
	res.Minute += p.Rows[tblk.Minute]
	res.Hourly += p.Rows[tblk.Hour]
	res.Daily += p.Rows[tblk.Day]
	return res, err
}

// filtersOf turns a Query's filters into topn filters, in Store's order.
func filtersOf(q Query) []tblk.Filter {
	var out []tblk.Filter
	for _, f := range []struct{ key, val string }{{"zone", q.Zone}, {"qname", q.QName}, {"qtype", q.QType}, {"rcode", q.RCode}} {
		if f.val != "" {
			out = append(out, tblk.Filter{Key: f.key, Value: f.val})
		}
	}
	if q.Client.IsValid() {
		out = append(out, tblk.Filter{Key: "net", Prefix: q.Client})
	}
	return out
}

// Dnstap answers one drill-down the way Store.Dnstap does.
func (k *TopN) Dnstap(ctx context.Context, q Query, dim string, limit int) (Breakdown, error) {
	b := Breakdown{Dim: dim, Rows: []Row{}, Series: [][2]int64{}}
	source := sourceOf(q, dim)
	b.Exact = source == "totals"
	col := map[string]string{"qname": "qname", "zone": "zone", "qtype": "qtype", "rcode": "rcode", "client": "net", "size": "up_to"}[dim]
	if col == "" {
		return b, fmt.Errorf("unknown dimension %q", dim)
	}
	table := nameTable(q, dim, source)
	base := tblk.Query{Owner: q.UserID, Collector: q.CollectorID, Start: q.Start, End: q.End, MinuteFrom: q.MinuteFrom}
	bq := base
	bq.Family, bq.Filters, bq.By = table, filtersOf(q), col
	bq.KeepOther = !foldsOther(table, dim)
	res, err := k.s.Query(ctx, bq)
	if err != nil {
		return b, err
	}
	for _, f := range res.Ignored {
		if f == "net" {
			f = "client"
		}
		b.Ignored = append(b.Ignored, f)
	}
	b.Other = res.Other[0]
	for _, r := range res.Rows {
		b.Rows = append(b.Rows, Row{Key: r.Key, Count: r.Values[0]})
	}
	if dim == "size" {
		sort.Slice(b.Rows, func(i, j int) bool { return sizeOrder(b.Rows[i].Key) < sizeOrder(b.Rows[j].Key) })
	}
	if limit > 0 && len(b.Rows) > limit {
		for _, r := range b.Rows[limit:] {
			b.Other += r.Count
		}
		b.Rows = b.Rows[:limit]
	}
	ft := foldTable(table, source)
	if (dim == "qname" || source != "totals") && !isNetTable(ft) {
		fq := q
		fq.QName, fq.QType, fq.RCode = wire.Other, "", ""
		f := base
		f.Family, f.Filters = ft, filtersOf(fq)
		fr, err := k.s.Query(ctx, f)
		if err != nil {
			return b, err
		}
		b.Approximate = fr.Total[0] > 0
	}
	if ft == "qnames" || ft == "qtypes" || isNetTable(ft) {
		// The bound rows carry every cut's error; they match no filter but
		// the collector, so they are summed on their own.
		eq := base
		eq.Family, eq.Measures = ft, []string{"err", "unbounded"}
		er, err := k.s.Query(ctx, eq)
		if err != nil {
			return b, err
		}
		b.ErrBound, b.Bounded = er.Total[0], er.Total[1] == 0
		if isNetTable(ft) {
			b.Approximate = b.ErrBound > 0 || !b.Bounded
		}
	}
	sq := base
	sq.Family, sq.Filters, sq.Step = source, filtersOf(q), max(q.Step, minuteMs)
	sr, err := k.s.Query(ctx, sq)
	if err != nil {
		return b, err
	}
	b.Total, b.Series = sr.Total[0], sr.Series
	shortOther(&b, table, source)
	mq := base
	mq.Family = "minutes"
	mr, err := k.s.Query(ctx, mq)
	if err != nil {
		return b, err
	}
	b.QueriesOnly = mr.Total[0] > 0
	return b, nil
}
