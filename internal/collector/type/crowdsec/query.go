package crowdsec

import (
	"context"
	"database/sql"
)

// Coverage says which table answered and how far back each table reaches.
type Coverage struct {
	Source              string `json:"source"`
	RawSince            int64  `json:"raw_since"`
	RollupSince         int64  `json:"rollup_since"`
	DetailExpiredBefore int64  `json:"detail_expired_before,omitempty"`
}

func (s *Store) coverage(ctx context.Context, userID string) (raw, rollup int64, err error) {
	var r, h sql.NullInt64
	if err = s.read.QueryRowContext(ctx, `SELECT MIN(started_at) FROM alerts WHERE user_id = ?`, userID).Scan(&r); err != nil {
		return
	}
	if err = s.read.QueryRowContext(ctx, `SELECT MIN(hour) FROM rollup_hourly WHERE user_id = ?`, userID).Scan(&h); err != nil {
		return
	}
	return r.Int64, h.Int64, nil
}

// plan picks raw alerts or the rollup. Anything the rollup lacks reads raw alerts;
// otherwise long ranges, or ranges older than raw retention, read the rollup. When
// raw detail is needed for a period whose raw alerts were pruned, it says so.
func (s *Store) plan(ctx context.Context, userID string, f Filter, needRaw bool) (Coverage, error) {
	raw, rollup, err := s.coverage(ctx, userID)
	if err != nil {
		return Coverage{}, err
	}
	c := Coverage{Source: "raw", RawSince: raw, RollupSince: rollup}
	pruned := rollup > 0 && (raw == 0 || rollup < hourOf(raw))
	need := needRaw || f.NeedsRaw()
	if !need && (f.End-f.Start > rollupAfterMs || (pruned && f.Start < raw)) {
		c.Source = "rollup"
		return c, nil
	}
	if pruned && (raw == 0 || f.Start < raw) {
		c.DetailExpiredBefore = raw
		if raw == 0 {
			c.DetailExpiredBefore = f.End
		}
	}
	return c, nil
}

type Summary struct {
	Coverage
	Alerts    int64            `json:"alerts"`
	Events    int64            `json:"events"`
	UniqueIPs *int64           `json:"unique_ips"`
	Active    map[string]int64 `json:"active"`
}

func (s *Store) Summary(ctx context.Context, userID string, f Filter, now int64) (*Summary, error) {
	cov, err := s.plan(ctx, userID, f, false)
	if err != nil {
		return nil, err
	}
	out := &Summary{Coverage: cov, Active: map[string]int64{}}
	if cov.Source == "rollup" {
		where, args := f.where(userID, true, true, "")
		if err := s.read.QueryRowContext(ctx, `SELECT COALESCE(SUM(alerts), 0), COALESCE(SUM(events), 0) FROM rollup_hourly WHERE `+where, args...).Scan(&out.Alerts, &out.Events); err != nil {
			return nil, err
		}
	} else {
		var ips int64
		where, args := f.where(userID, false, true, "")
		if err := s.read.QueryRowContext(ctx, `SELECT COUNT(*), COALESCE(SUM(events_count), 0), COUNT(DISTINCT NULLIF(ip, '')) FROM alerts WHERE `+where, args...).Scan(&out.Alerts, &out.Events, &ips); err != nil {
			return nil, err
		}
		out.UniqueIPs = &ips
	}
	// Active decisions are a "now" figure: filters apply, the time range does not.
	where, args := f.where(userID, false, false, "alerts.")
	rows, err := s.read.QueryContext(ctx, `SELECT d.type, COUNT(DISTINCT d.value) FROM decisions d JOIN alerts ON alerts.id = d.alert_id
		WHERE d.until_ms > ? AND `+where+` GROUP BY d.type`, append([]any{now}, args...)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var typ string
		var n int64
		if err := rows.Scan(&typ, &n); err != nil {
			return nil, err
		}
		out.Active[typ] = n
	}
	return out, rows.Err()
}

