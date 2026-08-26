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
	cache.Update("test-user", agentID, mockData)

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

func TestRealtimeCache_MultiDisk(t *testing.T) {
	cache := NewRealtimeCache()
	userID := "multi-disk-user"
	agentID := "multi-disk-agent"

	t0 := time.Now()
	// First update (interval 0)
	cache.Update(userID, agentID, &agentparser.ParsedData{
		Metrics: []agentparser.Telemetry{
			{
				Timestamp:       t0,
				CPUUsagePercent: 20.0,
				Disks: []agentparser.DiskTelemetry{
					{Path: "/", UsedBytes: 5000, TotalBytes: 10000, ReadBytes: 1000, WriteBytes: 2000},
					{Path: "/mnt/data", UsedBytes: 15000, TotalBytes: 30000, ReadBytes: 3000, WriteBytes: 4000},
				},
			},
		},
	})

	// Second update (interval 10s later)
	t1 := t0.Add(10 * time.Second)
	cache.Update(userID, agentID, &agentparser.ParsedData{
		Metrics: []agentparser.Telemetry{
			{
				Timestamp:       t1,
				CPUUsagePercent: 25.0,
				Disks: []agentparser.DiskTelemetry{
					{Path: "/", UsedBytes: 6000, TotalBytes: 10000, ReadBytes: 2000, WriteBytes: 3000},
					{Path: "/mnt/data", UsedBytes: 18000, TotalBytes: 30000, ReadBytes: 5000, WriteBytes: 6000},
				},
			},
		},
	})

	snap, ok := cache.Get(agentID)
	if !ok {
		t.Fatalf("expected snapshot to exist")
	}

	// Verify multi-disk slice is preserved
	if len(snap.Disks) != 2 {
		t.Fatalf("expected 2 disks in snapshot, got %d", len(snap.Disks))
	}

	// Verify totals are aggregated across all disks:
	// DiskUsedBytes = 6000 + 18000 = 24000
	// DiskTotalBytes = 10000 + 30000 = 40000
	if snap.DiskUsedBytes != 24000 {
		t.Errorf("expected aggregated DiskUsedBytes 24000, got %d", snap.DiskUsedBytes)
	}
	if snap.DiskTotalBytes != 40000 {
		t.Errorf("expected aggregated DiskTotalBytes 40000, got %d", snap.DiskTotalBytes)
	}

	// Verify throughput rates:
	// Total read bytes = 2000 + 5000 = 7000. Rate = 7000 / 10s = 700 B/s
	// Total write bytes = 3000 + 6000 = 9000. Rate = 9000 / 10s = 900 B/s
	if snap.DiskReadBps != 700 {
		t.Errorf("expected aggregated DiskReadBps 700, got %f", snap.DiskReadBps)
	}
	if snap.DiskWriteBps != 900 {
		t.Errorf("expected aggregated DiskWriteBps 900, got %f", snap.DiskWriteBps)
	}
}
