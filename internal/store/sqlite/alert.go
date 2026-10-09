package sqlite

import (
	"certainstats/internal/base/alert"
	nm "certainstats/internal/networkmonitor"
	"certainstats/internal/store"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// AlertCreate inserts the alert and maps all initial agents in a single transaction
func (s *Store) AlertCreate(ctx context.Context, d store.Alert) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	triggerJSON, err := json.Marshal(d.Trigger)
	if err != nil {
		return fmt.Errorf("failed to marshal trigger: %w", err)
	}

	actionJSON, err := json.Marshal(d.Action)
	if err != nil {
		return fmt.Errorf("failed to marshal action: %w", err)
	}
	_, err = tx.ExecContext(ctx, `
        INSERT INTO alerts (alert_id, user_id, nickname, enabled, trigger_config, action_config)
        VALUES (?, ?, ?, ?, ?, ?)
    `, d.AlertID, d.UserID, d.Nickname, d.Enabled, triggerJSON, actionJSON)
	if err != nil {
		return err
	}

	// 2. Insert agent mappings
	if len(d.Agents) > 0 {
		stmt, err := tx.PrepareContext(ctx, `INSERT INTO alert_agents (alert_id, agent_id) VALUES (?, ?)`)
		if err != nil {
			return err
		}
		defer stmt.Close()

		for _, agent := range d.Agents {
			if _, err := stmt.ExecContext(ctx, d.AlertID, agent.AgentID); err != nil {
				return err
			}
		}
	}

	if err = s.setAlertMonitors(ctx, tx, d); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) AlertList(ctx context.Context, userID string) ([]store.Alert, error) {
	// We use LEFT JOIN and GROUP_CONCAT to fetch the alert and all its agents in ONE query
	// By concatenating agent_id, status and error_message with a colon, we can split them later
	rows, err := s.db.QueryContext(ctx, `
        SELECT a.alert_id, a.user_id, a.nickname, a.enabled, a.trigger_config, a.action_config,
               COALESCE(GROUP_CONCAT(aa.agent_id || ':' || aa.status || ':' || REPLACE(REPLACE(IFNULL(aa.error_message, ''), ',', ' '), ':', ' '), ','), '') as agents
        FROM alerts a
        LEFT JOIN alert_agents aa ON a.alert_id = aa.alert_id
        WHERE a.user_id = ?
        GROUP BY a.alert_id
    `, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []store.Alert
	for rows.Next() {
		var a store.Alert
		var agentsStr string
		var triggerJSON, actionJSON string // Temporary strings to hold the JSON from SQLite

		// 1. Scan into the temporary string variables
		if err := rows.Scan(&a.AlertID, &a.UserID, &a.Nickname, &a.Enabled, &triggerJSON, &actionJSON, &agentsStr); err != nil {
			return nil, err
		}

		// 2. Unmarshal the JSON strings into the actual structs
		if err := json.Unmarshal([]byte(triggerJSON), &a.Trigger); err != nil {
			// You can choose to log this and continue, but returning the error is safer
			return nil, fmt.Errorf("failed to unmarshal trigger for alert %s: %w", a.AlertID, err)
		}
		if err := json.Unmarshal([]byte(actionJSON), &a.Action); err != nil {
			return nil, fmt.Errorf("failed to unmarshal action for alert %s: %w", a.AlertID, err)
		}

		// 3. Process the GROUP_CONCAT agent string (format: id:status:error_message,id:status:error_message)
		if agentsStr != "" {
			agentPairs := strings.Split(agentsStr, ",")
			for _, pair := range agentPairs {
				parts := strings.Split(pair, ":")
				if len(parts) >= 2 {
					var errMsg string
					if len(parts) >= 3 {
						errMsg = parts[2]
					}
					a.Agents = append(a.Agents, alert.AgentState{
						AgentID:      parts[0],
						Status:       parts[1],
						ErrorMessage: errMsg,
					})
				}
			}
		} else {
			a.Agents = []alert.AgentState{}
		}

		out = append(out, a)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()
	for i := range out {
		if err = s.loadAlertMonitors(ctx, &out[i]); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// AlertGetInfo fetches a single alert and its mapped agents
func (s *Store) AlertGetInfo(ctx context.Context, alertID string, userID string) (store.Alert, error) {
	row := s.db.QueryRowContext(ctx, `
        SELECT a.alert_id, a.user_id, a.nickname, a.enabled, a.trigger_config, a.action_config,
               COALESCE(GROUP_CONCAT(aa.agent_id, ','), '') as agents
        FROM alerts a
        LEFT JOIN alert_agents aa ON a.alert_id = aa.alert_id
        WHERE a.alert_id = ? AND a.user_id = ?
        GROUP BY a.alert_id
    `, alertID, userID)

	var a store.Alert
	var agentsStr string
	var triggerJSON, actionJSON string

	// Scan into strings
	err := row.Scan(&a.AlertID, &a.UserID, &a.Nickname, &a.Enabled, &triggerJSON, &actionJSON, &agentsStr)
	if err != nil {
		return store.Alert{}, err // Will return sql.ErrNoRows if not found
	}

	// Unmarshal JSON
	if err := json.Unmarshal([]byte(triggerJSON), &a.Trigger); err != nil {
		return store.Alert{}, err
	}
	if err := json.Unmarshal([]byte(actionJSON), &a.Action); err != nil {
		return store.Alert{}, err
	}

	if agentsStr != "" {
		agentIDs := strings.Split(agentsStr, ",")
		for _, agentID := range agentIDs {
			a.Agents = append(a.Agents, alert.AgentState{
				AgentID: agentID,
				Status:  "ok",
			})
		}
	} else {
		a.Agents = []alert.AgentState{}
	}

	if err = s.loadAlertMonitors(ctx, &a); err != nil {
		return store.Alert{}, err
	}
	return a, nil
}

// AlertAddAgents bulk adds new agents to an existing alert safely
func (s *Store) AlertAddAgents(ctx context.Context, alertID string, agentsID []string) error {
	if len(agentsID) == 0 {
		return nil
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// INSERT OR IGNORE prevents errors if the agent is already attached
	stmt, err := tx.PrepareContext(ctx, `INSERT OR IGNORE INTO alert_agents (alert_id, agent_id) VALUES (?, ?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, agentID := range agentsID {
		if _, err := stmt.ExecContext(ctx, alertID, agentID); err != nil {
			return err
		}
	}

	return tx.Commit()
}

// AlertRemoveAgents bulk removes agents from an alert
func (s *Store) AlertRemoveAgents(ctx context.Context, alertID string, agentsID []string) error {
	if len(agentsID) == 0 {
		return nil
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx, `DELETE FROM alert_agents WHERE alert_id = ? AND agent_id = ?`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, agentID := range agentsID {
		if _, err := stmt.ExecContext(ctx, alertID, agentID); err != nil {
			return err
		}
	}

	return tx.Commit()
}

// AlertUpdate fully updates an alert's configuration and performs a smart diff on its agents
func (s *Store) AlertUpdate(ctx context.Context, d store.Alert, newAgents []string) error {
	if d.Trigger.Type == alert.TriggerTypeNetworkLoss && len(newAgents) > 0 {
		return nm.Invalid("network rules require monitors and no agent selection")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// 1. Convert structs to JSON strings
	triggerJSON, err := json.Marshal(d.Trigger)
	if err != nil {
		return fmt.Errorf("failed to marshal trigger: %w", err)
	}

	actionJSON, err := json.Marshal(d.Action)
	if err != nil {
		return fmt.Errorf("failed to marshal action: %w", err)
	}

	// 2. Update the main alert record with the JSON strings
	res, err := tx.ExecContext(ctx, `
        UPDATE alerts
        SET nickname = ?, enabled = ?, trigger_config = ?, action_config = ?
        WHERE alert_id = ? AND user_id = ?
    `, d.Nickname, d.Enabled, string(triggerJSON), string(actionJSON), d.AlertID, d.UserID)
	if err != nil {
		return err
	}

	rowsAffected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return sql.ErrNoRows // Alert doesn't exist or doesn't belong to the user
	}

	// 3. Fetch existing agents to figure out the diff
	rows, err := tx.QueryContext(ctx, `SELECT agent_id FROM alert_agents WHERE alert_id = ?`, d.AlertID)
	if err != nil {
		return err
	}

	existingMap := make(map[string]bool)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		existingMap[id] = true
	}
	rows.Close()

	// 4. Diffing logic
	var toAdd []string
	var toDelete []string

	newMap := make(map[string]bool)
	for _, id := range newAgents {
		newMap[id] = true
		if !existingMap[id] {
			toAdd = append(toAdd, id)
		}
	}

	for id := range existingMap {
		if !newMap[id] {
			toDelete = append(toDelete, id)
		}
	}

	// 5. Apply Diff
	if len(toDelete) > 0 {
		stmtDel, err := tx.PrepareContext(ctx, `DELETE FROM alert_agents WHERE alert_id = ? AND agent_id = ?`)
		if err != nil {
			return err
		}
		defer stmtDel.Close()
		for _, id := range toDelete {
			if _, err := stmtDel.ExecContext(ctx, d.AlertID, id); err != nil {
				return err
			}
		}
	}

	if len(toAdd) > 0 {
		stmtAdd, err := tx.PrepareContext(ctx, `INSERT INTO alert_agents (alert_id, agent_id) VALUES (?, ?)`)
		if err != nil {
			return err
		}
		defer stmtAdd.Close()
		for _, id := range toAdd {
			if _, err := stmtAdd.ExecContext(ctx, d.AlertID, id); err != nil {
				return err
			}
		}
	}

	if err = s.setAlertMonitors(ctx, tx, d); err != nil {
		return err
	}
	return tx.Commit()
}

// AlertDelete instantly deletes the alert (and cascades to delete all agent mappings!)
func (s *Store) AlertDelete(ctx context.Context, alertID string, userID string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var owned int
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM alerts WHERE alert_id=? AND user_id=?`, alertID, userID).Scan(&owned); err != nil {
		return err
	}
	if owned == 0 {
		return sql.ErrNoRows
	}
	if err = closeIncidentMonitoring(ctx, tx, "alert_id", alertID); err != nil {
		return err
	}
	res, err := tx.ExecContext(ctx, `
        DELETE FROM alerts
        WHERE alert_id = ? AND user_id = ?
    `, alertID, userID)
	if err != nil {
		return err
	}

	rowsAffected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return sql.ErrNoRows
	}

	return tx.Commit()
}

func (s *Store) AlertTrigger(ctx context.Context, d store.Alert, agentID string, agentNickname string, historyID string, violationValue float64, notifStatus string, targetID string, targetName string, errorMsg string) error {
	now := time.Now().UTC()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	agentStatus := "firing"
	if notifStatus == "failed" {
		agentStatus = "failed"
	}

	// Update the agent's state
	res, err := tx.ExecContext(ctx, `UPDATE alert_agents SET status = ?, last_fired_at = ?, error_message = ? WHERE alert_id = ? AND agent_id = ? AND status='ok'`,
		agentStatus, now, errorMsg, d.AlertID, agentID)
	if err != nil {
		return err
	}

	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return sql.ErrNoRows
	}
	// Create a new History Log entry with all snapshot and denormalized columns
	_, err = tx.ExecContext(ctx, `INSERT INTO alert_history (history_id, alert_id, user_id, agent_id, triggered_at, trigger_value, notified_status, target_id, target_name, agent_nickname, alert_nickname, error_message, trigger_snapshot)
             VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		historyID, d.AlertID, d.UserID, agentID, now, violationValue, notifStatus, targetID, targetName, agentNickname, d.Nickname, errorMsg, triggerSnapshot(d.Trigger))
	if err != nil {
		return err
	}

	if _, err = tx.ExecContext(ctx, `INSERT INTO alert_history_events(event_id,history_id,kind,created_at) VALUES (? ,?,'firing',?)`, historyID+"_firing", historyID, now); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) AlertResolve(ctx context.Context, d store.Alert, agentID string) error {
	now := time.Now().UTC()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Update the agent's state back to OK
	_, err = tx.ExecContext(ctx, `UPDATE alert_agents SET status = 'ok', error_message = '' WHERE alert_id = ? AND agent_id = ?`,
		d.AlertID, agentID)
	if err != nil {
		return err
	}

	// Recovery is persisted before notification dispatch.
	if _, err = tx.ExecContext(ctx, `INSERT INTO alert_history_events(event_id,history_id,kind,created_at)
 SELECT history_id||'_resolved',history_id,'resolved',? FROM alert_history WHERE alert_id=? AND agent_id=? AND resolved_at IS NULL AND closed_at IS NULL`, now, d.AlertID, agentID); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE alert_history_events SET status='skipped',completed_at=?,error_message='Incident recovered before dispatch'
 WHERE status='queued' AND phase='firing' AND history_id IN (SELECT history_id FROM alert_history WHERE alert_id=? AND agent_id=? AND resolved_at IS NULL AND closed_at IS NULL)`, now, d.AlertID, agentID); err != nil {
		return err
	}
	// Update the history log with a resolved_at timestamp
	_, err = tx.ExecContext(ctx, `
        UPDATE alert_history
        SET resolved_at = ?
        WHERE alert_id = ? AND agent_id = ? AND resolved_at IS NULL AND closed_at IS NULL
    `, now, d.AlertID, agentID)
	if err != nil {
		return err
	}

	return tx.Commit()
}

func (s *Store) GetActiveAlertsWithState(ctx context.Context) ([]store.Alert, map[string]store.AgentInfo, error) {
	query := `
		SELECT
			a.alert_id, a.user_id, a.nickname, a.trigger_config, a.action_config,
			aa.agent_id, aa.status, aa.last_fired_at, aa.error_message,
			ag.is_online, COALESCE(ag.nickname, ag.agent_id),
			ag.ram_size, ag.swap_size, ag.disk_size
		FROM alerts a
		JOIN alert_agents aa ON a.alert_id = aa.alert_id
		JOIN agents ag ON aa.agent_id = ag.agent_id
		WHERE a.enabled = 1
	`

	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()

	// Use a map to group multiple agents under their parent alert
	alertMap := make(map[string]*store.Alert)
	agentInfoMap := make(map[string]store.AgentInfo)

	for rows.Next() {
		var alertID, userID, alertNickname, triggerJSON, actionJSON, agentID, status, nickname string
		var isOnline bool
		var ramSize, swapSize, diskSize uint64
		var lastFiredAt sql.NullTime
		var errorMsg sql.NullString

		err := rows.Scan(&alertID, &userID, &alertNickname, &triggerJSON, &actionJSON, &agentID, &status, &lastFiredAt, &errorMsg, &isOnline, &nickname, &ramSize, &swapSize, &diskSize)
		if err != nil {
			return nil, nil, err
		}

		// Update agent info map
		agentInfoMap[agentID] = store.AgentInfo{
			Nickname: nickname,
			IsOnline: isOnline,
			RamSize:  ramSize,
			SwapSize: swapSize,
			DiskSize: diskSize,
		}

		// If we haven't seen this alert yet, initialize it and parse the JSON configs
		if _, exists := alertMap[alertID]; !exists {
			var trigger alert.Trigger
			var action alert.AlertAction

			// Silently ignore unmarshal errors for corrupt rows, or log them
			_ = json.Unmarshal([]byte(triggerJSON), &trigger)
			_ = json.Unmarshal([]byte(actionJSON), &action)

			alertMap[alertID] = &store.Alert{
				AlertID:  alertID,
				UserID:   userID,
				Nickname: alertNickname,
				Enabled:  true,
				Trigger:  trigger,
				Action:   action,
				Agents:   []alert.AgentState{},
			}
		}

		var lastFiredPtr *time.Time
		if lastFiredAt.Valid {
			tVal := lastFiredAt.Time
			lastFiredPtr = &tVal
		}

		// Append this agent's state to the alert
		alertMap[alertID].Agents = append(alertMap[alertID].Agents, alert.AgentState{
			AgentID:      agentID,
			Status:       status,
			LastFiredAt:  lastFiredPtr,
			ErrorMessage: errorMsg.String,
		})
	}

	if err := rows.Err(); err != nil {
		return nil, nil, err
	}

	// Flatten the map into a slice for the Engine
	var out []store.Alert
	for _, alert := range alertMap {
		out = append(out, *alert)
	}

	return out, agentInfoMap, nil
}
