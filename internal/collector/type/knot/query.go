package knot

import (
	"context"
	"fmt"
	"net/netip"
	"sort"
	"strconv"
	"strings"

	"certainstats/internal/collector/type/knot/wire"
)

// Query selects dnstap rows. Empty fields match everything.
type Query struct {
	UserID      string
	CollectorID string
	Zone        string
	QName       string
	QType       string
	RCode       string
	// Client limits rows to client networks inside this prefix; zero matches everything.
	Client     netip.Prefix
	Start, End int64 // [Start, End) Unix ms
	Step       int64 // series bucket width
	// MinuteFrom is the oldest minute still kept; ranges starting earlier read hourly rows.
	MinuteFrom int64
}

// Row is one breakdown value.
type Row struct {
	Key   string `json:"key"`
	Count int64  `json:"count"`
}

// Breakdown is a dnstap answer for one dimension.
type Breakdown struct {
	Dim   string `json:"dim"`
	Total int64  `json:"total"`
	Rows  []Row  `json:"rows"`
	Other int64  `json:"other"`
	// Approximate is set when name rows below the per-minute cap were folded away,
	// so counts by name are lower bounds.
	Approximate bool `json:"approximate"`
	// Exact names the source of Total: true when it comes from uncapped totals.
	Exact bool `json:"exact"`
	// ErrBound is how far any count by name can be below the truth, summing
	// every cut made on the way (collector, hour, day); it is set for answers
	// read from the name tables. Bounded is false when part of the range came
	// from collectors that do not report their cuts, so ErrBound is incomplete.
	ErrBound int64 `json:"err_bound"`
	Bounded  bool  `json:"bounded"`
	// Ignored lists filters this dimension cannot apply (sizes carry no zone, name or client).
	Ignored []string   `json:"ignored,omitempty"`
	Series  [][2]int64 `json:"series"`
	// QueriesOnly is set when some minutes had no responses logged, so rcodes are unknown there.
	QueriesOnly bool `json:"queries_only"`
}

// spans splits [Start, End) into the hourly part (before rolledUntil) and the
// minute part. A range that starts within minute retention reads minutes only.
func spans(q Query, rolled int64) (hourly [2]int64, minute [2]int64) {
	if q.Start >= q.MinuteFrom || rolled <= q.Start {
		return [2]int64{0, 0}, [2]int64{q.Start, q.End}
	}
	cut := min(rolled, q.End)
	return [2]int64{hourOf(q.Start), cut}, [2]int64{cut, q.End}
}

// filters builds the WHERE clause for a table's key columns.
func filters(q Query, keys []string) (string, []any, []string) {
	where := []string{"user_id = ?"}
	args := []any{q.UserID}
	if q.CollectorID != "" {
		where = append(where, "collector_id = ?")
		args = append(args, q.CollectorID)
	}
	has := map[string]bool{}
	for _, k := range keys {
		has[k] = true
	}
	var ignored []string
	for _, f := range []struct{ col, val string }{{"zone", q.Zone}, {"qname", q.QName}, {"qtype", q.QType}, {"rcode", q.RCode}} {
		if f.val == "" {
			continue
		}
		if !has[f.col] {
			ignored = append(ignored, f.col)
			continue
		}
		where = append(where, f.col+" = ?")
		args = append(args, f.val)
	}
	if q.Client.IsValid() {
		if has["net"] {
			lo, hi := netRange(q.Client)
			where = append(where, "net >= ? AND net < ?")
			args = append(args, lo, hi)
		} else {
			ignored = append(ignored, "client")
		}
	}
	return strings.Join(where, " AND "), args, ignored
}

// union reads a select over the hourly and minute tables of one dimension.
func (s *Store) union(ctx context.Context, q Query, dim, sel, group string) ([][]any, []string, error) {
	rolled, err := s.rolledUntil(ctx)
	if err != nil {
		return nil, nil, err
	}
	var keys []string
	for _, d := range dims {
		if d.name == dim {
			keys = d.keys
		}
	}
	hourly, minute := spans(q, rolled)
	where, args, ignored := filters(q, keys)
	var parts []string
	var all []any
	for _, p := range []struct {
		table string
		span  [2]int64
	}{{"dnstap_" + dim + "_hourly", hourly}, {"dnstap_" + dim + "_minute", minute}} {
		if p.span[1] <= p.span[0] {
			continue
		}
		parts = append(parts, fmt.Sprintf(`SELECT %s FROM %s WHERE %s AND bucket >= ? AND bucket < ?`, sel, p.table, where))
		all = append(append(all, args...), p.span[0], p.span[1])
	}
	if len(parts) == 0 {
		return nil, ignored, nil
	}
	query := `SELECT k, SUM(n) FROM (` + strings.Join(parts, " UNION ALL ") + `) GROUP BY k`
	if group != "" {
		query += " " + group
	}
	rows, err := s.read.QueryContext(ctx, query, all...)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	var out [][]any
	for rows.Next() {
		var k any
		var n int64
		if err := rows.Scan(&k, &n); err != nil {
			return nil, nil, err
		}
		out = append(out, []any{k, n})
	}
	return out, ignored, rows.Err()
}

