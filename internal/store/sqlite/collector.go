package sqlite

import (
	"certainstats/internal/store"
	"context"
	"database/sql"
	"time"
)

var _ store.CollectorStore = (*Store)(nil)

const collectorColumns = `collector_id, user_id, name, agent_id, created_at, first_connected_at, last_seen_at, version`

func scanCollector(row scanner) (*store.Collector, error) {
	var c store.Collector
	if err := row.Scan(&c.CollectorID, &c.UserID, &c.Name, &c.AgentID, &c.CreatedAt, &c.FirstConnectedAt, &c.LastSeenAt, &c.Version); err != nil {
		return nil, err
	}
	return &c, nil
}

func (s *Store) CollectorCreate(ctx context.Context, c store.Collector, tokenHash string, types []string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `INSERT INTO collectors (collector_id, user_id, name, token_hash, agent_id, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
		c.CollectorID, c.UserID, c.Name, tokenHash, c.AgentID, c.CreatedAt); err != nil {
		return err
	}
	for _, typ := range types {
		if _, err := tx.ExecContext(ctx, `INSERT INTO collector_types (collector_id, type, enabled, updated_at) VALUES (?, ?, 1, ?)`, c.CollectorID, typ, c.CreatedAt); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) collectorTypes(ctx context.Context, ids []string) (map[string][]store.CollectorType, error) {
	result := make(map[string][]store.CollectorType, len(ids))
	for _, id := range ids {
		rows, err := s.db.QueryContext(ctx, `SELECT type, enabled, reported_on, status, error, state_json, updated_at FROM collector_types WHERE collector_id = ? ORDER BY type`, id)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var t store.CollectorType
			if err := rows.Scan(&t.Type, &t.Enabled, &t.ReportedOn, &t.Status, &t.Error, &t.State, &t.UpdatedAt); err != nil {
				rows.Close()
				return nil, err
			}
			result[id] = append(result[id], t)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
	}
	return result, nil
}

func (s *Store) withTypes(ctx context.Context, list []store.Collector) ([]store.Collector, error) {
	ids := make([]string, len(list))
	for i := range list {
		ids[i] = list[i].CollectorID
	}
	types, err := s.collectorTypes(ctx, ids)
	if err != nil {
		return nil, err
	}
	for i := range list {
		list[i].Types = types[list[i].CollectorID]
		if list[i].Types == nil {
			list[i].Types = []store.CollectorType{}
		}
	}
	return list, nil
}

func (s *Store) CollectorList(ctx context.Context, userID string) ([]store.Collector, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+collectorColumns+` FROM collectors WHERE user_id = ? AND revoked_at = 0 ORDER BY created_at, collector_id`, userID)
	if err != nil {
		return nil, err
	}
	list := []store.Collector{}
	for rows.Next() {
		c, err := scanCollector(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		list = append(list, *c)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	return s.withTypes(ctx, list)
}

func (s *Store) collectorOne(ctx context.Context, query string, args ...any) (*store.Collector, error) {
	c, err := scanCollector(s.db.QueryRowContext(ctx, `SELECT `+collectorColumns+` FROM collectors WHERE `+query, args...))
	if err != nil {
		return nil, err
	}
	list, err := s.withTypes(ctx, []store.Collector{*c})
	if err != nil {
		return nil, err
	}
	return &list[0], nil
}

func (s *Store) CollectorGet(ctx context.Context, collectorID, userID string) (*store.Collector, error) {
	return s.collectorOne(ctx, `collector_id = ? AND user_id = ? AND revoked_at = 0`, collectorID, userID)
}

func (s *Store) CollectorByTokenHash(ctx context.Context, tokenHash string) (*store.Collector, error) {
	return s.collectorOne(ctx, `token_hash = ? AND revoked_at = 0`, tokenHash)
}

// mustChange turns an update that matched no row into sql.ErrNoRows.
func mustChange(res sql.Result, err error) error {
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *Store) CollectorUpdate(ctx context.Context, collectorID, userID string, name, agentID *string) error {
	if name == nil && agentID == nil {
		_, err := s.CollectorGet(ctx, collectorID, userID)
		return err
	}
	return mustChange(s.db.ExecContext(ctx, `UPDATE collectors SET name = COALESCE(?, name), agent_id = COALESCE(?, agent_id) WHERE collector_id = ? AND user_id = ? AND revoked_at = 0`,
		name, agentID, collectorID, userID))
}

// CollectorRevoke keeps the row so stored data keeps its collector name; the token
// stops working at once. The hash is replaced so the old token cannot match again.
func (s *Store) CollectorRevoke(ctx context.Context, collectorID, userID string) error {
	return mustChange(s.db.ExecContext(ctx, `UPDATE collectors SET revoked_at = ?, token_hash = 'revoked:' || collector_id WHERE collector_id = ? AND user_id = ? AND revoked_at = 0`,
		time.Now().UnixMilli(), collectorID, userID))
}

func (s *Store) CollectorResetToken(ctx context.Context, collectorID, userID, tokenHash string) error {
	return mustChange(s.db.ExecContext(ctx, `UPDATE collectors SET token_hash = ? WHERE collector_id = ? AND user_id = ? AND revoked_at = 0`, tokenHash, collectorID, userID))
}

func (s *Store) CollectorSeen(ctx context.Context, collectorID, version string, at int64) error {
	_, err := s.db.ExecContext(ctx, `UPDATE collectors SET last_seen_at = ?, version = CASE WHEN ? = '' THEN version ELSE ? END,
		first_connected_at = CASE WHEN first_connected_at = 0 THEN ? ELSE first_connected_at END WHERE collector_id = ?`,
		at, version, version, at, collectorID)
	return err
}

// CollectorNames includes revoked collectors, whose data is still shown.
func (s *Store) CollectorNames(ctx context.Context, userID string) (map[string]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT collector_id, name FROM collectors WHERE user_id = ?`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	names := map[string]string{}
	for rows.Next() {
		var id, name string
		if err := rows.Scan(&id, &name); err != nil {
			return nil, err
		}
		names[id] = name
	}
	return names, rows.Err()
}

func (s *Store) CollectorTypeSetEnabled(ctx context.Context, collectorID, userID, typ string, enabled bool) error {
	if _, err := s.CollectorGet(ctx, collectorID, userID); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO collector_types (collector_id, type, enabled, updated_at) VALUES (?, ?, ?, ?)
		ON CONFLICT(collector_id, type) DO UPDATE SET enabled = excluded.enabled, updated_at = excluded.updated_at`,
		collectorID, typ, enabled, time.Now().UnixMilli())
	return err
}

func (s *Store) CollectorTypeReport(ctx context.Context, collectorID, typ string, reportedOn bool) error {
	_, err := s.db.ExecContext(ctx, `UPDATE collector_types SET reported_on = ? WHERE collector_id = ? AND type = ?`, reportedOn, collectorID, typ)
	return err
}

func (s *Store) CollectorTypeStatus(ctx context.Context, collectorID, typ, status, message string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE collector_types SET status = ?, error = ?, updated_at = ? WHERE collector_id = ? AND type = ?`,
		status, message, time.Now().UnixMilli(), collectorID, typ)
	return err
}

func (s *Store) CollectorTypeSaveState(ctx context.Context, collectorID, typ, state string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE collector_types SET state_json = ? WHERE collector_id = ? AND type = ?`, state, collectorID, typ)
	return err
}

func (s *Store) UserIDs(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT user_id FROM users`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
