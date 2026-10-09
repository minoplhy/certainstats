package sqlite

import (
	"certainstats/internal/agentmeta"
	alert "certainstats/internal/base/alert"
	nm "certainstats/internal/networkmonitor"
	"certainstats/internal/store"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"math"
	"time"
)

// Network loss rules evaluate the agent-reported one-hour loss.
const networkLossDuration = "1h"

// setAlertMonitors runs inside the rule transaction, keeping membership and closure atomic.
func (s *Store) setAlertMonitors(ctx context.Context, tx *sql.Tx, d store.Alert) error {
	if err := validateNetworkRule(d); err != nil {
		return err
	}

	wanted := make(map[string]bool, len(d.MonitorIDs))
	for _, monitorID := range d.MonitorIDs {
		if wanted[monitorID] {
			return nm.Invalid("duplicate monitor selection")
		}
		wanted[monitorID] = true
		if err := s.checkRuleMonitor(ctx, tx, d.UserID, monitorID); err != nil {
			return err
		}
	}

	existing, err := ruleMonitorIDs(ctx, tx, d.AlertID)
	if err != nil {
		return err
	}
	for _, monitorID := range existing {
		if !wanted[monitorID] || !d.Enabled || d.Trigger.Type != alert.TriggerTypeNetworkLoss {
			if err = closeIncidents(ctx, tx, monitorID, d.AlertID); err != nil {
				return err
			}
		}
		if !wanted[monitorID] {
			if _, err = tx.ExecContext(ctx,
				`DELETE FROM alert_monitors WHERE alert_id = ? AND monitor_id = ?`,
				d.AlertID, monitorID,
			); err != nil {
				return err
			}
		}
	}
	for monitorID := range wanted {
		if _, err = tx.ExecContext(ctx,
			`INSERT OR IGNORE INTO alert_monitors(alert_id, monitor_id) VALUES (?, ?)`,
			d.AlertID, monitorID,
		); err != nil {
			return err
		}
	}
	return nil
}

func validateNetworkRule(d store.Alert) error {
	if d.Trigger.Type != alert.TriggerTypeNetworkLoss {
		if len(d.MonitorIDs) > 0 {
			return nm.Invalid("Monitor selection is only valid for network loss rules")
		}
		return nil
	}
	if len(d.Agents) > 0 || len(d.MonitorIDs) == 0 {
		return nm.Invalid("network rules require monitors and no agent selection")
	}
	if d.Trigger.Operator != alert.OpGreaterThan || d.Trigger.Duration != networkLossDuration {
		return nm.Invalid("network loss rules require the > operator and a 1h window")
	}
	threshold := d.Trigger.Threshold
	if math.IsNaN(threshold) || math.IsInf(threshold, 0) || threshold < 0 || threshold > 100 {
		return nm.Invalid("loss threshold must be between 0 and 100")
	}
	return nil
}

// checkRuleMonitor verifies that an owned, current monitor can feed a loss rule.
func (s *Store) checkRuleMonitor(ctx context.Context, tx *sql.Tx, userID, monitorID string) error {
	var c nm.Config
	var agentID string
	err := tx.QueryRowContext(ctx, `
		SELECT agent_id, protocol, dns_server
		FROM   network_monitors
		WHERE  monitor_id = ? AND user_id = ? AND archived_at IS NULL`,
		monitorID, userID,
	).Scan(&agentID, &c.Protocol, &c.Server)
	if errors.Is(err, sql.ErrNoRows) {
		return nm.Invalid("invalid monitor selection")
	} else if err != nil {
		return err
	}

	agent, err := s.loadMonitorAgent(ctx, tx, agentID, userID)
	if err != nil {
		return err
	}
	caps := agent.capabilities
	if err = nm.CheckCapabilities(agentID, c, caps); err != nil {
		return err
	}
	if !caps.Supports(agentmeta.FeatureHourlyLoss) {
		key := agentmeta.Key(agentmeta.FeatureHourlyLoss)
		return &nm.CapabilityError{AgentID: agentID, Feature: key, Capability: caps[key]}
	}
	return nil
}

