package metrics

import (
	"certainstats/internal/agent_parser"
	"testing"
	"time"
)

func TestRealtimeCache(t *testing.T) {
	cache := NewRealtimeCache()
	agentID := "test-agent"

	// 1. Initial check
	if _, ok := cache.Get(agentID); ok {
		t.Fatal("expected agent not found in empty cache")
	}

	// 2. Update cache
	mockData := &agentparser.ParsedData{
		Metrics: []agentparser.Telemetry{
			{
				Timestamp:       time.Now(),
				CPUUsagePercent: 50.5,
				RAMUsedBytes:    4096,
			},
		},
	}
	cache.Update(agentID, mockData)

	snap, ok := cache.Get(agentID)
	if !ok {
		t.Fatal("expected agent snapshot to be cached after Update")
	}
	if snap.CPUUsagePercent != 50.5 {
		t.Errorf("expected CPUUsagePercent 50.5, got %f", snap.CPUUsagePercent)
	}

	// 3. Delete agent
	cache.Delete(agentID)
	if _, ok := cache.Get(agentID); ok {
		t.Fatal("expected agent snapshot to be removed after Delete")
	}
}
