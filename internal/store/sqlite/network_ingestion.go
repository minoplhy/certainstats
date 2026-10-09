package sqlite

import (
	nm "certainstats/internal/networkmonitor"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

func (s *Store) NetworkBegin(ctx context.Context, b nm.Batch) (*nm.Batch, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	// A replayed batch ID returns the journaled copy unchanged.
	var journaled []byte
	err = tx.QueryRowContext(ctx,
		`SELECT payload FROM network_monitor_ingestion WHERE batch_id = ? AND complete = 0`,
		b.ID,
	).Scan(&journaled)
	if err == nil {
		var existing nm.Batch
		err = json.Unmarshal(journaled, &existing)
		return &existing, err
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}

	accepted := map[string]nm.Result{}
	timestamp := time.Now().UnixMilli()
	for monitorID, result := range b.Results {
		var enabled bool
		var latestPayload sql.NullString
		var measuredAt sql.NullInt64
		err = tx.QueryRowContext(ctx, `
			SELECT m.enabled, l.payload, l.measurement_at
			FROM   network_monitors m
			LEFT   JOIN network_monitor_latest l ON l.monitor_id = m.monitor_id
			WHERE  m.monitor_id = ? AND m.user_id = ? AND m.agent_id = ? AND m.archived_at IS NULL`,
			monitorID, b.UserID, b.AgentID,
		).Scan(&enabled, &latestPayload, &measuredAt)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if !enabled || result.Validate(time.Now()) != nil {
			continue
		}
		if latestPayload.Valid {
			var latest nm.Latest
			if err = json.Unmarshal([]byte(latestPayload.String), &latest); err != nil {
				return nil, err
			}
			if latest.Epoch == b.Epoch && !newerResult(result, latest.Result) {
				continue
			}
		}
		// TSDB sample timestamps must stay unique and increasing per monitor.
		if measuredAt.Valid {
			timestamp = max(timestamp, measuredAt.Int64+1)
		}
		accepted[monitorID] = result
	}

	b.Results = accepted
	b.Timestamp = timestamp
	if len(accepted) == 0 {
		return &b, nil
	}
	raw, err := json.Marshal(b)
	if err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx,
		`INSERT INTO network_monitor_ingestion(batch_id, user_id, agent_id, payload) VALUES (?, ?, ?, ?)`,
		b.ID, b.UserID, b.AgentID, raw,
	); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return &b, nil
}

// newerResult reports whether a result from the same connection advances on
// the stored one. A late GetData response must not replace a newer
// observation or supply an obsolete loss value to incident evaluation.
func newerResult(result, stored nm.Result) bool {
	if result.LastProbeAt < stored.LastProbeAt || result.SampleCount < stored.SampleCount {
		return false
	}
	return result.LastProbeAt != stored.LastProbeAt || result.SampleCount != stored.SampleCount
}

func (s *Store) NetworkPending(ctx context.Context, agentID string) ([]nm.Batch, error) {
	query := `SELECT payload FROM network_monitor_ingestion WHERE complete = 0`
	args := []any{}
	if agentID != "" {
		query += ` AND agent_id = ?`
		args = append(args, agentID)
	}
	query += ` ORDER BY rowid`

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	batches := []nm.Batch{}
	for rows.Next() {
		var raw []byte
		var b nm.Batch
		if err = rows.Scan(&raw); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(raw, &b); err != nil {
			return nil, err
		}
		batches = append(batches, b)
	}
	return batches, rows.Err()
}

func (s *Store) NetworkComplete(ctx context.Context, b nm.Batch) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	for monitorID, result := range b.Results {
		var storedPayload string
		var measuredAt int64
		err = tx.QueryRowContext(ctx,
			`SELECT payload, measurement_at FROM network_monitor_latest WHERE monitor_id = ?`,
			monitorID,
		).Scan(&storedPayload, &measuredAt)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if measuredAt >= b.Timestamp {
			continue
		}

		latest := nm.Latest{Result: result, Epoch: b.Epoch, ReceivedAt: b.Timestamp}
		if result.Cert != nil {
			latest.CertificateReceivedAt = b.Timestamp
		} else if storedPayload != "" {
			// Certificates persist when a later packet omits them.
			var stored nm.Latest
			if err = json.Unmarshal([]byte(storedPayload), &stored); err != nil {
				return err
			}
			latest.Cert = stored.Cert
			latest.CertificateReceivedAt = stored.CertificateReceivedAt
		}
		raw, err := json.Marshal(latest)
		if err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `
			INSERT INTO network_monitor_latest(monitor_id, payload, measurement_at)
			SELECT monitor_id, ?, ?
			FROM   network_monitors
			WHERE  monitor_id = ? AND user_id = ? AND agent_id = ? AND archived_at IS NULL AND enabled = 1
			ON CONFLICT(monitor_id) DO UPDATE SET payload = excluded.payload, measurement_at = excluded.measurement_at`,
			string(raw), b.Timestamp, monitorID, b.UserID, b.AgentID,
		); err != nil {
			return err
		}
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM network_monitor_ingestion WHERE batch_id = ?`, b.ID); err != nil {
		return err
	}
	return tx.Commit()
}
