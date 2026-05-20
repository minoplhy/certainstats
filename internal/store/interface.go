package store

import (
	"certainstats/internal/base"
	c "certainstats/internal/base/alert"
	baseresponse "certainstats/internal/base/response"
	"certainstats/internal/dashboard/accessrules"
	"context"
	"time"
)

type AgentStore interface {
	// GetByToken resolves a raw token to its owner identity.
	// Implementations should cache this — it is called on every submit.
	AgentGetByToken(ctx context.Context, token string) (*AgentIdentity, error)

	// UpsertDetails writes a full hardware snapshot (sent on boot/change).
	AgentUpsertDetails(ctx context.Context, d Agent) error

	// UpdateHeartbeat refreshes last_seen and is_online only.
	AgentUpdateHeartbeat(ctx context.Context, agentID, userID string) error

	// List returns all agents owned by userID, online-first.
	AgentList(ctx context.Context, userID string) ([]Agent, error)

	// ListManagement returns all agents with tokens and SSH keys for bulk management.
	AgentListManagement(ctx context.Context, userID string) ([]AgentManagement, error)

	// Provision inserts a new pre-authorised agent row.
	AgentProvision(ctx context.Context, agentID, userID, token, nickname, agentType string) error

	// Rename updates the human-readable nickname.
	AgentRename(ctx context.Context, agentID, userID, nickname string) error

	// Delete removes the agent and invalidates any cached identity.
	AgentDelete(ctx context.Context, agentID, userID string) error

	// GetByID returns the full agent row for a given agent_id + user_id.
	AgentGetByID(ctx context.Context, agentID, userID string) (*Agent, error)

	// MarkOffline sets is_online=0 for agents not seen within olderThan.
	// Returns the number of agents marked offline.
	AgentMarkOffline(ctx context.Context, olderThan time.Duration) (int64, error)

	// AgentResetToken updates the authentication token for an agent.
	AgentResetToken(ctx context.Context, agentID, userID string, newToken string) error

	// AgentIncrementTraffic atomically increments traffic stats (rx, tx, and per-disk odometers).
	AgentIncrementTraffic(ctx context.Context, agentID, userID string, rx, tx uint64, disks []DiskDelta) error

	// BeszelSSHGet retrieves per-agent SSH keys for Beszel compatibility.
	BeszelSSHGet(ctx context.Context, agentID, userID string) (*BeszelSSH, error)

	// BeszelSSHSave persists per-agent SSH keys.
	BeszelSSHSave(ctx context.Context, ssh BeszelSSH, userID string) error
}

// SessionStore handles web session lifecycle.
type SessionStore interface {
	SessionCreate(ctx context.Context, s Session) error
	SessionGet(ctx context.Context, token string) (*Session, error)
	SessionDelete(ctx context.Context, token string) error
	SessionDeleteExpired(ctx context.Context) error
	SessionUpdateActivity(ctx context.Context, token string, lastConnected time.Time) error
	SessionListByUser(ctx context.Context, userID string) ([]Session, error)
	SessionDeleteOther(ctx context.Context, userID string, currentToken string) error
}

type AlertsStore interface {
	AlertCreate(ctx context.Context, d Alert) error
	AlertList(ctx context.Context, userID string) ([]Alert, error)
	AlertGetInfo(ctx context.Context, alertID string, userID string) (Alert, error)

	AlertAddAgents(ctx context.Context, alertID string, AgentsID []string) error
	AlertRemoveAgents(ctx context.Context, alertID string, AgentsID []string) error

	AlertUpdate(ctx context.Context, d Alert, newAgents []string) error
	AlertDelete(ctx context.Context, alertID string, userID string) error

	AlertTrigger(ctx context.Context, d Alert, agentID string, historyID string, violationValue float64, notifStatus string) error
	AlertResolve(ctx context.Context, d Alert, agentID string) error

	GetActiveAlertsWithState(ctx context.Context) ([]Alert, map[string]AgentInfo, error)

	AlertHistoryListPaginated(ctx context.Context, userID string, page, limit int) ([]c.AlertHistory, int, error)
}

// UserStore handles user account lookups and updates.
type UserStore interface {
	GetByUsername(ctx context.Context, username string) (*User, error)
	GetByID(ctx context.Context, userID string) (*User, error)
	UpdatePassword(ctx context.Context, userID string, passwordHash string) error
}

// DashboardStore handles public dashboard configuration and data.
type DashboardStore interface {
	DashboardCreate(ctx context.Context, d Dashboard) error
	DashboardList(ctx context.Context, userID string) ([]Dashboard, error)
	DashboardGetBySlug(ctx context.Context, slug string) (*Dashboard, error)
	DashboardGetInfo(ctx context.Context, dashboard_id string, userID string) (Dashboard, error)
	DashboardAddAgents(ctx context.Context, d Dashboard, a []baseresponse.CreateDashboardReqAgent) error
	DashboardUpdate(ctx context.Context, d Dashboard, newAgents []baseresponse.CreateDashboardReqAgent) error
	DashboardDelete(ctx context.Context, dashboard_id string, userID string) error

	// GetPublicAgents returns alias-resolved, rule-filtered agent rows.
	// rule must be pre-validated via accessrules.Validate() before calling.
	DashboardGetPublicAgents(ctx context.Context, slug string, rule accessrules.AccessRule) ([]PublicAgent, error)

	DashboardGetAgents(ctx context.Context, dashboard_id string, userID string) ([]PublicAgentIdentity, error)

	DashboardFindAgentbyPublicID(ctx context.Context, dashboardID string, publicAgentID string) (base.FindAgentByPublicID, error)
	DashboardGetPulseConfig(ctx context.Context, dashboardID string) (*Dashboard, []PublicAgentIdentity, error)
}

type DiskDelta struct {
	Path       string
	ReadBytes  uint64
	WriteBytes uint64
}
