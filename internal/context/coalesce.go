package context

import (
	"certainstats/internal/security"
	"net/http"
	"sync"
	"time"
)

var buildMu sync.Mutex
var builds = make(map[string]chan struct{})
var coalescedBuilds uint64
var activeBuilds = make(chan struct{}, 32)
var publicWaiters = security.PublicWaitSlots

// BeginBuild elects one builder per isolated versioned key. Followers wait for
// its cache publication, then repeat permission validation in the handler.
func BeginBuild(w http.ResponseWriter, r *http.Request, key string, retry http.HandlerFunc) (func(), bool) {
	buildMu.Lock()
	if done := builds[key]; done != nil {
		coalescedBuilds++
		buildMu.Unlock()
		select {
		case publicWaiters <- struct{}{}:
		default:
			security.Reject(w, 503, "Build queue full")
			return nil, false
		}
		defer func() { <-publicWaiters }()
		timer := time.NewTimer(2 * time.Second)
		defer timer.Stop()
		select {
		case <-done:
			if _, ok := GetCacheEntry(&MetricsCache, key); !ok {
				if _, ok = GetCacheEntry(&DashboardHTMLCache, key); !ok {
					if _, ok = GetCacheEntry(&DashboardCache, key); !ok {
						security.Reject(w, 503, "Build failed; retry")
						return nil, false
					}
				}
			}
			retry(w, r)
		case <-r.Context().Done():
		case <-timer.C:
			security.Reject(w, 503, "Build wait timed out")
		}
		return nil, false
	}
	select {
	case activeBuilds <- struct{}{}:
	default:
		buildMu.Unlock()
		security.Reject(w, 503, "Build capacity unavailable")
		return nil, false
	}
	if !security.PublicLimits.Allow(security.ClientIP(r), true) {
		<-activeBuilds
		buildMu.Unlock()
		security.Reject(w, 429, "Build limit exceeded")
		return nil, false
	}
	done := make(chan struct{})
	builds[key] = done
	buildMu.Unlock()
	return func() {
		buildMu.Lock()
		<-activeBuilds
		delete(builds, key)
		close(done)
		buildMu.Unlock()
	}, true
}

func BuildStatistics() map[string]any {
	buildMu.Lock()
	defer buildMu.Unlock()
	return map[string]any{"coalesced": coalescedBuilds, "active": len(builds), "waiting": len(publicWaiters)}
}