// Dnstap answers one drill-down: a breakdown by dim within the filters, with
// the matching total and its series over time.
func (s *Store) Dnstap(ctx context.Context, q Query, dim string, limit int) (Breakdown, error) {
	b := Breakdown{Dim: dim, Rows: []Row{}, Series: [][2]int64{}}
	// The total and series come from exact totals unless a filter forces finer rows:
	// a client filter needs client rows, a name filter name rows.
	source := sourceOf(q, dim)
	b.Exact = source == "totals"
	col := map[string]string{"qname": "qname", "zone": "zone", "qtype": "qtype", "rcode": "rcode", "client": "net", "size": "up_to"}[dim]
	if col == "" {
		return b, fmt.Errorf("unknown dimension %q", dim)
	}
	table := nameTable(q, dim, source)
	rows, ignored, err := s.union(ctx, q, table, col+" AS k, count AS n", "")
	if err != nil {
		return b, err
	}
	b.Ignored = ignored
	for _, r := range rows {
		key := fmt.Sprint(r[0])
		if v, ok := r[0].([]byte); ok {
			key = string(v)
		}
		if dim == "client" {
			key = netLabel(key)
		}
		n := r[1].(int64)
		if foldsOther(table, dim) && key == wire.Other {
			b.Other += n
			continue
		}
		b.Rows = append(b.Rows, Row{Key: key, Count: n})
	}
	sort.Slice(b.Rows, func(i, j int) bool {
		if b.Rows[i].Count != b.Rows[j].Count {
			return b.Rows[i].Count > b.Rows[j].Count
		}
		return b.Rows[i].Key < b.Rows[j].Key
	})
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
		// Any folded name row in range means names below the cap are missing from the list.
		fq := q
		fq.QName, fq.QType, fq.RCode = wire.Other, "", ""
		folded, _, err := s.union(ctx, fq, ft, "1 AS k, count AS n", "")
		if err != nil {
			return b, err
		}
		b.Approximate = len(folded) > 0 && folded[0][1].(int64) > 0
	}
	if ft == "qnames" || ft == "qtypes" || isNetTable(ft) {
		bq := Query{UserID: q.UserID, CollectorID: q.CollectorID, Start: q.Start, End: q.End, MinuteFrom: q.MinuteFrom}
		brows, _, err := s.union(ctx, bq, "bounds", "family || '/' || kind AS k, count AS n", "")
		if err != nil {
			return b, err
		}
		b.Bounded = true
		for _, r := range brows {
			key := fmt.Sprint(r[0])
			if v, ok := r[0].([]byte); ok {
				key = string(v)
			}
			switch key {
			case ft + "/err":
				b.ErrBound = r[1].(int64)
			case ft + "/unbounded":
				b.Bounded = r[1].(int64) == 0
			}
		}
		if isNetTable(ft) {
			// A client list is short of nothing unless a cut or an older
			// collector left an error.
			b.Approximate = b.ErrBound > 0 || !b.Bounded
		}
	}
	// Total and series over time, from the source table.
	step := max(q.Step, minuteMs)
	srows, _, err := s.union(ctx, q, source, fmt.Sprintf("(bucket / %d) * %d AS k, count AS n", step, step), "ORDER BY k")
	if err != nil {
		return b, err
	}
	for _, r := range srows {
		t, _ := strconv.ParseInt(fmt.Sprint(r[0]), 10, 64)
		n := r[1].(int64)
		b.Total += n
		b.Series = append(b.Series, [2]int64{t, n})
	}
	b.Series = zeroFill(b.Series, q.Start, q.End, step)
	shortOther(&b, table, source)
	where := "user_id = ? AND queries_only = 1 AND bucket >= ? AND bucket < ?"
	args := []any{q.UserID, q.Start, q.End}
	if q.CollectorID != "" {
		where += " AND collector_id = ?"
		args = append(args, q.CollectorID)
	}
	var qo int
	if err := s.read.QueryRowContext(ctx, `SELECT COUNT(*) FROM dnstap_minutes WHERE `+where, args...).Scan(&qo); err != nil {
		return b, err
	}
	b.QueriesOnly = qo > 0
	return b, nil
}

