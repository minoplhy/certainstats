package metrics

import (
	agentdata "certainstats/internal/agent_data"
	agentparser "certainstats/internal/agent_parser"
	"sync"
	"time"
)

// windowTTL is how long we keep in-memory telemetry samples.
const windowTTL = 24 * time.Hour

// AgentSnapshot holds the most recent data for an agent.
type AgentSnapshot struct {
	AgentID   string
	Timestamp time.Time

	// Latest Telemetry
	CPUUsagePercent  float64
	CPUIOWaitPercent float64
	CPUStealPercent  float64
	RAMUsedBytes     uint64
	RAMSwapUsedBytes uint64
	DiskUsedBytes    uint64
	DiskTotalBytes   uint64
	Disks            []agentparser.DiskTelemetry

	// Networking (current throughput, bytes/s)
	RXBytes float64
	TXBytes float64
	RXBps   float64
	TXBps   float64

	// Disk Activity (current throughput, bytes/s)
	DiskReadBps  float64
	DiskWriteBps float64

	// Hardware/Metadata snapshot
	Metadata *agentparser.ParsedMetadata
}

// TimeseriesPoint is a single (timestamp, value) sample stored in the
// sliding-window cache.
type TimeseriesPoint struct {
	Timestamp int64
	Value     float64
}

// TimeseriesWindow is the thread-safe, append-only circular buffer for one
// metric series.
type TimeseriesWindow struct {
	mu     sync.RWMutex
	Points []TimeseriesPoint
}

// RealtimeCache keeps two layers of state:
//  1. agents — the latest single-point snapshot for each agent (for live UI).
//  2. windows — 24-hour sliding-window timeseries per (agent × metric × path).
type RealtimeCache struct {
	mu      sync.RWMutex
	agents  map[string]*AgentSnapshot
	windows sync.Map // key → *TimeseriesWindow
}

func NewRealtimeCache() *RealtimeCache {
	return &RealtimeCache{
		agents: make(map[string]*AgentSnapshot),
	}
}

// EvictExpiredWindows sweeps the cache to evict completely empty windows.
// This prevents unbounded cardinality memory leaks if an attacker (or dynamic workload)
// creates millions of unique paths over time.
func (c *RealtimeCache) EvictExpiredWindows() {
	nowMs := time.Now().UnixMilli()
	cutoff := nowMs - windowTTL.Milliseconds()

	c.windows.Range(func(key, value any) bool {
		window := value.(*TimeseriesWindow)
		
		window.mu.Lock()
		// 1. Evict any points that expired since the last append.
		i := 0
		for i < len(window.Points) && window.Points[i].Timestamp < cutoff {
			i++
		}
		if i > 0 {
			remaining := window.Points[i:]
			if i > len(remaining) {
				fresh := make([]TimeseriesPoint, len(remaining))
				copy(fresh, remaining)
				window.Points = fresh
			} else {
				window.Points = remaining
			}
		}
		
		// 2. Mark for deletion if empty.
		isEmpty := len(window.Points) == 0
		window.mu.Unlock()

		// 3. Delete from sync.Map.
		// Note: A minuscule TOCTOU race exists here where a new point could be appended
		// between Unlock and Delete, resulting in the loss of that single point. 
		// For 24-hour telemetry on previously idle metrics, this is an acceptable tradeoff
		// to avoid complex lock-free data structures.
		if isEmpty {
			c.windows.Delete(key)
		}
		return true
	})
}

// windowKey constructs a collision-free lookup key.
// We use a tab separator (0x09) which cannot appear in metric names or paths.
func windowKey(agentID, metricName, path string) string {
	if path == "" {
		return agentID + "\t" + metricName
	}
	return agentID + "\t" + metricName + "\t" + path
}

// appendPoint appends a single sample to the named window and evicts samples
// older than windowTTL.  It is safe for concurrent use.
func (c *RealtimeCache) appendPoint(key string, tMs int64, val float64) {
	// LoadOrStore guarantees exactly one *TimeseriesWindow per key even under
	// concurrent first-writes, eliminating the previous TOCTOU race.
	actual, _ := c.windows.LoadOrStore(key, &TimeseriesWindow{})
	window := actual.(*TimeseriesWindow)

	window.mu.Lock()
	defer window.mu.Unlock()

	window.Points = append(window.Points, TimeseriesPoint{Timestamp: tMs, Value: val})

	// Evict expired points.  Because Points is always appended in-order we only
	// need to scan from the front until we find the first non-expired sample.
	cutoff := tMs - windowTTL.Milliseconds()
	i := 0
	for i < len(window.Points) && window.Points[i].Timestamp < cutoff {
		i++
	}
	if i > 0 {
		// Re-slice to drop expired prefix.  We copy to a new backing array when
		// more than half the slice has been evicted to avoid unbounded memory
		// growth from the old backing array never being GC'd.
		remaining := window.Points[i:]
		if i > len(remaining) {
			fresh := make([]TimeseriesPoint, len(remaining))
			copy(fresh, remaining)
			window.Points = fresh
		} else {
			window.Points = remaining
		}
	}
}

