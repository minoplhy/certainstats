package crowdsec

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"certainstats/internal/crowdsec/wire"
)

// healthSchema is added to crowdsec.db: the machines and bouncers a collector
// reported, each collector's health settings and last report, and one row per
// alert-level incident.
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
		target_id    TEXT NOT NULL DEFAULT '',
		checks       TEXT NOT NULL DEFAULT '{}',
		error        TEXT NOT NULL DEFAULT '',
		parts        TEXT NOT NULL DEFAULT '[]',
		active       TEXT NOT NULL DEFAULT '{}',
		reported_ms  INTEGER NOT NULL DEFAULT 0
	)`,
	`CREATE TABLE IF NOT EXISTS health_events (
		id             INTEGER PRIMARY KEY,
		user_id        TEXT NOT NULL,
		collector_id   TEXT NOT NULL,
		component_id   INTEGER NOT NULL,
		kind           TEXT NOT NULL,
		name           TEXT NOT NULL,
		check_name     TEXT NOT NULL,
		detail         TEXT NOT NULL DEFAULT '',
		value          REAL NOT NULL DEFAULT 0,
		target_id      TEXT NOT NULL DEFAULT '',
		opened_ms      INTEGER NOT NULL,
		resolved_ms    INTEGER NOT NULL DEFAULT 0,
		firing         TEXT NOT NULL DEFAULT 'none',
		firing_tries   INTEGER NOT NULL DEFAULT 0,
		recovery       TEXT NOT NULL DEFAULT '',
		recovery_tries INTEGER NOT NULL DEFAULT 0,
		error          TEXT NOT NULL DEFAULT ''
	)`,
	`CREATE INDEX IF NOT EXISTS health_events_open ON health_events(collector_id, component_id, check_name) WHERE resolved_ms = 0`,
	`CREATE INDEX IF NOT EXISTS health_events_user ON health_events(user_id, opened_ms)`,
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

// CollectorHealth is one collector's health settings and its last report.
type CollectorHealth struct {
	CollectorID string
	UserID      string
	TargetID    string
	Checks      map[string]CheckSetting
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

// Delivery states of an incident's two notifications.
const (
	notifyNone    = "none"    // nothing to send: no target set
	notifyPending = "pending" // to send, or to try again
	notifySent    = "sent"
	notifyFailed  = "failed" // gave up
	notifySkipped = "skipped"
	// maxNotifyTries bounds how often one notification is attempted.
	maxNotifyTries = 5
)

// HealthEvent is one period a check was unhealthy at alert level.
type HealthEvent struct {
	ID          int64
	UserID      string
	CollectorID string
	ComponentID int64
	Kind        string
	Name        string
	Check       string
	Detail      string
	Value       float64
	TargetID    string
	OpenedMs    int64
	ResolvedMs  int64
	Firing      string
	FiringTries int
	Recovery    string
	RecoveryTry int
	Error       string
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
// is deleted here, its open incidents closed without a notification.
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
			// Their open incidents end without a notification.
			if _, err := tx.ExecContext(ctx, `UPDATE health_events SET resolved_ms = ?, recovery = 'none',
				firing = CASE WHEN firing = 'pending' THEN 'skipped' ELSE firing END
				WHERE resolved_ms = 0 AND collector_id = ? AND component_id IN (SELECT id FROM components WHERE collector_id = ? AND kind = ? AND updated_ms < ?)`,
				now, collectorID, collectorID, kind, now); err != nil {
				return err
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
	healthOne = `SELECT collector_id, user_id, target_id, checks, error, parts, active, reported_ms
		FROM collector_health WHERE collector_id = ? AND user_id = ?`
	healthReported = `SELECT collector_id, user_id, target_id, checks, error, parts, active, reported_ms
		FROM collector_health WHERE reported_ms > 0 ORDER BY collector_id`
)

func scanHealth(row interface{ Scan(...any) error }) (*CollectorHealth, error) {
	h := &CollectorHealth{Checks: map[string]CheckSetting{}, Parts: []string{}, Active: map[string]int64{}}
	var checks, parts, active string
	if err := row.Scan(&h.CollectorID, &h.UserID, &h.TargetID, &checks, &h.Error, &parts, &active, &h.ReportedMs); err != nil {
		return nil, err
	}
	// A damaged column reads as empty: defaults for the checks, nothing reported.
	_ = json.Unmarshal([]byte(checks), &h.Checks)
	_ = json.Unmarshal([]byte(parts), &h.Parts)
	_ = json.Unmarshal([]byte(active), &h.Active)
	return h, nil
}

// Health returns a collector's settings and last report; defaults when it has none yet.
func (s *Store) Health(ctx context.Context, userID, collectorID string) (*CollectorHealth, error) {
	h, err := scanHealth(s.read.QueryRowContext(ctx, healthOne, collectorID, userID))
	if errors.Is(err, sql.ErrNoRows) {
		return &CollectorHealth{CollectorID: collectorID, UserID: userID, Checks: map[string]CheckSetting{}, Parts: []string{}, Active: map[string]int64{}}, nil
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

// SaveHealthSettings stores the notify target and the checks' configuration.
func (s *Store) SaveHealthSettings(ctx context.Context, userID, collectorID, targetID string, checks map[string]CheckSetting) error {
	_, err := s.write.ExecContext(ctx, `INSERT INTO collector_health (collector_id, user_id, target_id, checks) VALUES (?, ?, ?, ?)
		ON CONFLICT(collector_id) DO UPDATE SET target_id = excluded.target_id, checks = excluded.checks WHERE collector_health.user_id = excluded.user_id`,
		collectorID, userID, targetID, jsonText(checks))
	return err
}

// dropCollectorHealth removes everything health keeps about a collector that is gone.
func (s *Store) dropCollectorHealth(ctx context.Context, collectorID string) error {
	for _, q := range []string{`DELETE FROM components WHERE collector_id = ?`, `DELETE FROM health_events WHERE collector_id = ?`, `DELETE FROM collector_health WHERE collector_id = ?`} {
		if _, err := s.write.ExecContext(ctx, q, collectorID); err != nil {
			return err
		}
	}
	return nil
}

const (
	eventsOpenByCollector = `SELECT id, user_id, collector_id, component_id, kind, name, check_name, detail, value, target_id, opened_ms, resolved_ms, firing, firing_tries, recovery, recovery_tries, error
		FROM health_events WHERE collector_id = ? AND resolved_ms = 0 ORDER BY id`
	eventsOpenByUser = `SELECT id, user_id, collector_id, component_id, kind, name, check_name, detail, value, target_id, opened_ms, resolved_ms, firing, firing_tries, recovery, recovery_tries, error
		FROM health_events WHERE user_id = ? AND resolved_ms = 0 ORDER BY id`
	eventsPending = `SELECT id, user_id, collector_id, component_id, kind, name, check_name, detail, value, target_id, opened_ms, resolved_ms, firing, firing_tries, recovery, recovery_tries, error
		FROM health_events WHERE firing = 'pending' OR recovery = 'pending' ORDER BY id`
	eventsAll = `SELECT id, user_id, collector_id, component_id, kind, name, check_name, detail, value, target_id, opened_ms, resolved_ms, firing, firing_tries, recovery, recovery_tries, error
		FROM health_events ORDER BY id`
)

func (s *Store) events(ctx context.Context, query string, args ...any) ([]*HealthEvent, error) {
	rows, err := s.read.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list := []*HealthEvent{}
	for rows.Next() {
		e := &HealthEvent{}
		if err := rows.Scan(&e.ID, &e.UserID, &e.CollectorID, &e.ComponentID, &e.Kind, &e.Name, &e.Check, &e.Detail, &e.Value, &e.TargetID, &e.OpenedMs, &e.ResolvedMs,
			&e.Firing, &e.FiringTries, &e.Recovery, &e.RecoveryTry, &e.Error); err != nil {
			return nil, err
		}
		list = append(list, e)
	}
	return list, rows.Err()
}

// openEvent starts an incident. With no target there is nothing to send.
func (s *Store) openEvent(ctx context.Context, c *Component, f Finding, targetID string, now int64) error {
	firing := notifyPending
	if targetID == "" {
		firing = notifyNone
	}
	_, err := s.write.ExecContext(ctx, `INSERT INTO health_events (user_id, collector_id, component_id, kind, name, check_name, detail, value, target_id, opened_ms, firing)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, c.UserID, c.CollectorID, c.ID, c.Kind, c.Name, f.Check, f.Detail, f.Value, targetID, now, firing)
	return err
}

