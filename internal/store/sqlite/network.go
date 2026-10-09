package sqlite

import (
	"certainstats/internal/agentmeta"
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

// monitorSelect is followed by a WHERE clause on the m alias.
const monitorSelect = `
	SELECT m.monitor_id, m.user_id, m.agent_id, m.agent_name, m.target, m.protocol, m.port,
	       m.interval_seconds, m.dns_server, m.enabled, m.created_at, m.updated_at,
	       m.archived_at, m.replacement_id, COALESCE(l.payload, ''),
	       COALESCE(s.desired_generation, 0), COALESCE(s.ack_generation, 0),
	       COALESCE(s.error, ''), s.last_attempt, s.last_ack
	FROM   network_monitors m
	LEFT   JOIN network_monitor_latest l ON l.monitor_id = m.monitor_id
	LEFT   JOIN network_monitor_sync s ON s.agent_id = m.agent_id
	`

var likeEscaper = strings.NewReplacer(`\`, `\\`, "%", `\%`, "_", `\_`)

// likePattern matches text as a literal substring in a LIKE ... ESCAPE '\' clause.
func likePattern(text string) string {
	return "%" + likeEscaper.Replace(text) + "%"
}

func scanMonitor(row scanner) (*nm.Monitor, error) {
	m := new(nm.Monitor)
	var archivedAt, lastAttempt, lastAck sql.NullTime
	var latest string
	err := row.Scan(
		&m.ID, &m.UserID, &m.AgentID, &m.AgentName, &m.Target, &m.Protocol, &m.Port,
		&m.Interval, &m.Server, &m.Enabled, &m.CreatedAt, &m.UpdatedAt,
		&archivedAt, &m.ReplacementID, &latest,
		&m.Sync.Desired, &m.Sync.Ack,
		&m.Sync.Error, &lastAttempt, &lastAck,
	)
	if err != nil {
		return nil, err
	}
	if archivedAt.Valid {
		m.ArchivedAt = &archivedAt.Time
	}
	if lastAttempt.Valid {
		m.Sync.LastAttempt = &lastAttempt.Time
	}
	if lastAck.Valid {
		m.Sync.LastAck = &lastAck.Time
	}
	if latest != "" {
		if err = json.Unmarshal([]byte(latest), &m.Latest); err != nil {
			return nil, err
		}
	}
	return m, nil
}

func scanMonitors(rows *sql.Rows) ([]nm.Monitor, error) {
	defer rows.Close()
	out := []nm.Monitor{}
	for rows.Next() {
		m, err := scanMonitor(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *m)
	}
	return out, rows.Err()
}

// applyState derives the monitor's UI state. A nil agent reads as offline.
func (s *Store) applyState(m *nm.Monitor, agent *store.Agent) {
	switch {
	case m.ArchivedAt != nil:
		m.State = nm.StateArchived
		return
	case !m.Enabled:
		m.State = nm.StatePaused
		return
	case agent == nil:
		m.State = nm.StateOffline
		return
	}

	m.Capabilities = s.agentCapabilities(agent)
	if !m.Capabilities.SupportsConfig(m.Protocol, m.Server) {
		m.State = unsupportedState(m.Capabilities, m.Protocol, m.Server)
		return
	}

	switch {
	case !agent.IsOnline:
		m.State = nm.StateOffline
	case m.Latest == nil:
		m.State = nm.StateWaiting
	case nm.Fresh(m.Latest.Result, m.Interval, time.Now()):
		m.State = nm.StateActive
	default:
		m.State = nm.StateStale
	}
}

// unsupportedState is unknown when any required feature is unresolved.
func unsupportedState(caps agentmeta.Capabilities, protocol, dnsServer string) string {
	for _, feature := range agentmeta.ConfigFeatures(protocol, dnsServer) {
		if caps[agentmeta.Key(feature)].State == agentmeta.StateUnknown {
			return nm.StateUnknown
		}
	}
	return nm.StateUnsupported
}

// applyStates derives every monitor's state from one owner agent list.
func (s *Store) applyStates(monitors []nm.Monitor, agents []store.Agent) {
	byID := make(map[string]*store.Agent, len(agents))
	for i := range agents {
		byID[agents[i].AgentID] = &agents[i]
	}
	for i := range monitors {
		s.applyState(&monitors[i], byID[monitors[i].AgentID])
	}
}

func (s *Store) NetworkGet(ctx context.Context, userID, monitorID string) (*nm.Monitor, error) {
	m, err := scanMonitor(s.db.QueryRowContext(ctx,
		monitorSelect+`WHERE m.user_id = ? AND m.monitor_id = ?`,
		userID, monitorID,
	))
	if err != nil {
		return nil, err
	}
	agent, err := s.AgentGetByID(ctx, m.AgentID, m.UserID)
	if errors.Is(err, sql.ErrNoRows) {
		agent = nil
	} else if err != nil {
		return nil, err
	}
	s.applyState(m, agent)
	return m, nil
}

func (s *Store) NetworkList(ctx context.Context, userID string, filter store.NetworkListFilter, page, limit int) ([]nm.Monitor, int, error) {
	page = max(page, 1)
	if limit < 1 {
		limit = nm.DefaultPageSize
	}
	limit = min(limit, nm.MaxPageSize)

	where := `WHERE m.user_id = ?`
	args := []any{userID}
	if filter.AgentID != "" {
		where += ` AND m.agent_id = ?`
		args = append(args, filter.AgentID)
	}
	switch filter.State {
	case nm.StateArchived:
		where += ` AND m.archived_at IS NOT NULL`
	case nm.StatePaused:
		where += ` AND m.archived_at IS NULL AND m.enabled = 0`
	case nm.StateActive:
		where += ` AND m.archived_at IS NULL AND m.enabled = 1`
	case "all":
	default:
		where += ` AND m.archived_at IS NULL`
	}
	if filter.Query != "" {
		pattern := likePattern(filter.Query)
		where += ` AND (m.target LIKE ? ESCAPE '\' OR m.agent_name LIKE ? ESCAPE '\')`
		args = append(args, pattern, pattern)
	}
	if filter.Target != "" {
		where += ` AND m.target LIKE ? ESCAPE '\'`
		args = append(args, likePattern(filter.Target))
	}
	if filter.Protocol != "" {
		where += ` AND m.protocol = ?`
		args = append(args, filter.Protocol)
	}

	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM network_monitors m `+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.db.QueryContext(ctx,
		monitorSelect+where+` ORDER BY m.created_at DESC, m.monitor_id DESC LIMIT ? OFFSET ?`,
		append(args, limit, (page-1)*limit)...,
	)
	if err != nil {
		return nil, 0, err
	}
	monitors, err := scanMonitors(rows)
	if err != nil {
		return nil, 0, err
	}
	agents, err := s.AgentList(ctx, userID)
	if err != nil {
		return nil, 0, err
	}
	s.applyStates(monitors, agents)
	return monitors, total, nil
}

// NetworkLive returns a complete owner pulse without HTTP pagination. Readings
// come from the committed SQLite latest table, never the graph/sample cache.
func (s *Store) NetworkLive(ctx context.Context, userID string, agents []store.Agent) ([]nm.Monitor, error) {
	rows, err := s.db.QueryContext(ctx,
		monitorSelect+`WHERE m.user_id = ? AND m.archived_at IS NULL ORDER BY m.monitor_id`,
		userID,
	)
	if err != nil {
		return nil, err
	}
	monitors, err := scanMonitors(rows)
	if err != nil {
		return nil, err
	}
	s.applyStates(monitors, agents)
	return monitors, nil
}

// bumpSync marks the agent's configuration as changed so it is resent.
func bumpSync(ctx context.Context, tx *sql.Tx, userID, agentID string) error {
	_, err := tx.ExecContext(ctx, `
		INSERT INTO network_monitor_sync(agent_id, user_id, desired_generation)
		VALUES (?, ?, 1)
		ON CONFLICT(agent_id) DO UPDATE SET desired_generation = desired_generation + 1, error = ''`,
		agentID, userID,
	)
	return err
}

func insertMonitor(ctx context.Context, tx *sql.Tx, m nm.Monitor) error {
	_, err := tx.ExecContext(ctx, `
		INSERT INTO network_monitors(
			monitor_id, user_id, agent_id, agent_name, target, protocol, port,
			interval_seconds, dns_server, enabled, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		m.ID, m.UserID, m.AgentID, m.AgentName, m.Target, m.Protocol, m.Port,
		m.Interval, m.Server, m.Enabled, m.CreatedAt, m.UpdatedAt,
	)
	if err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed: network_monitors") {
		return nm.ErrConflict
	}
	return err
}

