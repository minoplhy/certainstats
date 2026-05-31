package metrics

import (
	apiresponse "certainstats/internal/response"

	"context"
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	base "certainstats/internal/base"
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

		// Check if it's a relative query (hours parameter, no custom start/end)
		isRelative := r.URL.Query().Get("start") == "" && r.URL.Query().Get("end") == ""
		var cacheKey string
		var snapped uint64

		if isRelative {
			rawHours := parseHoursParam(r, defaultHours, maxRangeHours)
			snapped = snapToStandardHours(uint64(rawHours))
			cacheKey = "priv_" + userID + "_" + agentID + "_" + metricName + "_" + strconv.FormatUint(snapped, 10)

			// 1. Check compiled-payload cache (fastest path).
			if val, hit := ctx.MetricsCache.Load(cacheKey); hit {
				entry := val.(*ctx.CacheEntry)
				if time.Now().Before(entry.ExpiresAt) {
					ae := r.Header.Get("Accept-Encoding")
					if strings.Contains(ae, "zstd") && len(entry.ZstdPayload) > 0 {
						w.Header().Set("Content-Encoding", "zstd")
						w.Header().Set("Content-Type", "application/json")
						w.Write(entry.ZstdPayload)
						return
					} else if strings.Contains(ae, "gzip") && len(entry.GzipPayload) > 0 {
						w.Header().Set("Content-Encoding", "gzip")
						w.Header().Set("Content-Type", "application/json")
						w.Write(entry.GzipPayload)
						return
					}
					w.Header().Set("Content-Type", "application/json")
					w.Write(entry.Payload)
					return
				}
			}
		}

		var tr TimeRange
		if isRelative {
			now := time.Now()
			tr = TimeRange{
				StartMs: now.Add(-time.Duration(snapped) * time.Hour).UnixMilli(),
				EndMs:   now.UnixMilli(),
			}
		} else {
			var ok bool
			tr, ok = parsePrivateTimeRange(r)
			if !ok {
				apiresponse.Error(w, http.StatusBadRequest, "Invalid time range")
				return
			}
		}

		// 2. Fast path: serve from the 24-hour sliding-window memory cache.
		if cache != nil {
			if series, ok := buildFromWindowCache(cache, agentID, metricName, tr); ok {
				payload, _ := json.Marshal(map[string]any{
					"metric": metricName,
					"series": series,
				})
				if isRelative {
					ctx.MetricsCache.Store(cacheKey, ctx.NewCacheEntry(payload, 60*time.Second))
				}
				w.Header().Set("Content-Type", "application/json")
				w.Write(payload)
				return
			}
		}

		// 3. Slow path: query the TSDB.
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

		payload, _ := json.Marshal(map[string]any{
			"metric": metricName,
			"series": allSeries,
		})

		if isRelative {
			ctx.MetricsCache.Store(cacheKey, ctx.NewCacheEntry(payload, 60*time.Second))
		}

		w.Header().Set("Content-Type", "application/json")
		w.Write(payload)
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

		// 1. Get agent metadata and access rules (using PublicAgentCache)
		var agent base.FindAgentByPublicID
		var rules accessrules.AccessRules

		cacheKeyLookup := dashboardID + "_" + publicAgentID
		if val, hit := ctx.PublicAgentCache.Load(cacheKeyLookup); hit {
			entry := val.(*ctx.PublicAgentCacheEntry)
			if time.Now().Before(entry.ExpiresAt) {
				agent = entry.Agent
				rules = entry.ParsedRules
			}
		}

		if agent.RealAgentID == "" {
			var err error
			agent, err = dashboard.DashboardFindAgentbyPublicID(r.Context(), dashboardID, publicAgentID)
			if err != nil {
				apiresponse.Error(w, http.StatusForbidden, "Forbidden")
				return
			}

			rules, err = accessrules.ParseRules(agent.RulesJSON)
			if err != nil {
				apiresponse.Error(w, http.StatusInternalServerError, "Internal server error")
				return
			}

			ctx.PublicAgentCache.Store(cacheKeyLookup, &ctx.PublicAgentCacheEntry{
				Agent:       agent,
				ParsedRules: rules,
				ExpiresAt:   time.Now().Add(60 * time.Second),
			})
		}

		// 2. Resolve the snapped timeframe once for all metrics in the request.
		rawHours, _ := strconv.ParseUint(r.URL.Query().Get("hours"), 10, 64)
		if rawHours == 0 {
			rawHours = 24
		}
		maxHoursAllowed := rules[accessrules.PUBLIC].MaxDays * uint(24)
		if rawHours > uint64(maxHoursAllowed) {
			apiresponse.Error(w, http.StatusBadRequest, "Forbidden: requested timeframe exceeds dashboard limit")
			return
		}
		snapped := snapToStandardHours(rawHours)

		now := time.Now()
		tr := TimeRange{
			StartMs: now.Add(-time.Duration(snapped) * time.Hour).UnixMilli(),
			EndMs:   now.UnixMilli(),
		}

		metricNames := strings.Split(metricName, ",")
		isBatch := len(metricNames) > 1

		type metricResponseEnvelope struct {
			Metric string           `json:"metric"`
			Series []map[string]any `json:"series"`
		}

		results := make([]metricResponseEnvelope, 0, len(metricNames))

		for _, singleMetric := range metricNames {
			singleMetric = strings.TrimSpace(singleMetric)
			if singleMetric == "" {
				continue
			}

			// Verify access rules for this metric
			if _, allowed := rules[accessrules.PUBLIC].MetricSet()[singleMetric]; !allowed {
				apiresponse.Error(w, http.StatusForbidden, "Forbidden")
				return
			}

			cacheKey := "pub_" + dashboardID + "_" + publicAgentID + "_" + singleMetric + "_" + strconv.FormatUint(snapped, 10)

			// 3. Check the compiled-payload cache (fastest path).
			if val, hit := ctx.MetricsCache.Load(cacheKey); hit {
				entry := val.(*ctx.CacheEntry)
				if time.Now().Before(entry.ExpiresAt) {
					if !isBatch {
						ae := r.Header.Get("Accept-Encoding")
						if strings.Contains(ae, "zstd") && len(entry.ZstdPayload) > 0 {
							w.Header().Set("Content-Encoding", "zstd")
							w.Header().Set("Content-Type", "application/json")
							w.Write(entry.ZstdPayload)
							return
						} else if strings.Contains(ae, "gzip") && len(entry.GzipPayload) > 0 {
							w.Header().Set("Content-Encoding", "gzip")
							w.Header().Set("Content-Type", "application/json")
							w.Write(entry.GzipPayload)
							return
						}
						w.Header().Set("Content-Type", "application/json")
						w.Write(entry.Payload)
						return
					}
					var decoded metricResponseEnvelope
					if err := json.Unmarshal(entry.Payload, &decoded); err == nil {
						results = append(results, decoded)
						continue
					}
				}
			}

			// 4. Try the sliding-window memory cache.
			var singleSeries []map[string]any
			served := false

			if cache != nil {
				if series, ok := buildFromWindowCache(cache, agent.RealAgentID, singleMetric, tr); ok {
					singleSeries = series
					served = true

					// Cache it
					payload, _ := json.Marshal(metricResponseEnvelope{
						Metric: singleMetric,
						Series: series,
					})
					ctx.MetricsCache.Store(cacheKey, ctx.NewCacheEntry(payload, 60*time.Second))
				}
			}

			if !served {
				// 5. Slow path: query the TSDB.
				release := acquireTSDB()
				querier, err := tsb.Querier(tr.StartMs, tr.EndMs)
				if err != nil {
					release()
					apiresponse.Error(w, http.StatusInternalServerError, "TSDB error")
					return
				}

				matchers := []*labels.Matcher{
					labels.MustNewMatcher(labels.MatchEqual, "__name__", singleMetric),
					labels.MustNewMatcher(labels.MatchEqual, "agent_id", agent.RealAgentID),
					labels.MustNewMatcher(labels.MatchEqual, "user_id", agent.OwnerID),
				}

				singleSeries = queryTSDB(querier, matchers, singleMetric, tr)
				querier.Close()
				release()

				// Cache it
				payload, _ := json.Marshal(metricResponseEnvelope{
					Metric: singleMetric,
					Series: singleSeries,
				})
				ctx.MetricsCache.Store(cacheKey, ctx.NewCacheEntry(payload, 60*time.Second))
			}

			results = append(results, metricResponseEnvelope{
				Metric: singleMetric,
				Series: singleSeries,
			})
		}

		w.Header().Set("Content-Type", "application/json")
		if isBatch {
			json.NewEncoder(w).Encode(results)
		} else {
			json.NewEncoder(w).Encode(results[0])
		}
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


