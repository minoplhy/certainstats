package sqlite

import "fmt"

// The rebuild and backfill commit together. A failed copy leaves the original
// table intact, and the snapshot column is the durable migration marker.
func (s *Store) migrateIncidentHistory() error {
	var migrated int
	if err := s.db.QueryRow(`SELECT count(*) FROM pragma_table_info('alert_history') WHERE name='trigger_snapshot'`).Scan(&migrated); err != nil {
		return err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if migrated == 0 {
		statements := []string{
			`CREATE TABLE alert_history_v2 (
    history_id TEXT PRIMARY KEY, alert_id TEXT NOT NULL, agent_id TEXT NOT NULL,
    user_id TEXT NOT NULL REFERENCES users(user_id) ON DELETE CASCADE,
    triggered_at DATETIME NOT NULL, resolved_at DATETIME, closed_at DATETIME,
    close_reason TEXT NOT NULL DEFAULT '', trigger_value REAL NOT NULL,
    notified_status TEXT NOT NULL, target_id TEXT NOT NULL DEFAULT '', target_name TEXT NOT NULL DEFAULT '',
    agent_nickname TEXT NOT NULL DEFAULT '', alert_nickname TEXT NOT NULL DEFAULT '', error_message TEXT NOT NULL DEFAULT '',
    trigger_snapshot TEXT NOT NULL DEFAULT '{}', legacy BOOLEAN NOT NULL DEFAULT 0)`,
			`INSERT INTO alert_history_v2 (history_id,alert_id,agent_id,user_id,triggered_at,resolved_at,trigger_value,notified_status,target_id,target_name,agent_nickname,alert_nickname,error_message,trigger_snapshot,legacy)
    SELECT h.history_id,h.alert_id,h.agent_id,h.user_id,h.triggered_at,h.resolved_at,h.trigger_value,h.notified_status,
    COALESCE(h.target_id,''),COALESCE(h.target_name,''),COALESCE(h.agent_nickname,''),COALESCE(h.alert_nickname,''),COALESCE(h.error_message,''),COALESCE(a.trigger_config,'{}'),1
    FROM alert_history h LEFT JOIN alerts a ON a.alert_id=h.alert_id`,
			`DROP TABLE alert_history`, `ALTER TABLE alert_history_v2 RENAME TO alert_history`,
		}
		for _, q := range statements {
			if _, err = tx.Exec(q); err != nil {
				return fmt.Errorf("incident history migration: %w", err)
			}
		}
	}
	statements := []string{
		`CREATE INDEX IF NOT EXISTS idx_alert_history_user_triggered ON alert_history(user_id,triggered_at DESC,history_id DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_alert_history_user_agent ON alert_history(user_id,agent_id,triggered_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_alert_history_alert ON alert_history(alert_id)`,
		`CREATE INDEX IF NOT EXISTS idx_alert_history_agent ON alert_history(agent_id)`,
		`CREATE INDEX IF NOT EXISTS idx_alert_history_triggered_desc ON alert_history(triggered_at DESC)`,
		`CREATE TABLE IF NOT EXISTS alert_history_events (
   event_id TEXT PRIMARY KEY, history_id TEXT NOT NULL REFERENCES alert_history(history_id) ON DELETE CASCADE,
   kind TEXT NOT NULL, phase TEXT NOT NULL DEFAULT '', status TEXT NOT NULL DEFAULT '',
   created_at DATETIME NOT NULL, started_at DATETIME, completed_at DATETIME,
   error_message TEXT NOT NULL DEFAULT '', retry_of TEXT NOT NULL DEFAULT '')`,
		`CREATE INDEX IF NOT EXISTS idx_incident_events ON alert_history_events(history_id,created_at,event_id)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_incident_pending ON alert_history_events(history_id,phase) WHERE kind='notification' AND status IN ('queued','pending')`,
		`CREATE INDEX IF NOT EXISTS idx_incident_delivery_status ON alert_history_events(status,created_at) WHERE kind='notification'`,
	}
	if migrated == 0 {
		statements = append(statements,
			`INSERT INTO alert_history_events(event_id,history_id,kind,created_at) SELECT history_id||'_firing',history_id,'firing',triggered_at FROM alert_history`,
			`INSERT INTO alert_history_events(event_id,history_id,kind,created_at) SELECT history_id||'_resolved',history_id,'resolved',resolved_at FROM alert_history WHERE resolved_at IS NOT NULL`)
	}
	// These triggers also cover deletion initiated by account cleanup. Names and
	// original IDs remain in the archive; no fake recovery is generated.
	for _, entity := range []struct{ table, column string }{{"alerts", "alert_id"}, {"agents", "agent_id"}} {
		statements = append(statements, fmt.Sprintf(`CREATE TRIGGER IF NOT EXISTS incident_remove_%s BEFORE DELETE ON %s BEGIN
   INSERT INTO alert_history_events(event_id,history_id,kind,created_at)
    SELECT lower(hex(randomblob(16))),history_id,'monitoring_removed',strftime('%%Y-%%m-%%d %%H:%%M:%%f','now') FROM alert_history
    WHERE %s=OLD.%s AND resolved_at IS NULL AND closed_at IS NULL;
   UPDATE alert_history SET closed_at=strftime('%%Y-%%m-%%d %%H:%%M:%%f','now'),close_reason='monitoring_removed'
    WHERE %s=OLD.%s AND resolved_at IS NULL AND closed_at IS NULL;
   UPDATE alert_history_events SET status='skipped',completed_at=strftime('%%Y-%%m-%%d %%H:%%M:%%f','now'),error_message='Monitoring removed'
    WHERE status='queued' AND history_id IN (SELECT history_id FROM alert_history WHERE %s=OLD.%s);
  END`, entity.table, entity.table, entity.column, entity.column, entity.column, entity.column, entity.column, entity.column))
	}
	for _, q := range statements {
		if _, err = tx.Exec(q); err != nil {
			return fmt.Errorf("incident events migration: %w", err)
		}
	}
	// A previous process may have delivered these messages before crashing. Never
	// automatically send them again: only an explicit retry can accept that risk.
	if _, err = tx.Exec(`UPDATE alert_history_events SET status='unknown',completed_at=strftime('%Y-%m-%d %H:%M:%f','now'),error_message='Delivery interrupted by restart; outcome unknown' WHERE status IN ('queued','pending')`); err != nil {
		return err
	}
	if _, err = tx.Exec(`UPDATE alert_history SET notified_status='unknown',error_message='Delivery interrupted by restart; outcome unknown' WHERE notified_status='pending'`); err != nil {
		return err
	}
	return tx.Commit()
}
