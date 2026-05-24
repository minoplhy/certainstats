package sqlite

import (
	"certainstats/internal/base/alert"
	"context"
	"database/sql"
	"fmt"
	"time"
)

// TargetCreate inserts a new alert target.
func (s *Store) TargetCreate(ctx context.Context, t alert.AlertTarget) error {
	now := time.Now()
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO alert_targets (target_id, user_id, name, type, destination, payload, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, t.TargetID, t.UserID, t.Name, string(t.Type), t.Destination, t.Payload, now)
	return err
}

// TargetList lists all alert targets belonging to a specific user.
func (s *Store) TargetList(ctx context.Context, userID string) ([]alert.AlertTarget, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT target_id, user_id, name, type, destination, payload, created_at
		FROM alert_targets
		WHERE user_id = ?
		ORDER BY created_at DESC
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	targets := []alert.AlertTarget{}
	for rows.Next() {
		var t alert.AlertTarget
		var targetType string
		err := rows.Scan(&t.TargetID, &t.UserID, &t.Name, &targetType, &t.Destination, &t.Payload, &t.CreatedAt)
		if err != nil {
			return nil, err
		}
		t.Type = alert.DestinationType(targetType)
		targets = append(targets, t)
	}
	return targets, rows.Err()
}

// TargetGetByID retrieves a target by ID, strictly scoped to its owner (user_id).
func (s *Store) TargetGetByID(ctx context.Context, targetID string, userID string) (alert.AlertTarget, error) {
	var t alert.AlertTarget
	var targetType string
	err := s.db.QueryRowContext(ctx, `
		SELECT target_id, user_id, name, type, destination, payload, created_at
		FROM alert_targets
		WHERE target_id = ? AND user_id = ?
	`, targetID, userID).Scan(&t.TargetID, &t.UserID, &t.Name, &targetType, &t.Destination, &t.Payload, &t.CreatedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return t, fmt.Errorf("alert target not found")
		}
		return t, err
	}
	t.Type = alert.DestinationType(targetType)
	return t, nil
}

// TargetUpdate updates an existing alert target.
func (s *Store) TargetUpdate(ctx context.Context, t alert.AlertTarget) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE alert_targets
		SET name = ?, type = ?, destination = ?, payload = ?
		WHERE target_id = ? AND user_id = ?
	`, t.Name, string(t.Type), t.Destination, t.Payload, t.TargetID, t.UserID)
	return err
}

// TargetDelete deletes an alert target.
func (s *Store) TargetDelete(ctx context.Context, targetID string, userID string) error {
	_, err := s.db.ExecContext(ctx, `
		DELETE FROM alert_targets
		WHERE target_id = ? AND user_id = ?
	`, targetID, userID)
	return err
}
