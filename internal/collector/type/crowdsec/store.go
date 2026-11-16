// Package crowdsec is the CrowdSec collector type: it receives alerts from
// collectors, stores them in their own SQLite file and serves the CrowdSec page.
package crowdsec

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"

	"certainstats/internal/collector/type/crowdsec/wire"

	_ "modernc.org/sqlite"
)

var schema = []string{
	`CREATE TABLE IF NOT EXISTS alerts (
		id           INTEGER PRIMARY KEY,
		user_id      TEXT NOT NULL,
		collector_id TEXT NOT NULL,
		uuid         TEXT NOT NULL,
		lapi_id      INTEGER NOT NULL DEFAULT 0,
		machine_id   TEXT NOT NULL DEFAULT '',
		scenario     TEXT NOT NULL DEFAULT '',
		message      TEXT NOT NULL DEFAULT '',
		kind         TEXT NOT NULL DEFAULT '',
		ip           TEXT NOT NULL DEFAULT '',
		ip_range     TEXT NOT NULL DEFAULT '',
		scope        TEXT NOT NULL DEFAULT '',
		value        TEXT NOT NULL DEFAULT '',
		country      TEXT NOT NULL DEFAULT '',
		asn          TEXT NOT NULL DEFAULT '',
		as_name      TEXT NOT NULL DEFAULT '',
		action       TEXT NOT NULL DEFAULT 'none',
		events_count INTEGER NOT NULL DEFAULT 0,
		simulated    INTEGER NOT NULL DEFAULT 0,
		started_at   INTEGER NOT NULL,
		stopped_at   INTEGER NOT NULL DEFAULT 0,
		created_at   INTEGER NOT NULL DEFAULT 0,
		received_at  INTEGER NOT NULL,
		UNIQUE (user_id, uuid)
	)`,
	`CREATE INDEX IF NOT EXISTS alerts_time ON alerts(user_id, started_at DESC)`,
	`CREATE INDEX IF NOT EXISTS alerts_ip ON alerts(user_id, ip, started_at)`,
	`CREATE INDEX IF NOT EXISTS alerts_scenario ON alerts(user_id, scenario, started_at)`,
	`CREATE INDEX IF NOT EXISTS alerts_started ON alerts(started_at)`,
	`CREATE TABLE IF NOT EXISTS decisions (
		alert_id INTEGER NOT NULL REFERENCES alerts(id) ON DELETE CASCADE,
		uuid     TEXT NOT NULL DEFAULT '',
		type     TEXT NOT NULL DEFAULT '',
		origin   TEXT NOT NULL DEFAULT '',
		scope    TEXT NOT NULL DEFAULT '',
		value    TEXT NOT NULL DEFAULT '',
		duration TEXT NOT NULL DEFAULT '',
		until_ms INTEGER NOT NULL DEFAULT 0
	)`,
	`CREATE INDEX IF NOT EXISTS decisions_alert ON decisions(alert_id)`,
	`CREATE INDEX IF NOT EXISTS decisions_until ON decisions(until_ms)`,
	`CREATE TABLE IF NOT EXISTS rollup_hourly (
		user_id    TEXT NOT NULL,
		hour       INTEGER NOT NULL,
		scenario   TEXT NOT NULL,
		country    TEXT NOT NULL,
		machine_id TEXT NOT NULL,
		action     TEXT NOT NULL,
		alerts     INTEGER NOT NULL,
		events     INTEGER NOT NULL,
		decisions  INTEGER NOT NULL DEFAULT 0,
		PRIMARY KEY (user_id, hour, scenario, country, machine_id, action)
	) WITHOUT ROWID`,
}

// Store owns crowdsec.db. Writes go through one connection; reads use a small pool,
// so a long query never holds up ingestion or the main state database.
type Store struct {
	write *sql.DB
	read  *sql.DB
}

func dsn(path string) string {
	q := url.Values{}
	for _, p := range []string{"journal_mode(WAL)", "foreign_keys(ON)", "synchronous(NORMAL)", "busy_timeout(5000)"} {
		q.Add("_pragma", p)
	}
	return "file:" + path + "?" + q.Encode()
}

func Open(path string) (*Store, error) {
	write, err := sql.Open("sqlite", dsn(path))
	if err != nil {
		return nil, err
	}
	write.SetMaxOpenConns(1)
	for _, stmt := range append(append([]string{}, schema...), healthSchema...) {
		if _, err := write.Exec(stmt); err != nil {
			write.Close()
			return nil, fmt.Errorf("crowdsec schema: %w", err)
		}
	}
	if err := migrate(write); err != nil {
		write.Close()
		return nil, fmt.Errorf("crowdsec migrate: %w", err)
	}
	read, err := sql.Open("sqlite", dsn(path))
	if err != nil {
		write.Close()
		return nil, err
	}
	read.SetMaxOpenConns(4)
	return &Store{write: write, read: read}, nil
}

