package agent

import (
	api_response "certainstats/internal/response"
	"database/sql"
	"math"
	"net/http"
	"strings"

	a "certainstats/internal/base/agent"
	ctx "certainstats/internal/context"
	"certainstats/internal/store"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/tsdb"
)

func RevokeAgentHandler(agent store.AgentStore, tdb *tsdb.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {

		userID := r.Context().Value(ctx.UserIDKey).(string)
		agentID := r.URL.Query().Get("agent_id")
		if agentID == "" {
			api_response.Error(w, http.StatusBadRequest, "Missing agent_id")
			return
		}

		err := agent.AgentDelete(r.Context(), agentID, userID)
		if err != nil {
			api_response.Error(w, http.StatusInternalServerError, "Database error")
			return
		}

		if err == sql.ErrNoRows {
			api_response.Error(w, http.StatusNotFound, "Agent not found or unauthorized")
			return
		}

		// 1. Evict from in-memory token cache so new submits are rejected immediately.
		ctx.DeviceCache.Range(func(key, value any) bool {
			if v, ok := value.(*a.DeviceIdentity); ok && v.AgentID == agentID {
				ctx.DeviceCache.Delete(key)
				return false
			}
			return true
		})

		// 2. Evict from PublicAgentCache and collect public agent IDs
		var publicAgentIDs []string
		ctx.PublicAgentCache.Range(func(key, value any) bool {
			if v, ok := value.(*ctx.PublicAgentCacheEntry); ok && v.Agent.RealAgentID == agentID {
				if kStr, ok := key.(string); ok {
					parts := strings.Split(kStr, "_")
					if len(parts) > 1 {
						publicAgentIDs = append(publicAgentIDs, parts[1])
					}
				}
				ctx.PublicAgentCache.Delete(key)
			}
			return true
		})

		// 3. Evict from MetricsCache (both public and private cached payloads)
		ctx.MetricsCache.Range(func(key, value any) bool {
			kStr, ok := key.(string)
			if !ok {
				return true
			}
			// Private keys: priv_userID_agentID_metric_hours
			if strings.HasPrefix(kStr, "priv_") && strings.Contains(kStr, "_"+agentID+"_") {
				ctx.MetricsCache.Delete(key)
				return true
			}
			// Public keys: pub_dashboardID_publicAgentID_metric_hours
			for _, pubID := range publicAgentIDs {
				if strings.HasPrefix(kStr, "pub_") && strings.Contains(kStr, "_"+pubID+"_") {
					ctx.MetricsCache.Delete(key)
					break
				}
			}
			return true
		})

		// 4. PURGE ALL METRICS from TSDB
		if tdb != nil {
			matchers := []*labels.Matcher{
				labels.MustNewMatcher(labels.MatchEqual, "agent_id", agentID),
			}
			// Delete all data for this agent across all metrics
			_ = tdb.Delete(r.Context(), math.MinInt64, math.MaxInt64, matchers...)
		}

		w.Header().Set("Content-Type", "application/json")
		api_response.JSON(w, http.StatusOK, map[string]string{
			"status":  "success",
			"message": "Agent permanently revoked and metrics purged",
		})
	}
}