// GetTimeseries returns the cached samples for (agentID, metricName, path)
// within [startMs, endMs].
//
// Returns (nil, false) on a cache miss, meaning the caller must fall back to
// the TSDB.  A miss occurs when:
//   - no window exists yet (agent has not submitted since restart)
//   - startMs predates the oldest cached sample (range older than windowTTL)
func (c *RealtimeCache) GetTimeseries(agentID, metricName, path string, startMs, endMs int64) ([]TimeseriesPoint, bool) {
	v, ok := c.windows.Load(windowKey(agentID, metricName, path))
	if !ok {
		return nil, false
	}

	window := v.(*TimeseriesWindow)
	window.mu.RLock()
	defer window.mu.RUnlock()

	if len(window.Points) == 0 || startMs < window.Points[0].Timestamp {
		return nil, false
	}

	// Linear scan is fast: at most 1 440 points (24 h ÷ 60 s).
	var out []TimeseriesPoint
	for _, pt := range window.Points {
		if pt.Timestamp >= startMs && pt.Timestamp <= endMs {
			out = append(out, pt)
		}
	}
	return out, true
}

// Update refreshes the live snapshot for an agent and appends all metrics from
// the submitted batch to the sliding-window cache.
func (c *RealtimeCache) Update(agentID string, data *agentparser.ParsedData) {
	if data == nil || len(data.Metrics) == 0 {
		return
	}

	latest := data.Metrics[len(data.Metrics)-1]

	// --- 1. Refresh live snapshot (needs the agents write-lock) ---------------
	c.mu.Lock()

	snapshot, exists := c.agents[agentID]
	if !exists {
		snapshot = &AgentSnapshot{AgentID: agentID}
		c.agents[agentID] = snapshot
	}

	if exists {
		dt := latest.Timestamp.Sub(snapshot.Timestamp).Seconds()
		if dt > 0 {
			rxRate := max0(latest.RXBytes / dt)
			txRate := max0(latest.TXBytes / dt)
			snapshot.RXBps = rxRate
			snapshot.TXBps = txRate

			if len(latest.Disks) > 0 {
				snapshot.DiskReadBps = max0(float64(latest.Disks[0].ReadBytes) / dt)
				snapshot.DiskWriteBps = max0(float64(latest.Disks[0].WriteBytes) / dt)
			}
		}
	}

	snapshot.Timestamp = latest.Timestamp
	snapshot.CPUUsagePercent = latest.CPUUsagePercent
	snapshot.CPUIOWaitPercent = latest.CPUIOWaitPercent
	snapshot.CPUStealPercent = latest.CPUStealPercent
	snapshot.RAMUsedBytes = latest.RAMUsedBytes
	snapshot.RAMSwapUsedBytes = latest.RAMSwapUsedBytes
	snapshot.RXBytes = latest.RXBytes
	snapshot.TXBytes = latest.TXBytes
	snapshot.Disks = latest.Disks
	if len(latest.Disks) > 0 {
		snapshot.DiskUsedBytes = latest.Disks[0].UsedBytes
		snapshot.DiskTotalBytes = latest.Disks[0].TotalBytes
	}
	if data.AgentInfo != nil {
		snapshot.Metadata = data.AgentInfo
	}

	c.mu.Unlock() // release before the sliding-window writes (sync.Map is independent)

	// --- 2. Append to sliding-window cache (lock-free per window) -------------
	now := time.Now()
	for i, s := range data.Metrics {
		ago := len(data.Metrics) - 1 - i
		tMs := now.Add(time.Duration(-ago*agentdata.TIME_DIFF) * time.Second).UnixMilli()

		c.appendPoint(windowKey(agentID, "agent_cpu_usage", ""), tMs, s.CPUUsagePercent)
		c.appendPoint(windowKey(agentID, "agent_cpu_iowait", ""), tMs, s.CPUIOWaitPercent)
		c.appendPoint(windowKey(agentID, "agent_cpu_steal", ""), tMs, s.CPUStealPercent)
		c.appendPoint(windowKey(agentID, "agent_ram_used", ""), tMs, float64(s.RAMUsedBytes))
		c.appendPoint(windowKey(agentID, "agent_swap_used", ""), tMs, float64(s.RAMSwapUsedBytes))
		c.appendPoint(windowKey(agentID, "agent_rx_bytes", ""), tMs, s.RXBytes)
		c.appendPoint(windowKey(agentID, "agent_tx_bytes", ""), tMs, s.TXBytes)

		for _, disk := range s.Disks {
			c.appendPoint(windowKey(agentID, "agent_disk_used", disk.Path), tMs, float64(disk.UsedBytes))
			c.appendPoint(windowKey(agentID, "agent_disk_read_bytes", disk.Path), tMs, float64(disk.ReadBytes))
			c.appendPoint(windowKey(agentID, "agent_disk_write_bytes", disk.Path), tMs, float64(disk.WriteBytes))

			usagePct := 0.0
			if disk.TotalBytes > 0 {
				usagePct = float64(disk.UsedBytes) / float64(disk.TotalBytes) * 100.0
			}
			c.appendPoint(windowKey(agentID, "agent_disk_usage", disk.Path), tMs, usagePct)
		}
	}
}

// Get retrieves the latest snapshot for an agent.
func (c *RealtimeCache) Get(agentID string) (*AgentSnapshot, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	s, ok := c.agents[agentID]
	return s, ok
}

// GetAll returns a shallow copy of all agent snapshots.
func (c *RealtimeCache) GetAll() map[string]*AgentSnapshot {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make(map[string]*AgentSnapshot, len(c.agents))
	for k, v := range c.agents {
		out[k] = v
	}
	return out
}

func max0(v float64) float64 {
	if v < 0 {
		return 0
	}
	return v
}
