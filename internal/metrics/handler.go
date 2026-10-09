package metrics

import (
	apiresponse "certainstats/internal/response"

	"certainstats/internal/security"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
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

var publicTSDBSlots = make(chan struct{}, 16)
var publicTSDBWaiters = security.PublicWaitSlots

func acquireTSDB(request context.Context, public bool) (func(), error) {
	if public {
		select {
		case publicTSDBWaiters <- struct{}{}:
		default:
			return nil, fmt.Errorf("query queue full")
		}
		defer func() { <-publicTSDBWaiters }()
		var cancel context.CancelFunc
		request, cancel = context.WithTimeout(request, 2*time.Second)
		defer cancel()
		select {
		case publicTSDBSlots <- struct{}{}:
		case <-request.Done():
			return nil, request.Err()
		}
	}
	select {
	case globalTSDBQuerySemaphore <- struct{}{}:
	case <-request.Done():
		if public {
			<-publicTSDBSlots
		}
		return nil, request.Err()
	}
	return func() {
		<-globalTSDBQuerySemaphore
		if public {
			<-publicTSDBSlots
		}
	}, nil
}

func hasAnyDataPoints(series []map[string]any) bool {
	for _, s := range series {
		if data, ok := s["data"].([]m.DataPoint); ok && len(data) > 0 {
			return true
		}
	}
	return false
}

// MetricsQueryHandler serves the private (authenticated) metrics endpoint.
func MetricsQueryHandler(db store.AgentStore, tsb *tsdb.DB, cache *RealtimeCache) http.HandlerFunc {
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

		if _, ok := parsePrivateTimeRange(r); !ok {
			apiresponse.Error(w, http.StatusBadRequest, "Invalid time range")
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
			if entry, hit := ctx.GetCacheEntry(&ctx.MetricsCache, cacheKey); hit {
				entry.Serve(w, r, "application/json", http.StatusOK)
				return
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
			if series, ok := buildFromWindowCache(cache, userID, agentID, metricName, tr); ok {
				if hasAnyDataPoints(series) {
					payload, _ := json.Marshal(map[string]any{
						"metric": metricName,
						"series": series,
					})
					entry := ctx.NewCacheEntry(payload, ctx.DefaultCacheTTL)
					if isRelative {
						ctx.MetricsCache.Store(cacheKey, entry)
					}
					entry.Serve(w, r, "application/json", http.StatusOK)
					return
				}
			}
		}

		// 3. Slow path: query the TSDB.
		if tsb == nil {
			apiresponse.Error(w, http.StatusNotFound, "Not found")
			return
		}

		release, err := acquireTSDB(r.Context(), false)
		if err != nil {
			security.Reject(w, 503, "Query capacity unavailable")
			return
		}
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

		allSeries, err := queryTSDB(r.Context(), querier, matchers, metricName, tr)
		if err != nil {
			apiresponse.Error(w, 500, "Historical query failed")
			return
		}
		if !hasAnyDataPoints(allSeries) {
			apiresponse.Error(w, http.StatusNotFound, "Not found")
			return
		}

		payload, _ := json.Marshal(map[string]any{
			"metric": metricName,
			"series": allSeries,
		})

		entry := ctx.NewCacheEntry(payload, ctx.DefaultCacheTTL)
		if isRelative {
			ctx.MetricsCache.Store(cacheKey, entry)
		}
		entry.Serve(w, r, "application/json", http.StatusOK)
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

		// Always resolve current permissions before consulting a response cache.
		agent, err := dashboard.DashboardFindAgentbyPublicID(r.Context(), dashboardID, publicAgentID)
		if err != nil {
			apiresponse.Error(w, http.StatusNotFound, "Not found")
			return
		}
		rules, err := accessrules.ParseRules(agent.RulesJSON)
		if err != nil {
			apiresponse.Error(w, http.StatusForbidden, "Public access disabled")
			return
		}
		rule, ok := rules[accessrules.PUBLIC]
		if !ok || rule.IsEmpty() {
			apiresponse.Error(w, http.StatusForbidden, "Public access disabled")
			return
		}
		// 2. Resolve the snapped timeframe once for all metrics in the request.
		tr, _, ok := parsePublicTimeRange(r, rule.MaxDays*24)
		if !ok {
			apiresponse.Error(w, http.StatusBadRequest, "Invalid public range")
			return
		}
		snapped := uint64((tr.EndMs - tr.StartMs) / time.Hour.Milliseconds())
		// Verify access rules for this metric
		if _, allowed := rules[accessrules.PUBLIC].MetricSet()[metricName]; !allowed {
			apiresponse.Error(w, http.StatusNotFound, "Not found")
			return
		}

		cacheKey := fmt.Sprintf("pub:%s:%d:%s:%s:%d", dashboardID, agent.Version, publicAgentID, metricName, snapped)
		valid := func() bool {
			current, err := dashboard.DashboardFindAgentbyPublicID(r.Context(), dashboardID, publicAgentID)
			return err == nil && current.Version == agent.Version && current.RulesJSON == agent.RulesJSON && current.RealAgentID == agent.RealAgentID
		}

		// 3. Check the compiled-payload cache (fastest path).
		if entry, hit := ctx.GetCacheEntry(&ctx.MetricsCache, cacheKey); hit {
			entry.Serve(w, r, "application/json", http.StatusOK)
			return
		}

		finish, leader := ctx.BeginBuild(w, r, cacheKey, PublicMetricsHandler(tsb, dashboard, cache))
		if !leader {
			return
		}
		defer finish()

		// 4. Try the sliding-window memory cache.
		var singleSeries []map[string]any
		served := false

		if cache != nil {
			if series, ok := buildFromWindowCache(cache, agent.OwnerID, agent.RealAgentID, metricName, tr); ok {
				if hasAnyDataPoints(series) {
					singleSeries = series
					served = true

					payload, _ := json.Marshal(map[string]any{
						"metric": metricName,
						"series": series,
					})
					entry := ctx.NewCacheEntry(payload, ctx.DefaultCacheTTL)
					if !valid() {
						apiresponse.Error(w, http.StatusConflict, "Dashboard changed; retry")
						return
					}
					ctx.MetricsCache.Store(cacheKey, entry)
					entry.Serve(w, r, "application/json", http.StatusOK)
					return
				}
			}
		}

		if !served {
			// 5. Slow path: query the TSDB.
			if tsb == nil {
				apiresponse.Error(w, http.StatusNotFound, "Not found")
				return
			}

			release, err := acquireTSDB(r.Context(), true)
			if err != nil {
				security.Reject(w, 503, "Query capacity unavailable")
				return
			}
			querier, err := tsb.Querier(tr.StartMs, tr.EndMs)
			if err != nil {
				release()
				apiresponse.Error(w, http.StatusInternalServerError, "TSDB error")
				return
			}

			matchers := []*labels.Matcher{
				labels.MustNewMatcher(labels.MatchEqual, "__name__", metricName),
				labels.MustNewMatcher(labels.MatchEqual, "agent_id", agent.RealAgentID),
				labels.MustNewMatcher(labels.MatchEqual, "user_id", agent.OwnerID),
			}

			singleSeries, err = queryTSDB(r.Context(), querier, matchers, metricName, tr)
			querier.Close()
			release()
			if err != nil {
				apiresponse.Error(w, 500, "Historical query failed")
				return
			}

			if !hasAnyDataPoints(singleSeries) {
				apiresponse.Error(w, http.StatusNotFound, "Not found")
				return
			}

			payload, _ := json.Marshal(map[string]any{
				"metric": metricName,
				"series": singleSeries,
			})
			entry := ctx.NewCacheEntry(payload, ctx.DefaultCacheTTL)
			if !valid() {
				apiresponse.Error(w, http.StatusConflict, "Dashboard changed; retry")
				return
			}
			ctx.MetricsCache.Store(cacheKey, entry)
			entry.Serve(w, r, "application/json", http.StatusOK)
			return
		}
	}
}

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

// serveFromWindowCache checks the sliding-window cache and writes the JSON
// response directly if served.  Returns true if the response was written.
func serveFromWindowCache(w http.ResponseWriter, cache *RealtimeCache, userID, agentID, metricName string, tr TimeRange) bool {
	series, ok := buildFromWindowCache(cache, userID, agentID, metricName, tr)
	if !ok {
		return false
	}
	writeJSON(w, metricName, series)
	return true
}

// buildFromWindowCache constructs the series slice from the sliding-window
// cache.  Returns (series, true) on a full cache hit, (nil, false) on any miss.
func buildFromWindowCache(cache *RealtimeCache, userID, agentID, metricName string, tr TimeRange) ([]map[string]any, bool) {
	paths := diskPaths(cache, agentID, metricName)
	if paths == nil {
		return nil, false
	}

	step := stepForRange(tr.EndMs - tr.StartMs)
	isDelta := isDeltaMetric(metricName)

	series := make([]map[string]any, 0, len(paths))
	for _, path := range paths {
		pts, hit := cache.GetTimeseries(userID, agentID, metricName, path, tr.StartMs, tr.EndMs)
		if !hit {
			return nil, false
		}
		agg := downsamplePoints(pts, step, isDelta)
		var rates, intervals []m.DataPoint
		legacy := false
		if isDelta {
			durations, _ := cache.GetTimeseries(userID, agentID, "agent_sample_interval_seconds", "", tr.StartMs, tr.EndMs)
			values := make(map[int64]float64)
			for _, d := range durations {
				values[d.Timestamp] = d.Value
			}
			rates, intervals, legacy = rateSeries(pts, values, step)
		}

		labelsMap := map[string]string{}
		if path != "" {
			labelsMap["path"] = path
		}
		series = append(series, map[string]any{
			"labels":    labelsMap,
			"data":      agg,
			"rate_data": rates, "interval_seconds": intervals, "legacy_estimate": legacy,
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
func queryTSDB(request context.Context, querier storage.Querier, matchers []*labels.Matcher, metricName string, tr TimeRange) ([]map[string]any, error) {
	seriesSet := querier.Select(request, false, nil, matchers...)

	step := stepForRange(tr.EndMs - tr.StartMs)
	isDelta := isDeltaMetric(metricName)

	durationValues, err := loadIntervals(request, querier, matchers, tr)
	if err != nil {
		return nil, err
	}
	var allSeries []map[string]any

	for seriesSet.Next() {
		series := seriesSet.At()

		rawMap := series.Labels().Map()
		labelsMap := map[string]string{}
		if val, ok := rawMap["path"]; ok {
			labelsMap["path"] = val
		}

		raw, err := readRawSeries(series.Iterator(nil), tr)
		if err != nil {
			return nil, err
		}
		pts := downsamplePoints(raw, step, isDelta)
		var rates, intervals []m.DataPoint
		legacy := false
		if isDelta {
			rates, intervals, legacy = rateSeries(raw, durationValues, step)
		}

		allSeries = append(allSeries, map[string]any{
			"labels":    labelsMap,
			"data":      pts,
			"rate_data": rates, "interval_seconds": intervals, "legacy_estimate": legacy,
		})
	}
	if err := seriesSet.Err(); err != nil {
		return nil, err
	}

	if allSeries == nil {
		allSeries = []map[string]any{}
	}
	return allSeries, nil
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

func readRawSeries(it chunkenc.Iterator, tr TimeRange) ([]TimeseriesPoint, error) {
	var result []TimeseriesPoint
	for it.Next() != chunkenc.ValNone {
		t, v := it.At()
		if t >= tr.StartMs && t <= tr.EndMs {
			result = append(result, TimeseriesPoint{Timestamp: t, Value: v})
		}
	}
	return result, it.Err()
}
func loadIntervals(request context.Context, q storage.Querier, matchers []*labels.Matcher, tr TimeRange) (map[int64]float64, error) {
	filtered := []*labels.Matcher{labels.MustNewMatcher(labels.MatchEqual, "__name__", "agent_sample_interval_seconds")}
	for _, m := range matchers {
		if m.Name != "__name__" {
			filtered = append(filtered, m)
		}
	}
	values := make(map[int64]float64)
	set := q.Select(request, false, nil, filtered...)
	for set.Next() {
		points, err := readRawSeries(set.At().Iterator(nil), tr)
		if err != nil {
			return nil, err
		}
		for _, p := range points {
			values[p.Timestamp] = p.Value
		}
	}
	return values, set.Err()
}