// migrate upgrades a database made by an older version, tracked in user_version.
// Version 1 adds rollup_hourly.decisions and fills it from the raw alerts still
// kept; hours whose raw alerts were already pruned keep 0.
func migrate(db *sql.DB) error {
	var version int
	if err := db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
		return err
	}
	if version >= 1 {
		return nil
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var has int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('rollup_hourly') WHERE name = 'decisions'`).Scan(&has); err != nil {
		return err
	}
	if has == 0 {
		if _, err := tx.Exec(`ALTER TABLE rollup_hourly ADD COLUMN decisions INTEGER NOT NULL DEFAULT 0`); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(`WITH counted AS (
			SELECT a.user_id, a.started_at - ((a.started_at % 3600000) + 3600000) % 3600000 AS hour,
				a.scenario, a.country, a.machine_id, a.action, COUNT(*) AS n
			FROM decisions d JOIN alerts a ON a.id = d.alert_id
			GROUP BY 1, 2, 3, 4, 5, 6)
		UPDATE rollup_hourly SET decisions = counted.n FROM counted
		WHERE rollup_hourly.user_id = counted.user_id AND rollup_hourly.hour = counted.hour AND rollup_hourly.scenario = counted.scenario
			AND rollup_hourly.country = counted.country AND rollup_hourly.machine_id = counted.machine_id AND rollup_hourly.action = counted.action`); err != nil {
		return err
	}
	if _, err := tx.Exec(`PRAGMA user_version = 1`); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) Close() error {
	err := s.read.Close()
	if e := s.write.Close(); err == nil {
		err = e
	}
	return err
}

// Insert stores a batch. A row that already exists (same owner and uuid) is a
// duplicate and changes nothing, so redelivery is harmless. Decisions and the
// hourly rollup are written in the same transaction, only for new rows.
// It returns the newest alert start time in the batch.
func (s *Store) Insert(ctx context.Context, userID, collectorID string, alerts []wire.Alert, receivedAt int64) (stored, duplicates int, newest int64, err error) {
	tx, err := s.write.BeginTx(ctx, nil)
	if err != nil {
		return 0, 0, 0, err
	}
	defer tx.Rollback()
	for i := range alerts {
		row := normalize(collectorID, &alerts[i], receivedAt)
		newest = max(newest, row.StartedAt)
		res, err := tx.ExecContext(ctx, `INSERT INTO alerts (user_id, collector_id, uuid, lapi_id, machine_id, scenario, message, kind, ip, ip_range, scope, value,
			country, asn, as_name, action, events_count, simulated, started_at, stopped_at, created_at, received_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?) ON CONFLICT(user_id, uuid) DO NOTHING`,
			userID, collectorID, row.UUID, row.LapiID, row.Machine, row.Scenario, row.Message, row.Kind, row.IP, row.Range, row.Scope, row.Value,
			row.Country, row.ASN, row.ASName, row.Action, row.Events, row.Simulated, row.StartedAt, row.StoppedAt, row.CreatedAt, receivedAt)
		if err != nil {
			return 0, 0, 0, err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return 0, 0, 0, err
		}
		if n == 0 {
			duplicates++
			continue
		}
		id, err := res.LastInsertId()
		if err != nil {
			return 0, 0, 0, err
		}
		for _, d := range row.Decisions {
			if _, err := tx.ExecContext(ctx, `INSERT INTO decisions (alert_id, uuid, type, origin, scope, value, duration, until_ms) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
				id, d.UUID, d.Type, d.Origin, d.Scope, d.Value, d.Duration, d.UntilMs); err != nil {
				return 0, 0, 0, err
			}
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO rollup_hourly (user_id, hour, scenario, country, machine_id, action, alerts, events, decisions) VALUES (?, ?, ?, ?, ?, ?, 1, ?, ?)
			ON CONFLICT(user_id, hour, scenario, country, machine_id, action) DO UPDATE SET alerts = alerts + 1, events = events + excluded.events, decisions = decisions + excluded.decisions`,
			userID, hourOf(row.StartedAt), row.Scenario, row.Country, row.Machine, row.Action, row.Events, len(row.Decisions)); err != nil {
			return 0, 0, 0, err
		}
		stored++
	}
	return stored, duplicates, newest, tx.Commit()
}

const hourMs = int64(3_600_000)

func hourOf(ms int64) int64 { return ms - ((ms%hourMs)+hourMs)%hourMs }
