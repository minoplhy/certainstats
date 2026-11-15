package sqlite

import "strings"

// collectorSchema adds collectors and the types each may send. Additive and idempotent.
var collectorSchema = []string{
	`CREATE TABLE IF NOT EXISTS collectors (
		collector_id       TEXT PRIMARY KEY,
		user_id            TEXT NOT NULL REFERENCES users(user_id) ON DELETE CASCADE,
		name               TEXT NOT NULL,
		token_hash         TEXT NOT NULL UNIQUE,
		agent_id           TEXT NOT NULL DEFAULT '',
		created_at         INTEGER NOT NULL,
		first_connected_at INTEGER NOT NULL DEFAULT 0,
		last_seen_at       INTEGER NOT NULL DEFAULT 0,
		version            TEXT NOT NULL DEFAULT '',
		revoked_at         INTEGER NOT NULL DEFAULT 0
	)`,
	`CREATE INDEX IF NOT EXISTS collectors_owner ON collectors(user_id, revoked_at, created_at)`,
	`CREATE TABLE IF NOT EXISTS collector_types (
		collector_id TEXT NOT NULL REFERENCES collectors(collector_id) ON DELETE CASCADE,
		type         TEXT NOT NULL,
		enabled      INTEGER NOT NULL DEFAULT 1,
		reported_on  INTEGER NOT NULL DEFAULT 0,
		status       TEXT NOT NULL DEFAULT '',
		error        TEXT NOT NULL DEFAULT '',
		state_json   TEXT NOT NULL DEFAULT '{}',
		updated_at   INTEGER NOT NULL DEFAULT 0,
		PRIMARY KEY (collector_id, type)
	)`,
}

// collectorColumnsAdded are additive columns; "duplicate column" means already applied.
var collectorColumnsAdded = []string{
	// token is kept, like agent tokens, so install steps can be shown again.
	// Collectors created before it have '' until their token is reset.
	`ALTER TABLE collectors ADD COLUMN token TEXT NOT NULL DEFAULT ''`,
}

func (s *Store) migrateCollectors() error {
	for _, stmt := range collectorSchema {
		if _, err := s.db.Exec(stmt); err != nil {
			return err
		}
	}
	for _, stmt := range collectorColumnsAdded {
		if _, err := s.db.Exec(stmt); err != nil && !strings.Contains(err.Error(), "duplicate column") {
			return err
		}
	}
	return nil
}
