package metrics

import (
	m "certainstats/internal/base/metrics"
	"net/http"
	"net/url"
	"testing"
	"time"
)

func TestSnapToStandardHours(t *testing.T) {
	tests := []struct {
		requested uint64
		expected  uint64
	}{
		{0, 1},
		{1, 1},
		{2, 6},
		{5, 6},
		{6, 6},
		{7, 12},
		{20, 24},
		{100, 168},
		{10000, 17520},
		{20000, 17520},
	}

	for _, tc := range tests {
		actual := snapToStandardHours(tc.requested)
		if actual != tc.expected {
			t.Errorf("snapToStandardHours(%d): expected %d, got %d", tc.requested, tc.expected, actual)
		}
	}
}

func TestIsDeltaMetric(t *testing.T) {
	tests := []struct {
		metricName string
		expected   bool
	}{
		{"agent_cpu_usage", false},
		{"agent_disk_read_bytes", true},
		{"agent_disk_write_bytes", true},
		{"agent_rx_bytes", true},
		{"agent_tx_bytes", true},
		{"agent_ram_used", false},
	}

	for _, tc := range tests {
		actual := isDeltaMetric(tc.metricName)
		if actual != tc.expected {
			t.Errorf("isDeltaMetric(%q): expected %t, got %t", tc.metricName, tc.expected, actual)
		}
	}
}

func TestStepForRange(t *testing.T) {
	tests := []struct {
		durationMs int64
		expected   int64
	}{
		{0, 0},
		{10_000, 0},     // 10s -> step = 10ms < 15_000ms -> returns 0
		{10_000_000, 0}, // 10k seconds -> step = 10,000ms < 15_000ms -> returns 0
		{30_000_000, 30_000}, // 30k seconds -> step = 30,000ms >= 15_000ms -> returns 30,000
	}

	for _, tc := range tests {
		actual := stepForRange(tc.durationMs)
		if actual != tc.expected {
			t.Errorf("stepForRange(%d): expected %d, got %d", tc.durationMs, tc.expected, actual)
		}
	}
}

func TestDownsamplePoints(t *testing.T) {
	pts := []TimeseriesPoint{
		{Timestamp: 1000, Value: 10.0},
		{Timestamp: 2000, Value: 20.0},
		{Timestamp: 3000, Value: 30.0},
		{Timestamp: 11000, Value: 40.0},
		{Timestamp: 12000, Value: 50.0},
	}

	t.Run("stepMs is zero", func(t *testing.T) {
		out := downsamplePoints(pts, 0, false)
		if len(out) != len(pts) {
			t.Fatalf("expected %d points, got %d", len(pts), len(out))
		}
		for i, p := range out {
			if p[0] != float64(pts[i].Timestamp) || p[1] != pts[i].Value {
				t.Errorf("mismatch at index %d: got %+v", i, p)
			}
		}
	})

	t.Run("gauge aggregation (average)", func(t *testing.T) {
		// buckets of size 10000ms:
		// [1000, 11000) -> 1000, 2000, 3000. Avg = (10+20+30)/3 = 20. Start = 1000
		// [11000, 21000) -> 11000, 12000. Avg = (40+50)/2 = 45. Start = 11000
		out := downsamplePoints(pts, 10000, false)
		expected := []m.DataPoint{
			{1000, 20.0},
			{11000, 45.0},
		}

		if len(out) != len(expected) {
			t.Fatalf("expected %d points, got %d", len(expected), len(out))
		}
		for i, p := range out {
			if p[0] != expected[i][0] || p[1] != expected[i][1] {
				t.Errorf("mismatch at index %d: expected %+v, got %+v", i, expected[i], p)
			}
		}
	})

	t.Run("delta aggregation (sum)", func(t *testing.T) {
		// buckets of size 10000ms:
		// [1000, 11000) -> 1000, 2000, 3000. Sum = 10+20+30 = 60. Start = 1000
		// [11000, 21000) -> 11000, 12000. Sum = 40+50 = 90. Start = 11000
		out := downsamplePoints(pts, 10000, true)
		expected := []m.DataPoint{
			{1000, 60.0},
			{11000, 90.0},
		}

		if len(out) != len(expected) {
			t.Fatalf("expected %d points, got %d", len(expected), len(out))
		}
		for i, p := range out {
			if p[0] != expected[i][0] || p[1] != expected[i][1] {
				t.Errorf("mismatch at index %d: expected %+v, got %+v", i, expected[i], p)
			}
		}
	})
}

