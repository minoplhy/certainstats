package routine

import (
	"certainstats/internal/metrics"
	"certainstats/internal/networkservice"
	"certainstats/internal/store/sqlite"
	"certainstats/internal/ws"

	"github.com/prometheus/prometheus/tsdb"
)

type Routine struct {
	Network     *networkservice.Service
	Store       *sqlite.Store
	TSDB        *tsdb.DB
	WS          *ws.Manager
	Cache       *metrics.RealtimeCache
	Broadcaster *ws.AgentBroadcaster
	// Deliver sends a queued alert attempt; nil means alertdelivery.Go.
	Deliver func(eventID string)

	beszelTicks map[string]int // track polling count per agent for periodic full refresh
}