func (s *Store) NetworkCreate(ctx context.Context, userID string, agentIDs []string, c nm.Config, enabled bool) ([]nm.Monitor, error) {
	if err := c.Normalize(); err != nil {
		return nil, err
	}
	if len(agentIDs) == 0 || len(agentIDs) > nm.MaxAgentsPerCreate {
		return nil, nm.Invalid(fmt.Sprintf("select between 1 and %d agents", nm.MaxAgentsPerCreate))
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	created := make([]nm.Monitor, 0, len(agentIDs))
	seen := make(map[string]bool, len(agentIDs))
	for _, agentID := range agentIDs {
		if seen[agentID] {
			return nil, nm.Invalid("duplicate agent selection")
		}
		seen[agentID] = true

		m, err := s.createMonitor(ctx, tx, userID, agentID, c, enabled)
		if err != nil {
			return nil, err
		}
		created = append(created, *m)
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return created, nil
}

func (s *Store) createMonitor(ctx context.Context, tx *sql.Tx, userID, agentID string, c nm.Config, enabled bool) (*nm.Monitor, error) {
	agent, err := s.loadMonitorAgent(ctx, tx, agentID, userID)
	if err != nil {
		return nil, err
	}
	if err = nm.CheckCapabilities(agentID, c, agent.capabilities); err != nil {
		return nil, err
	}

	var count int
	if err = tx.QueryRowContext(ctx,
		`SELECT count(*) FROM network_monitors WHERE agent_id = ? AND archived_at IS NULL`,
		agentID,
	).Scan(&count); err != nil {
		return nil, err
	}
	if count >= nm.MaxMonitorsPerAgent {
		return nil, nm.Invalid("agent monitor limit reached")
	}

	c.ID = nm.ID()
	now := time.Now().UTC()
	m := &nm.Monitor{
		Config:    c,
		UserID:    userID,
		AgentID:   agentID,
		AgentName: agent.name,
		Enabled:   enabled,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err = insertMonitor(ctx, tx, *m); err != nil {
		return nil, err
	}
	if err = bumpSync(ctx, tx, userID, agentID); err != nil {
		return nil, err
	}
	return m, nil
}

// closeIncidents closes open incidents for a monitor, or for one rule's
// pairing with it when alertID is set, as "monitoring removed". Queued
// notifications are skipped so no false recovery is sent.
func closeIncidents(ctx context.Context, tx *sql.Tx, monitorID, alertID string) error {
	now := time.Now().UTC()
	match := `monitor_id = ?`
	args := []any{monitorID}
	if alertID != "" {
		match += ` AND alert_id = ?`
		args = append(args, alertID)
	}
	withTime := append([]any{now}, args...)

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO alert_history_events(event_id, history_id, kind, created_at)
		SELECT lower(hex(randomblob(16))), history_id, 'monitoring_removed', ?
		FROM   alert_history
		WHERE  `+match+` AND resolved_at IS NULL AND closed_at IS NULL`,
		withTime...,
	); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE alert_history
		SET    closed_at = ?, close_reason = 'monitoring_removed'
		WHERE  `+match+` AND resolved_at IS NULL AND closed_at IS NULL`,
		withTime...,
	); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE alert_history_events
		SET    status = 'skipped', completed_at = ?, error_message = 'Monitoring removed'
		WHERE  status = 'queued'
		AND    history_id IN (SELECT history_id FROM alert_history WHERE `+match+` AND closed_at IS NOT NULL)`,
		withTime...,
	); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `
		UPDATE alert_monitors
		SET    status = 'ok', history_id = '', error_message = ''
		WHERE  `+match,
		args...,
	)
	return err
}

func (s *Store) NetworkUpdate(ctx context.Context, userID, monitorID string, c nm.Config, enabled bool) (*nm.Monitor, error) {
	if err := c.Normalize(); err != nil {
		return nil, err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	m, err := scanMonitor(tx.QueryRowContext(ctx,
		monitorSelect+`WHERE m.user_id = ? AND m.monitor_id = ?`,
		userID, monitorID,
	))
	if err != nil {
		return nil, err
	}
	if m.ArchivedAt != nil {
		return nil, nm.ErrConflict
	}
	// Pausing remains possible even when an agent loses a capability.
	if enabled {
		agent, err := s.loadMonitorAgent(ctx, tx, m.AgentID, userID)
		if err != nil {
			return nil, err
		}
		if err = nm.CheckCapabilities(m.AgentID, c, agent.capabilities); err != nil {
			return nil, err
		}
	}

	identityChanged := m.Target != c.Target || m.Protocol != c.Protocol || m.Port != c.Port || m.Server != c.Server
	if identityChanged {
		err = replaceMonitor(ctx, tx, m, c, enabled)
	} else {
		err = editMonitor(ctx, tx, monitorID, c.Interval, enabled)
	}
	if err != nil {
		return nil, err
	}
	if err = bumpSync(ctx, tx, userID, m.AgentID); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}

	updated, err := s.NetworkGet(ctx, userID, m.ID)
	if err != nil {
		return nil, err
	}
	updated.ReplacesID = m.ReplacesID
	return updated, nil
}

// replaceMonitor archives m and creates a new identity for the changed
// configuration, transferring alert membership. m is updated in place.
func replaceMonitor(ctx context.Context, tx *sql.Tx, m *nm.Monitor, c nm.Config, enabled bool) error {
	oldID := m.ID
	newID := nm.ID()
	now := time.Now().UTC()

	if err := closeIncidents(ctx, tx, oldID, ""); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE network_monitors
		SET    archived_at = ?, enabled = 0, replacement_id = ?, updated_at = ?
		WHERE  monitor_id = ?`,
		now, newID, now, oldID,
	); err != nil {
		return err
	}

	m.Config = c
	m.ID = newID
	m.CreatedAt = now
	m.UpdatedAt = now
	m.Enabled = enabled
	m.Latest = nil
	m.ReplacesID = oldID
	if err := insertMonitor(ctx, tx, *m); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `UPDATE alert_monitors SET monitor_id = ? WHERE monitor_id = ?`, newID, oldID)
	return err
}

