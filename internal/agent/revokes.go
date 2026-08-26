package agent

import (
	api_response "certainstats/internal/response"
	"database/sql"
	"math"
	"net/http"

	ctx "certainstats/internal/context"
	"certainstats/internal/metrics"
	"certainstats/internal/store"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/tsdb"
)

func RevokeAgentHandler(agent store.AgentStore, tdb *tsdb.DB, cache *metrics.RealtimeCache) http.HandlerFunc {
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

		// 0. Evict from in-memory telemetry cache
		if cache != nil {
			cache.Delete(agentID)
		}

		// 1. Evict from in-memory caches (DeviceCache, PublicAgentCache, MetricsCache)
		ctx.InvalidateAgent(agentID)

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
