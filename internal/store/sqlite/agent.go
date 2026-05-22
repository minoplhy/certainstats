package sqlite

import (
	b "certainstats/internal/base/agent"
	"certainstats/internal/store"
	"context"
	"database/sql"
	"time"
)

func (s *Store) AgentGetByToken(ctx context.Context, token string) (*store.AgentIdentity, error) {
	if val, ok := s.cache.Load(token); ok {
		return val.(*store.AgentIdentity), nil
	}

	var id store.AgentIdentity
	err := s.db.QueryRowContext(ctx,
		`SELECT user_id, agent_id FROM agents WHERE token = ?`, token,
	).Scan(&id.UserID, &id.AgentID)
	if err != nil {
		return nil, err
	}

	s.cache.Store(token, &id)
	return &id, nil
}

func (s *Store) AgentUpsertDetails(ctx context.Context, d store.Agent) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE agents SET
			last_seen     = ?,
			is_online     = 1,
			uptime        = ?,
			linux_version = CASE WHEN ? != '' THEN ? ELSE linux_version END,
			cpu_model     = CASE WHEN ? != '' THEN ? ELSE cpu_model END,
			cpu_cores     = CASE WHEN ? > 0  THEN ? ELSE cpu_cores END,
			ram_size      = CASE WHEN ? > 0  THEN ? ELSE ram_size END,
			swap_size     = CASE WHEN ? > 0  THEN ? ELSE swap_size END,
			disk_size     = CASE WHEN ? > 0  THEN ? ELSE disk_size END
		WHERE agent_id = ? AND user_id = ?`,
		time.Now(),
		d.Uptime,
		d.LinuxVersion, d.LinuxVersion,
		d.CpuModel, d.CpuModel,
		d.CpuCores, d.CpuCores,
		d.RamSize, d.RamSize,
		d.SwapSize, d.SwapSize,
		d.DiskSize, d.DiskSize,
		d.AgentID, d.UserID,
	)
	return err
}

func (s *Store) AgentUpdateHeartbeat(ctx context.Context, agentID, userID string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE agents SET last_seen = ?, is_online = 1 WHERE agent_id = ? AND user_id = ?`,
		time.Now(), agentID, userID,
	)
	return err
}

