package sqlite

import (
	"certainstats/internal/store"
	"context"
	"database/sql"
)

func (s *Store) IngestionGet(ctx context.Context, id string) (*store.IngestionRecord, error) {
	var r store.IngestionRecord
	err := s.db.QueryRowContext(ctx, "SELECT batch_id,agent_id,user_id,payload,complete FROM ingestion_journal WHERE batch_id = ?", id).Scan(&r.ID, &r.AgentID, &r.UserID, &r.Payload, &r.Complete)
	return &r, err
}
func (s *Store) IngestionBegin(ctx context.Context, r store.IngestionRecord, rx, tx uint64, disks []store.DiskDelta) error {
	conn, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer conn.Rollback()
	res, err := conn.ExecContext(ctx, "INSERT OR IGNORE INTO ingestion_journal(batch_id,agent_id,user_id,payload,complete) VALUES(?,?,?,?,0)", r.ID, r.AgentID, r.UserID, r.Payload)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 1 {
		if err := incrementTraffic(ctx, conn, r.AgentID, r.UserID, rx, tx, disks); err != nil {
			return err
		}
	}
	return conn.Commit()
}
func (s *Store) IngestionComplete(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, "UPDATE ingestion_journal SET complete=1, payload=X'' WHERE batch_id=?", id)
	return err
}
func (s *Store) IngestionPending(ctx context.Context) ([]store.IngestionRecord, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT batch_id,agent_id,user_id,payload,complete FROM ingestion_journal WHERE complete=0 ORDER BY rowid")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []store.IngestionRecord
	for rows.Next() {
		var r store.IngestionRecord
		if err := rows.Scan(&r.ID, &r.AgentID, &r.UserID, &r.Payload, &r.Complete); err != nil {
			return nil, err
		}
		result = append(result, r)
	}
	return result, rows.Err()
}

var _ store.IngestionJournal = (*Store)(nil)
var _ *sql.Tx