var timelineWidths = []int64{60_000, 5 * 60_000, 15 * 60_000, 30 * 60_000, hourMs, 3 * hourMs, 6 * hourMs, 12 * hourMs, 24 * hourMs, 7 * 24 * hourMs}

// bucketWidth picks the smallest width giving at most 360 buckets, and never more than 1,000.
func bucketWidth(span int64, rollup bool) int64 {
	for _, w := range timelineWidths {
		if rollup && w < hourMs {
			continue
		}
		if span/w <= 360 {
			return w
		}
	}
	return timelineWidths[len(timelineWidths)-1]
}

type Timeline struct {
	Coverage
	WidthMs int64 `json:"width_ms"`
	// Points are [bucket start, alerts, decisions issued by those alerts].
	Points [][3]int64 `json:"points"`
}

func (s *Store) Timeline(ctx context.Context, userID string, f Filter) (*Timeline, error) {
	cov, err := s.plan(ctx, userID, f, false)
	if err != nil {
		return nil, err
	}
	rollup := cov.Source == "rollup"
	w := bucketWidth(f.End-f.Start, rollup)
	off := f.TZOffset * 60_000
	where, args := f.where(userID, rollup, true, "")
	query := `SELECT ((started_at + ?) / ?) * ? - ?, COUNT(*), COALESCE(SUM((SELECT COUNT(*) FROM decisions WHERE decisions.alert_id = alerts.id)), 0)
		FROM alerts WHERE ` + where + ` GROUP BY 1`
	if rollup {
		query = `SELECT ((hour + ?) / ?) * ? - ?, SUM(alerts), SUM(decisions) FROM rollup_hourly WHERE ` + where + ` GROUP BY 1`
	}
	rows, err := s.read.QueryContext(ctx, query, append([]any{off, w, w, off}, args...)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	counts := map[int64][2]int64{}
	for rows.Next() {
		var b, n, d int64
		if err := rows.Scan(&b, &n, &d); err != nil {
			return nil, err
		}
		counts[b] = [2]int64{n, d}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := &Timeline{Coverage: cov, WidthMs: w, Points: [][3]int64{}}
	first := ((f.Start+off)/w)*w - off
	for b := first; b < f.End && len(out.Points) < 1000; b += w {
		c := counts[b]
		out.Points = append(out.Points, [3]int64{b, c[0], c[1]})
	}
	return out, nil
}

type Group struct {
	Value  string `json:"value"`
	Label  string `json:"label,omitempty"`
	Alerts int64  `json:"alerts"`
	Events int64  `json:"events"`
}

type Groups struct {
	Coverage
	Field string  `json:"field"`
	Total int64   `json:"total"`
	Items []Group `json:"items"`
}

func (s *Store) Groups(ctx context.Context, userID string, f Filter, fd field, limit int) (*Groups, error) {
	cov, err := s.plan(ctx, userID, f, !fd.Rollup)
	if err != nil {
		return nil, err
	}
	out := &Groups{Coverage: cov, Field: fd.Param, Items: []Group{}}
	var query string
	rollup := cov.Source == "rollup"
	where, args := f.where(userID, rollup, true, "")
	label := "''"
	if fd.Param == "asn" {
		label = "MAX(as_name)"
	}
	if rollup {
		query = `SELECT ` + fd.Column + `, '', SUM(alerts), SUM(events), SUM(SUM(alerts)) OVER () FROM rollup_hourly WHERE ` + where
	} else {
		query = `SELECT ` + fd.Column + `, ` + label + `, COUNT(*), SUM(events_count), SUM(COUNT(*)) OVER () FROM alerts WHERE ` + where
	}
	query += ` GROUP BY 1 ORDER BY 3 DESC, 1 LIMIT ?`
	rows, err := s.read.QueryContext(ctx, query, append(args, limit)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var g Group
		if err := rows.Scan(&g.Value, &g.Label, &g.Alerts, &g.Events, &out.Total); err != nil {
			return nil, err
		}
		out.Items = append(out.Items, g)
	}
	return out, rows.Err()
}
