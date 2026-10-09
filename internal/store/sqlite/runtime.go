package sqlite

import (
	"certainstats/internal/agentmeta"
	"certainstats/internal/store"
	"context"
	"database/sql"
	"encoding/json"
	"time"
)

// runtimeColumns are scanned by runtimeScan, in this order.
const runtimeColumns = `agent_version, protocol_version, agent_version_source,
	agent_version_observed_at, reported_capabilities_json, capabilities_reported_at`

type queryRow interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// runtimeScan holds the nullable runtime columns of an agents row.
type runtimeScan struct {
	version, protocol, capabilities sql.NullString
	source                          sql.NullString
	observed, reported              sql.NullTime
}

func (r *runtimeScan) targets() []any {
	return []any{&r.version, &r.protocol, &r.source, &r.observed, &r.capabilities, &r.reported}
}

func (r *runtimeScan) runtime() (agentmeta.Runtime, error) {
	var out agentmeta.Runtime
	if r.version.Valid {
		out.AgentVersion = &r.version.String
	}
	if r.protocol.Valid {
		out.ProtocolVersion = &r.protocol.String
	}
	out.VersionSource = r.source.String
	if r.capabilities.Valid {
		var declaration agentmeta.Declaration
		if err := json.Unmarshal([]byte(r.capabilities.String), &declaration); err != nil {
			return out, err
		}
		out.ReportedCapabilities = &declaration
	}
	return out, nil
}

// apply copies the scanned runtime metadata onto an agent.
func (r *runtimeScan) apply(a *store.Agent) error {
	runtime, err := r.runtime()
	if err != nil {
		return err
	}
	a.AgentVersion = runtime.AgentVersion
	a.ProtocolVersion = runtime.ProtocolVersion
	a.AgentVersionSource = runtime.VersionSource
	a.ReportedCapabilities = runtime.ReportedCapabilities
	if r.observed.Valid {
		a.AgentVersionObservedAt = &r.observed.Time
	}
	if r.reported.Valid {
		a.CapabilitiesReportedAt = &r.reported.Time
	}
	return nil
}

// agentCapabilities resolves an agent's network capabilities through its provider.
func (s *Store) agentCapabilities(a *store.Agent) agentmeta.Capabilities {
	return s.providers().Capabilities(a.AgentType, a.Runtime())
}

// monitorAgent is the agent data a monitor mutation needs inside a transaction.
type monitorAgent struct {
	name         string
	capabilities agentmeta.Capabilities
}

func (s *Store) loadMonitorAgent(ctx context.Context, q queryRow, agentID, userID string) (*monitorAgent, error) {
	var agentType string
	var out monitorAgent
	var scan runtimeScan
	targets := append([]any{&out.name, &agentType}, scan.targets()...)
	err := q.QueryRowContext(ctx, `
		SELECT nickname, agent_type, `+runtimeColumns+`
		FROM   agents
		WHERE  agent_id = ? AND user_id = ?`,
		agentID, userID,
	).Scan(targets...)
	if err != nil {
		return nil, err
	}
	runtime, err := scan.runtime()
	if err != nil {
		return nil, err
	}
	out.capabilities = s.providers().Capabilities(agentType, runtime)
	return &out, nil
}

func (s *Store) AgentUpdateRuntime(ctx context.Context, agentID, userID string, r agentmeta.Runtime) error {
	if err := agentmeta.Validate(r); err != nil {
		return err
	}
	var capabilities any
	if r.ReportedCapabilities != nil {
		raw, err := json.Marshal(r.ReportedCapabilities)
		if err != nil {
			return err
		}
		capabilities = string(raw)
	}
	reported := r.AgentVersion != nil || r.ProtocolVersion != nil
	now := time.Now().UTC()
	// Missing values keep the previous observation.
	_, err := s.db.ExecContext(ctx, `
		UPDATE agents
		SET    agent_version              = COALESCE(?, agent_version),
		       protocol_version           = COALESCE(?, protocol_version),
		       agent_version_source       = CASE WHEN ? THEN ? ELSE agent_version_source END,
		       agent_version_observed_at  = CASE WHEN ? THEN ? ELSE agent_version_observed_at END,
		       reported_capabilities_json = COALESCE(?, reported_capabilities_json),
		       capabilities_reported_at   = CASE WHEN ? IS NOT NULL THEN ? ELSE capabilities_reported_at END
		WHERE  agent_id = ? AND user_id = ?`,
		r.AgentVersion, r.ProtocolVersion,
		reported, r.VersionSource,
		reported, now,
		capabilities,
		capabilities, now,
		agentID, userID,
	)
	return err
}

func (s *Store) AgentToken(ctx context.Context, agentID, userID string) (string, error) {
	var token string
	err := s.db.QueryRowContext(ctx,
		`SELECT token FROM agents WHERE agent_id = ? AND user_id = ?`,
		agentID, userID,
	).Scan(&token)
	return token, err
}