// editMonitor changes settings that keep the monitor's identity and history.
func editMonitor(ctx context.Context, tx *sql.Tx, monitorID string, interval uint16, enabled bool) error {
	if !enabled {
		if err := closeIncidents(ctx, tx, monitorID, ""); err != nil {
			return err
		}
	}
	_, err := tx.ExecContext(ctx, `
		UPDATE network_monitors
		SET    interval_seconds = ?, enabled = ?, updated_at = ?
		WHERE  monitor_id = ?`,
		interval, enabled, time.Now().UTC(), monitorID,
	)
	return err
}

func (s *Store) NetworkArchive(ctx context.Context, userID, monitorID string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var agentID string
	if err = tx.QueryRowContext(ctx,
		`SELECT agent_id FROM network_monitors WHERE monitor_id = ? AND user_id = ?`,
		monitorID, userID,
	).Scan(&agentID); err != nil {
		return err
	}
	if err = closeIncidents(ctx, tx, monitorID, ""); err != nil {
		return err
	}
	now := time.Now().UTC()
	if _, err = tx.ExecContext(ctx, `
		UPDATE network_monitors
		SET    archived_at = COALESCE(archived_at, ?), enabled = 0, updated_at = ?
		WHERE  monitor_id = ?`,
		now, now, monitorID,
	); err != nil {
		return err
	}
	if err = bumpSync(ctx, tx, userID, agentID); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) NetworkConfigs(ctx context.Context, userID, agentID string) ([]nm.Config, nm.Sync, error) {
	var state nm.Sync
	var lastAttempt, lastAck sql.NullTime
	err := s.db.QueryRowContext(ctx, `
		SELECT desired_generation, ack_generation, error, last_attempt, last_ack
		FROM   network_monitor_sync
		WHERE  user_id = ? AND agent_id = ?`,
		userID, agentID,
	).Scan(&state.Desired, &state.Ack, &state.Error, &lastAttempt, &lastAck)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, state, err
	}
	if lastAttempt.Valid {
		state.LastAttempt = &lastAttempt.Time
	}
	if lastAck.Valid {
		state.LastAck = &lastAck.Time
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT monitor_id, target, protocol, port, interval_seconds, dns_server
		FROM   network_monitors
		WHERE  user_id = ? AND agent_id = ? AND enabled = 1 AND archived_at IS NULL
		ORDER  BY monitor_id`,
		userID, agentID,
	)
	if err != nil {
		return nil, state, err
	}
	defer rows.Close()

	configs := []nm.Config{}
	for rows.Next() {
		var c nm.Config
		if err = rows.Scan(&c.ID, &c.Target, &c.Protocol, &c.Port, &c.Interval, &c.Server); err != nil {
			return nil, state, err
		}
		configs = append(configs, c)
	}
	return configs, state, rows.Err()
}

func (s *Store) NetworkAck(ctx context.Context, userID, agentID string, generation int64, message string) error {
	now := time.Now().UTC()
	if message != "" {
		// The agent's error text is not shown; generic text avoids leaking it to the UI.
		_, err := s.db.ExecContext(ctx, `
			UPDATE network_monitor_sync
			SET    last_attempt = ?, error = ?
			WHERE  user_id = ? AND agent_id = ? AND desired_generation = ?`,
			now, "Agent synchronization failed; retry pending", userID, agentID, generation,
		)
		return err
	}
	_, err := s.db.ExecContext(ctx, `
		UPDATE network_monitor_sync
		SET    ack_generation = MAX(ack_generation, ?), last_attempt = ?, last_ack = ?, error = ''
		WHERE  user_id = ? AND agent_id = ?`,
		generation, now, now, userID, agentID,
	)
	return err
}
