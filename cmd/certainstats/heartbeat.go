package main

import (
	log "certainstats/internal/logger"
	"certainstats/internal/metrics"
	"certainstats/internal/store"
	"context"
	"time"
)

func startHeartbeatSweeper(ctx context.Context, agents store.AgentStore, cache *metrics.RealtimeCache) {
	const (
		sweepInterval = 1 * time.Minute
		offlineAfter  = 3 * time.Minute
	)

	ticker := time.NewTicker(sweepInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}

		offlineIDs, err := agents.AgentMarkOffline(ctx, offlineAfter)
		if err != nil {
			log.Printf("heartbeat sweeper: %v", err)
			continue
		}
		if len(offlineIDs) > 0 {
			log.Debugf("sweeper: marked %d agent(s) offline", len(offlineIDs))
			for _, id := range offlineIDs {
				cache.Delete(id)
			}
		}
	}
}

func startSessionSweeper(ctx context.Context, sessions store.SessionStore) {
	const sweepInterval = 15 * time.Minute

	ticker := time.NewTicker(sweepInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}

		err := sessions.SessionDeleteExpired(ctx)
		if err != nil {
			log.Printf("session sweeper: %v", err)
			continue
		}
		log.Debugf("session sweeper: cleared expired sessions")
	}
}
