package sqlite

import (
	log "certainstats/internal/logger"
)

func (s *Store) migrate() error {
	// Each statement is executed independently so a single failure cannot
	// prevent subsequent CREATE TABLE or ALTER TABLE steps from running.
	schemas := []string{
		`CREATE TABLE IF NOT EXISTS users (
			user_id       TEXT PRIMARY KEY,
			username      TEXT UNIQUE NOT NULL,
			password_hash TEXT NOT NULL,
			is_admin      BOOLEAN NOT NULL DEFAULT 0,
			created_at    DATETIME NOT NULL
		)`,

		`CREATE TABLE IF NOT EXISTS agents (
			agent_id      TEXT PRIMARY KEY,
			user_id       TEXT NOT NULL REFERENCES users(user_id) ON DELETE CASCADE,
			token         TEXT UNIQUE NOT NULL,
			agent_type    TEXT NOT NULL DEFAULT 'ltstats',
			nickname      TEXT NOT NULL DEFAULT '',
			last_seen     DATETIME,
			is_online     BOOLEAN NOT NULL DEFAULT 0,
			uptime        INTEGER  DEFAULT 0,
			linux_version TEXT     DEFAULT 'Pending connection...',
			cpu_model     TEXT     DEFAULT 'Waiting for data...',
			cpu_cores     INTEGER  DEFAULT 0,
			ram_size      BIGINT   DEFAULT 0,
			swap_size     BIGINT   DEFAULT 0,
			disk_size     BIGINT   DEFAULT 0,
			total_rx_bytes         BIGINT DEFAULT 0,
			total_tx_bytes         BIGINT DEFAULT 0,
			total_disk_read_bytes  BIGINT DEFAULT 0,
			total_disk_write_bytes BIGINT DEFAULT 0,
			note                   TEXT DEFAULT ''
		)`,

		`CREATE TABLE IF NOT EXISTS agent_disk_odometers (
			agent_id    TEXT NOT NULL REFERENCES agents(agent_id) ON DELETE CASCADE,
			path        TEXT NOT NULL,
			read_bytes  BIGINT DEFAULT 0,
			write_bytes BIGINT DEFAULT 0,
			PRIMARY KEY (agent_id, path)
		)`,

		`CREATE INDEX IF NOT EXISTS idx_agents_user  ON agents(user_id)`,
		`CREATE INDEX IF NOT EXISTS idx_agents_token ON agents(token)`,

		`CREATE TABLE IF NOT EXISTS sessions (
			session_token     TEXT PRIMARY KEY,
			user_id           TEXT NOT NULL REFERENCES users(user_id) ON DELETE CASCADE,
			expires_at        DATETIME NOT NULL,
			created_at        DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			last_connected_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			ip_address        TEXT DEFAULT 'Unknown',
			user_agent        TEXT DEFAULT 'Unknown'
		)`,

		`CREATE TABLE IF NOT EXISTS dashboards (
			dashboard_id TEXT PRIMARY KEY,
			user_id      TEXT NOT NULL REFERENCES users(user_id) ON DELETE CASCADE,
			slug         TEXT UNIQUE NOT NULL,
			title        TEXT NOT NULL,
			access_rules TEXT NOT NULL DEFAULT '{}'
		)`,

		`CREATE TABLE IF NOT EXISTS dashboard_agents (
			dashboard_id          TEXT NOT NULL REFERENCES dashboards(dashboard_id) ON DELETE CASCADE,
			agent_id              TEXT NOT NULL REFERENCES agents(agent_id)         ON DELETE CASCADE,
			agent_public_id       TEXT NOT NULL,
			agent_public_nickname TEXT NOT NULL DEFAULT '',
			sort_key              TEXT DEFAULT NULL,
			PRIMARY KEY (dashboard_id, agent_id)
		)`,

		`CREATE TABLE IF NOT EXISTS alerts (
			alert_id       TEXT PRIMARY KEY,
			user_id        TEXT NOT NULL REFERENCES users(user_id) ON DELETE CASCADE,
			enabled        BOOLEAN NOT NULL DEFAULT 1,
			trigger_config TEXT NOT NULL,
			action_config  TEXT NOT NULL
		)`,

		`CREATE INDEX IF NOT EXISTS idx_alerts_user ON alerts(user_id)`,

		`CREATE TABLE IF NOT EXISTS alert_agents (
			alert_id      TEXT NOT NULL REFERENCES alerts(alert_id) ON DELETE CASCADE,
			agent_id      TEXT NOT NULL REFERENCES agents(agent_id) ON DELETE CASCADE,
			status        TEXT NOT NULL DEFAULT 'ok',
			last_fired_at DATETIME,
			PRIMARY KEY (alert_id, agent_id)
		)`,

		`CREATE TABLE IF NOT EXISTS alert_history (
			history_id      TEXT PRIMARY KEY,
			alert_id        TEXT NOT NULL REFERENCES alerts(alert_id) ON DELETE CASCADE,
			agent_id        TEXT NOT NULL REFERENCES agents(agent_id) ON DELETE CASCADE,
			triggered_at    DATETIME NOT NULL,
			resolved_at     DATETIME,
			trigger_value   REAL NOT NULL,
			notified_status TEXT NOT NULL
		)`,

		`CREATE TABLE IF NOT EXISTS alert_targets (
			target_id   TEXT PRIMARY KEY,
			user_id     TEXT NOT NULL REFERENCES users(user_id) ON DELETE CASCADE,
			name        TEXT NOT NULL,
			type        TEXT NOT NULL,
			destination TEXT NOT NULL,
			payload     TEXT NOT NULL DEFAULT '',
			created_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,

		`CREATE INDEX IF NOT EXISTS idx_alert_targets_user ON alert_targets(user_id)`,
		`CREATE INDEX IF NOT EXISTS idx_alert_history_alert ON alert_history(alert_id)`,
		`CREATE INDEX IF NOT EXISTS idx_alert_history_agent ON alert_history(agent_id)`,
		`CREATE INDEX IF NOT EXISTS idx_alert_history_triggered_desc ON alert_history(triggered_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_alert_agents_agent ON alert_agents(agent_id)`,

		`CREATE TABLE IF NOT EXISTS beszel_ssh (
			agent_id    TEXT PRIMARY KEY REFERENCES agents(agent_id) ON DELETE CASCADE,
			public_key  TEXT NOT NULL,
			private_key TEXT NOT NULL
		)`,
	}

	for _, stmt := range schemas {
		if _, err := s.db.Exec(stmt); err != nil {
			return err
		}
	}

	// Additive column migrations — safe to run on every boot.
	// "duplicate column" errors are intentionally swallowed; all other
	// errors are logged so startup problems are never silent.
	for _, m := range []string{
		`ALTER TABLE agents ADD COLUMN cpu_cores INTEGER DEFAULT 0`,
		`ALTER TABLE agents ADD COLUMN swap_size BIGINT  DEFAULT 0`,
		`ALTER TABLE agents ADD COLUMN disk_size BIGINT  DEFAULT 0`,
		`ALTER TABLE agents ADD COLUMN nickname  TEXT    DEFAULT ''`,
		`ALTER TABLE alert_agents ADD COLUMN status TEXT DEFAULT 'ok'`,
		`ALTER TABLE alert_agents ADD COLUMN last_fired_at DATETIME`,
		`ALTER TABLE agents ADD COLUMN agent_type TEXT DEFAULT 'ltstats'`,
		`ALTER TABLE agents ADD COLUMN total_rx_bytes         BIGINT DEFAULT 0`,
		`ALTER TABLE agents ADD COLUMN total_tx_bytes         BIGINT DEFAULT 0`,
		`ALTER TABLE agents ADD COLUMN total_disk_read_bytes  BIGINT DEFAULT 0`,
		`ALTER TABLE agents ADD COLUMN total_disk_write_bytes BIGINT DEFAULT 0`,
		`ALTER TABLE sessions ADD COLUMN created_at DATETIME DEFAULT NULL`,
		`ALTER TABLE sessions ADD COLUMN last_connected_at DATETIME DEFAULT NULL`,
		`ALTER TABLE sessions ADD COLUMN ip_address TEXT DEFAULT 'Unknown'`,
		`ALTER TABLE sessions ADD COLUMN user_agent TEXT DEFAULT 'Unknown'`,
		`ALTER TABLE dashboard_agents ADD COLUMN sort_key TEXT DEFAULT NULL`,
		`ALTER TABLE agents ADD COLUMN note TEXT DEFAULT ''`,
		`ALTER TABLE alerts ADD COLUMN nickname TEXT DEFAULT ''`,
		`ALTER TABLE alert_history ADD COLUMN user_id TEXT DEFAULT ''`,
		`ALTER TABLE alert_history ADD COLUMN target_id TEXT DEFAULT ''`,
		`ALTER TABLE alert_history ADD COLUMN target_name TEXT DEFAULT ''`,
		`ALTER TABLE alert_history ADD COLUMN agent_nickname TEXT DEFAULT ''`,
		`ALTER TABLE alert_history ADD COLUMN alert_nickname TEXT DEFAULT ''`,
		`CREATE INDEX IF NOT EXISTS idx_alert_history_user ON alert_history(user_id)`,
		`CREATE INDEX IF NOT EXISTS idx_alert_history_user_triggered ON alert_history(user_id, triggered_at DESC)`,
		`UPDATE alert_history SET user_id = (SELECT user_id FROM agents WHERE agents.agent_id = alert_history.agent_id) WHERE COALESCE(user_id, '') = ''`,
		`UPDATE alert_history SET alert_nickname = (SELECT nickname FROM alerts WHERE alerts.alert_id = alert_history.alert_id) WHERE COALESCE(alert_nickname, '') = ''`,
		`UPDATE alert_history SET agent_nickname = (SELECT nickname FROM agents WHERE agents.agent_id = alert_history.agent_id) WHERE COALESCE(agent_nickname, '') = ''`,
	} {
		if _, err := s.db.Exec(m); err != nil {
			// Ignore "duplicate column" — expected on every boot after first run.
			// Log everything else so schema drift is immediately visible.
			log.Debugf("[migrate] %s — %v", m, err)
		}
	}

	return nil
}