// sourceOf is the table an answer's total and series come from: exact
// totals unless a filter forces finer rows. A client filter reads the client
// list that applies the most filters (the per-network counts when there is
// no other filter, since rows below the cut lose their network in the split
// lists); a name filter reads the split name rows.
func sourceOf(q Query, dim string) string {
	switch {
	case q.Client.IsValid():
		return clientSource(q)
	case q.QName != "":
		return "qtypes"
	}
	return "totals"
}

// nameTable is the table a breakdown by dim reads, given the source of its
// total: names by qname read qnames unless a qtype or rcode filter needs the
// split rows (qtypes); client answers read one of the client lists (see
// clientTable). Both engines route the same way.
func nameTable(q Query, dim, source string) string {
	if isNetTable(source) {
		if t := clientTable(q, dim); t != "" {
			return t
		}
		if dim == "size" {
			return "sizes"
		}
		return source
	}
	switch dim {
	case "qname":
		if q.QType == "" && q.RCode == "" {
			return "qnames"
		}
		return "qtypes"
	case "client":
		return clientSource(q)
	case "size":
		return "sizes"
	}
	return source
}

// isNetTable reports whether t is one of the client lists.
func isNetTable(t string) bool { return t == "nets" || t == "netnames" || t == "netcodes" }

// clientSource is the client list that applies the most of q's filters: a
// name filter needs netnames, a zone, qtype or rcode filter netcodes, and no
// filter the per-network counts.
func clientSource(q Query) string {
	switch {
	case q.QName != "":
		return "netnames"
	case q.Zone != "" || q.QType != "" || q.RCode != "":
		return "netcodes"
	}
	return "nets"
}

// clientTable is the client list a breakdown by dim reads: networks from the
// list that applies the most filters, names from netnames, qtypes and
// rcodes from netcodes, zones from either. A filter the list lacks (a qtype
// with names, a name with qtypes) is reported as ignored. Sizes are not
// split by network ("").
func clientTable(q Query, dim string) string {
	switch dim {
	case "client":
		return clientSource(q)
	case "qname":
		return "netnames"
	case "qtype", "rcode":
		return "netcodes"
	case "zone":
		if q.QName != "" {
			return "netnames"
		}
		return "netcodes"
	}
	return ""
}

// foldTable is the table whose folded rows and cut errors describe an answer:
// a client list read; else the source when a filter forced finer rows, else
// the table read.
func foldTable(table, source string) string {
	switch {
	case isNetTable(table):
		return table
	case source != "totals":
		return source
	}
	return table
}

// shortOther adds to Other what a client answer's rows leave out of its
// total: the total comes from the per-network counts, while rows below the
// cut in the split lists lost their network.
func shortOther(b *Breakdown, table, source string) {
	if !isNetTable(table) || table == source {
		return
	}
	listed := b.Other
	for _, r := range b.Rows {
		listed += r.Count
	}
	if b.Total > listed {
		b.Other += b.Total - listed
	}
}

// foldsOther reports whether a table's Other rows count as Other rather than
// as a row of their own: names by qname, and every client list.
func foldsOther(table, dim string) bool {
	return isNetTable(table) || (dim == "qname" && (table == "qnames" || table == "qtypes"))
}

// maxFill bounds how many buckets zeroFill writes; the page's windows stay
// near 1,000.
const maxFill = 5000

// zeroFill gives every bucket of [start, end) a point. Only buckets with
// traffic have rows, and a quiet minute means no queries, not an outage: the
// chart would otherwise draw the gaps as offline bands.
func zeroFill(series [][2]int64, start, end, step int64) [][2]int64 {
	if step <= 0 || end <= start || (end-start)/step > maxFill {
		return series
	}
	first := start - start%step
	out := make([][2]int64, 0, (end-first)/step+1)
	i := 0
	for t := first; t < end; t += step {
		for i < len(series) && series[i][0] < t {
			out = append(out, series[i]) // off-grid rows are kept as they are
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

func sizeOrder(k string) int {
	n, _ := strconv.Atoi(k)
	if n == 0 {
		return 1 << 30
	}
	return n
}
