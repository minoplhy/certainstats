package routine

import (
	"certainstats/internal/metrics"
	"certainstats/internal/networkservice"
	"certainstats/internal/store/sqlite"
	"certainstats/internal/ws"
	"context"

	"github.com/prometheus/prometheus/tsdb"
)

type Routine struct {
	Network     *networkservice.Service
	Store       *sqlite.Store
	TSDB        *tsdb.DB
	WS          *ws.Manager
	Cache       *metrics.RealtimeCache
	Broadcaster *ws.AgentBroadcaster
	// Maintenance runs on the hourly cleanup, for stores outside the main database.
	Maintenance []func(context.Context)
	// Deliver sends a queued alert attempt; nil means alertdelivery.Go.
	Deliver func(eventID string)

	beszelTicks map[string]int // track polling count per agent for periodic full refresh
}
