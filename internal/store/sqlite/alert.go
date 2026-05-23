package sqlite

import (
	"certainstats/internal/base/alert"
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

	return tx.Commit()
}

// AlertList fetches all alerts and neatly packs their Agents into the AgentsID slice
// AlertList fetches all alerts and neatly packs their Agents into the Agents slice
func (s *Store) AlertList(ctx context.Context, userID string) ([]store.Alert, error) {
	// We use LEFT JOIN and GROUP_CONCAT to fetch the alert and all its agents in ONE query
	// By concatenating agent_id and status with a colon, we can split them later
	rows, err := s.db.QueryContext(ctx, `
        SELECT a.alert_id, a.user_id, a.nickname, a.enabled, a.trigger_config, a.action_config,
               COALESCE(GROUP_CONCAT(aa.agent_id || ':' || aa.status, ','), '') as agents
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

		// 3. Process the GROUP_CONCAT agent string (format: id:status,id:status)
		if agentsStr != "" {
			agentPairs := strings.Split(agentsStr, ",")
			for _, pair := range agentPairs {
				parts := strings.Split(pair, ":")
				if len(parts) == 2 {
					a.Agents = append(a.Agents, alert.AgentState{
						AgentID: parts[0],
						Status:  parts[1],
					})
				}
			}
		} else {
			a.Agents = []alert.AgentState{}
		}

		out = append(out, a)
	}
	return out, rows.Err()
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

	return tx.Commit()
}

// AlertDelete instantly deletes the alert (and cascades to delete all agent mappings!)
func (s *Store) AlertDelete(ctx context.Context, alertID string, userID string) error {
	res, err := s.db.ExecContext(ctx, `
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

	return nil
}

func (s *Store) AlertTrigger(ctx context.Context, d store.Alert, agentID string, historyID string, violationValue float64, notifStatus string) error {
	now := time.Now()

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
	_, err = tx.ExecContext(ctx, `UPDATE alert_agents SET status = ?, last_fired_at = ? WHERE alert_id = ? AND agent_id = ?`,
		agentStatus, now, d.AlertID, agentID)
	if err != nil {
		return err
	}

	// Create a new History Log entry
	_, err = tx.ExecContext(ctx, `INSERT INTO alert_history (history_id, alert_id, agent_id, triggered_at, trigger_value, notified_status) 
             VALUES (?, ?, ?, ?, ?, ?)`,
		historyID, d.AlertID, agentID, now, violationValue, notifStatus)
	if err != nil {
		return err
	}

	return tx.Commit()
}

func (s *Store) AlertResolve(ctx context.Context, d store.Alert, agentID string) error {
	now := time.Now()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Update the agent's state back to OK
	_, err = tx.ExecContext(ctx, `UPDATE alert_agents SET status = 'ok' WHERE alert_id = ? AND agent_id = ?`,
		d.AlertID, agentID)
	if err != nil {
		return err
	}

	// Update the most recent history log with a resolved_at timestamp
	_, err = tx.ExecContext(ctx, `
        UPDATE alert_history 
        SET resolved_at = ? 
        WHERE alert_id = ? AND agent_id = ? AND resolved_at IS NULL
    `, now, d.AlertID, agentID)
	if err != nil {
		return err
	}

	return tx.Commit()
}

func (s *Store) GetActiveAlertsWithState(ctx context.Context) ([]store.Alert, map[string]store.AgentInfo, error) {
	query := `
		SELECT 
			a.alert_id, a.trigger_config, a.action_config, 
			aa.agent_id, aa.status, aa.last_fired_at,
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
		var alertID, triggerJSON, actionJSON, agentID, status, nickname string
		var isOnline bool
		var ramSize, swapSize, diskSize uint64
		var lastFiredAt sql.NullTime

		err := rows.Scan(&alertID, &triggerJSON, &actionJSON, &agentID, &status, &lastFiredAt, &isOnline, &nickname, &ramSize, &swapSize, &diskSize)
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
				AlertID: alertID,
				Trigger: trigger,
				Action:  action,
				Agents:  []alert.AgentState{},
			}
		}

		var lastFiredPtr *time.Time
		if lastFiredAt.Valid {
			tVal := lastFiredAt.Time
			lastFiredPtr = &tVal
		}

		// Append this agent's state to the alert
		alertMap[alertID].Agents = append(alertMap[alertID].Agents, alert.AgentState{
			AgentID:     agentID,
			Status:      status,
			LastFiredAt: lastFiredPtr,
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

func (s *Store) AlertHistoryListPaginated(ctx context.Context, userID string, page, limit int, search string, status string) ([]alert.AlertHistory, int, error) {
	if page < 1 {
		page = 1
	}
	if limit < 1 {
		limit = 25
	}
	if limit > 100 {
		limit = 100
	}
	offset := (page - 1) * limit

	var conditions []string
	var args []interface{}

	// Always filter by userID
	conditions = append(conditions, "a.user_id = ?")
	args = append(args, userID)

	switch status {
	case "firing":
		conditions = append(conditions, "h.resolved_at IS NULL")
	case "resolved":
		conditions = append(conditions, "h.resolved_at IS NOT NULL")
	}

	if search != "" {
		searchPattern := "%" + search + "%"
		conditions = append(conditions, "(ag.nickname LIKE ? OR h.agent_id LIKE ? OR a.trigger_config LIKE ? OR a.nickname LIKE ?)")
		args = append(args, searchPattern, searchPattern, searchPattern, searchPattern)
	}

	whereClause := "WHERE " + strings.Join(conditions, " AND ")

	// Performance optimization: Avoid joining the large agents table for count queries
	// unless we are actively filtering by its attributes (nickname/id/config).
	var countJoinClause string
	if search != "" {
		countJoinClause = "LEFT JOIN agents ag ON h.agent_id = ag.agent_id"
	}

	countQuery := fmt.Sprintf(`
		SELECT COUNT(1)
		FROM alert_history h
		JOIN alerts a ON h.alert_id = a.alert_id
		%s
		%s
	`, countJoinClause, whereClause)

	var total int
	err := s.db.QueryRowContext(ctx, countQuery, args...).Scan(&total)
	if err != nil {
		return nil, 0, err
	}

	// Build final query arguments
	queryArgs := append(args, limit, offset)

	query := fmt.Sprintf(`
		SELECT 
			h.history_id, h.alert_id, h.agent_id, 
			COALESCE(NULLIF(ag.nickname, ''), h.agent_id),
			h.triggered_at, h.resolved_at, h.trigger_value, h.notified_status,
			a.trigger_config, a.nickname
		FROM alert_history h
		JOIN alerts a ON h.alert_id = a.alert_id
		LEFT JOIN agents ag ON h.agent_id = ag.agent_id
		%s
		ORDER BY h.triggered_at DESC
		LIMIT ? OFFSET ?
	`, whereClause)

	rows, err := s.db.QueryContext(ctx, query, queryArgs...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var out []alert.AlertHistory
	for rows.Next() {
		var h alert.AlertHistory
		var resolvedAt sql.NullTime
		var triggerJSON string

		err := rows.Scan(
			&h.HistoryID, &h.AlertID, &h.AgentID,
			&h.AgentNickname,
			&h.TriggeredAt, &resolvedAt, &h.TriggerValue, &h.NotifiedStatus,
			&triggerJSON, &h.AlertNickname,
		)
		if err != nil {
			return nil, 0, err
		}

		if resolvedAt.Valid {
			h.ResolvedAt = &resolvedAt.Time
		}

		if err := json.Unmarshal([]byte(triggerJSON), &h.Trigger); err != nil {
			// Log and continue, or ignore
		}

		out = append(out, h)
	}

	return out, total, rows.Err()
}
