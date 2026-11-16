package sqlite

import (
	alert "certainstats/internal/base/alert"
	nm "certainstats/internal/networkmonitor"
	"certainstats/internal/store"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Collector rules watch subjects that live in a collector type's own files
// (CrowdSec machines and bouncers, Knot DNS servers and zones), which this
// database cannot reference. A rule keeps its subjects in alert_subjects; its
// incidents are ordinary alert_history rows with the source (the type name) in
// subject_kind, the collector's ID in agent_id, and the subject in subject_id.

// Subject kinds that are not collector sources. Every other kind is one.
const (
	kindAgent   = "agent"
	kindNetwork = "network_monitor"
	// sqlCollectorKind matches collector incidents in alert_history.
	sqlCollectorKind = `subject_kind NOT IN ('` + kindAgent + `', '` + kindNetwork + `')`
)

// collectorAlertColumns are added to alert_history when missing; each statement is written out in full.
var collectorAlertColumns = []struct{ name, add string }{
	{"subject_id", `ALTER TABLE alert_history ADD COLUMN subject_id TEXT NOT NULL DEFAULT ''`},
	{"subject_snapshot", `ALTER TABLE alert_history ADD COLUMN subject_snapshot TEXT NOT NULL DEFAULT '{}'`},
}

var collectorAlertSchema = []string{
	`CREATE TABLE IF NOT EXISTS alert_subjects (
		alert_id     TEXT NOT NULL REFERENCES alerts(alert_id) ON DELETE CASCADE,
		source       TEXT NOT NULL,
		collector_id TEXT NOT NULL,
		scope        TEXT NOT NULL,
		name         TEXT NOT NULL DEFAULT '',
		PRIMARY KEY (alert_id, source, collector_id, scope, name)
	)`,
	// Only collector incidents set subject_id.
	`CREATE UNIQUE INDEX IF NOT EXISTS collector_incident_open
		ON alert_history(alert_id, subject_id)
		WHERE subject_id != '' AND resolved_at IS NULL AND closed_at IS NULL`,
}

// migrateCollectorAlerts adds the subject columns and tables, and moves rules
// from the CrowdSec-only table of older versions (alert_crowdsec_subjects) and
// its index. CrowdSec incidents already have subject_kind 'crowdsec', their source.
func (s *Store) migrateCollectorAlerts() error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, column := range collectorAlertColumns {
		var exists int
		if err = tx.QueryRow(`SELECT count(*) FROM pragma_table_info('alert_history') WHERE name = ?`, column.name).Scan(&exists); err != nil {
			return err
		}
		if exists > 0 {
			continue
		}
		if _, err = tx.Exec(column.add); err != nil {
			return err
		}
	}
	if _, err = tx.Exec(`DROP INDEX IF EXISTS crowdsec_incident_open`); err != nil {
		return err
	}
	for _, statement := range collectorAlertSchema {
		if _, err = tx.Exec(statement); err != nil {
			return fmt.Errorf("collector alert migration: %w", err)
		}
	}
	var legacy int
	if err = tx.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = 'alert_crowdsec_subjects'`).Scan(&legacy); err != nil {
		return err
	}
	if legacy > 0 {
		if _, err = tx.Exec(`INSERT OR IGNORE INTO alert_subjects (alert_id, source, collector_id, scope, name)
			SELECT alert_id, 'crowdsec', collector_id, scope, name FROM alert_crowdsec_subjects`); err != nil {
			return fmt.Errorf("collector alert migration: %w", err)
		}
		if _, err = tx.Exec(`DROP TABLE alert_crowdsec_subjects`); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// setAlertSubjects replaces a rule's subjects inside the rule transaction. Open
// incidents of subjects no longer watched are closed by the next evaluation.
func (s *Store) setAlertSubjects(ctx context.Context, tx *sql.Tx, d store.Alert) error {
	source, isSubject := alert.SourceOf(d.Trigger.Type)
	if isSubject && len(d.Subjects) == 0 {
		return nm.Invalid("Collector rules require something to watch")
	}
	if !isSubject && len(d.Subjects) > 0 {
		return nm.Invalid("Collector subjects are only valid for collector rules")
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM alert_subjects WHERE alert_id = ?`, d.AlertID); err != nil {
		return err
	}
	for _, sub := range d.Subjects {
		if sub.Source != source || !sub.Valid() {
			return nm.Invalid("invalid selection for this rule")
		}
		var owned int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM collectors WHERE collector_id = ? AND user_id = ? AND revoked_at = 0`, sub.CollectorID, d.UserID).Scan(&owned); err != nil {
			return err
		}
		if owned == 0 {
			return nm.Invalid("invalid collector in the selection")
		}
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO alert_subjects (alert_id, source, collector_id, scope, name) VALUES (?, ?, ?, ?, ?)`,
			d.AlertID, sub.Source, sub.CollectorID, sub.Scope, sub.Name); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) loadAlertSubjects(ctx context.Context, d *store.Alert) error {
	rows, err := s.db.QueryContext(ctx, `SELECT source, collector_id, scope, name FROM alert_subjects WHERE alert_id = ? ORDER BY source, collector_id, scope, name`, d.AlertID)
	if err != nil {
		return err
	}
	defer rows.Close()
	d.Subjects = nil
	for rows.Next() {
		var sub alert.Subject
		if err := rows.Scan(&sub.Source, &sub.CollectorID, &sub.Scope, &sub.Name); err != nil {
			return err
		}
		d.Subjects = append(d.Subjects, sub)
	}
	return rows.Err()
}