func TestParsePrivateTimeRange(t *testing.T) {
	t.Run("absolute start and end", func(t *testing.T) {
		req := &http.Request{
			URL: &url.URL{
				RawQuery: "start=1718021000&end=1718022000",
			},
		}
		tr, ok := parsePrivateTimeRange(req)
		if !ok {
			t.Fatal("expected parse success")
		}
		if tr.StartMs != 1718021000 || tr.EndMs != 1718022000 || !tr.IsCustom {
			t.Errorf("unexpected time range: %+v", tr)
		}
	})

	t.Run("absolute future limit end rejected", func(t *testing.T) {
		req := &http.Request{
			URL: &url.URL{
				RawQuery: "start=1718021000&end=999999999999999",
			},
		}
		_, ok := parsePrivateTimeRange(req)
		if ok {
			t.Error("expected future end range to be rejected")
		}
	})

	t.Run("relative hours fallback", func(t *testing.T) {
		req := &http.Request{
			URL: &url.URL{
				RawQuery: "hours=24",
			},
		}
		tr, ok := parsePrivateTimeRange(req)
		if !ok {
			t.Fatal("expected success")
		}
		if tr.IsCustom {
			t.Error("expected IsCustom to be false")
		}
		durationMs := tr.EndMs - tr.StartMs
		expectedMs := int64(24 * time.Hour / time.Millisecond)
		// Small buffer since time.Now() moves slightly
		if durationMs < expectedMs-1000 || durationMs > expectedMs+1000 {
			t.Errorf("expected duration around %d ms, got %d ms", expectedMs, durationMs)
		}
	})
}

func TestParsePublicTimeRange(t *testing.T) {
	t.Run("valid hours within limits", func(t *testing.T) {
		req := &http.Request{
			URL: &url.URL{
				RawQuery: "hours=3&dashboard_id=dash-1&agent_id=agent-1&metric=cpu",
			},
		}
		tr, cacheKey, ok := parsePublicTimeRange(req, 24)
		if !ok {
			t.Fatal("expected parse success")
		}
		// snapped to 6 hours
		expectedDurationMs := int64(6 * time.Hour / time.Millisecond)
		actualDurationMs := tr.EndMs - tr.StartMs
		if actualDurationMs < expectedDurationMs-1000 || actualDurationMs > expectedDurationMs+1000 {
			t.Errorf("expected snapped duration around 6h (%d ms), got %d ms", expectedDurationMs, actualDurationMs)
		}
		if cacheKey != "pub_dash-1_agent-1_cpu_6" {
			t.Errorf("unexpected cache key: %q", cacheKey)
		}
	})

	t.Run("exceeds maxHoursAllowed", func(t *testing.T) {
		req := &http.Request{
			URL: &url.URL{
				RawQuery: "hours=48",
			},
		}
		_, _, ok := parsePublicTimeRange(req, 24)
		if ok {
			t.Error("expected request exceeding limit to be rejected")
		}
	})
}

func TestPublicCacheKeys(t *testing.T) {
	req := &http.Request{
		URL: &url.URL{
			RawQuery: "dashboard_id=d123&agent_id=a456&metric=ram",
		},
	}

	t.Run("publicRelCacheKey", func(t *testing.T) {
		key := publicRelCacheKey(req, 12)
		expected := "pub_d123_a456_ram_12"
		if key != expected {
			t.Errorf("expected %q, got %q", expected, key)
		}
	})

	t.Run("publicAbsCacheKey", func(t *testing.T) {
		tr := TimeRange{
			StartMs: 1718021000000, // exact millisecond unix timestamps
			EndMs:   1718021060000,
		}
		key := publicAbsCacheKey(req, tr)
		// 1718021000000 / 60000 = 28633683. 28633683 * 60000 = 1718020980000 (quantized to minute)
		// 1718021060000 / 60000 = 28633684. 28633684 * 60000 = 1718021040000 (quantized to minute)
		expected := "pub_d123_a456_ram_abs_1718020980000_1718021040000"
		if key != expected {
			t.Errorf("expected %q, got %q", expected, key)
		}
	})
}
