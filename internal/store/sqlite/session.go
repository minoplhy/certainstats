package sqlite

import (
	"certainstats/internal/store"
	"context"
	"database/sql"
	"fmt"
	"time"
)

func (s *Store) SessionCreate(ctx context.Context, sess store.Session) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO sessions (session_token, user_id, expires_at, created_at, last_connected_at, ip_address, user_agent) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		sess.Token, sess.UserID, sess.ExpiresAt, sess.CreatedAt, sess.LastConnectedAt, sess.IPAddress, sess.UserAgent,
	)
	return err
}

var timeFormats = []string{
	"2006-01-02 15:04:05.999999999Z07:00",     // Standard Go-SQLite string (no 'T')
	"2006-01-02 15:04:05",                     // SQLite standard (e.g., from datetime('now'))
	time.RFC3339Nano,                          // "2006-01-02T15:04:05.999999999Z07:00"
	time.RFC3339,                              // "2006-01-02T15:04:05Z07:00"
	"2006-01-02 15:04:05.999999999 -0700 MST", // Output of Go's time.String()
}

func parseTimeStr(val string) (time.Time, error) {
	for _, layout := range timeFormats {
		t, err := time.Parse(layout, val)
		if err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("could not parse sqlite time %q", val)
}

// parseNullableTime safely converts a sql.NullString datetime column.
// Returns fallback when the column is NULL (pre-migration rows).
func parseNullableTime(ns sql.NullString, fallback time.Time) time.Time {
	if !ns.Valid || ns.String == "" {
		return fallback
	}
	t, err := parseTimeStr(ns.String)
	if err != nil {
		return fallback
	}
	return t
}

func (s *Store) SessionGet(ctx context.Context, token string) (*store.Session, error) {
	var sess store.Session
	var expiresAt string
	var createdAt, lastConnectedAt sql.NullString

	err := s.db.QueryRowContext(ctx,
		`SELECT session_token, user_id, expires_at, created_at, last_connected_at, ip_address, user_agent FROM sessions WHERE session_token = ?`,
		token,
	).Scan(&sess.Token, &sess.UserID, &expiresAt, &createdAt, &lastConnectedAt, &sess.IPAddress, &sess.UserAgent)
	if err != nil {
		return nil, err
	}

	var parseErr error
	sess.ExpiresAt, parseErr = parseTimeStr(expiresAt)
	if parseErr != nil {
		return nil, parseErr
	}

	now := time.Now()
	sess.CreatedAt = parseNullableTime(createdAt, now)
	sess.LastConnectedAt = parseNullableTime(lastConnectedAt, now)

	return &sess, nil
}

func (s *Store) SessionDelete(ctx context.Context, token string) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM sessions WHERE session_token = ?`, token,
	)
	return err
}

// SessionDeleteExpired removes all sessions past their hard expiry time or the
// 15-day idle threshold. The IS NOT NULL guard ensures pre-migration rows with a
// NULL last_connected_at are only removed via their expires_at date, not evicted
// immediately as if they had been idle since the epoch.
func (s *Store) SessionDeleteExpired(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM sessions WHERE expires_at < ? OR (last_connected_at IS NOT NULL AND last_connected_at < ?)`,
		time.Now(), time.Now().Add(-15*24*time.Hour),
	)
	return err
}

func (s *Store) SessionUpdateActivity(ctx context.Context, token string, lastConnected time.Time) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE sessions SET last_connected_at = ? WHERE session_token = ?`,
		lastConnected, token,
	)
	return err
}

func (s *Store) SessionListByUser(ctx context.Context, userID string) ([]store.Session, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT session_token, user_id, expires_at, created_at, last_connected_at, ip_address, user_agent FROM sessions WHERE user_id = ? ORDER BY last_connected_at DESC`,
		userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sessions []store.Session
	for rows.Next() {
		var sess store.Session
		var expiresAt string
		var createdAt, lastConnectedAt sql.NullString

		if err := rows.Scan(&sess.Token, &sess.UserID, &expiresAt, &createdAt, &lastConnectedAt, &sess.IPAddress, &sess.UserAgent); err != nil {
			return nil, err
		}
		now := time.Now()
		sess.ExpiresAt, _ = parseTimeStr(expiresAt)
		sess.CreatedAt = parseNullableTime(createdAt, now)
		sess.LastConnectedAt = parseNullableTime(lastConnectedAt, now)
		sessions = append(sessions, sess)
	}
	return sessions, rows.Err()
}

func (s *Store) SessionDeleteOther(ctx context.Context, userID string, currentToken string) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM sessions WHERE user_id = ? AND session_token != ?`,
		userID, currentToken,
	)
	return err
}

// ErrNotFound is returned by Get when the session doesn't exist.
// Callers can use errors.Is(err, sql.ErrNoRows) directly, but this
// alias makes handler code read more clearly.
var ErrNotFound = sql.ErrNoRows