// SubjectRules returns every enabled rule whose trigger is one of triggers, with its subjects.
func (s *Store) SubjectRules(ctx context.Context, triggers []alert.TriggerType) ([]store.Alert, error) {
	if len(triggers) == 0 {
		return nil, nil
	}
	marks := strings.TrimSuffix(strings.Repeat("?,", len(triggers)), ",")
	args := make([]any, len(triggers))
	for i, t := range triggers {
		args[i] = string(t)
	}
	rows, err := s.db.QueryContext(ctx, `SELECT alert_id, user_id, nickname, trigger_config, action_config FROM alerts
		WHERE enabled = 1 AND json_extract(trigger_config, '$.type') IN (`+marks+`) ORDER BY alert_id`, args...)
	if err != nil {
		return nil, err
	}
	var out []store.Alert
	for rows.Next() {
		var a store.Alert
		var trigger, action string
		if err := rows.Scan(&a.AlertID, &a.UserID, &a.Nickname, &trigger, &action); err != nil {
			rows.Close()
			return nil, err
		}
		if err := json.Unmarshal([]byte(trigger), &a.Trigger); err != nil {
			rows.Close()
			return nil, fmt.Errorf("rule %s: %w", a.AlertID, err)
		}
		if err := json.Unmarshal([]byte(action), &a.Action); err != nil {
			rows.Close()
			return nil, fmt.Errorf("rule %s: %w", a.AlertID, err)
		}
		a.Enabled = true
		out = append(out, a)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	for i := range out {
		if err := s.loadAlertSubjects(ctx, &out[i]); err != nil {
			return nil, err
		}
	}
	return out, nil
}

const subjectOpenSelect = `SELECT h.history_id, h.alert_id, h.alert_nickname, h.user_id, h.agent_id, h.subject_id,
		COALESCE(json_extract(h.trigger_snapshot, '$.type'), ''),
		COALESCE((SELECT e.status FROM alert_history_events e WHERE e.history_id = h.history_id AND e.phase = 'firing' ORDER BY e.created_at DESC, e.event_id DESC LIMIT 1), h.notified_status),
		h.error_message
		FROM alert_history h WHERE h.subject_kind = ? AND h.resolved_at IS NULL AND h.closed_at IS NULL`

// SubjectOpenIncidents lists open incidents of one source, of one owner or,
// with userID empty, of everyone.
func (s *Store) SubjectOpenIncidents(ctx context.Context, source, userID string) ([]store.SubjectIncident, error) {
	var rows *sql.Rows
	var err error
	if userID == "" {
		rows, err = s.db.QueryContext(ctx, subjectOpenSelect+` ORDER BY h.history_id`, source)
	} else {
		rows, err = s.db.QueryContext(ctx, subjectOpenSelect+` AND h.user_id = ? ORDER BY h.history_id`, source, userID)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []store.SubjectIncident
	for rows.Next() {
		var i store.SubjectIncident
		var trigger string
		if err := rows.Scan(&i.HistoryID, &i.AlertID, &i.AlertNickname, &i.UserID, &i.CollectorID, &i.SubjectID, &trigger, &i.Notified, &i.Error); err != nil {
			return nil, err
		}
		i.Trigger = alert.TriggerType(trigger)
		out = append(out, i)
	}
	return out, rows.Err()
}

// SubjectFire opens an incident for one subject of a source and queues its
// notification. It returns the queued event, or "" when one is already open.
func (s *Store) SubjectFire(ctx context.Context, source string, rule store.Alert, collectorID, subjectID string, snap alert.SubjectSnapshot, value float64) (string, error) {
	if source == "" || source == kindAgent || source == kindNetwork || subjectID == "" {
		return "", fmt.Errorf("subject incident needs a collector source and a subject")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	var open int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM alert_history WHERE alert_id = ? AND subject_id = ? AND subject_kind = ? AND resolved_at IS NULL AND closed_at IS NULL`,
		rule.AlertID, subjectID, source).Scan(&open); err != nil {
		return "", err
	}
	if open > 0 {
		return "", nil
	}
	targetName := ""
	if rule.Action.TargetID != "" {
		err := tx.QueryRowContext(ctx, `SELECT name FROM alert_targets WHERE target_id = ? AND user_id = ?`, rule.Action.TargetID, rule.UserID).Scan(&targetName)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return "", err
		}
	}
	trigger, err := json.Marshal(rule.Trigger)
	if err != nil {
		return "", err
	}
	snap.Source = source
	snapshot, err := json.Marshal(snap)
	if err != nil {
		return "", err
	}
	now := time.Now().UTC()
	historyID := nm.ID()
	nickname := snap.Name
	if snap.Collector != "" && snap.Collector != snap.Name {
		nickname += " on " + snap.Collector
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO alert_history (
			history_id, alert_id, user_id, agent_id, agent_nickname, alert_nickname, triggered_at, trigger_value, notified_status,
			target_id, target_name, trigger_snapshot, subject_kind, subject_id, subject_snapshot
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, 'pending', ?, ?, ?, ?, ?, ?)`,
		historyID, rule.AlertID, rule.UserID, collectorID, nickname, rule.Nickname, now, value,
		rule.Action.TargetID, targetName, string(trigger), source, subjectID, string(snapshot)); err != nil {
		return "", err
	}
	eventID, err := queueNotification(ctx, tx, historyID, "firing", now)
	if err != nil {
		return "", err
	}
	return eventID, tx.Commit()
}

// SubjectResolve ends a collector incident because its check passed again, and
// queues the recovery notification. It returns "" when the incident had already ended.
func (s *Store) SubjectResolve(ctx context.Context, historyID string) (string, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	now := time.Now().UTC()
	res, err := tx.ExecContext(ctx, `UPDATE alert_history SET resolved_at = ? WHERE history_id = ? AND `+sqlCollectorKind+` AND resolved_at IS NULL AND closed_at IS NULL`, now, historyID)
	if err != nil {
		return "", err
	}
	if n, err := res.RowsAffected(); err != nil || n == 0 {
		return "", err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE alert_history_events SET status = 'skipped', completed_at = ?, error_message = 'Incident recovered'
		WHERE history_id = ? AND phase = 'firing' AND status = 'queued'`, now, historyID); err != nil {
		return "", err
	}
	eventID, err := queueNotification(ctx, tx, historyID, "recovery", now)
	if err != nil {
		return "", err
	}
	return eventID, tx.Commit()
}

// SubjectClose ends a collector incident without a notification: its rule was
// disabled, deleted or changed, or the subject was muted or removed.
func (s *Store) SubjectClose(ctx context.Context, historyID string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := closeIncidentMonitoring(ctx, tx, "history_id", historyID); err != nil {
		return err
	}
	return tx.Commit()
}
