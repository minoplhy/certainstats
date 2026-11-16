package knot

import (
	"context"
	"fmt"
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
	Start, End  int64 // [Start, End) Unix ms
	Step        int64 // series bucket width
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
	// Ignored lists filters this dimension cannot apply (clients and sizes are per collector only).
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
	// The total and series come from exact totals unless a name filter forces name rows.
	source := "totals"
	if q.QName != "" {
		source = "names"
	}
	b.Exact = source == "totals"
	col := map[string]string{"qname": "qname", "zone": "zone", "qtype": "qtype", "rcode": "rcode", "client": "subnet", "size": "up_to"}[dim]
	if col == "" {
		return b, fmt.Errorf("unknown dimension %q", dim)
	}
	table := source
	switch dim {
	case "qname":
		table = "names"
	case "client":
		table = "clients"
	case "size":
		table = "sizes"
	}
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
		n := r[1].(int64)
		if dim == "qname" && key == wire.Other {
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
	if dim == "qname" || source == "names" {
		// Any folded name row in range means names below the cap are missing from the list.
		fq := q
		fq.QName, fq.QType, fq.RCode = wire.Other, "", ""
		folded, _, err := s.union(ctx, fq, "names", "1 AS k, count AS n", "")
		if err != nil {
			return b, err
		}
		b.Approximate = len(folded) > 0 && folded[0][1].(int64) > 0
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

func sizeOrder(k string) int {
	n, _ := strconv.Atoi(k)
	if n == 0 {
		return 1 << 30
	}
	return n
}
