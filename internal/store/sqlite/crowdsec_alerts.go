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
	"time"
)

// CrowdSec rules watch machines and bouncers that live in crowdsec.db, which
// this database cannot reference. A rule keeps its subjects here; its incidents
// are ordinary alert_history rows with subject_kind 'crowdsec', the collector's
// ID in agent_id, and the machine or bouncer in subject_id.

// crowdsecAlertColumns are added to alert_history when missing; each statement is written out in full.
var crowdsecAlertColumns = []struct{ name, add string }{
	{"subject_id", `ALTER TABLE alert_history ADD COLUMN subject_id TEXT NOT NULL DEFAULT ''`},
	{"subject_snapshot", `ALTER TABLE alert_history ADD COLUMN subject_snapshot TEXT NOT NULL DEFAULT '{}'`},
}

var crowdsecAlertSchema = []string{
	`CREATE TABLE IF NOT EXISTS alert_crowdsec_subjects (
		alert_id     TEXT NOT NULL REFERENCES alerts(alert_id) ON DELETE CASCADE,
		collector_id TEXT NOT NULL,
		scope        TEXT NOT NULL,
		name         TEXT NOT NULL DEFAULT '',
		PRIMARY KEY (alert_id, collector_id, scope, name)
	)`,
	`CREATE UNIQUE INDEX IF NOT EXISTS crowdsec_incident_open
		ON alert_history(alert_id, subject_id)
		WHERE subject_kind = 'crowdsec' AND resolved_at IS NULL AND closed_at IS NULL`,
}

func (s *Store) migrateCrowdSecAlerts() error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, column := range crowdsecAlertColumns {
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
	for _, statement := range crowdsecAlertSchema {
		if _, err = tx.Exec(statement); err != nil {
			return fmt.Errorf("crowdsec alert migration: %w", err)
		}
	}
	return tx.Commit()
}

// setAlertCrowdSec replaces a rule's subjects inside the rule transaction. Open
// incidents of subjects no longer watched are closed by the next evaluation.
func (s *Store) setAlertCrowdSec(ctx context.Context, tx *sql.Tx, d store.Alert) error {
	isCS := alert.IsCrowdSecTrigger(d.Trigger.Type)
	if isCS && len(d.CrowdSec) == 0 {
		return nm.Invalid("CrowdSec rules require machines or bouncers")
	}
	if !isCS && len(d.CrowdSec) > 0 {
		return nm.Invalid("Machine and bouncer selection is only valid for CrowdSec rules")
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM alert_crowdsec_subjects WHERE alert_id = ?`, d.AlertID); err != nil {
		return err
	}
	for _, sub := range d.CrowdSec {
		if _, ok := alert.ParseCrowdSecSubject(sub.Key()); !ok {
			return nm.Invalid("invalid machine or bouncer selection")
		}
		var owned int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM collectors WHERE collector_id = ? AND user_id = ? AND revoked_at = 0`, sub.CollectorID, d.UserID).Scan(&owned); err != nil {
			return err
		}
		if owned == 0 {
			return nm.Invalid("invalid collector in machine or bouncer selection")
		}
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO alert_crowdsec_subjects (alert_id, collector_id, scope, name) VALUES (?, ?, ?, ?)`,
			d.AlertID, sub.CollectorID, sub.Scope, sub.Name); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) loadAlertCrowdSec(ctx context.Context, d *store.Alert) error {
	rows, err := s.db.QueryContext(ctx, `SELECT collector_id, scope, name FROM alert_crowdsec_subjects WHERE alert_id = ? ORDER BY collector_id, scope, name`, d.AlertID)
	if err != nil {
		return err
	}
	defer rows.Close()
	d.CrowdSec = nil
	for rows.Next() {
		var sub alert.CrowdSecSubject
		if err := rows.Scan(&sub.CollectorID, &sub.Scope, &sub.Name); err != nil {
			return err
		}
		d.CrowdSec = append(d.CrowdSec, sub)
	}
	return rows.Err()
}

// CrowdSecRules returns every enabled CrowdSec rule with its subjects.
func (s *Store) CrowdSecRules(ctx context.Context) ([]store.Alert, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT alert_id, user_id, nickname, trigger_config, action_config FROM alerts
		WHERE enabled = 1 AND json_extract(trigger_config, '$.type') LIKE 'crowdsec\_%' ESCAPE '\' ORDER BY alert_id`)
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
		if err := s.loadAlertCrowdSec(ctx, &out[i]); err != nil {
			return nil, err
		}
	}
	return out, nil
}

