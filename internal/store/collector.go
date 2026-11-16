package store

import "context"

// Collector is one installed certainstats-collector. It gathers data of one or
// more types (such as CrowdSec or Knot DNS) from the host it runs on. Times are Unix ms; zero is unknown.
type Collector struct {
	CollectorID      string          `json:"collector_id"`
	UserID           string          `json:"-"`
	Name             string          `json:"name"`
	AgentID          string          `json:"agent_id"`
	CreatedAt        int64           `json:"created_at"`
	FirstConnectedAt int64           `json:"first_connected_at"`
	LastSeenAt       int64           `json:"last_seen_at"`
	Version          string          `json:"version"`
	Types            []CollectorType `json:"types"`
}

// CollectorType is one type a collector may send. State holds the type's own
// small JSON state, such as a cursor.
type CollectorType struct {
	Type       string `json:"type"`
	Enabled    bool   `json:"enabled"`
	ReportedOn bool   `json:"reported_on"`
	Status     string `json:"status"`
	Error      string `json:"error"`
	State      string `json:"-"`
	UpdatedAt  int64  `json:"updated_at"`
}

// Type returns the collector's row for typ, if it has one.
func (c *Collector) Type(typ string) (CollectorType, bool) {
	for _, t := range c.Types {
		if t.Type == typ {
			return t, true
		}
	}
	return CollectorType{}, false
}

type CollectorStore interface {
	// CollectorCreate keeps the token, as agent tokens are kept, and its hash for lookups.
	CollectorCreate(ctx context.Context, c Collector, token, tokenHash string, types []string) error
	CollectorList(ctx context.Context, userID string) ([]Collector, error)
	CollectorGet(ctx context.Context, collectorID, userID string) (*Collector, error)
	// CollectorByTokenHash resolves a live (not revoked) collector from its token hash.
	CollectorByTokenHash(ctx context.Context, tokenHash string) (*Collector, error)
	CollectorUpdate(ctx context.Context, collectorID, userID string, name, agentID *string) error
	CollectorRevoke(ctx context.Context, collectorID, userID string) error
	CollectorResetToken(ctx context.Context, collectorID, userID, token, tokenHash string) error
	// CollectorToken returns the kept token, empty for collectors made before tokens were kept.
	CollectorToken(ctx context.Context, collectorID, userID string) (string, error)
	// CollectorSeen records a connection or keepalive; first marks the first connection.
	CollectorSeen(ctx context.Context, collectorID, version string, at int64) error
	CollectorNames(ctx context.Context, userID string) (map[string]string, error)
	CollectorTypeSetEnabled(ctx context.Context, collectorID, userID, typ string, enabled bool) error
	CollectorTypeReport(ctx context.Context, collectorID, typ string, reportedOn bool) error
	CollectorTypeStatus(ctx context.Context, collectorID, typ, status, message string) error
	CollectorTypeSaveState(ctx context.Context, collectorID, typ, state string) error
	// UserIDs lists every account, so stores in other files can drop rows of deleted users.
	UserIDs(ctx context.Context) ([]string, error)
}