func ruleMonitorIDs(ctx context.Context, tx *sql.Tx, alertID string) ([]string, error) {
	rows, err := tx.QueryContext(ctx, `SELECT monitor_id FROM alert_monitors WHERE alert_id = ?`, alertID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := []string{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (s *Store) loadAlertMonitors(ctx context.Context, d *store.Alert) error {
	rows, err := s.db.QueryContext(ctx,
		`SELECT monitor_id FROM alert_monitors WHERE alert_id = ? ORDER BY monitor_id`,
		d.AlertID,
	)
	if err != nil {
		return err
	}
	defer rows.Close()
	d.MonitorIDs = []string{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			return err
		}
		d.MonitorIDs = append(d.MonitorIDs, id)
	}
	return rows.Err()
}

func (s *Store) NetworkEvaluate(ctx context.Context, agentID string) ([]string, error) {
	// Preflight state checks run outside the transaction; evaluateMonitor
	// repeats the live checks inside it.
	query := `
		SELECT DISTINCT m.monitor_id, m.user_id
		FROM   network_monitors m
		JOIN   alert_monitors am ON am.monitor_id = m.monitor_id
		WHERE  m.enabled = 1 AND m.archived_at IS NULL`
	args := []any{}
	if agentID != "" {
		query += ` AND m.agent_id = ?`
		args = append(args, agentID)
	}
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	type ownedMonitor struct{ monitorID, userID string }
	candidates := []ownedMonitor{}
	for rows.Next() {
		var c ownedMonitor
		if err = rows.Scan(&c.monitorID, &c.userID); err != nil {
			rows.Close()
			return nil, err
		}
		candidates = append(candidates, c)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}

	events := []string{}
	for _, c := range candidates {
		m, err := s.NetworkGet(ctx, c.userID, c.monitorID)
		if err != nil {
			return nil, err
		}
		if m.State != nm.StateActive || m.Latest == nil || m.Latest.SampleCount < nm.MinAlertSamples ||
			!m.Capabilities.Supports(agentmeta.FeatureHourlyLoss) {
			continue
		}
		queued, err := s.evaluateMonitor(ctx, m)
		if err != nil {
			return nil, err
		}
		events = append(events, queued...)
	}
	return events, nil
}

// lossRule is a loss rule paired with one monitor's alert state.
type lossRule struct {
	alertID   string
	name      string
	trigger   string
	action    string
	status    string
	historyID string
}

func (s *Store) evaluateMonitor(ctx context.Context, m *nm.Monitor) ([]string, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	latest, err := s.evaluationReading(ctx, tx, m)
	if err != nil || latest == nil {
		return nil, err
	}
	rules, err := monitorRules(ctx, tx, m)
	if err != nil {
		return nil, err
	}

	events := []string{}
	now := time.Now().UTC()
	for _, rule := range rules {
		var trigger alert.Trigger
		if err = json.Unmarshal([]byte(rule.trigger), &trigger); err != nil {
			return nil, err
		}
		if trigger.Type != alert.TriggerTypeNetworkLoss {
			continue
		}

		violates := latest.Loss1h > trigger.Threshold
		var phase, historyID string
		switch {
		case violates && rule.status == "ok":
			phase = "firing"
			historyID, err = fireIncident(ctx, tx, m, rule, latest.Loss1h, now)
		case !violates && rule.status != "ok" && rule.historyID != "":
			phase = "recovery"
			historyID = rule.historyID
			var recovered bool
			recovered, err = recoverIncident(ctx, tx, m.ID, rule, now)
			if !recovered {
				phase = ""
			}
		}
		if err != nil {
			return nil, err
		}
		if phase == "" {
			continue
		}

		eventID, err := queueNotification(ctx, tx, historyID, phase, now)
		if err != nil {
			return nil, err
		}
		events = append(events, eventID)
	}
	return events, tx.Commit()
}

// evaluationReading returns the latest reading when the monitor can still be
// evaluated inside tx, or nil when a capability, online or freshness check fails.
func (s *Store) evaluationReading(ctx context.Context, tx *sql.Tx, m *nm.Monitor) (*nm.Latest, error) {
	agent, err := s.loadMonitorAgent(ctx, tx, m.AgentID, m.UserID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	caps := agent.capabilities
	if !caps.SupportsConfig(m.Protocol, m.Server) || !caps.Supports(agentmeta.FeatureHourlyLoss) {
		return nil, nil
	}

	var payload string
	err = tx.QueryRowContext(ctx, `
		SELECT l.payload
		FROM   network_monitor_latest l
		JOIN   network_monitors m ON m.monitor_id = l.monitor_id
		JOIN   agents n ON n.agent_id = m.agent_id AND n.user_id = m.user_id
		WHERE  m.monitor_id = ? AND m.enabled = 1 AND m.archived_at IS NULL AND n.is_online = 1`,
		m.ID,
	).Scan(&payload)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}

	var latest nm.Latest
	if err = json.Unmarshal([]byte(payload), &latest); err != nil {
		return nil, err
	}
	if latest.SampleCount < nm.MinAlertSamples || !nm.Fresh(latest.Result, m.Interval, time.Now()) {
		return nil, nil
	}
	return &latest, nil
}

func monitorRules(ctx context.Context, tx *sql.Tx, m *nm.Monitor) ([]lossRule, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT r.alert_id, r.nickname, r.trigger_config, r.action_config, am.status, am.history_id
		FROM   alerts r
		JOIN   alert_monitors am ON am.alert_id = r.alert_id
		WHERE  am.monitor_id = ? AND r.user_id = ? AND r.enabled = 1`,
		m.ID, m.UserID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	rules := []lossRule{}
	for rows.Next() {
		var r lossRule
		if err = rows.Scan(&r.alertID, &r.name, &r.trigger, &r.action, &r.status, &r.historyID); err != nil {
			return nil, err
		}
		rules = append(rules, r)
	}
	return rules, rows.Err()
}

// fireIncident opens an incident with the monitor's configuration snapshot.
func fireIncident(ctx context.Context, tx *sql.Tx, m *nm.Monitor, rule lossRule, loss float64, now time.Time) (string, error) {
	var action alert.AlertAction
	if err := json.Unmarshal([]byte(rule.action), &action); err != nil {
		return "", err
	}
	targetName := ""
	if action.TargetID != "" {
		err := tx.QueryRowContext(ctx,
			`SELECT name FROM alert_targets WHERE target_id = ? AND user_id = ?`,
			action.TargetID, m.UserID,
		).Scan(&targetName)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return "", err
		}
	}
	snapshot, err := json.Marshal(m.Config)
	if err != nil {
		return "", err
	}

	historyID := nm.ID()
	if _, err = tx.ExecContext(ctx, `
		INSERT INTO alert_history(
			history_id, alert_id, user_id, agent_id, agent_nickname, alert_nickname,
			triggered_at, trigger_value, notified_status, target_id, target_name,
			trigger_snapshot, subject_kind, monitor_id, monitor_snapshot
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, 'pending', ?, ?, ?, 'network_monitor', ?, ?)`,
		historyID, rule.alertID, m.UserID, m.AgentID, m.AgentName, rule.name,
		now, loss, action.TargetID, targetName,
		rule.trigger, m.ID, string(snapshot),
	); err != nil {
		return "", err
	}
	if _, err = tx.ExecContext(ctx, `
		UPDATE alert_monitors
		SET    status = 'firing', last_fired_at = ?, history_id = ?
		WHERE  alert_id = ? AND monitor_id = ? AND status = 'ok'`,
		now, historyID, rule.alertID, m.ID,
	); err != nil {
		return "", err
	}
	return historyID, nil
}

// recoverIncident resolves the rule's open incident. It reports false when the
// incident was already resolved or closed elsewhere.
func recoverIncident(ctx context.Context, tx *sql.Tx, monitorID string, rule lossRule, now time.Time) (bool, error) {
	res, err := tx.ExecContext(ctx, `
		UPDATE alert_history
		SET    resolved_at = ?
		WHERE  history_id = ? AND resolved_at IS NULL AND closed_at IS NULL`,
		now, rule.historyID,
	)
	if err != nil {
		return false, err
	}
	resolved, err := res.RowsAffected()
	if err != nil || resolved == 0 {
		return false, err
	}
	if _, err = tx.ExecContext(ctx, `
		UPDATE alert_monitors
		SET    status = 'ok', history_id = '', error_message = ''
		WHERE  alert_id = ? AND monitor_id = ?`,
		rule.alertID, monitorID,
	); err != nil {
		return false, err
	}
	_, err = tx.ExecContext(ctx, `
		UPDATE alert_history_events
		SET    status = 'skipped', completed_at = ?, error_message = 'Incident recovered'
		WHERE  history_id = ? AND phase = 'firing' AND status = 'queued'`,
		now, rule.historyID,
	)
	return err == nil, err
}

// queueNotification records the phase event and queues its notification.
func queueNotification(ctx context.Context, tx *sql.Tx, historyID, phase string, now time.Time) (string, error) {
	kind := phase
	if phase == "recovery" {
		kind = "resolved"
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO alert_history_events(event_id, history_id, kind, created_at) VALUES (?, ?, ?, ?)`,
		nm.ID(), historyID, kind, now,
	); err != nil {
		return "", err
	}
	eventID := nm.ID()
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO alert_history_events(event_id, history_id, kind, phase, status, created_at)
		VALUES (?, ?, 'notification', ?, 'queued', ?)`,
		eventID, historyID, phase, now,
	); err != nil {
		return "", err
	}
	return eventID, nil
}