const (
	crowdsecOpenAll = `SELECT h.history_id, h.alert_id, h.alert_nickname, h.user_id, h.agent_id, h.subject_id,
		COALESCE(json_extract(h.trigger_snapshot, '$.type'), ''),
		COALESCE((SELECT e.status FROM alert_history_events e WHERE e.history_id = h.history_id AND e.phase = 'firing' ORDER BY e.created_at DESC, e.event_id DESC LIMIT 1), h.notified_status),
		h.error_message
		FROM alert_history h WHERE h.subject_kind = 'crowdsec' AND h.resolved_at IS NULL AND h.closed_at IS NULL ORDER BY h.history_id`
	crowdsecOpenByUser = `SELECT h.history_id, h.alert_id, h.alert_nickname, h.user_id, h.agent_id, h.subject_id,
		COALESCE(json_extract(h.trigger_snapshot, '$.type'), ''),
		COALESCE((SELECT e.status FROM alert_history_events e WHERE e.history_id = h.history_id AND e.phase = 'firing' ORDER BY e.created_at DESC, e.event_id DESC LIMIT 1), h.notified_status),
		h.error_message
		FROM alert_history h WHERE h.subject_kind = 'crowdsec' AND h.resolved_at IS NULL AND h.closed_at IS NULL AND h.user_id = ? ORDER BY h.history_id`
)

// CrowdSecOpenIncidents lists open CrowdSec incidents, of one owner or, with
// userID empty, of everyone.
func (s *Store) CrowdSecOpenIncidents(ctx context.Context, userID string) ([]store.CrowdSecIncident, error) {
	var rows *sql.Rows
	var err error
	if userID == "" {
		rows, err = s.db.QueryContext(ctx, crowdsecOpenAll)
	} else {
		rows, err = s.db.QueryContext(ctx, crowdsecOpenByUser, userID)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []store.CrowdSecIncident
	for rows.Next() {
		var i store.CrowdSecIncident
		var trigger string
		if err := rows.Scan(&i.HistoryID, &i.AlertID, &i.AlertNickname, &i.UserID, &i.CollectorID, &i.SubjectID, &trigger, &i.Notified, &i.Error); err != nil {
			return nil, err
		}
		i.Trigger = alert.TriggerType(trigger)
		out = append(out, i)
	}
	return out, rows.Err()
}

// CrowdSecFire opens an incident for one machine or bouncer and queues its
// notification. It returns the queued event, or "" when one is already open.
func (s *Store) CrowdSecFire(ctx context.Context, rule store.Alert, collectorID, subjectID string, snap alert.CrowdSecSnapshot, value float64) (string, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	var open int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM alert_history WHERE alert_id = ? AND subject_id = ? AND subject_kind = 'crowdsec' AND resolved_at IS NULL AND closed_at IS NULL`,
		rule.AlertID, subjectID).Scan(&open); err != nil {
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
	snapshot, err := json.Marshal(snap)
	if err != nil {
		return "", err
	}
	now := time.Now().UTC()
	historyID := nm.ID()
	if _, err := tx.ExecContext(ctx, `INSERT INTO alert_history (
			history_id, alert_id, user_id, agent_id, agent_nickname, alert_nickname, triggered_at, trigger_value, notified_status,
			target_id, target_name, trigger_snapshot, subject_kind, subject_id, subject_snapshot
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, 'pending', ?, ?, ?, 'crowdsec', ?, ?)`,
		historyID, rule.AlertID, rule.UserID, collectorID, snap.Name+" on "+snap.Collector, rule.Nickname, now, value,
		rule.Action.TargetID, targetName, string(trigger), subjectID, string(snapshot)); err != nil {
		return "", err
	}
	eventID, err := queueNotification(ctx, tx, historyID, "firing", now)
	if err != nil {
		return "", err
	}
	return eventID, tx.Commit()
}

// CrowdSecResolve ends an incident because its check passed again, and queues
// the recovery notification. It returns "" when the incident had already ended.
func (s *Store) CrowdSecResolve(ctx context.Context, historyID string) (string, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	now := time.Now().UTC()
	res, err := tx.ExecContext(ctx, `UPDATE alert_history SET resolved_at = ? WHERE history_id = ? AND subject_kind = 'crowdsec' AND resolved_at IS NULL AND closed_at IS NULL`, now, historyID)
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

// CrowdSecClose ends an incident without a notification: its rule was disabled,
// deleted or changed, or the machine or bouncer was muted or removed.
func (s *Store) CrowdSecClose(ctx context.Context, historyID string) error {
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
