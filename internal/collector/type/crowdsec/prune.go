package crowdsec

import (
	"context"
	"fmt"
)

const pruneBatch = 5000

// Retention is how long each table keeps data, in milliseconds; zero keeps it forever.
type Retention struct {
	RawMs    int64
	RollupMs int64
}

type PruneResult struct {
	Alerts, Rollup        int64 // rows deleted
	AlertRows, RollupRows int64 // rows kept
}

func (r PruneResult) String() string {
	return fmt.Sprintf("crowdsec_alerts=%d crowdsec_rollup=%d pruned_alerts=%d pruned_rollup=%d", r.AlertRows, r.RollupRows, r.Alerts, r.Rollup)
}

// deleteBatches repeats a bounded DELETE so the writer is never held for long.
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

// Prune applies retention and removes rows of accounts that no longer exist,
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
		n, err := s.deleteBatches(ctx, `DELETE FROM alerts WHERE id IN (SELECT id FROM alerts WHERE user_id = ? LIMIT ?)`, owner)
		out.Alerts += n
		if err != nil {
			return out, err
		}
		n, err = s.deleteBatches(ctx, `DELETE FROM rollup_hourly WHERE (user_id, hour, scenario, country, machine_id, action) IN
			(SELECT user_id, hour, scenario, country, machine_id, action FROM rollup_hourly WHERE user_id = ? LIMIT ?)`, owner)
		out.Rollup += n
		if err != nil {
			return out, err
		}
	}
	if keep.RawMs > 0 {
		n, err := s.deleteBatches(ctx, `DELETE FROM alerts WHERE id IN (SELECT id FROM alerts WHERE started_at < ? LIMIT ?)`, now-keep.RawMs)
		out.Alerts += n
		if err != nil {
			return out, err
		}
	}
	if keep.RollupMs > 0 {
		for _, owner := range users {
			n, err := s.deleteBatches(ctx, `DELETE FROM rollup_hourly WHERE (user_id, hour, scenario, country, machine_id, action) IN
				(SELECT user_id, hour, scenario, country, machine_id, action FROM rollup_hourly WHERE user_id = ? AND hour < ? LIMIT ?)`, owner, hourOf(now-keep.RollupMs))
			out.Rollup += n
			if err != nil {
				return out, err
			}
		}
	}
	if s.rec != nil {
		// The record store applies the same retention and drops the rows of
		// accounts that no longer exist; crowdsec.db's alert rows above are
		// what was kept before switching, pruned as before.
		var before int64
		if keep.RawMs > 0 {
			before = now - keep.RawMs
		}
		st, err := s.rec.s.Stats(ctx)
		if err != nil {
			return out, err
		}
		if err := s.rec.s.Prune(ctx, before, users); err != nil {
			return out, err
		}
		after, err := s.rec.s.Stats(ctx)
		if err != nil {
			return out, err
		}
		out.Alerts += st.Rows - after.Rows
		out.AlertRows += after.Rows
	}
	var sqlRows int64
	if err := s.read.QueryRowContext(ctx, `SELECT COUNT(*) FROM alerts`).Scan(&sqlRows); err != nil {
		return out, err
	}
	out.AlertRows += sqlRows
	err = s.read.QueryRowContext(ctx, `SELECT COUNT(*) FROM rollup_hourly`).Scan(&out.RollupRows)
	return out, err
}

func (s *Store) owners(ctx context.Context) ([]string, error) {
	rows, err := s.read.QueryContext(ctx, `SELECT DISTINCT user_id FROM alerts UNION SELECT DISTINCT user_id FROM rollup_hourly`)
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
