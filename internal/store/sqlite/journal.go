package sqlite

import (
	"certainstats/internal/store"
	"context"
	"database/sql"
	"encoding/binary"
	"fmt"
	"time"

	log "certainstats/internal/logger"
)

// journalPruneChunk bounds how long one prune pass holds the write connection.
const journalPruneChunk = 5000

// completionStamp encodes a completion time as 8-byte big-endian unix ms.
// Completed rows keep this in payload, which is never read again once
// complete, so BLOB comparison orders rows by completion time. Legacy rows
// hold an empty payload, which sorts below every stamp and so always counts as expired.
func completionStamp(t time.Time) []byte {
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, uint64(t.UnixMilli()))
	return b
}

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
	_, err := s.db.ExecContext(ctx, "UPDATE ingestion_journal SET complete=1, payload=? WHERE batch_id=?", completionStamp(time.Now()), id)
	return err
}

// IngestionPrune deletes completed rows that finished before the cutoff, in
// chunks. Pending rows are never touched: recovery replays them.
func (s *Store) IngestionPrune(ctx context.Context, before time.Time) (int64, error) {
	cutoff := completionStamp(before)
	var total int64
	for {
		if err := ctx.Err(); err != nil {
			return total, err
		}
		res, err := s.db.ExecContext(ctx, "DELETE FROM ingestion_journal WHERE rowid IN (SELECT rowid FROM ingestion_journal WHERE complete=1 AND payload < ? LIMIT ?)", cutoff, journalPruneChunk)
		if err != nil {
			return total, err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return total, err
		}
		total += n
		if n < journalPruneChunk {
			return total, nil
		}
	}
}

// ReclaimSpace returns freed pages to the filesystem. The first call switches
// the database to incremental auto_vacuum, which needs one full VACUUM; later
// calls only release the freelist.
func (s *Store) ReclaimSpace(ctx context.Context) error {
	var mode int
	if err := s.db.QueryRowContext(ctx, "PRAGMA auto_vacuum").Scan(&mode); err != nil {
		return err
	}
	if mode != 2 {
		log.Printf("[Maintenance] converting database to incremental auto_vacuum (one-time VACUUM)")
		start := time.Now()
		if _, err := s.db.ExecContext(ctx, "PRAGMA auto_vacuum=INCREMENTAL"); err != nil {
			return err
		}
		if _, err := s.db.ExecContext(ctx, "VACUUM"); err != nil {
			return fmt.Errorf("vacuum: %w", err)
		}
		log.Printf("[Maintenance] database converted in %v", time.Since(start).Round(time.Millisecond))
	} else if err := s.drainPragma(ctx, "PRAGMA incremental_vacuum"); err != nil {
		return err
	}
	return s.drainPragma(ctx, "PRAGMA wal_checkpoint(TRUNCATE)")
}

// drainPragma runs a pragma that does its work while its result rows are stepped.
func (s *Store) drainPragma(ctx context.Context, pragma string) error {
	rows, err := s.db.QueryContext(ctx, pragma)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
	}
	return rows.Err()
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
