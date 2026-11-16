package knot

import (
	"context"
	"fmt"
	"time"
)

const pruneBatch = 5000

// Default dnstap retention: minutes for a week, hours for 90 days.
const (
	DefaultMinuteRetention = 7 * 24 * time.Hour
	DefaultHourlyRetention = 90 * 24 * time.Hour
)

// Retention is how long dnstap rows are kept, in milliseconds; zero keeps them forever.
type Retention struct {
	MinuteMs int64
	HourlyMs int64
}

// PruneResult counts deleted rows.
type PruneResult struct {
	Minute, Hourly, Zones int64
}

func (r PruneResult) String() string {
	return fmt.Sprintf("knot_pruned_minute=%d knot_pruned_hourly=%d knot_pruned_zones=%d", r.Minute, r.Hourly, r.Zones)
}

// deleteBatches repeats a bounded DELETE so the writer is never held for long.
// query must select the rows to delete by rowid-free key with a trailing LIMIT ?.
func (s *Store) deleteBatches(ctx context.Context, query string, args ...any) (int64, error) {
	var total int64
	for {
		res, err := s.write.ExecContext(ctx, query, append(args, pruneBatch)...)
		if err != nil {
			return total, err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return total, err
		}
		total += n
		if n < pruneBatch || ctx.Err() != nil {
			return total, ctx.Err()
		}
	}
}

func pkOf(dim string) string {
	for _, d := range dims {
		if d.name == dim {
			pk := "user_id, collector_id, bucket"
			for _, k := range d.keys {
				pk += ", " + k
			}
			return pk
		}
	}
	return ""
}

// Prune applies retention and drops rows of accounts that no longer exist,
// since this file cannot reference the main database's users.
func (s *Store) Prune(ctx context.Context, keep Retention, now int64, users []string) (PruneResult, error) {
	var out PruneResult
	known := make(map[string]bool, len(users))
	for _, u := range users {
		known[u] = true
	}
	owners, err := s.owners(ctx)
	if err != nil {
		return out, err
	}
	for _, owner := range owners {
		if known[owner] {
			continue
		}
		for _, d := range dims {
			for _, grain := range []string{"minute", "hourly"} {
				table, pk := "dnstap_"+d.name+"_"+grain, pkOf(d.name)
				n, err := s.deleteBatches(ctx, fmt.Sprintf(`DELETE FROM %s WHERE (%s) IN (SELECT %s FROM %s WHERE user_id = ? LIMIT ?)`, table, pk, pk, table), owner)
				if grain == "minute" {
					out.Minute += n
				} else {
					out.Hourly += n
				}
				if err != nil {
					return out, err
				}
			}
		}
		n, err := s.deleteBatches(ctx, `DELETE FROM zones WHERE (user_id, collector_id, zone) IN (SELECT user_id, collector_id, zone FROM zones WHERE user_id = ? LIMIT ?)`, owner)
		out.Zones += n
		if err != nil {
			return out, err
		}
		if _, err := s.deleteBatches(ctx, `DELETE FROM dnstap_minutes WHERE (user_id, collector_id, bucket) IN (SELECT user_id, collector_id, bucket FROM dnstap_minutes WHERE user_id = ? LIMIT ?)`, owner); err != nil {
			return out, err
		}
	}
	if keep.MinuteMs > 0 {
		cut := now - keep.MinuteMs
		for _, d := range dims {
			table, pk := "dnstap_"+d.name+"_minute", pkOf(d.name)
			n, err := s.deleteBatches(ctx, fmt.Sprintf(`DELETE FROM %s WHERE (%s) IN (SELECT %s FROM %s WHERE bucket < ? LIMIT ?)`, table, pk, pk, table), cut)
			out.Minute += n
			if err != nil {
				return out, err
			}
		}
		if _, err := s.deleteBatches(ctx, `DELETE FROM dnstap_minutes WHERE (user_id, collector_id, bucket) IN (SELECT user_id, collector_id, bucket FROM dnstap_minutes WHERE bucket < ? LIMIT ?)`, cut); err != nil {
			return out, err
		}
	}
	if keep.HourlyMs > 0 {
		cut := now - keep.HourlyMs
		for _, d := range dims {
			table, pk := "dnstap_"+d.name+"_hourly", pkOf(d.name)
			n, err := s.deleteBatches(ctx, fmt.Sprintf(`DELETE FROM %s WHERE (%s) IN (SELECT %s FROM %s WHERE bucket < ? LIMIT ?)`, table, pk, pk, table), cut)
			out.Hourly += n
			if err != nil {
				return out, err
			}
		}
	}
	return out, nil
}

func (s *Store) owners(ctx context.Context) ([]string, error) {
	rows, err := s.read.QueryContext(ctx, `SELECT user_id FROM zones UNION SELECT user_id FROM dnstap_minutes UNION SELECT user_id FROM dnstap_totals_hourly`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		list = append(list, id)
	}
	return list, rows.Err()
}
