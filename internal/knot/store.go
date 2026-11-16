// Package knot is the Knot DNS collector type. Counters go to the collector
// TSDB; zone status and dnstap buckets live in their own SQLite file, knot.db.
package knot

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"time"

	"certainstats/internal/knot/wire"

	_ "modernc.org/sqlite"
)

const (
	minuteMs = int64(time.Minute / time.Millisecond)
	hourMs   = int64(time.Hour / time.Millisecond)
)

// dims are the dnstap tables, each kept per minute and per hour. The key
// columns follow user, collector and bucket; count is the value.
var dims = []struct {
	name string
	keys []string
}{
	{"totals", []string{"zone", "qtype", "rcode"}},
	{"names", []string{"zone", "qname", "qtype", "rcode"}},
	{"clients", []string{"subnet"}},
	{"sizes", []string{"up_to"}},
}

func dimSchema() []string {
	var out []string
	for _, d := range dims {
		for _, grain := range []string{"minute", "hourly"} {
			table := "dnstap_" + d.name + "_" + grain
			cols, pk := "", "user_id, collector_id, bucket"
			for _, k := range d.keys {
				typ := "TEXT NOT NULL"
				if k == "up_to" {
					typ = "INTEGER NOT NULL"
				}
				cols += k + " " + typ + ", "
				pk += ", " + k
			}
			out = append(out,
				fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (user_id TEXT NOT NULL, collector_id TEXT NOT NULL, bucket INTEGER NOT NULL, %scount INTEGER NOT NULL, PRIMARY KEY (%s)) WITHOUT ROWID`, table, cols, pk),
				fmt.Sprintf(`CREATE INDEX IF NOT EXISTS %s_bucket ON %s(bucket)`, table, table),
				fmt.Sprintf(`CREATE INDEX IF NOT EXISTS %s_user ON %s(user_id, bucket)`, table, table))
			if d.name == "totals" || d.name == "names" {
				out = append(out, fmt.Sprintf(`CREATE INDEX IF NOT EXISTS %s_zone ON %s(user_id, zone, bucket)`, table, table))
			}
			if d.name == "names" {
				out = append(out, fmt.Sprintf(`CREATE INDEX IF NOT EXISTS %s_qname ON %s(user_id, qname, bucket)`, table, table))
			}
		}
	}
	return out
}

var schema = append([]string{
	`CREATE TABLE IF NOT EXISTS zones (
		user_id           TEXT NOT NULL,
		collector_id      TEXT NOT NULL,
		zone              TEXT NOT NULL,
		role              TEXT NOT NULL DEFAULT '',
		serial            INTEGER NOT NULL DEFAULT 0,
		has_serial        INTEGER NOT NULL DEFAULT 0,
		serial_changed_at INTEGER NOT NULL DEFAULT 0,
		refresh_at        INTEGER NOT NULL DEFAULT 0,
		expire_at         INTEGER NOT NULL DEFAULT 0,
		resign_at         INTEGER NOT NULL DEFAULT 0,
		frozen            INTEGER NOT NULL DEFAULT 0,
		error             TEXT NOT NULL DEFAULT '',
		seen_at           INTEGER NOT NULL,
		PRIMARY KEY (user_id, collector_id, zone)
	) WITHOUT ROWID`,
	`CREATE INDEX IF NOT EXISTS zones_name ON zones(user_id, zone)`,
	// dnstap_minutes records each stored minute: when it arrived and whether rcodes are known.
	`CREATE TABLE IF NOT EXISTS dnstap_minutes (
		user_id      TEXT NOT NULL,
		collector_id TEXT NOT NULL,
		bucket       INTEGER NOT NULL,
		queries_only INTEGER NOT NULL DEFAULT 0,
		received_at  INTEGER NOT NULL,
		PRIMARY KEY (user_id, collector_id, bucket)
	) WITHOUT ROWID`,
	`CREATE INDEX IF NOT EXISTS dnstap_minutes_bucket ON dnstap_minutes(bucket)`,
	`CREATE TABLE IF NOT EXISTS meta (key TEXT PRIMARY KEY, value INTEGER NOT NULL) WITHOUT ROWID`,
}, dimSchema()...)

// Store owns knot.db: one writer connection, a small read pool.
type Store struct {
	write *sql.DB
	read  *sql.DB
	// TopHourly is how many name rows each collector keeps per hour in the rollup.
	TopHourly int
}

func dsn(path string) string {
	q := url.Values{}
	for _, p := range []string{"journal_mode(WAL)", "synchronous(NORMAL)", "busy_timeout(5000)"} {
		q.Add("_pragma", p)
	}
	return "file:" + path + "?" + q.Encode()
}

// Open creates or opens knot.db.
func Open(path string) (*Store, error) {
	write, err := sql.Open("sqlite", dsn(path))
	if err != nil {
		return nil, err
	}
	write.SetMaxOpenConns(1)
	for _, stmt := range schema {
		if _, err := write.Exec(stmt); err != nil {
			write.Close()
			return nil, fmt.Errorf("knot schema: %w", err)
		}
	}
	// Version 1 is the first schema; later versions migrate from here.
	if _, err := write.Exec(`PRAGMA user_version = 1`); err != nil {
		write.Close()
		return nil, err
	}
	read, err := sql.Open("sqlite", dsn(path))
	if err != nil {
		write.Close()
		return nil, err
	}
	read.SetMaxOpenConns(4)
	return &Store{write: write, read: read, TopHourly: 1000}, nil
}

func (s *Store) Close() error {
	s.read.Close()
	return s.write.Close()
}

// ZoneRow is one zone as one collector last reported it.
type ZoneRow struct {
	CollectorID     string `json:"collector_id"`
	Zone            string `json:"zone"`
	Role            string `json:"role"`
	Serial          uint32 `json:"serial"`
	HasSerial       bool   `json:"has_serial"`
	SerialChangedAt int64  `json:"serial_changed_at"`
	RefreshAt       int64  `json:"refresh_at"`
	ExpireAt        int64  `json:"expire_at"`
	ResignAt        int64  `json:"resign_at"`
	Frozen          bool   `json:"frozen"`
	Error           string `json:"error"`
	SeenAt          int64  `json:"seen_at"`
}

// SaveZones replaces a collector's zones with a fresh reading. A zone whose
// serial changed, or that is new, gets serial_changed_at = now.
func (s *Store) SaveZones(ctx context.Context, userID, collectorID string, zones []wire.Zone, now int64) error {
	tx, err := s.write.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	old := map[string][2]int64{} // zone → serial, has_serial
	rows, err := tx.QueryContext(ctx, `SELECT zone, serial, has_serial FROM zones WHERE user_id = ? AND collector_id = ?`, userID, collectorID)
	if err != nil {
		return err
	}
	for rows.Next() {
		var z string
		var serial, has int64
		if err := rows.Scan(&z, &serial, &has); err != nil {
			rows.Close()
			return err
		}
		old[z] = [2]int64{serial, has}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	stmt, err := tx.PrepareContext(ctx, `INSERT INTO zones (user_id, collector_id, zone, role, serial, has_serial, serial_changed_at, refresh_at, expire_at, resign_at, frozen, error, seen_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (user_id, collector_id, zone) DO UPDATE SET role = excluded.role, serial = excluded.serial, has_serial = excluded.has_serial,
			serial_changed_at = CASE WHEN zones.serial != excluded.serial OR zones.has_serial != excluded.has_serial THEN excluded.serial_changed_at ELSE zones.serial_changed_at END,
			refresh_at = excluded.refresh_at, expire_at = excluded.expire_at, resign_at = excluded.resign_at, frozen = excluded.frozen,
			error = excluded.error, seen_at = excluded.seen_at`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, z := range zones {
		if _, err := stmt.ExecContext(ctx, userID, collectorID, z.Name, z.Role, int64(z.Serial), b2i(z.HasSerial), now,
			z.RefreshAtMs, z.ExpireAtMs, z.ResignAtMs, b2i(z.Frozen), z.Error, now); err != nil {
			return err
		}
		delete(old, z.Name)
	}
	for z := range old {
		if _, err := tx.ExecContext(ctx, `DELETE FROM zones WHERE user_id = ? AND collector_id = ? AND zone = ?`, userID, collectorID, z); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

// Zones returns every zone row of the owner's collectors, optionally one zone.
func (s *Store) Zones(ctx context.Context, userID, zone string) ([]ZoneRow, error) {
	q := `SELECT collector_id, zone, role, serial, has_serial, serial_changed_at, refresh_at, expire_at, resign_at, frozen, error, seen_at
		FROM zones WHERE user_id = ?`
	args := []any{userID}
	if zone != "" {
		q += ` AND zone = ?`
		args = append(args, zone)
	}
	rows, err := s.read.QueryContext(ctx, q+` ORDER BY zone, collector_id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ZoneRow{}
	for rows.Next() {
		var z ZoneRow
		var serial int64
		var has, frozen int
		if err := rows.Scan(&z.CollectorID, &z.Zone, &z.Role, &serial, &has, &z.SerialChangedAt, &z.RefreshAt, &z.ExpireAt, &z.ResignAt, &frozen, &z.Error, &z.SeenAt); err != nil {
			return nil, err
		}
		z.Serial, z.HasSerial, z.Frozen = uint32(serial), has == 1, frozen == 1
		out = append(out, z)
	}
	return out, rows.Err()
}

// SaveDnstap stores one dnstap part, replacing rows by key, so a resent part changes nothing.
func (s *Store) SaveDnstap(ctx context.Context, userID, collectorID string, d wire.Dnstap, now int64) error {
	bucket := d.MinuteMs - d.MinuteMs%minuteMs
	tx, err := s.write.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `INSERT INTO dnstap_minutes (user_id, collector_id, bucket, queries_only, received_at) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT (user_id, collector_id, bucket) DO UPDATE SET queries_only = excluded.queries_only, received_at = excluded.received_at`,
		userID, collectorID, bucket, b2i(d.QueriesOnly), now); err != nil {
		return err
	}
	put := func(table string, keys []string, rows [][]any) error {
		if len(rows) == 0 {
			return nil
		}
		cols, marks, pk := "", "", "user_id, collector_id, bucket"
		for _, k := range keys {
			cols += ", " + k
			marks += ", ?"
			pk += ", " + k
		}
		stmt, err := tx.PrepareContext(ctx, fmt.Sprintf(`INSERT INTO %s (user_id, collector_id, bucket%s, count) VALUES (?, ?, ?%s, ?)
			ON CONFLICT (%s) DO UPDATE SET count = excluded.count`, table, cols, marks, pk))
		if err != nil {
			return err
		}
		defer stmt.Close()
		for _, r := range rows {
			if _, err := stmt.ExecContext(ctx, append([]any{userID, collectorID, bucket}, r...)...); err != nil {
				return err
			}
		}
		return nil
	}
	var totals, names, clients, sizes [][]any
	for _, t := range d.Totals {
		totals = append(totals, []any{t.Zone, t.QType, t.RCode, int64(t.Count)})
	}
	for _, n := range d.Names {
		names = append(names, []any{n.Zone, n.QName, n.QType, n.RCode, int64(n.Count)})
	}
	for _, c := range d.Clients {
		clients = append(clients, []any{c.Subnet, int64(c.Count)})
	}
	for _, z := range d.Sizes {
		sizes = append(sizes, []any{z.UpTo, int64(z.Count)})
	}
	for i, rows := range [][][]any{totals, names, clients, sizes} {
		if err := put("dnstap_"+dims[i].name+"_minute", dims[i].keys, rows); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// LastDnstap returns the newest stored minute with traffic per collector of the owner.
func (s *Store) LastDnstap(ctx context.Context, userID string) (map[string]int64, error) {
	rows, err := s.read.QueryContext(ctx, `SELECT collector_id, MAX(bucket) FROM dnstap_totals_minute WHERE user_id = ? GROUP BY collector_id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int64{}
	for rows.Next() {
		var id string
		var b int64
		if err := rows.Scan(&id, &b); err != nil {
			return nil, err
		}
		out[id] = b
	}
	return out, rows.Err()
}

func (s *Store) metaGet(ctx context.Context, key string) (int64, error) {
	var v int64
	err := s.read.QueryRowContext(ctx, `SELECT value FROM meta WHERE key = ?`, key).Scan(&v)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	return v, err
}

func (s *Store) metaSet(ctx context.Context, tx *sql.Tx, key string, v int64) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO meta (key, value) VALUES (?, ?) ON CONFLICT (key) DO UPDATE SET value = excluded.value`, key, v)
	return err
}

// rolledUntil is the hour up to which minute rows have been rolled into the
// hourly tables; reads use hourly rows before it and minute rows after.
func (s *Store) rolledUntil(ctx context.Context) (int64, error) {
	return s.metaGet(ctx, "rolled_until")
}
