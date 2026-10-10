package sqlite

import (
	nm "certainstats/internal/networkmonitor"
	"certainstats/internal/store"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
)

func sameGroupConfig(a, b nm.Config) bool {
	return a.Target == b.Target && a.Protocol == b.Protocol && a.Port == b.Port && a.Server == b.Server && a.Interval == b.Interval
}

func groupSnapshot(ctx context.Context, q interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}, userID, target string) (*store.NetworkGroup, error) {
	rows, err := q.QueryContext(ctx, monitorSelect+`WHERE m.user_id = ? AND m.target = ? AND m.archived_at IS NULL ORDER BY m.monitor_id`, userID, target)
	if err != nil {
		return nil, err
	}
	items, err := scanMonitors(rows)
	if err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, sql.ErrNoRows
	}
	type revisionItem struct {
		ID        string
		Config    nm.Config
		Enabled   bool
		UpdatedAt string
	}
	revision := make([]revisionItem, 0, len(items))
	agents := map[string]bool{}
	for _, m := range items {
		if !sameGroupConfig(items[0].Config, m.Config) || agents[m.AgentID] {
			return nil, fmt.Errorf("%w: this target contains inconsistent checks", nm.ErrConflict)
		}
		agents[m.AgentID] = true
		revision = append(revision, revisionItem{m.ID, m.Config, m.Enabled, m.UpdatedAt.UTC().Format("2006-01-02T15:04:05.999999999Z07:00")})
	}
	encoded, err := json.Marshal(revision)
	if err != nil {
		return nil, err
	}
	return &store.NetworkGroup{Items: items, Revision: fmt.Sprintf("%x", sha256.Sum256(encoded))}, nil
}

func (s *Store) NetworkGroup(ctx context.Context, userID, target string) (*store.NetworkGroup, error) {
	return groupSnapshot(ctx, s.db, userID, target)
}

// Individual writes cannot introduce mixed settings or duplicate probers.
func checkGroupPolicy(ctx context.Context, tx *sql.Tx, userID, agentID, exceptID string, c nm.Config) error {
	rows, err := tx.QueryContext(ctx, monitorSelect+`WHERE m.user_id = ? AND m.target = ? AND m.monitor_id <> ? AND m.archived_at IS NULL`, userID, c.Target, exceptID)
	if err != nil {
		return err
	}
	items, err := scanMonitors(rows)
	if err != nil {
		return err
	}
	for _, m := range items {
		if m.AgentID == agentID {
			return fmt.Errorf("%w: this agent already probes the target", nm.ErrConflict)
		}
		if !sameGroupConfig(m.Config, c) {
			return fmt.Errorf("%w: edit the group to change shared probe settings", nm.ErrConflict)
		}
	}
	return nil
}

func (s *Store) NetworkEditGroup(ctx context.Context, userID string, edit store.NetworkGroupEdit) (*store.NetworkGroupChange, error) {
	c := edit.Config
	if err := c.Normalize(); err != nil {
		return nil, err
	}
	if len(edit.AgentIDs) == 0 || len(edit.AgentIDs) > nm.MaxAgentsPerCreate {
		return nil, nm.Invalid(fmt.Sprintf("select between 1 and %d agents", nm.MaxAgentsPerCreate))
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	before, err := groupSnapshot(ctx, tx, userID, edit.Target)
	if err != nil {
		return nil, err
	}
	if edit.Revision == "" || before.Revision != edit.Revision {
		return nil, fmt.Errorf("%w: group changed; reload it before saving", nm.ErrConflict)
	}
	if c.Target != edit.Target {
		var count int
		if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM network_monitors WHERE user_id = ? AND target = ? AND archived_at IS NULL`, userID, c.Target).Scan(&count); err != nil {
			return nil, err
		}
		if count != 0 {
			return nil, fmt.Errorf("%w: the destination target already has a group", nm.ErrConflict)
		}
	}
	oldByAgent := map[string]nm.Monitor{}
	for _, m := range before.Items {
		oldByAgent[m.AgentID] = m
	}
	selected := map[string]bool{}
	for _, id := range edit.AgentIDs {
		if selected[id] {
			return nil, nm.Invalid("duplicate agent selection")
		}
		selected[id] = true
		old, exists := oldByAgent[id]
		enabled := !exists || old.Enabled
		if edit.Enabled != nil {
			enabled = *edit.Enabled
		}
		// Existing unsupported checks can be removed or paused. New checks must be supported.
		if !exists || enabled {
			agent, e := s.loadMonitorAgent(ctx, tx, id, userID)
			if e != nil {
				return nil, e
			}
			if e = nm.CheckCapabilities(id, c, agent.capabilities); e != nil {
				return nil, e
			}
		}
	}
	for _, m := range before.Items {
		if selected[m.AgentID] {
			continue
		}
		if err = archiveGroupMonitor(ctx, tx, m.ID); err != nil {
			return nil, err
		}
		if err = bumpSync(ctx, tx, userID, m.AgentID); err != nil {
			return nil, err
		}
	}
	ordered := make([]string, 0, len(edit.AgentIDs))
	for _, id := range edit.AgentIDs {
		if _, exists := oldByAgent[id]; exists {
			ordered = append(ordered, id)
		}
	}
	for _, id := range edit.AgentIDs {
		if _, exists := oldByAgent[id]; !exists {
			ordered = append(ordered, id)
		}
	}
	for _, id := range ordered {
		old, exists := oldByAgent[id]
		enabled := !exists || old.Enabled
		if edit.Enabled != nil {
			enabled = *edit.Enabled
		}
		if exists {
			if old.Target != c.Target || old.Protocol != c.Protocol || old.Port != c.Port || old.Server != c.Server {
				err = replaceMonitor(ctx, tx, &old, c, enabled)
			} else if old.Interval != c.Interval || old.Enabled != enabled {
				err = editMonitor(ctx, tx, old.ID, c.Interval, enabled)
			}
			if err != nil {
				return nil, err
			}
			if err = bumpSync(ctx, tx, userID, id); err != nil {
				return nil, err
			}
		} else {
			if _, err = s.createMonitor(ctx, tx, userID, id, c, enabled); err != nil {
				return nil, err
			}
		}
	}
	after, err := groupSnapshot(ctx, tx, userID, c.Target)
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return &store.NetworkGroupChange{NetworkGroup: *after, Before: before.Items}, nil
}

func archiveGroupMonitor(ctx context.Context, tx *sql.Tx, id string) error {
	if err := closeIncidents(ctx, tx, id, ""); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `UPDATE network_monitors SET archived_at = CURRENT_TIMESTAMP, enabled = 0, updated_at = CURRENT_TIMESTAMP WHERE monitor_id = ?`, id)
	return err
}