func (s *Store) AgentList(ctx context.Context, userID string) ([]store.Agent, error) {
	// 1. Fetch all per-disk odometers first
	diskRows, err := s.db.QueryContext(ctx, `
		SELECT ado.agent_id, ado.path, ado.read_bytes, ado.write_bytes
		FROM   agent_disk_odometers ado
		JOIN   agents a ON ado.agent_id = a.agent_id
		WHERE  a.user_id = ?`,
		userID,
	)
	disksMap := make(map[string][]b.DiskOdometer)
	if err == nil {
		for diskRows.Next() {
			var agentID string
			var d b.DiskOdometer
			if err := diskRows.Scan(&agentID, &d.Path, &d.ReadBytes, &d.WriteBytes); err == nil {
				disksMap[agentID] = append(disksMap[agentID], d)
			}
		}
		diskRows.Close() // Release connection back to pool explicitly
	}

	// 2. Query agents next
	rows, err := s.db.QueryContext(ctx, `
		SELECT agent_id, user_id, agent_type, nickname, last_seen, is_online, uptime,
		       linux_version, cpu_model, cpu_cores, ram_size, swap_size, disk_size,
		       total_rx_bytes, total_tx_bytes, total_disk_read_bytes, total_disk_write_bytes,
		       note
		FROM   agents
		WHERE  user_id = ?
		ORDER  BY is_online DESC, nickname ASC`,
		userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []store.Agent
	for rows.Next() {
		var a store.Agent
		var lastSeen sql.NullTime
		if err := rows.Scan(
			&a.AgentID, &a.UserID, &a.AgentType, &a.Nickname, &lastSeen, &a.IsOnline, &a.Uptime,
			&a.LinuxVersion, &a.CpuModel, &a.CpuCores, &a.RamSize, &a.SwapSize, &a.DiskSize,
			&a.TotalRxBytes, &a.TotalTxBytes, &a.TotalDiskReadBytes, &a.TotalDiskWriteBytes,
			&a.Note,
		); err != nil {
			return nil, err
		}
		if lastSeen.Valid {
			a.LastSeen = &lastSeen.Time
		}
		a.Disks = disksMap[a.AgentID]
		out = append(out, a)
	}
	if out == nil {
		out = []store.Agent{}
	}
	return out, rows.Err()
}

func (s *Store) AgentProvision(ctx context.Context, agentID, userID, token, nickname, agentType string) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO agents (agent_id, user_id, token, agent_type, nickname, is_online, linux_version, cpu_model)
		VALUES (?, ?, ?, ?, ?, 0, 'Pending connection...', 'Waiting for data...')`,
		agentID, userID, token, agentType, nickname,
	)
	return err
}

func (s *Store) AgentUpdate(ctx context.Context, agentID, userID string, nickname *string, note *string) error {
	if nickname == nil && note == nil {
		return nil
	}

	query := "UPDATE agents SET "
	var args []any
	if nickname != nil {
		query += "nickname = ?"
		args = append(args, *nickname)
	}
	if note != nil {
		if nickname != nil {
			query += ", "
		}
		query += "note = ?"
		args = append(args, *note)
	}
	query += " WHERE agent_id = ? AND user_id = ?"
	args = append(args, agentID, userID)

	res, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *Store) AgentDelete(ctx context.Context, agentID, userID string) error {
	res, err := s.db.ExecContext(ctx,
		`DELETE FROM agents WHERE agent_id = ? AND user_id = ?`,
		agentID, userID,
	)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}

	// Evict from identity cache immediately so in-flight submits are rejected.
	s.cache.Range(func(key, val any) bool {
		if val.(*store.AgentIdentity).AgentID == agentID {
			s.cache.Delete(key)
			return false
		}
		return true
	})
	return nil
}

func (s *Store) AgentGetByID(ctx context.Context, agentID, userID string) (*store.Agent, error) {
	var a store.Agent
	var lastSeen sql.NullTime
	err := s.db.QueryRowContext(ctx, `
		SELECT agent_id, user_id, agent_type, nickname, last_seen, is_online, uptime,
		       linux_version, cpu_model, cpu_cores, ram_size, swap_size, disk_size,
		       total_rx_bytes, total_tx_bytes, total_disk_read_bytes, total_disk_write_bytes,
		       note
		FROM   agents
		WHERE  agent_id = ? AND user_id = ?`,
		agentID, userID,
	).Scan(
		&a.AgentID, &a.UserID, &a.AgentType, &a.Nickname, &lastSeen, &a.IsOnline, &a.Uptime,
		&a.LinuxVersion, &a.CpuModel, &a.CpuCores, &a.RamSize, &a.SwapSize, &a.DiskSize,
		&a.TotalRxBytes, &a.TotalTxBytes, &a.TotalDiskReadBytes, &a.TotalDiskWriteBytes,
		&a.Note,
	)
	if err != nil {
		return nil, err
	}
	if lastSeen.Valid {
		a.LastSeen = &lastSeen.Time
	}
	return &a, nil
}

func (s *Store) AgentMarkOffline(ctx context.Context, olderThan time.Duration) (int64, error) {
	threshold := time.Now().Add(-olderThan)
	res, err := s.db.ExecContext(ctx, `
		UPDATE agents
		SET    is_online = 0
		WHERE  is_online = 1 AND last_seen < ?`,
		threshold,
	)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return n, nil
}
func (s *Store) AgentResetToken(ctx context.Context, agentID, userID, newToken string) error {
	// 1. Evict any old tokens for this agent from the cache
	s.cache.Range(func(key, val any) bool {
		if val.(*store.AgentIdentity).AgentID == agentID {
			s.cache.Delete(key)
			return false
		}
		return true
	})

	// 2. Update the token in DB
	res, err := s.db.ExecContext(ctx,
		`UPDATE agents SET token = ? WHERE agent_id = ? AND user_id = ?`,
		newToken, agentID, userID,
	)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}
func (s *Store) AgentListManagement(ctx context.Context, userID string) ([]store.AgentManagement, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT a.agent_id, a.agent_type, a.nickname, a.token, IFNULL(b.public_key, '')
		FROM agents a
		LEFT JOIN beszel_ssh b ON a.agent_id = b.agent_id
		WHERE a.user_id = ?
		ORDER BY a.nickname ASC`,
		userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []store.AgentManagement
	for rows.Next() {
		var a store.AgentManagement
		if err := rows.Scan(&a.AgentID, &a.AgentType, &a.Nickname, &a.Token, &a.BeszelPublicKey); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	if out == nil {
		out = []store.AgentManagement{}
	}
	return out, rows.Err()
}

func (s *Store) AgentIncrementTraffic(ctx context.Context, agentID, userID string, rx, tx uint64, disks []store.DiskDelta) error {
	txConn, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer txConn.Rollback()

	// Update network stats on the agent
	_, err = txConn.ExecContext(ctx, `
		UPDATE agents SET
			total_rx_bytes = total_rx_bytes + ?,
			total_tx_bytes = total_tx_bytes + ?
		WHERE agent_id = ? AND user_id = ?`,
		rx, tx, agentID, userID,
	)
	if err != nil {
		return err
	}

	// Update disk odometers
	for _, d := range disks {
		_, err = txConn.ExecContext(ctx, `
			INSERT INTO agent_disk_odometers (agent_id, path, read_bytes, write_bytes)
			VALUES (?, ?, ?, ?)
			ON CONFLICT(agent_id, path) DO UPDATE SET
				read_bytes  = read_bytes + excluded.read_bytes,
				write_bytes = write_bytes + excluded.write_bytes`,
			agentID, d.Path, d.ReadBytes, d.WriteBytes,
		)
		if err != nil {
			return err
		}
	}

	return txConn.Commit()
}
