package metrics

import (
	apiresponse "certainstats/internal/response"

	"context"
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"time"

	m "certainstats/internal/base/metrics"
	ctx "certainstats/internal/context"
	accessrules "certainstats/internal/dashboard/accessrules"
	"certainstats/internal/store"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/storage"
	"github.com/prometheus/prometheus/tsdb"
	"github.com/prometheus/prometheus/tsdb/chunkenc"
)

// globalTSDBQuerySemaphore limits concurrent raw TSDB reads to prevent I/O
// saturation under heavy load or scraping attacks.
var globalTSDBQuerySemaphore = make(chan struct{}, 32)

// acquireTSDB blocks until a TSDB slot is available and returns a release func.
func acquireTSDB() func() {
	globalTSDBQuerySemaphore <- struct{}{}
	return func() { <-globalTSDBQuerySemaphore }
}

// MetricsQueryHandler serves the private (authenticated) metrics endpoint.
func MetricsQueryHandler(tsb *tsdb.DB, cache *RealtimeCache) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {

		userID := r.Context().Value(ctx.UserIDKey).(string)
		agentID := r.URL.Query().Get("agent_id")
		metricName := r.URL.Query().Get("metric")

		if agentID == "" || metricName == "" {
			apiresponse.Error(w, http.StatusBadRequest, "Missing agent_id or metric")
			return
		}
		if !allowedMetrics[metricName] {
			apiresponse.Error(w, http.StatusBadRequest, "Unknown metric")
			return
		}

		tr, ok := parsePrivateTimeRange(r)
		if !ok {
			apiresponse.Error(w, http.StatusBadRequest, "Invalid time range")
			return
		}

		// Fast path: serve from the 24-hour sliding-window memory cache.
		if cache != nil {
			if served := serveFromWindowCache(w, cache, agentID, metricName, tr); served {
				return
			}
		}

		// Slow path: query the TSDB.
		release := acquireTSDB()
		defer release()

		querier, err := tsb.Querier(tr.StartMs, tr.EndMs)
		if err != nil {
			log.Printf("TSDB querier: %v", err)
			apiresponse.Error(w, http.StatusInternalServerError, "Database error")
			return
		}
		defer querier.Close()

		// Labels written by SubmitHandler use "user_id" — must match exactly.
		matchers := []*labels.Matcher{
			labels.MustNewMatcher(labels.MatchEqual, "__name__", metricName),
			labels.MustNewMatcher(labels.MatchEqual, "user_id", userID),
			labels.MustNewMatcher(labels.MatchEqual, "agent_id", agentID),
		}

		allSeries := queryTSDB(querier, matchers, metricName, tr)

		writeJSON(w, metricName, allSeries)
	}
}

// PublicMetricsHandler serves the public dashboard metrics endpoint.
func PublicMetricsHandler(tsb *tsdb.DB, dashboard store.DashboardStore, cache *RealtimeCache) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {

		dashboardID := r.URL.Query().Get("dashboard_id")
		publicAgentID := r.URL.Query().Get("agent_id")
		metricName := r.URL.Query().Get("metric")

		if dashboardID == "" || publicAgentID == "" || metricName == "" {
			apiresponse.Error(w, http.StatusBadRequest, "Missing dashboard_id, agent_id or metric")
			return
		}

		// 1. Translate public agent ID → real agent ID and load ACL rules.
		agent, err := dashboard.DashboardFindAgentbyPublicID(r.Context(), dashboardID, publicAgentID)
		if err != nil {
			apiresponse.Error(w, http.StatusForbidden, "Forbidden")
			return
		}

		rules, err := accessrules.ParseRules(agent.RulesJSON)
		if err != nil {
			apiresponse.Error(w, http.StatusInternalServerError, "Internal server error")
			return
		}

		if _, allowed := rules[accessrules.PUBLIC].MetricSet()[metricName]; !allowed {
			apiresponse.Error(w, http.StatusForbidden, "Forbidden")
			return
		}

		maxHoursAllowed := rules[accessrules.PUBLIC].MaxDays * uint(24)

		// 2. Resolve the time range (enforces MaxDays ACL).
		tr, cacheKey, ok := parsePublicTimeRange(r, maxHoursAllowed)
		if !ok {
			apiresponse.Error(w, http.StatusBadRequest, "Forbidden: requested timeframe exceeds dashboard limit")
			return
		}

		// 3. Check the compiled-payload cache (fastest path).
		if val, hit := ctx.MetricsCache.Load(cacheKey); hit {
			entry := val.(*ctx.CacheEntry)
			if time.Now().Before(entry.ExpiresAt) {
				w.Header().Set("Content-Type", "application/json")
				w.Write(entry.Payload)
				return
			}
		}

		// 4. Try the sliding-window memory cache.
		if cache != nil {
			if series, served := buildFromWindowCache(cache, agent.RealAgentID, metricName, tr); served {
				payload, _ := json.Marshal(map[string]any{
					"metric": metricName,
					"series": series,
				})
				ctx.MetricsCache.Store(cacheKey, &ctx.CacheEntry{
					Payload:   payload,
					ExpiresAt: time.Now().Add(60 * time.Second),
				})
				w.Header().Set("Content-Type", "application/json")
				w.Write(payload)
				return
			}
		}

		// 5. Slow path: query the TSDB.
		release := acquireTSDB()
		defer release()

		querier, err := tsb.Querier(tr.StartMs, tr.EndMs)
		if err != nil {
			apiresponse.Error(w, http.StatusInternalServerError, "TSDB error")
			return
		}
		defer querier.Close()

		// CRITICAL: enforce both realAgentID and ownerID so data cannot leak
		// between tenants.
		matchers := []*labels.Matcher{
			labels.MustNewMatcher(labels.MatchEqual, "__name__", metricName),
			labels.MustNewMatcher(labels.MatchEqual, "agent_id", agent.RealAgentID),
			labels.MustNewMatcher(labels.MatchEqual, "user_id", agent.OwnerID),
		}

		allSeries := queryTSDB(querier, matchers, metricName, tr)

		payload, _ := json.Marshal(map[string]any{
			"metric": metricName,
			"series": allSeries,
		})
		ctx.MetricsCache.Store(cacheKey, &ctx.CacheEntry{
			Payload:   payload,
			ExpiresAt: time.Now().Add(60 * time.Second),
		})
		w.Header().Set("Content-Type", "application/json")
		w.Write(payload)
	}
}

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

