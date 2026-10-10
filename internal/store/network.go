package store

import (
	"certainstats/internal/agentmeta"
	nm "certainstats/internal/networkmonitor"
	"context"
)

// Runtime returns the agent's reported runtime metadata for capability resolution.
func (a *Agent) Runtime() agentmeta.Runtime {
	return agentmeta.Runtime{
		AgentVersion:         a.AgentVersion,
		ProtocolVersion:      a.ProtocolVersion,
		VersionSource:        a.AgentVersionSource,
		ReportedCapabilities: a.ReportedCapabilities,
	}
}

// NetworkListFilter narrows a monitor list. Empty fields do not filter.
type NetworkListFilter struct {
	AgentID     string
	Query       string
	Target      string
	TargetExact string
	Protocol    string
	State       string
}

// NetworkGroup is a snapshot of all current checks for an exact target.
type NetworkGroup struct {
	Items    []nm.Monitor
	Revision string
}

type NetworkGroupEdit struct {
	Target   string
	Revision string
	Config   nm.Config
	AgentIDs []string
	Enabled  *bool
}

type NetworkGroupChange struct {
	NetworkGroup
	Before []nm.Monitor
}

// NetworkMonitorStore persists monitor configuration, readings and alert state.
type NetworkMonitorStore interface {
	NetworkGroup(ctx context.Context, userID, target string) (*NetworkGroup, error)
	NetworkEditGroup(ctx context.Context, userID string, edit NetworkGroupEdit) (*NetworkGroupChange, error)

	// NetworkList returns one page of the owner's monitors and the total match count.
	NetworkList(ctx context.Context, userID string, filter NetworkListFilter, page, limit int) ([]nm.Monitor, int, error)

	// NetworkTargets returns distinct owned targets matching node, protocol and state.
	NetworkTargets(ctx context.Context, userID string, filter NetworkListFilter) ([]string, error)

	// NetworkLive returns every non-archived monitor for the owner, with state
	// derived from the given agent list, for the live WebSocket pulse.
	NetworkLive(ctx context.Context, userID string, agents []Agent) ([]nm.Monitor, error)

	// NetworkGet returns one owned monitor with its derived state.
	NetworkGet(ctx context.Context, userID, monitorID string) (*nm.Monitor, error)

	// NetworkCreate creates the same configuration on each agent atomically.
	NetworkCreate(ctx context.Context, userID string, agentIDs []string, c nm.Config, enabled bool) ([]nm.Monitor, error)

	// NetworkUpdate edits a monitor. Identity changes archive it and create a replacement.
	NetworkUpdate(ctx context.Context, userID, monitorID string, c nm.Config, enabled bool) (*nm.Monitor, error)

	// NetworkArchive archives a monitor and closes its open incidents.
	NetworkArchive(ctx context.Context, userID, monitorID string) error

	// NetworkConfigs returns the agent's enabled configurations and sync generations.
	NetworkConfigs(ctx context.Context, userID, agentID string) ([]nm.Config, nm.Sync, error)

	// NetworkAck records a synchronization attempt for the given generation.
	// A non-empty message records a failure.
	NetworkAck(ctx context.Context, userID, agentID string, generation int64, message string) error

	// NetworkBegin journals the accepted results of a batch before TSDB writes.
	NetworkBegin(ctx context.Context, b nm.Batch) (*nm.Batch, error)

	// NetworkPending returns uncommitted batches, for one agent or all when agentID is empty.
	NetworkPending(ctx context.Context, agentID string) ([]nm.Batch, error)

	// NetworkComplete stores the latest readings and removes the journal entry.
	NetworkComplete(ctx context.Context, b nm.Batch) error

	// NetworkEvaluate evaluates loss rules and returns queued notification event IDs.
	NetworkEvaluate(ctx context.Context, agentID string) ([]string, error)
}
