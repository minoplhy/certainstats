package metrics

import (
	parser "certainstats/internal/agent_parser"
	"fmt"
	"testing"
	"time"
)

func windowCount(c *RealtimeCache) int {
	n := 0
	c.windows.Range(func(any, any) bool { n++; return true })
	return n
}

func TestRealtimeCacheSnapshotsReleasedOnDelete(t *testing.T) {
	const n = 500
	c := NewRealtimeCache()
	now := time.Now()
	for i := 0; i < n; i++ {
		id := fmt.Sprintf("agent-%d", i)
		c.Update("user", id, &parser.ParsedData{Metrics: []parser.Telemetry{{Timestamp: now, IntervalSeconds: 30, CPUUsagePercent: 1}}})
	}
	if got := c.Len(); got != n {
		t.Fatalf("Len=%d after %d updates", got, n)
	}
	for i := 0; i < n; i++ {
		c.Delete(fmt.Sprintf("agent-%d", i))
	}
	if got := c.Len(); got != 0 {
		t.Fatalf("Len=%d after deleting every agent", got)
	}
}

func TestRealtimeCacheWindowsExpire(t *testing.T) {
	const n = 500
	c := NewRealtimeCache()
	old := time.Now().Add(-windowTTL - time.Hour).UnixMilli()
	for i := 0; i < n; i++ {
		c.appendPoint(windowKey("user", fmt.Sprintf("agent-%d", i), "cpu", ""), old, 1)
	}
	if got := windowCount(c); got != n {
		t.Fatalf("%d windows, want %d", got, n)
	}
	c.EvictExpiredWindows()
	if got := windowCount(c); got != 0 {
		t.Fatalf("%d windows left after eviction", got)
	}
}

func TestRealtimeCacheWindowPointsBounded(t *testing.T) {
	c := NewRealtimeCache()
	key := windowKey("user", "agent", "cpu", "")
	start := time.Now().Add(-72 * time.Hour)
	for ts := start; ts.Before(time.Now()); ts = ts.Add(time.Minute) {
		c.appendPoint(key, ts.UnixMilli(), 1)
	}
	v, _ := c.windows.Load(key)
	w := v.(*TimeseriesWindow)
	limit := int(windowTTL/time.Minute) + 1
	if len(w.Points) > limit {
		t.Fatalf("window holds %d points, want at most %d", len(w.Points), limit)
	}
	if cap(w.Points) > 2*limit+1 {
		t.Fatalf("window backing array capacity %d keeps expired points alive", cap(w.Points))
	}
	if got := windowCount(c); got != 1 {
		t.Fatalf("%d windows for one key", got)
	}
}
