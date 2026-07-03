package main

import (
	"certainstats/internal/metrics"
	"certainstats/internal/store"
	"context"
	log "certainstats/internal/logger"
	"time"
)

func startHeartbeatSweeper(agents store.AgentStore, cache *metrics.RealtimeCache) {
	const (
		sweepInterval = 1 * time.Minute
		offlineAfter  = 3 * time.Minute
	)

	for {
		time.Sleep(sweepInterval)

		offlineIDs, err := agents.AgentMarkOffline(context.Background(), offlineAfter)
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

func startSessionSweeper(sessions store.SessionStore) {
	const sweepInterval = 15 * time.Minute

	for {
		time.Sleep(sweepInterval)

		err := sessions.SessionDeleteExpired(context.Background())
		if err != nil {
			log.Printf("session sweeper: %v", err)
			continue
		}
		log.Debugf("session sweeper: cleared expired sessions")
	}
}
