package metrics

import (
	parser "certainstats/internal/agent_parser"
	"certainstats/internal/base"
	csctx "certainstats/internal/context"
	"certainstats/internal/store"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestPublicRangeCannotExpandPastPermission(t *testing.T) {
	for _, q := range []string{"hours=25", "hours=0", "hours=bad", "start=1&end=2"} {
		if _, _, ok := parsePublicTimeRange(httptest.NewRequest("GET", "/?"+q, nil), 24); ok {
			t.Fatalf("accepted %s", q)
		}
	}
	if _, _, ok := parsePublicTimeRange(httptest.NewRequest("GET", "/?hours=24", nil), 24); !ok {
		t.Fatal("valid permitted range rejected")
	}
}
func TestCancelledTSDBAcquisition(t *testing.T) {
	for i := 0; i < cap(globalTSDBQuerySemaphore); i++ {
		globalTSDBQuerySemaphore <- struct{}{}
	}
	defer func() {
		for i := 0; i < cap(globalTSDBQuerySemaphore); i++ {
			<-globalTSDBQuerySemaphore
		}
	}()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := acquireTSDB(ctx, true); err == nil {
		t.Fatal("cancelled query acquired slot")
	}
	if len(publicTSDBSlots) != 0 {
		t.Fatal("public slot leaked")
	}
}
func TestIntervalWeightedRate(t *testing.T) {
	points := []TimeseriesPoint{{Timestamp: 1000, Value: 100}, {Timestamp: 31000, Value: 900}}
	rates, durations, legacy := rateSeries(points, map[int64]float64{1000: 10, 31000: 30}, time.Minute.Milliseconds())
	if len(rates) != 1 || rates[0][1] != 25 || durations[0][1] != 40 || legacy {
		t.Fatalf("rate mismatch: %v %v %v", rates, durations, legacy)
	}
	_, _, legacy = rateSeries(points, nil, 0)
	if !legacy {
		t.Fatal("legacy samples must be marked")
	}
}
func TestSnapshotImmutability(t *testing.T) {
	c := NewRealtimeCache()
	c.agents["a"] = &AgentSnapshot{Temperatures: map[string]float64{"cpu": 1}}
	s, _ := c.Get("a")
	s.Temperatures["cpu"] = 99
	s.AgentID = "mutated"
	other, _ := c.Get("a")
	if other.Temperatures["cpu"] != 1 || other.AgentID == "mutated" {
		t.Fatal("snapshot aliases cache")
	}
}

func TestMissingNetworkSnapshotRemainsUnavailable(t *testing.T) {
	cache := NewRealtimeCache()
	cache.Update("u", "a", &parser.ParsedData{Metrics: []parser.Telemetry{{Timestamp: time.Now(), IntervalSeconds: 30, NetworkMissing: true}}})
	snap, _ := cache.Get("a")
	raw, err := json.Marshal(snap)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"rx_bytes", "tx_bytes", "rx_bps", "tx_bps"} {
		if fields[key] != nil {
			t.Fatalf("%s fabricated zero: %s", key, raw)
		}
	}
}

type changingDashboard struct {
	store.DashboardStore
	calls   int
	changed bool
	owner   string
}

func (d *changingDashboard) DashboardFindAgentbyPublicID(_ context.Context, _, _ string) (base.FindAgentByPublicID, error) {
	d.calls++
	version := int64(1)
	if d.changed && d.calls > 1 {
		version = 2
	}
	return base.FindAgentByPublicID{Version: version, OwnerID: d.owner, RealAgentID: "agent", RulesJSON: `{"public":{"allowed_metrics":["agent_cpu_usage"],"max_days":1}}`}, nil
}
func TestPublicBuildCannotPublishRevokedVersion(t *testing.T) {
	cache := NewRealtimeCache()
	now := time.Now()
	cache.appendSample(windowKey("owner", "agent", "agent_cpu_usage", ""), now.Add(-2*time.Hour).UnixMilli(), 1, false)
	cache.appendSample(windowKey("owner", "agent", "agent_cpu_usage", ""), now.Add(-time.Minute).UnixMilli(), 42, false)
	dashboard := &changingDashboard{changed: true, owner: "owner"}
	req := httptest.NewRequest("GET", "/?dashboard_id="+t.Name()+"&agent_id=public&metric=agent_cpu_usage&hours=1", nil)
	w := httptest.NewRecorder()
	PublicMetricsHandler(nil, dashboard, cache)(w, req)
	if w.Code != http.StatusConflict {
		t.Fatalf("revoked build returned %d: %s", w.Code, w.Body.String())
	}
	key := fmt.Sprintf("pub:%s:1:public:agent_cpu_usage:1", t.Name())
	if _, ok := csctx.MetricsCache.Load(key); ok {
		t.Fatal("revoked build restored cached data")
	}
}
func TestPublicCacheIsIsolatedByDashboard(t *testing.T) {
	cache := NewRealtimeCache()
	now := time.Now()
	for _, item := range []struct {
		owner string
		value float64
	}{{"first", 11}, {"second", 99}} {
		cache.appendSample(windowKey(item.owner, "agent", "agent_cpu_usage", ""), now.Add(-2*time.Hour).UnixMilli(), item.value, false)
		cache.appendSample(windowKey(item.owner, "agent", "agent_cpu_usage", ""), now.Add(-time.Minute).UnixMilli(), item.value, false)
	}
	for index, owner := range []string{"first", "second"} {
		dashboardID := fmt.Sprintf("%s-%d", t.Name(), index)
		req := httptest.NewRequest("GET", "/?dashboard_id="+dashboardID+"&agent_id=public&metric=agent_cpu_usage&hours=1", nil)
		w := httptest.NewRecorder()
		PublicMetricsHandler(nil, &changingDashboard{owner: owner}, cache)(w, req)
		if w.Code != 200 {
			t.Fatalf("%s: %d %s", owner, w.Code, w.Body.String())
		}
		var result struct {
			Series []struct {
				Data [][]float64 `json:"data"`
			} `json:"series"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		expected := float64(11)
		if index == 1 {
			expected = 99
		}
		if len(result.Series) != 1 || len(result.Series[0].Data) != 1 || result.Series[0].Data[0][1] != expected {
			t.Fatalf("cross-dashboard result: %s", w.Body.String())
		}
		csctx.MetricsCache.Delete(fmt.Sprintf("pub:%s:1:public:agent_cpu_usage:1", dashboardID))
	}
}
func TestPublicQueueRejectsBeyondBound(t *testing.T) {
	for i := 0; i < cap(publicTSDBWaiters); i++ {
		publicTSDBWaiters <- struct{}{}
	}
	defer func() {
		for i := 0; i < cap(publicTSDBWaiters); i++ {
			<-publicTSDBWaiters
		}
	}()
	if _, err := acquireTSDB(context.Background(), true); err == nil {
		t.Fatal("full public queue accepted query")
	}
	if len(publicTSDBSlots) != 0 {
		t.Fatal("rejected request acquired a read slot")
	}
}
