package sqlite

import "fmt"

// networkColumns are added to existing tables when missing, in order.
var networkColumns = []struct{ table, name, definition string }{
	{"agents", "agent_version", "TEXT"},
	{"agents", "protocol_version", "TEXT"},
	{"agents", "agent_version_source", "TEXT NOT NULL DEFAULT ''"},
	{"agents", "agent_version_observed_at", "DATETIME"},
	{"agents", "reported_capabilities_json", "TEXT"},
	{"agents", "capabilities_reported_at", "DATETIME"},
	{"alert_history", "subject_kind", "TEXT NOT NULL DEFAULT 'agent'"},
	{"alert_history", "monitor_id", "TEXT NOT NULL DEFAULT ''"},
	{"alert_history", "monitor_snapshot", "TEXT NOT NULL DEFAULT '{}'"},
}

var networkSchema = []string{
	`CREATE TABLE IF NOT EXISTS network_monitors (
		monitor_id       TEXT PRIMARY KEY,
		user_id          TEXT NOT NULL REFERENCES users(user_id) ON DELETE CASCADE,
		agent_id         TEXT NOT NULL,
		agent_name       TEXT NOT NULL,
		target           TEXT NOT NULL,
		protocol         TEXT NOT NULL,
		port             INTEGER NOT NULL DEFAULT 0,
		interval_seconds INTEGER NOT NULL,
		dns_server       TEXT NOT NULL DEFAULT '',
		enabled          BOOLEAN NOT NULL,
		created_at       DATETIME NOT NULL,
		updated_at       DATETIME NOT NULL,
		archived_at      DATETIME,
		replacement_id   TEXT NOT NULL DEFAULT ''
	)`,
	`CREATE UNIQUE INDEX IF NOT EXISTS network_monitor_config
		ON network_monitors(agent_id, protocol, target, port, dns_server)
		WHERE archived_at IS NULL`,
	`CREATE INDEX IF NOT EXISTS network_monitor_owner
		ON network_monitors(user_id, archived_at, agent_id, created_at DESC, monitor_id DESC)`,
	`CREATE TABLE IF NOT EXISTS network_monitor_latest (
		monitor_id     TEXT PRIMARY KEY REFERENCES network_monitors(monitor_id) ON DELETE CASCADE,
		payload        TEXT NOT NULL,
		measurement_at INTEGER NOT NULL
	)`,
	`CREATE TABLE IF NOT EXISTS network_monitor_sync (
		agent_id           TEXT PRIMARY KEY,
		user_id            TEXT NOT NULL REFERENCES users(user_id) ON DELETE CASCADE,
		desired_generation INTEGER NOT NULL DEFAULT 1,
		ack_generation     INTEGER NOT NULL DEFAULT 0,
		last_attempt       DATETIME,
		last_ack           DATETIME,
		error              TEXT NOT NULL DEFAULT ''
	)`,
	`CREATE TABLE IF NOT EXISTS network_monitor_ingestion (
		batch_id TEXT PRIMARY KEY,
		user_id  TEXT NOT NULL REFERENCES users(user_id) ON DELETE CASCADE,
		agent_id TEXT NOT NULL,
		payload  BLOB NOT NULL,
		complete BOOLEAN NOT NULL DEFAULT 0
	)`,
	`CREATE INDEX IF NOT EXISTS network_monitor_pending
		ON network_monitor_ingestion(complete, agent_id)`,
	`CREATE TABLE IF NOT EXISTS alert_monitors (
		alert_id      TEXT NOT NULL REFERENCES alerts(alert_id) ON DELETE CASCADE,
		monitor_id    TEXT NOT NULL REFERENCES network_monitors(monitor_id),
		status        TEXT NOT NULL DEFAULT 'ok',
		last_fired_at DATETIME,
		history_id    TEXT NOT NULL DEFAULT '',
		error_message TEXT NOT NULL DEFAULT '',
		PRIMARY KEY (alert_id, monitor_id)
	)`,
	`CREATE UNIQUE INDEX IF NOT EXISTS network_incident_open
		ON alert_history(alert_id, agent_id, monitor_id)
		WHERE monitor_id != '' AND resolved_at IS NULL AND closed_at IS NULL`,
	// Deleting a node archives its monitors and forgets its sync state.
	`CREATE TRIGGER IF NOT EXISTS network_agent_delete BEFORE DELETE ON agents
	BEGIN
		UPDATE network_monitors
		SET    archived_at = strftime('%Y-%m-%d %H:%M:%f', 'now'), enabled = 0
		WHERE  agent_id = OLD.agent_id AND archived_at IS NULL;
		DELETE FROM network_monitor_sync WHERE agent_id = OLD.agent_id;
	END`,
}

func (s *Store) migrateNetworkMonitors() error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	for _, column := range networkColumns {
		var exists int
		if err = tx.QueryRow(
			`SELECT count(*) FROM pragma_table_info(?) WHERE name = ?`,
			column.table, column.name,
		).Scan(&exists); err != nil {
			return err
		}
		if exists > 0 {
			continue
		}
		if _, err = tx.Exec(fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", column.table, column.name, column.definition)); err != nil {
			return err
		}
	}
	for _, statement := range networkSchema {
		if _, err = tx.Exec(statement); err != nil {
			return fmt.Errorf("network migration: %w", err)
		}
	}
	return tx.Commit()
}
