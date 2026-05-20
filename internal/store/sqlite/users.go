package sqlite

import (
	"certainstats/internal/store"
	"context"
	"time"
)

func (s *Store) GetByUsername(ctx context.Context, username string) (*store.User, error) {
	var u store.User
	var createdAt string
	err := s.db.QueryRowContext(ctx,
		`SELECT user_id, username, password_hash, is_admin, created_at
		 FROM users WHERE username = ?`,
		username,
	).Scan(&u.UserID, &u.Username, &u.PasswordHash, &u.IsAdmin, &createdAt)
	if err != nil {
		return nil, err
	}
	u.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", createdAt)
	return &u, nil
}

func (s *Store) GetByID(ctx context.Context, userID string) (*store.User, error) {
	var u store.User
	var createdAt string
	err := s.db.QueryRowContext(ctx,
		`SELECT user_id, username, password_hash, is_admin, created_at
		 FROM users WHERE user_id = ?`,
		userID,
	).Scan(&u.UserID, &u.Username, &u.PasswordHash, &u.IsAdmin, &createdAt)
	if err != nil {
		return nil, err
	}
	u.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", createdAt)
	return &u, nil
}

func (s *Store) UpdatePassword(ctx context.Context, userID string, passwordHash string) error {
	_, err := s.db.ExecContext(ctx,
		"UPDATE users SET password_hash = ? WHERE user_id = ?",
		passwordHash, userID,
	)
	return err
}
