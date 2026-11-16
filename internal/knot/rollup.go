package knot

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"certainstats/internal/knot/wire"
)

const (
	// rollLag keeps the newest hours as minutes only: collectors resend
	// unacknowledged minutes for up to an hour, so an hour is rolled once settled.
	rollLag = 2 * time.Hour
	// rollBatch caps the hours rolled per maintenance run.
	rollBatch = 72
)

func hourOf(ms int64) int64 { return ms - ms%hourMs }

// Rollup sums settled hours of minute rows into the hourly tables. Names keep
// the top TopHourly rows per collector and hour; the rest fold into Other rows
// per zone, qtype and rcode, so they still add up to the totals.
func (s *Store) Rollup(ctx context.Context, now int64) (int, error) {
	until, err := s.rolledUntil(ctx)
	if err != nil {
		return 0, err
	}
	if until == 0 {
		var oldest *int64
		if err := s.read.QueryRowContext(ctx, `SELECT MIN(bucket) FROM dnstap_minutes`).Scan(&oldest); err != nil {
			return 0, err
		}
		if oldest == nil {
			return 0, nil
		}
		until = hourOf(*oldest)
	}
	target := hourOf(now - rollLag.Milliseconds())
	done := 0
	for h := until; h < target && done < rollBatch && ctx.Err() == nil; h += hourMs {
		if err := s.rollHour(ctx, h); err != nil {
			return done, err
		}
		done++
	}
	return done, ctx.Err()
}

func (s *Store) rollHour(ctx context.Context, h int64) error {
	tx, err := s.write.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, d := range dims {
		minuteT, hourT := "dnstap_"+d.name+"_minute", "dnstap_"+d.name+"_hourly"
		if _, err := tx.ExecContext(ctx, `DELETE FROM `+hourT+` WHERE bucket = ?`, h); err != nil {
			return err
		}
		if d.name == "names" {
			continue
		}
		keys := strings.Join(d.keys, ", ")
		if _, err := tx.ExecContext(ctx, fmt.Sprintf(`INSERT INTO %s (user_id, collector_id, bucket, %s, count)
			SELECT user_id, collector_id, ?, %s, SUM(count) FROM %s WHERE bucket >= ? AND bucket < ? GROUP BY user_id, collector_id, %s`,
			hourT, keys, keys, minuteT, keys), h, h, h+hourMs); err != nil {
			return err
		}
	}
	rows, err := tx.QueryContext(ctx, `SELECT user_id, collector_id, zone, qname, qtype, rcode, SUM(count) AS n FROM dnstap_names_minute
		WHERE bucket >= ? AND bucket < ? GROUP BY user_id, collector_id, zone, qname, qtype, rcode`, h, h+hourMs)
	if err != nil {
		return err
	}
	type row struct {
		user, col, zone, qname, qtype, rcode string
		n                                    int64
	}
	groups := map[[2]string][]row{}
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.user, &r.col, &r.zone, &r.qname, &r.qtype, &r.rcode, &r.n); err != nil {
			rows.Close()
			return err
		}
		groups[[2]string{r.user, r.col}] = append(groups[[2]string{r.user, r.col}], r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	stmt, err := tx.PrepareContext(ctx, `INSERT INTO dnstap_names_hourly (user_id, collector_id, bucket, zone, qname, qtype, rcode, count) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, list := range groups {
		named := list[:0:0]
		other := map[[3]string]int64{}
		for _, r := range list {
			if r.qname == wire.Other {
				other[[3]string{r.zone, r.qtype, r.rcode}] += r.n
				continue
			}
			named = append(named, r)
		}
		sort.Slice(named, func(i, j int) bool {
			if named[i].n != named[j].n {
				return named[i].n > named[j].n
			}
			return named[i].qname < named[j].qname
		})
		if len(named) > s.TopHourly {
			for _, r := range named[s.TopHourly:] {
				other[[3]string{r.zone, r.qtype, r.rcode}] += r.n
			}
			named = named[:s.TopHourly]
		}
		for _, r := range named {
			if _, err := stmt.ExecContext(ctx, r.user, r.col, h, r.zone, r.qname, r.qtype, r.rcode, r.n); err != nil {
				return err
			}
		}
		for k, n := range other {
			if _, err := stmt.ExecContext(ctx, list[0].user, list[0].col, h, k[0], wire.Other, k[1], k[2], n); err != nil {
				return err
			}
		}
	}
	if err := s.metaSet(ctx, tx, "rolled_until", h+hourMs); err != nil {
		return err
	}
	return tx.Commit()
}
