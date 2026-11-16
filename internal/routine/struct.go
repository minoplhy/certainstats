package routine

import (
	"certainstats/internal/metrics"
	"certainstats/internal/networkservice"
	"certainstats/internal/store/sqlite"
	"certainstats/internal/ws"
	"context"
	"time"

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
	// Tick runs on every central tick, for checks outside the alert rules.
	Tick []func(context.Context)
	// JournalRetention is how long completed ingestion batches are kept to
	// detect resent batches; zero keeps them forever.
	JournalRetention time.Duration
	// MemoryLog adds an hourly memory line to the maintenance log (MEMORY_LOG=true).
	MemoryLog bool
	// MemoryStats supplies extra counts for the memory line, from packages
	// routine does not import.
	MemoryStats func() map[string]int
	// Deliver sends a queued alert attempt; nil means alertdelivery.Go.
	Deliver func(eventID string)

	beszelTicks map[string]int // track polling count per agent for periodic full refresh
}
