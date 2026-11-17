package knot

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"time"

	"certainstats/internal/collector/type/knot/wire"
)

const (
	// rollLag keeps the newest hours as minutes only: collectors resend
	// unacknowledged minutes for up to an hour, so an hour is rolled once settled.
	rollLag = 2 * time.Hour
	// rollBatch caps the hours rolled per maintenance run.
	rollBatch = 72
)

func hourOf(ms int64) int64 { return ms - ms%hourMs }

// Rollup sums settled hours of minute rows into the hourly tables. Names and
// client rows keep the top TopHourly rows per collector and hour; the rest fold
// into Other rows per zone, qtype and rcode (and network, for client rows), so
// they still add up.
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
		if d.name == "names" || d.name == "clients" {
			continue
		}
		keys := strings.Join(d.keys, ", ")
		if _, err := tx.ExecContext(ctx, fmt.Sprintf(`INSERT INTO %s (user_id, collector_id, bucket, %s, count)
			SELECT user_id, collector_id, ?, %s, SUM(count) FROM %s WHERE bucket >= ? AND bucket < ? GROUP BY user_id, collector_id, %s`,
			hourT, keys, keys, minuteT, keys), h, h, h+hourMs); err != nil {
			return err
		}
	}
	if err := s.rollTop(ctx, tx, h, "names", nil); err != nil {
		return err
	}
	if err := s.rollTop(ctx, tx, h, "clients", []string{"net"}); err != nil {
		return err
	}
	if err := s.metaSet(ctx, tx, "rolled_until", h+hourMs); err != nil {
		return err
	}
	return tx.Commit()
}

// rollTop sums one hour of a names-type table (zone, qname, qtype, rcode, after
// the lead keys) and keeps the top TopHourly rows per collector; the rest fold
// into Other rows per lead keys, zone, qtype and rcode, so they still add up.
func (s *Store) rollTop(ctx context.Context, tx *sql.Tx, h int64, dim string, lead []string) error {
	keys := append(append([]string{}, lead...), "zone", "qname", "qtype", "rcode")
	qi := len(lead) + 1 // index of qname in keys
	cols := strings.Join(keys, ", ")
	rows, err := tx.QueryContext(ctx, fmt.Sprintf(`SELECT user_id, collector_id, %s, SUM(count) FROM dnstap_%s_minute
		WHERE bucket >= ? AND bucket < ? GROUP BY user_id, collector_id, %s`, cols, dim, cols), h, h+hourMs)
	if err != nil {
		return err
	}
	type row struct {
		key []string
		n   int64
	}
	groups := map[[2]string][]row{}
	for rows.Next() {
		var user, col string
		r := row{key: make([]string, len(keys))}
		dest := []any{&user, &col}
		for i := range r.key {
			dest = append(dest, &r.key[i])
		}
		if err := rows.Scan(append(dest, &r.n)...); err != nil {
			rows.Close()
			return err
		}
		g := [2]string{user, col}
		groups[g] = append(groups[g], r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	marks := strings.TrimSuffix(strings.Repeat("?, ", len(keys)), ", ")
	stmt, err := tx.PrepareContext(ctx, fmt.Sprintf(`INSERT INTO dnstap_%s_hourly (user_id, collector_id, bucket, %s, count) VALUES (?, ?, ?, %s, ?)`, dim, cols, marks))
	if err != nil {
		return err
	}
	defer stmt.Close()
	// foldKey is the row's key with qname set to Other, joined for map use.
	foldKey := func(k []string) string {
		f := append([]string{}, k...)
		f[qi] = wire.Other
		return strings.Join(f, "\x00")
	}
	for g, list := range groups {
		named := list[:0:0]
		other := map[string]int64{}
		for _, r := range list {
			if r.key[qi] == wire.Other {
				other[foldKey(r.key)] += r.n
				continue
			}
			named = append(named, r)
		}
		sort.Slice(named, func(i, j int) bool {
			if named[i].n != named[j].n {
				return named[i].n > named[j].n
			}
			return strings.Join(named[i].key, "\x00") < strings.Join(named[j].key, "\x00")
		})
		if len(named) > s.TopHourly {
			for _, r := range named[s.TopHourly:] {
				other[foldKey(r.key)] += r.n
			}
			named = named[:s.TopHourly]
		}
		put := func(key []string, n int64) error {
			args := []any{g[0], g[1], h}
			for _, k := range key {
				args = append(args, k)
			}
			_, err := stmt.ExecContext(ctx, append(args, n)...)
			return err
		}
		for _, r := range named {
			if err := put(r.key, r.n); err != nil {
				return err
			}
		}
		for k, n := range other {
			if err := put(strings.Split(k, "\x00"), n); err != nil {
				return err
			}
		}
	}
	return nil
}
