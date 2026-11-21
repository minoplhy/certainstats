package knot

import (
	"context"
	"database/sql"
	"encoding/binary"
	"fmt"
	"slices"
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
		// names and clients are no longer written; the name and client lists
		// are capped below.
		if d.name == "names" || d.name == "clients" || d.name == "qnames" || d.name == "qtypes" || isNetTable(d.name) {
			continue
		}
		keys := strings.Join(d.keys, ", ")
		if _, err := tx.ExecContext(ctx, fmt.Sprintf(`INSERT INTO %s (user_id, collector_id, bucket, %s, count)
			SELECT user_id, collector_id, ?, %s, SUM(count) FROM %s WHERE bucket >= ? AND bucket < ? GROUP BY user_id, collector_id, %s`,
			hourT, keys, keys, minuteT, keys), h, h, h+hourMs); err != nil {
			return err
		}
	}
	for _, t := range []struct {
		dim  string
		keys []string
		fold []int
	}{
		{"qtypes", []string{"zone", "qname", "qtype", "rcode"}, []int{1}},
		{"qnames", []string{"zone", "qname"}, []int{1}},
		{"nets", []string{"net"}, []int{0}},
		{"netnames", []string{"net", "zone", "qname"}, []int{0, 2}},
		{"netcodes", []string{"net", "zone", "qtype", "rcode"}, []int{0, 2, 3}},
	} {
		if err := s.rollTop(ctx, tx, h, t.dim, t.keys, t.fold); err != nil {
			return err
		}
	}
	if err := s.metaSet(ctx, tx, "rolled_until", h+hourMs); err != nil {
		return err
	}
	return tx.Commit()
}

// rollTop sums one hour of a bounded table and cuts it per collector with the
// bound rule at TopHourly: rows above the cut stay, the rest, ties at the cut
// included, fold into Other rows (the fold keys set to Other, the others
// kept), so they still add up. The cut is added to the hour's err in the
// bounds table, on top of the minutes' errors that the plain hourly sum
// already carried over.
func (s *Store) rollTop(ctx context.Context, tx *sql.Tx, h int64, dim string, keys []string, fold []int) error {
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
	for g, list := range groups {
		named := list[:0:0]
		// other sums the folded rows by their key with the fold keys set to
		// Other. Map keys are length-prefixed (a 0x00 separator would merge
		// ("a\x00b", "c") with ("a", "b\x00c")).
		other := map[string]*row{}
		addOther := func(k []string, n int64) {
			f := append([]string{}, k...)
			for _, i := range fold {
				f[i] = wire.Other
			}
			id := keyID(f)
			if o := other[id]; o != nil {
				o.n += n
				return
			}
			other[id] = &row{key: f, n: n}
		}
		for _, r := range list {
			if r.key[fold[0]] == wire.Other {
				addOther(r.key, r.n)
				continue
			}
			named = append(named, r)
		}
		sort.Slice(named, func(i, j int) bool {
			if named[i].n != named[j].n {
				return named[i].n > named[j].n
			}
			return slices.Compare(named[i].key, named[j].key) < 0
		})
		{
			counts := make([]uint64, len(named))
			for i, r := range named {
				counts[i] = uint64(r.n)
			}
			cut := int64(wire.Cut(counts, s.TopHourly))
			kept := named[:0:0]
			for _, r := range named {
				if r.n > cut {
					kept = append(kept, r)
					continue
				}
				addOther(r.key, r.n)
			}
			named = kept
			if cut > 0 {
				if _, err := tx.ExecContext(ctx, `INSERT INTO dnstap_bounds_hourly (user_id, collector_id, bucket, family, kind, count) VALUES (?, ?, ?, ?, 'err', ?)
					ON CONFLICT (user_id, collector_id, bucket, family, kind) DO UPDATE SET count = count + excluded.count`, g[0], g[1], h, dim, cut); err != nil {
					return err
				}
			}
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
		for _, o := range other {
			if err := put(o.key, o.n); err != nil {
				return err
			}
		}
	}
	return nil
}

// keyID is a map key for a key list: each key length-prefixed, so no two
// different lists share one.
func keyID(keys []string) string {
	var b []byte
	for _, k := range keys {
		b = binary.AppendUvarint(b, uint64(len(k)))
		b = append(b, k...)
	}
	return string(b)
}
