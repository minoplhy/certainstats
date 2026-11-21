package knot

import (
	"context"

	"certainstats/internal/collector/type/knot/wire"
)

// Engines for dnstap rows, chosen with KNOT_ENGINE on the server.
const (
	// EngineSQL keeps dnstap rows in knot.db's bucket tables (the default).
	EngineSQL = "sql"
	// EngineTopN keeps them in the block engine of internal/tblk, in its own directory.
	EngineTopN = "topn"
)

// dnstapStore holds dnstap rows: writes from collectors, the hourly
// maintenance and the drill-down reads. Store and TopN implement it.
type dnstapStore interface {
	SaveDnstap(ctx context.Context, userID, collectorID string, d wire.Dnstap, now int64) error
	Dnstap(ctx context.Context, q Query, dim string, limit int) (Breakdown, error)
	Rollup(ctx context.Context, now int64) (int, error)
	Prune(ctx context.Context, keep Retention, now int64, users []string) (PruneResult, error)
}

var (
	_ dnstapStore = (*Store)(nil)
	_ dnstapStore = (*TopN)(nil)
)

// dnstapRows is the store that holds dnstap rows: TopN when set, else knot.db.
func (t *Type) dnstapRows() dnstapStore {
	if t.TopN != nil {
		return t.TopN
	}
	return t.Store
}
