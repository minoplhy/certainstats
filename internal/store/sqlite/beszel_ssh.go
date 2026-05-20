package sqlite

import (
	"certainstats/internal/store"
	"context"
	"database/sql"
	"errors"
)

func (s *Store) BeszelSSHGet(ctx context.Context, agentID, userID string) (*store.BeszelSSH, error) {
	var ssh store.BeszelSSH
	err := s.db.QueryRowContext(ctx, `
		SELECT b.agent_id, b.public_key, b.private_key 
		FROM beszel_ssh b
		JOIN agents a ON b.agent_id = a.agent_id
		WHERE b.agent_id = ? AND a.user_id = ?
	`, agentID, userID).Scan(&ssh.AgentID, &ssh.PublicKey, &ssh.PrivateKey)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}

	return &ssh, nil
}

func (s *Store) BeszelSSHSave(ctx context.Context, ssh store.BeszelSSH, userID string) error {
	// Verify ownership first
	var exists bool
	err := s.db.QueryRowContext(ctx, "SELECT 1 FROM agents WHERE agent_id = ? AND user_id = ?", ssh.AgentID, userID).Scan(&exists)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return sql.ErrNoRows // Unauthorized or not found
		}
		return err
	}

	_, err = s.db.ExecContext(ctx, `
		INSERT INTO beszel_ssh (agent_id, public_key, private_key)
		VALUES (?, ?, ?)
		ON CONFLICT(agent_id) DO UPDATE SET
			public_key = excluded.public_key,
			private_key = excluded.private_key
	`, ssh.AgentID, ssh.PublicKey, ssh.PrivateKey)
	return err
}