// resolveEvent ends an incident. A recovery is announced only when the firing
// notification went out; one still waiting is dropped, since the trouble is over.
func (s *Store) resolveEvent(ctx context.Context, e *HealthEvent, recovered bool, now int64) error {
	firing, recovery := e.Firing, notifyNone
	if firing == notifyPending {
		firing = notifySkipped
	}
	if recovered && firing == notifySent {
		recovery = notifyPending
	}
	_, err := s.write.ExecContext(ctx, `UPDATE health_events SET resolved_ms = ?, firing = ?, recovery = ? WHERE id = ? AND resolved_ms = 0`, now, firing, recovery, e.ID)
	return err
}

// recordDelivery stores the result of one notification attempt. A failure is
// tried again on a later tick until maxNotifyTries.
func (s *Store) recordDelivery(ctx context.Context, e *HealthEvent, phase string, sendErr error) error {
	status, message := notifySent, ""
	tries := e.FiringTries + 1
	if phase == "recovery" {
		tries = e.RecoveryTry + 1
	}
	if sendErr != nil {
		status, message = notifyPending, sendErr.Error()
		if tries >= maxNotifyTries {
			status = notifyFailed
		}
	}
	query := `UPDATE health_events SET firing = ?, firing_tries = ?, error = ? WHERE id = ? AND firing = 'pending'`
	if phase == "recovery" {
		query = `UPDATE health_events SET recovery = ?, recovery_tries = ?, error = ? WHERE id = ? AND recovery = 'pending'`
	}
	_, err := s.write.ExecContext(ctx, query, status, tries, message, e.ID)
	return err
}

// pruneHealth drops resolved incidents past retention and everything owned by
// accounts that no longer exist.
func (s *Store) pruneHealth(ctx context.Context, keepMs, now int64, users []string) error {
	known := map[string]bool{}
	for _, u := range users {
		known[u] = true
	}
	rows, err := s.read.QueryContext(ctx, `SELECT DISTINCT user_id FROM collector_health UNION SELECT DISTINCT user_id FROM components UNION SELECT DISTINCT user_id FROM health_events`)
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
		for _, q := range []string{`DELETE FROM components WHERE user_id = ?`, `DELETE FROM health_events WHERE user_id = ?`, `DELETE FROM collector_health WHERE user_id = ?`} {
			if _, err := s.write.ExecContext(ctx, q, id); err != nil {
				return err
			}
		}
	}
	if keepMs > 0 {
		if _, err := s.deleteBatches(ctx, `DELETE FROM health_events WHERE id IN (SELECT id FROM health_events WHERE resolved_ms > 0 AND resolved_ms < ? LIMIT ?)`, now-keepMs); err != nil {
			return err
		}
	}
	return nil
}
