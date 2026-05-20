package routine

import (
	"certainstats/internal/metrics"
	"certainstats/internal/store/sqlite"
	"certainstats/internal/ws"

	"github.com/prometheus/prometheus/tsdb"
)

type Routine struct {
	Store       *sqlite.Store
	TSDB        *tsdb.DB
	WS          *ws.Manager
	Cache       *metrics.RealtimeCache
	Broadcaster *ws.AgentBroadcaster

	beszelTicks map[string]int // track polling count per agent for periodic full refresh
}
