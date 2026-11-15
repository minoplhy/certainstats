package crowdsec

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"certainstats/internal/crowdsec/wire"
)

// healthSchema is added to crowdsec.db: the machines and bouncers a collector
// reported, and each collector's last report. Alert rules and their incidents
// live in the main database with every other rule.
var healthSchema = []string{
	`CREATE TABLE IF NOT EXISTS components (
		id            INTEGER PRIMARY KEY,
		user_id       TEXT NOT NULL,
		collector_id  TEXT NOT NULL,
		kind          TEXT NOT NULL,
		name          TEXT NOT NULL,
		version       TEXT NOT NULL DEFAULT '',
		ip            TEXT NOT NULL DEFAULT '',
		type          TEXT NOT NULL DEFAULT '',
		os            TEXT NOT NULL DEFAULT '',
		signals       TEXT NOT NULL DEFAULT '{}',
		findings      TEXT NOT NULL DEFAULT '[]',
		progress      TEXT NOT NULL DEFAULT '{}',
		muted         INTEGER NOT NULL DEFAULT 0,
		first_seen_ms INTEGER NOT NULL,
		updated_ms    INTEGER NOT NULL,
		UNIQUE (collector_id, kind, name)
	)`,
	`CREATE INDEX IF NOT EXISTS components_user ON components(user_id)`,
	`CREATE TABLE IF NOT EXISTS collector_health (
		collector_id TEXT PRIMARY KEY,
		user_id      TEXT NOT NULL,
		error        TEXT NOT NULL DEFAULT '',
		parts        TEXT NOT NULL DEFAULT '[]',
		active       TEXT NOT NULL DEFAULT '{}',
		reported_ms  INTEGER NOT NULL DEFAULT 0
	)`,
}

// Component is one machine or bouncer a collector reported.
type Component struct {
	ID          int64
	UserID      string
	CollectorID string
	Kind        string
	Name        string
	Version     string
	IP          string
	Type        string
	OS          string
	// Signals holds what each source reported, keyed by source.
	Signals     map[string]json.RawMessage
	Findings    []Finding
	Progress    map[string]progress
	Muted       bool
	FirstSeenMs int64
	UpdatedMs   int64
}

// CollectorHealth is one collector's last health report.
type CollectorHealth struct {
	CollectorID string
	UserID      string
	Error       string
	Parts       []string
	Active      map[string]int64
	ReportedMs  int64
}

func (h *CollectorHealth) partSet() map[string]bool {
	set := map[string]bool{}
	for _, p := range h.Parts {
		set[p] = true
	}
	return set
}

func jsonText(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return "{}"
	}
	return string(b)
}

