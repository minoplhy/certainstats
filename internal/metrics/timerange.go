package metrics

import (
	"net/http"
	"strconv"
	"time"
)

const (
	// maxRangeHours is the absolute ceiling for private-panel queries (2 years).
	maxRangeHours = 24 * 365 * 2
	// defaultHours is the fallback window when no range parameters are supplied.
	defaultHours = 6
)

// snapToStandardHours rounds up requested hours to the nearest predefined
// bucket.  This caps cache-key cardinality to exactly 11 values, preventing
// attackers from filling the MetricsCache with arbitrary keys.
func snapToStandardHours(requested uint64) uint64 {
	standards := []uint64{1, 6, 12, 24, 48, 168, 720, 2160, 4320, 8760, 17520}
	for _, std := range standards {
		if requested <= std {
			return std
		}
	}
	return standards[len(standards)-1]
}

// TimeRange is the resolved query window expressed as Unix millisecond timestamps.
type TimeRange struct {
	StartMs  int64
	EndMs    int64
	IsCustom bool // true when start/end were provided explicitly
}

// parsePrivateTimeRange resolves the query window for the private (admin) panel.
// Priority: ?start + ?end (absolute ms) → ?hours (relative) → defaultHours.
// The private panel has no upper limit on historical depth; the 2-year guard is
// only a sanity cap against accidental runaway queries.
func parsePrivateTimeRange(r *http.Request) (TimeRange, bool) {
	if r.URL.Query().Get("start") != "" || r.URL.Query().Get("end") != "" {
		tr, ok := parseAbsoluteRange(r)
		if !ok || tr.EndMs-tr.StartMs > int64(maxRangeHours)*time.Hour.Milliseconds() {
			return TimeRange{}, false
		}
		// Reject clearly invalid or future end timestamps (>10 min from now).
		now := time.Now().UnixMilli()
		if tr.EndMs > now+10*60*1000 {
			return TimeRange{}, false
		}
		return tr, true
	}

	if raw := r.URL.Query().Get("hours"); raw != "" {
		h, e := strconv.Atoi(raw)
		if e != nil || h <= 0 || h > maxRangeHours {
			return TimeRange{}, false
		}
	}
	hours := parseHoursParam(r, defaultHours, maxRangeHours)
	now := time.Now()
	return TimeRange{
		StartMs: now.Add(-time.Duration(hours) * time.Hour).UnixMilli(),
		EndMs:   now.UnixMilli(),
	}, true
}

// parsePublicTimeRange resolves the query window for a public dashboard.
// Public queries are hours-only (no absolute start/end) to limit attack surface
// and keep cache-key cardinality bounded. Enforces the dashboard's MaxDays ACL.
// Returns (TimeRange, cacheKey, ok).
func parsePublicTimeRange(r *http.Request, maxHoursAllowed uint) (TimeRange, string, bool) {
	if r.URL.Query().Get("start") != "" || r.URL.Query().Get("end") != "" {
		return TimeRange{}, "", false
	}
	raw := uint64(24)
	if val := r.URL.Query().Get("hours"); val != "" {
		n, e := strconv.ParseUint(val, 10, 64)
		if e != nil || n == 0 {
			return TimeRange{}, "", false
		}
		raw = n
	}
	snapped := snapToStandardHours(raw)
	if raw > uint64(maxHoursAllowed) || snapped > uint64(maxHoursAllowed) {
		return TimeRange{}, "", false
	}
	now := time.Now()
	tr := TimeRange{
		StartMs: now.Add(-time.Duration(snapped) * time.Hour).UnixMilli(),
		EndMs:   now.UnixMilli(),
	}
	key := publicRelCacheKey(r, snapped)
	return tr, key, true
}

// --- helpers -----------------------------------------------------------------

func parseAbsoluteRange(r *http.Request) (TimeRange, bool) {
	startStr := r.URL.Query().Get("start")
	endStr := r.URL.Query().Get("end")
	if startStr == "" || endStr == "" {
		return TimeRange{}, false
	}
	start, err1 := strconv.ParseInt(startStr, 10, 64)
	end, err2 := strconv.ParseInt(endStr, 10, 64)
	if err1 != nil || err2 != nil || start <= 0 || end <= start {
		return TimeRange{}, false
	}
	return TimeRange{StartMs: start, EndMs: end, IsCustom: true}, true
}

func parseHoursParam(r *http.Request, def, max int) int {
	h, err := strconv.Atoi(r.URL.Query().Get("hours"))
	if err != nil || h <= 0 {
		return def
	}
	if h > max {
		return max
	}
	return h
}

// publicRelCacheKey produces a stable cache key for a relative-hour public query.
func publicRelCacheKey(r *http.Request, snappedHours uint64) string {
	dashID := r.URL.Query().Get("dashboard_id")
	pubAgentID := r.URL.Query().Get("agent_id")
	metric := r.URL.Query().Get("metric")
	return "pub_" + dashID + "_" + pubAgentID + "_" + metric + "_" + strconv.FormatUint(snappedHours, 10)
}

// publicAbsCacheKey produces a stable, quantized cache key for an absolute-range
// public query.  Timestamps are rounded to the nearest minute to maximise cache
// hits across closely-spaced requests and to prevent cardinality exhaustion.
func publicAbsCacheKey(r *http.Request, tr TimeRange) string {
	dashID := r.URL.Query().Get("dashboard_id")
	pubAgentID := r.URL.Query().Get("agent_id")
	metric := r.URL.Query().Get("metric")
	// Quantize to 1-minute (60 000 ms) boundaries.
	qs := strconv.FormatInt((tr.StartMs/60_000)*60_000, 10)
	qe := strconv.FormatInt((tr.EndMs/60_000)*60_000, 10)
	return "pub_" + dashID + "_" + pubAgentID + "_" + metric + "_abs_" + qs + "_" + qe
}
