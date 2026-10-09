package sqlite

import (
	a "certainstats/internal/base/alert"
	"certainstats/internal/store"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

var ErrAttemptUnavailable = store.ErrAlertAttemptUnavailable

func triggerSnapshot(t a.Trigger) string { b, _ := json.Marshal(t); return string(b) }

const incidentSelect = `SELECT h.history_id,h.alert_id,h.agent_id,h.user_id,h.agent_nickname,h.alert_nickname,
 h.triggered_at,h.resolved_at,h.closed_at,h.close_reason,h.trigger_value,h.notified_status,h.trigger_snapshot,
 h.target_id,h.target_name,h.error_message,h.legacy,h.subject_kind,h.monitor_id,h.monitor_snapshot,
 EXISTS(SELECT 1 FROM alerts a JOIN agents n ON n.agent_id=h.agent_id WHERE a.alert_id=h.alert_id AND a.user_id=h.user_id AND n.user_id=h.user_id) AND h.closed_at IS NULL AND (h.monitor_id='' OR EXISTS(SELECT 1 FROM network_monitors m JOIN alert_monitors am ON am.monitor_id=m.monitor_id AND am.alert_id=h.alert_id WHERE m.monitor_id=h.monitor_id AND m.enabled=1 AND m.archived_at IS NULL AND EXISTS(SELECT 1 FROM alerts r WHERE r.alert_id=h.alert_id AND r.enabled=1))),
 EXISTS(SELECT 1 FROM alerts a JOIN agents n ON n.agent_id=h.agent_id WHERE a.alert_id=h.alert_id AND a.user_id=h.user_id AND n.user_id=h.user_id
 AND ((h.target_id!='' AND EXISTS(SELECT 1 FROM alert_targets t WHERE t.target_id=h.target_id AND t.user_id=h.user_id))
 OR (h.target_id='' AND json_extract(a.action_config,'$.type')!='preset' AND COALESCE(json_extract(a.action_config,'$.destination'),'')!=''))) AND h.closed_at IS NULL AND (h.monitor_id='' OR EXISTS(SELECT 1 FROM network_monitors m JOIN alert_monitors am ON am.monitor_id=m.monitor_id AND am.alert_id=h.alert_id WHERE m.monitor_id=h.monitor_id AND m.enabled=1 AND m.archived_at IS NULL AND EXISTS(SELECT 1 FROM alerts r WHERE r.alert_id=h.alert_id AND r.enabled=1))),
 COALESCE((SELECT e.status FROM alert_history_events e WHERE e.history_id=h.history_id AND e.phase='firing' ORDER BY e.created_at DESC,e.event_id DESC LIMIT 1),h.notified_status),
 COALESCE((SELECT e.status FROM alert_history_events e WHERE e.history_id=h.history_id AND e.phase='recovery' ORDER BY e.created_at DESC,e.event_id DESC LIMIT 1),'')
 FROM alert_history h `

type scanner interface{ Scan(...any) error }

func scanIncident(row scanner) (*a.AlertHistory, error) {
	h := new(a.AlertHistory)
	var resolved, closed sql.NullTime
	var snapshot, monitor string
	err := row.Scan(&h.HistoryID, &h.AlertID, &h.AgentID, &h.UserID, &h.AgentNickname, &h.AlertNickname, &h.TriggeredAt, &resolved, &closed, &h.CloseReason, &h.TriggerValue, &h.NotifiedStatus, &snapshot, &h.TargetID, &h.TargetName, &h.ErrorMessage, &h.Legacy, &h.SubjectKind, &h.MonitorID, &monitor, &h.MonitoringAvailable, &h.RetryAvailable, &h.FiringDelivery, &h.RecoveryDelivery)
	if err != nil {
		return nil, err
	}
	if resolved.Valid {
		h.ResolvedAt = &resolved.Time
	}
	if closed.Valid {
		h.ClosedAt = &closed.Time
	}
	_ = json.Unmarshal([]byte(snapshot), &h.Trigger)
	_ = json.Unmarshal([]byte(monitor), &h.Monitor)
	return h, nil
}
func (s *Store) AlertHistoryGetByID(ctx context.Context, id, user string) (*a.AlertHistory, error) {
	h, e := scanIncident(s.db.QueryRowContext(ctx, incidentSelect+`WHERE h.history_id=? AND h.user_id=?`, id, user))
	if e != nil {
		return nil, e
	}
	if e = s.incidentCapabilities(ctx, s.db, h); e != nil {
		return nil, e
	}
	return h, nil
}
func (s *Store) AlertHistoryListPaginated(ctx context.Context, user string, page, limit int, q, status string) ([]a.AlertHistory, int, error) {
	return s.AlertHistoryListFiltered(ctx, user, page, limit, q, status, "")
}
func (s *Store) AlertHistoryListFiltered(ctx context.Context, user string, page, limit int, q, status, node string) ([]a.AlertHistory, int, error) {
	if page < 1 {
		page = 1
	}
	if limit < 1 {
		limit = 25
	}
	if limit > 100 {
		limit = 100
	}
	clauses := []string{"h.user_id=?"}
	args := []any{user}
	switch status {
	case "firing":
		clauses = append(clauses, "h.resolved_at IS NULL AND h.closed_at IS NULL")
	case "resolved":
		clauses = append(clauses, "h.resolved_at IS NOT NULL")
	case "closed":
		clauses = append(clauses, "h.closed_at IS NOT NULL")
	}
	if node != "" {
		clauses = append(clauses, "h.agent_id=?")
		args = append(args, node)
	}
	if q != "" {
		clauses = append(clauses, `(h.agent_nickname LIKE ? ESCAPE '\' OR h.agent_id LIKE ? ESCAPE '\' OR h.alert_nickname LIKE ? ESCAPE '\' OR json_extract(h.monitor_snapshot,'$.target') LIKE ? ESCAPE '\')`)
		q = "%" + strings.NewReplacer(`\`, `\\`, "%", `\%`, "_", `\_`).Replace(q) + "%"
		args = append(args, q, q, q, q)
	}
	where := "WHERE " + strings.Join(clauses, " AND ")
	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM alert_history h `+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.db.QueryContext(ctx, incidentSelect+where+` ORDER BY h.triggered_at DESC,h.history_id DESC LIMIT ? OFFSET ?`, append(args, limit, (page-1)*limit)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []a.AlertHistory{}
	for rows.Next() {
		h, err := scanIncident(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, *h)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	rows.Close()
	for i := range out {
		if err := s.incidentCapabilities(ctx, s.db, &out[i]); err != nil {
			return nil, 0, err
		}
	}
	return out, total, nil
}
func (s *Store) AlertHistorySummary(ctx context.Context, user string) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM alert_history WHERE user_id=? AND resolved_at IS NULL AND closed_at IS NULL`, user).Scan(&n)
	return n, err
}

const eventSelect = `SELECT event_id,history_id,kind,phase,status,created_at,started_at,completed_at,error_message,retry_of FROM alert_history_events `

func scanEvent(row scanner) (a.HistoryEvent, error) {
	var e a.HistoryEvent
	var start, end sql.NullTime
	err := row.Scan(&e.EventID, &e.HistoryID, &e.Kind, &e.Phase, &e.Status, &e.CreatedAt, &start, &end, &e.ErrorMessage, &e.RetryOf)
	if start.Valid {
		e.StartedAt = &start.Time
	}
	if end.Valid {
		e.CompletedAt = &end.Time
	}
	return e, err
}
func (s *Store) AlertHistoryEvents(ctx context.Context, user, id string, page, limit int) ([]a.HistoryEvent, int, error) {
	h, err := s.AlertHistoryGetByID(ctx, id, user)
	if err != nil {
		return nil, 0, err
	}
	if page < 1 {
		page = 1
	}
	if limit < 1 {
		limit = 50
	}
	if limit > 100 {
		limit = 100
	}
	var total int
	if err = s.db.QueryRowContext(ctx, `SELECT count(*) FROM alert_history_events WHERE history_id=?`, id).Scan(&total); err != nil {
		return nil, 0, err
	}
	// Only the latest attempt in a phase is retryable, regardless of event pagination.
	latest := map[string]string{}
	for _, phase := range []string{"firing", "recovery"} {
		var eventID string
		_ = s.db.QueryRowContext(ctx, `SELECT event_id FROM alert_history_events WHERE history_id=? AND phase=? ORDER BY created_at DESC,event_id DESC LIMIT 1`, id, phase).Scan(&eventID)
		latest[phase] = eventID
	}
	rows, err := s.db.QueryContext(ctx, eventSelect+`WHERE history_id=? ORDER BY created_at,event_id LIMIT ? OFFSET ?`, id, limit, (page-1)*limit)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []a.HistoryEvent{}
	for rows.Next() {
		e, err := scanEvent(rows)
		if err != nil {
			return nil, 0, err
		}
		e.RetryAvailable = h.RetryAvailable && latest[e.Phase] == e.EventID && (e.Status == "failed" || e.Status == "unknown") && (e.Phase == "recovery" || h.ResolvedAt == nil)
		out = append(out, e)
	}
	return out, total, rows.Err()
}
func (s *Store) AlertAttemptQueue(ctx context.Context, user, id, phase, retryOf string) (a.HistoryEvent, error) {
	e := a.HistoryEvent{}
	if phase != "firing" && phase != "recovery" {
		return e, ErrAttemptUnavailable
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return e, err
	}
	defer tx.Rollback()
	h, err := scanIncident(tx.QueryRowContext(ctx, incidentSelect+`WHERE h.history_id=? AND h.user_id=?`, id, user))
	if err != nil {
		return e, err
	}
	if err = s.incidentCapabilities(ctx, tx, h); err != nil {
		return e, err
	}
	if !h.MonitoringAvailable || ((retryOf != "" || h.Legacy) && !h.RetryAvailable) || (phase == "firing" && h.ResolvedAt != nil) || (phase == "recovery" && h.ResolvedAt == nil) {
		return e, ErrAttemptUnavailable
	}
	previous, prevErr := scanEvent(tx.QueryRowContext(ctx, eventSelect+`WHERE history_id=? AND phase=? ORDER BY created_at DESC,event_id DESC LIMIT 1`, id, phase))
	if prevErr != nil && prevErr != sql.ErrNoRows {
		return e, prevErr
	}
	if prevErr == nil {
		if retryOf != previous.EventID || (previous.Status != "failed" && previous.Status != "unknown") {
			return e, ErrAttemptUnavailable
		}
	} else if retryOf != "" {
		return e, ErrAttemptUnavailable
	}
	e = a.HistoryEvent{HistoryID: id, Kind: "notification", Phase: phase, Status: "queued", CreatedAt: time.Now().UTC(), RetryOf: retryOf}
	// SQLite creates the ID without depending on a process-local counter.
	if err = tx.QueryRowContext(ctx, `SELECT lower(hex(randomblob(16)))`).Scan(&e.EventID); err != nil {
		return e, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO alert_history_events(event_id,history_id,kind,phase,status,created_at,retry_of) VALUES (?,?,'notification',?,'queued',?,?)`, e.EventID, id, phase, e.CreatedAt, retryOf)
	if err != nil {
		return e, err
	}
	if phase == "firing" {
		if _, err = tx.ExecContext(ctx, `UPDATE alert_history SET notified_status='pending',error_message='' WHERE history_id=?`, id); err != nil {
			return e, err
		}
	}
	return e, tx.Commit()
}
func (s *Store) AlertAttemptStart(ctx context.Context, id string) (*a.AlertHistory, a.HistoryEvent, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, a.HistoryEvent{}, err
	}
	defer tx.Rollback()
	e, err := scanEvent(tx.QueryRowContext(ctx, eventSelect+`WHERE event_id=?`, id))
	if err != nil {
		return nil, e, err
	}
	h, err := scanIncident(tx.QueryRowContext(ctx, incidentSelect+`WHERE h.history_id=?`, e.HistoryID))
	if err != nil {
		return nil, e, err
	}
	if e.Status != "queued" {
		return nil, e, ErrAttemptUnavailable
	}
	if err = s.incidentCapabilities(ctx, tx, h); err != nil {
		return nil, e, err
	}
	if !h.MonitoringAvailable || (e.Phase == "firing" && h.ResolvedAt != nil) {
		_, err = tx.ExecContext(ctx, `UPDATE alert_history_events SET status='skipped',completed_at=?,error_message='Incident is no longer eligible' WHERE event_id=? AND status='queued'`, time.Now().UTC(), id)
		if err != nil {
			return nil, e, err
		}
		if err = tx.Commit(); err != nil {
			return nil, e, err
		}
		return nil, e, ErrAttemptUnavailable
	}
	now := time.Now().UTC()
	res, err := tx.ExecContext(ctx, `UPDATE alert_history_events SET status='pending',started_at=? WHERE event_id=? AND status='queued'`, now, id)
	if err != nil {
		return nil, e, err
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return nil, e, ErrAttemptUnavailable
	}
	e.Status = "pending"
	e.StartedAt = &now
	return h, e, tx.Commit()
}
func (s *Store) AlertAttemptComplete(ctx context.Context, id, status, message string) error {
	if status != "success" && status != "failed" && status != "skipped" {
		return fmt.Errorf("invalid completion status")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	e, err := scanEvent(tx.QueryRowContext(ctx, eventSelect+`WHERE event_id=?`, id))
	if err != nil {
		return err
	}
	if e.Status != "pending" {
		return ErrAttemptUnavailable
	}
	if _, err = tx.ExecContext(ctx, `UPDATE alert_history_events SET status=?,completed_at=?,error_message=? WHERE event_id=? AND status='pending'`, status, time.Now().UTC(), message, id); err != nil {
		return err
	}
	if e.Phase == "firing" {
		if _, err = tx.ExecContext(ctx, `UPDATE alert_history SET notified_status=?,error_message=? WHERE history_id=?`, status, message, e.HistoryID); err != nil {
			return err
		}
		agentStatus := "firing"
		if status == "failed" {
			agentStatus = "failed"
		}
		if status != "skipped" {
			if _, err = tx.ExecContext(ctx, `UPDATE alert_monitors SET status=?,error_message=? WHERE EXISTS(SELECT 1 FROM alert_history h WHERE h.history_id=? AND h.alert_id=alert_monitors.alert_id AND h.monitor_id=alert_monitors.monitor_id AND h.history_id=alert_monitors.history_id AND h.resolved_at IS NULL AND h.closed_at IS NULL)`, agentStatus, message, e.HistoryID); err != nil {
				return err
			}

			if _, err = tx.ExecContext(ctx, `UPDATE alert_agents SET status=?,error_message=? WHERE EXISTS(
   SELECT 1 FROM alert_history h WHERE h.history_id=? AND h.alert_id=alert_agents.alert_id AND h.agent_id=alert_agents.agent_id
   AND h.monitor_id='' AND h.resolved_at IS NULL AND h.closed_at IS NULL AND h.triggered_at=alert_agents.last_fired_at)`, agentStatus, message, e.HistoryID); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}
func (s *Store) AlertAttemptsFailed(ctx context.Context) ([]a.HistoryEvent, error) {
	rows, err := s.db.QueryContext(ctx, eventSelect+`WHERE kind='notification' AND status='failed' AND created_at>? AND
 NOT EXISTS(SELECT 1 FROM alert_history_events newer WHERE newer.history_id=alert_history_events.history_id AND newer.phase=alert_history_events.phase
 AND (newer.created_at>alert_history_events.created_at OR (newer.created_at=alert_history_events.created_at AND newer.event_id>alert_history_events.event_id)))
 ORDER BY created_at`, time.Now().Add(-24*time.Hour))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []a.HistoryEvent{}
	for rows.Next() {
		e, err := scanEvent(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// Compatibility helpers for callers that still consume firing delivery status.
func (s *Store) AlertHistoryUpdateStatus(ctx context.Context, id, status, message string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE alert_history SET notified_status=?,error_message=? WHERE history_id=?`, status, message, id)
	return err
}
func (s *Store) AlertAgentUpdateStatus(ctx context.Context, id, node, status, message string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE alert_agents SET status=?,error_message=? WHERE alert_id=? AND agent_id=?`, status, message, id, node)
	return err
}
func (s *Store) AlertHistoryGetFailed(ctx context.Context) ([]*a.AlertHistory, error) {
	rows, err := s.db.QueryContext(ctx, incidentSelect+`WHERE h.notified_status='failed' AND h.triggered_at>?`, time.Now().Add(-24*time.Hour))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*a.AlertHistory{}
	for rows.Next() {
		h, err := scanIncident(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

func (s *Store) AlertHistoryActive(ctx context.Context, user, id, node string) (*a.AlertHistory, error) {
	return scanIncident(s.db.QueryRowContext(ctx, incidentSelect+`WHERE h.user_id=? AND h.alert_id=? AND h.agent_id=? AND h.resolved_at IS NULL AND h.closed_at IS NULL ORDER BY h.triggered_at DESC,h.history_id DESC LIMIT 1`, user, id, node))
}
func (s *Store) AlertHistoryNodes(ctx context.Context, user string) ([]a.HistoryNode, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT h.agent_id,COALESCE(NULLIF(n.nickname,''),NULLIF(h.agent_nickname,''),h.agent_id) FROM alert_history h LEFT JOIN agents n ON n.agent_id=h.agent_id WHERE h.user_id=? AND h.history_id=(SELECT h2.history_id FROM alert_history h2 WHERE h2.user_id=h.user_id AND h2.agent_id=h.agent_id ORDER BY h2.triggered_at DESC,h2.history_id DESC LIMIT 1) ORDER BY 2,1`, user)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []a.HistoryNode{}
	for rows.Next() {
		var n a.HistoryNode
		if err = rows.Scan(&n.AgentID, &n.Nickname); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

func (s *Store) AlertAttemptIncident(ctx context.Context, id string) (*a.AlertHistory, error) {
	return scanIncident(s.db.QueryRowContext(ctx, incidentSelect+`WHERE h.history_id=(SELECT history_id FROM alert_history_events WHERE event_id=?)`, id))
}

// Close before the delete so application timestamps retain subsecond ordering.
// Database triggers remain a fallback for cascading account deletion.
func closeIncidentMonitoring(ctx context.Context, tx *sql.Tx, column, id string) error {
	now := time.Now().UTC()
	if _, err := tx.ExecContext(ctx, `INSERT INTO alert_history_events(event_id,history_id,kind,created_at) SELECT lower(hex(randomblob(16))),history_id,'monitoring_removed',? FROM alert_history WHERE `+column+`=? AND resolved_at IS NULL AND closed_at IS NULL`, now, id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE alert_history SET closed_at=?,close_reason='monitoring_removed' WHERE `+column+`=? AND resolved_at IS NULL AND closed_at IS NULL`, now, id); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `UPDATE alert_history_events SET status='skipped',completed_at=?,error_message='Monitoring removed' WHERE status='queued' AND history_id IN (SELECT history_id FROM alert_history WHERE `+column+`=?)`, now, id)
	return err
}