// SaveReport stores one cs_health. The report is the whole registry of the
// parts it read, so a component missing from it was removed from CrowdSec and
// is deleted here; the next evaluation closes its open incidents quietly.
func (s *Store) SaveReport(ctx context.Context, userID, collectorID string, rep wire.Health, now int64) error {
	tx, err := s.write.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	parts := rep.Parts
	if parts == nil {
		parts = []string{}
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO collector_health (collector_id, user_id, error, parts, active, reported_ms) VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(collector_id) DO UPDATE SET error = excluded.error, parts = excluded.parts, active = excluded.active, reported_ms = excluded.reported_ms`,
		collectorID, userID, rep.Error, jsonText(parts), jsonText(rep.Active), now); err != nil {
		return err
	}
	if rep.Error != "" {
		// What was known stays; it is simply not judged until reads work again.
		return tx.Commit()
	}
	seen := map[[2]string]bool{}
	for _, c := range rep.Components {
		if (c.Kind != wire.KindMachine && c.Kind != wire.KindBouncer) || c.Name == "" || seen[[2]string{c.Kind, c.Name}] {
			continue
		}
		seen[[2]string{c.Kind, c.Name}] = true
		sig := DBSignals{LastSeenMs: c.LastSeenMs, Validated: c.Validated, Revoked: c.Revoked, MetricsMs: c.MetricsMs, Held: c.Held, Dropped: c.Dropped, Processed: c.Processed}
		var stored string
		err := tx.QueryRowContext(ctx, `SELECT signals FROM components WHERE collector_id = ? AND kind = ? AND name = ?`, collectorID, c.Kind, c.Name).Scan(&stored)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		signals := map[string]json.RawMessage{}
		_ = json.Unmarshal([]byte(stored), &signals) // other sources' signals are kept
		var before DBSignals
		_ = json.Unmarshal(signals[SourceDB], &before) // nothing stored yet reads as zero
		sig.HoldsDecisions = len(sig.Held) > 0 || sig.Processed > 0 || sig.Dropped > 0 || before.HoldsDecisions
		signals[SourceDB] = json.RawMessage(jsonText(sig))
		if _, err := tx.ExecContext(ctx, `INSERT INTO components (user_id, collector_id, kind, name, version, ip, type, os, signals, first_seen_ms, updated_ms)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(collector_id, kind, name) DO UPDATE SET version = excluded.version, ip = excluded.ip, type = excluded.type, os = excluded.os,
				signals = excluded.signals, updated_ms = excluded.updated_ms`,
			userID, collectorID, c.Kind, c.Name, c.Version, c.IP, c.Type, c.OS, jsonText(signals), now, now); err != nil {
			return err
		}
	}
	// A report cut off at the cap is not the whole registry; delete nothing then.
	if len(rep.Components) < wire.MaxComponents {
		for part, kind := range map[string]string{wire.PartMachines: wire.KindMachine, wire.PartBouncers: wire.KindBouncer} {
			read := false
			for _, p := range parts {
				read = read || p == part
			}
			if !read {
				continue
			}
			if _, err := tx.ExecContext(ctx, `DELETE FROM components WHERE collector_id = ? AND kind = ? AND updated_ms < ?`, collectorID, kind, now); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}

// Every read is a complete statement; none is assembled from parts.
const (
	componentsByUser = `SELECT id, user_id, collector_id, kind, name, version, ip, type, os, signals, findings, progress, muted, first_seen_ms, updated_ms
		FROM components WHERE user_id = ? ORDER BY kind DESC, name, id`
	componentsByCollector = `SELECT id, user_id, collector_id, kind, name, version, ip, type, os, signals, findings, progress, muted, first_seen_ms, updated_ms
		FROM components WHERE collector_id = ? ORDER BY kind DESC, name, id`
)

func scanComponent(rows *sql.Rows) (*Component, error) {
	c := &Component{Signals: map[string]json.RawMessage{}, Progress: map[string]progress{}}
	var signals, findings, prog string
	if err := rows.Scan(&c.ID, &c.UserID, &c.CollectorID, &c.Kind, &c.Name, &c.Version, &c.IP, &c.Type, &c.OS, &signals, &findings, &prog, &c.Muted, &c.FirstSeenMs, &c.UpdatedMs); err != nil {
		return nil, err
	}
	// These columns are written only by this package. A damaged one reads as
	// empty and is rewritten by the next report or evaluation.
	_ = json.Unmarshal([]byte(signals), &c.Signals)
	_ = json.Unmarshal([]byte(findings), &c.Findings)
	_ = json.Unmarshal([]byte(prog), &c.Progress)
	return c, nil
}

func (s *Store) components(ctx context.Context, query string, args ...any) ([]*Component, error) {
	rows, err := s.read.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list := []*Component{}
	for rows.Next() {
		c, err := scanComponent(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, c)
	}
	return list, rows.Err()
}

// Components lists an owner's machines and bouncers, machines first.
func (s *Store) Components(ctx context.Context, userID string) ([]*Component, error) {
	return s.components(ctx, componentsByUser, userID)
}

func (s *Store) collectorComponents(ctx context.Context, collectorID string) ([]*Component, error) {
	return s.components(ctx, componentsByCollector, collectorID)
}

// saveEvaluation stores a component's findings and progress after an evaluation.
func (s *Store) saveEvaluation(ctx context.Context, c *Component) error {
	findings := c.Findings
	if findings == nil {
		findings = []Finding{}
	}
	_, err := s.write.ExecContext(ctx, `UPDATE components SET findings = ?, progress = ? WHERE id = ?`, jsonText(findings), jsonText(c.Progress), c.ID)
	return err
}

// SetMuted mutes or unmutes one of the owner's components.
func (s *Store) SetMuted(ctx context.Context, userID string, id int64, muted bool) error {
	res, err := s.write.ExecContext(ctx, `UPDATE components SET muted = ? WHERE id = ? AND user_id = ?`, muted, id, userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

const (
	healthOne = `SELECT collector_id, user_id, error, parts, active, reported_ms
		FROM collector_health WHERE collector_id = ? AND user_id = ?`
	healthReported = `SELECT collector_id, user_id, error, parts, active, reported_ms
		FROM collector_health WHERE reported_ms > 0 ORDER BY collector_id`
)

func scanHealth(row interface{ Scan(...any) error }) (*CollectorHealth, error) {
	h := &CollectorHealth{Parts: []string{}, Active: map[string]int64{}}
	var parts, active string
	if err := row.Scan(&h.CollectorID, &h.UserID, &h.Error, &parts, &active, &h.ReportedMs); err != nil {
		return nil, err
	}
	// A damaged column reads as empty, as if nothing was reported.
	_ = json.Unmarshal([]byte(parts), &h.Parts)
	_ = json.Unmarshal([]byte(active), &h.Active)
	return h, nil
}

// Health returns a collector's settings and last report; defaults when it has none yet.
func (s *Store) Health(ctx context.Context, userID, collectorID string) (*CollectorHealth, error) {
	h, err := scanHealth(s.read.QueryRowContext(ctx, healthOne, collectorID, userID))
	if errors.Is(err, sql.ErrNoRows) {
		return &CollectorHealth{CollectorID: collectorID, UserID: userID, Parts: []string{}, Active: map[string]int64{}}, nil
	}
	return h, err
}

// reportedHealth lists every collector that has sent a health report.
func (s *Store) reportedHealth(ctx context.Context) ([]*CollectorHealth, error) {
	rows, err := s.read.QueryContext(ctx, healthReported)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list := []*CollectorHealth{}
	for rows.Next() {
		h, err := scanHealth(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, h)
	}
	return list, rows.Err()
}

// dropCollectorHealth removes everything health keeps about a collector that is gone.
func (s *Store) dropCollectorHealth(ctx context.Context, collectorID string) error {
	for _, q := range []string{`DELETE FROM components WHERE collector_id = ?`, `DELETE FROM collector_health WHERE collector_id = ?`} {
		if _, err := s.write.ExecContext(ctx, q, collectorID); err != nil {
			return err
		}
	}
	return nil
}

// pruneHealth drops everything owned by accounts that no longer exist.
func (s *Store) pruneHealth(ctx context.Context, users []string) error {
	known := map[string]bool{}
	for _, u := range users {
		known[u] = true
	}
	rows, err := s.read.QueryContext(ctx, `SELECT DISTINCT user_id FROM collector_health UNION SELECT DISTINCT user_id FROM components`)
	if err != nil {
		return err
	}
	var gone []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		if !known[id] {
			gone = append(gone, id)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, id := range gone {
		for _, q := range []string{`DELETE FROM components WHERE user_id = ?`, `DELETE FROM collector_health WHERE user_id = ?`} {
			if _, err := s.write.ExecContext(ctx, q, id); err != nil {
				return err
			}
		}
	}
	return nil
}