// serveFromWindowCache checks the sliding-window cache and writes the JSON
// response directly if served.  Returns true if the response was written.
func serveFromWindowCache(w http.ResponseWriter, cache *RealtimeCache, agentID, metricName string, tr TimeRange) bool {
	series, ok := buildFromWindowCache(cache, agentID, metricName, tr)
	if !ok {
		return false
	}
	writeJSON(w, metricName, series)
	return true
}

// buildFromWindowCache constructs the series slice from the sliding-window
// cache.  Returns (series, true) on a full cache hit, (nil, false) on any miss.
func buildFromWindowCache(cache *RealtimeCache, agentID, metricName string, tr TimeRange) ([]map[string]any, bool) {
	paths := diskPaths(cache, agentID, metricName)
	if paths == nil {
		return nil, false
	}

	step := stepForRange(tr.EndMs - tr.StartMs)
	isDelta := isDeltaMetric(metricName)

	series := make([]map[string]any, 0, len(paths))
	for _, path := range paths {
		pts, hit := cache.GetTimeseries(agentID, metricName, path, tr.StartMs, tr.EndMs)
		if !hit {
			return nil, false
		}
		agg := downsamplePoints(pts, step, isDelta)

		labelsMap := map[string]string{}
		if path != "" {
			labelsMap["path"] = path
		}
		series = append(series, map[string]any{
			"labels": labelsMap,
			"data":   agg,
		})
	}
	return series, true
}

// diskPaths returns the set of disk mount paths for disk metrics, or []string{""}
// for single-series metrics.  Returns nil when the agent snapshot is not yet
// populated (cache miss — agent has never submitted since startup).
func diskPaths(cache *RealtimeCache, agentID, metricName string) []string {
	if !strings.HasPrefix(metricName, "agent_disk_") {
		return []string{""}
	}
	snapshot, ok := cache.Get(agentID)
	if !ok {
		return nil // no snapshot yet — fall back to TSDB
	}
	paths := make([]string, 0, len(snapshot.Disks))
	for _, d := range snapshot.Disks {
		paths = append(paths, d.Path)
	}
	if len(paths) == 0 {
		return nil
	}
	return paths
}

// queryTSDB runs the Prometheus TSDB query and returns the downsampled series.
func queryTSDB(querier storage.Querier, matchers []*labels.Matcher, metricName string, tr TimeRange) []map[string]any {
	seriesSet := querier.Select(context.Background(), false, nil, matchers...)

	step := stepForRange(tr.EndMs - tr.StartMs)
	isDelta := isDeltaMetric(metricName)

	var allSeries []map[string]any

	for seriesSet.Next() {
		series := seriesSet.At()

		rawMap := series.Labels().Map()
		labelsMap := map[string]string{}
		if val, ok := rawMap["path"]; ok {
			labelsMap["path"] = val
		}

		pts := readTSDBSeries(series.Iterator(nil), tr, step, isDelta)

		allSeries = append(allSeries, map[string]any{
			"labels": labelsMap,
			"data":   pts,
		})
	}
	if err := seriesSet.Err(); err != nil {
		log.Printf("TSDB series iteration error for %s: %v", metricName, err)
	}

	if allSeries == nil {
		allSeries = []map[string]any{}
	}
	return allSeries
}

// readTSDBSeries converts a raw TSDB chunk iterator into downsampled DataPoints.
func readTSDBSeries(it chunkenc.Iterator, tr TimeRange, step int64, isDelta bool) []m.DataPoint {
	var raw []TimeseriesPoint
	for it.Next() != chunkenc.ValNone {
		ts, val := it.At()
		if ts >= tr.StartMs && ts <= tr.EndMs {
			raw = append(raw, TimeseriesPoint{Timestamp: ts, Value: val})
		}
	}
	if err := it.Err(); err != nil {
		log.Printf("TSDB iterator error: %v", err)
	}
	return downsamplePoints(raw, step, isDelta)
}

// writeJSON serialises the standard metrics response envelope and writes it to w.
func writeJSON(w http.ResponseWriter, metricName string, allSeries []map[string]any) {
	payload, _ := json.Marshal(map[string]any{
		"metric": metricName,
		"series": allSeries,
	})
	w.Header().Set("Content-Type", "application/json")
	w.Write(payload)
}


