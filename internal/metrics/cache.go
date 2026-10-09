package metrics

import (
	agentparser "certainstats/internal/agent_parser"
	"math"
	"sync"
	"time"
)

// windowTTL is how long we keep in-memory telemetry samples.
const windowTTL = 24 * time.Hour

// AgentSnapshot holds the most recent data for an agent.
type AgentSnapshot struct {
	Missing   map[string]bool `json:"missing,omitempty"`
	AgentID   string          `json:"agent_id"`
	Timestamp time.Time       `json:"timestamp"`

	// Latest Telemetry
	CPUUsagePercent  float64                     `json:"cpu_usage_percent"`
	CPUIOWaitPercent float64                     `json:"cpu_iowait_percent"`
	CPUStealPercent  float64                     `json:"cpu_steal_percent"`
	RAMUsedBytes     uint64                      `json:"ram_used_bytes"`
	RAMSwapUsedBytes uint64                      `json:"ram_swap_used_bytes"`
	DiskUsedBytes    uint64                      `json:"disk_used_bytes"`
	DiskTotalBytes   uint64                      `json:"disk_total_bytes"`
	Disks            []agentparser.DiskTelemetry `json:"disks"`
	LoadAvg          [3]float64                  `json:"load_avg"`
	Temperatures     map[string]float64          `json:"temperatures,omitempty"`

	// Networking (current throughput, bytes/s)
	RXBytes float64 `json:"rx_bytes"`
	TXBytes float64 `json:"tx_bytes"`
	RXBps   float64 `json:"rx_bps"`
	TXBps   float64 `json:"tx_bps"`

	// Disk Activity (current throughput, bytes/s)
	DiskReadBps  float64 `json:"disk_read_bps"`
	DiskWriteBps float64 `json:"disk_write_bps"`

	// Hardware/Metadata snapshot
	Metadata *agentparser.ParsedMetadata `json:"metadata,omitempty"`
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
	network networkCache
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
	c.evictNetwork("", true)
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

		if len(window.Points) == 0 {
			c.windows.Delete(key)
		}
		window.mu.Unlock()

		return true
	})
}

// windowKey constructs a collision-free lookup key partitioned by owner (userID).
// We use a tab separator (0x09) which cannot appear in metric names or paths.
func windowKey(userID, agentID, metricName, path string) string {
	if path == "" {
		return userID + "\t" + agentID + "\t" + metricName
	}
	return userID + "\t" + agentID + "\t" + metricName + "\t" + path
}

// appendPoint appends a single sample to the named window and evicts samples
// older than windowTTL.  It is safe for concurrent use.
func (c *RealtimeCache) appendPoint(key string, tMs int64, val float64) {
	// LoadOrStore guarantees exactly one *TimeseriesWindow per key even under
	// concurrent first-writes, eliminating the previous TOCTOU race.
	var window *TimeseriesWindow
	for {
		actual, _ := c.windows.LoadOrStore(key, &TimeseriesWindow{})
		window = actual.(*TimeseriesWindow)
		window.mu.Lock()
		current, ok := c.windows.Load(key)
		if ok && current == window {
			break
		}
		window.mu.Unlock()
	}

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

// GetTimeseries returns the cached samples for (userID, agentID, metricName, path)
// within [startMs, endMs].
//
// Returns (nil, false) on a cache miss, meaning the caller must fall back to
// the TSDB.  A miss occurs when:
//   - no window exists yet (agent has not submitted since restart)
//   - startMs predates the oldest cached sample (range older than windowTTL)
func (c *RealtimeCache) GetTimeseries(userID, agentID, metricName, path string, startMs, endMs int64) ([]TimeseriesPoint, bool) {
	v, ok := c.windows.Load(windowKey(userID, agentID, metricName, path))
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
// the submitted batch to the sliding-window cache partitioned by owner (userID).
func (c *RealtimeCache) Update(userID, agentID string, data *agentparser.ParsedData) {
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

	dt := latest.IntervalSeconds
	if dt <= 0 && exists {
		dt = latest.Timestamp.Sub(snapshot.Timestamp).Seconds()
	}
	if dt > 0 {
		snapshot.RXBps = max0(latest.RXBytes / dt)
		snapshot.TXBps = max0(latest.TXBytes / dt)
		var read, write float64
		for _, d := range latest.Disks {
			read += float64(d.ReadBytes)
			write += float64(d.WriteBytes)
		}
		snapshot.DiskReadBps = max0(read / dt)
		snapshot.DiskWriteBps = max0(write / dt)
	}

	snapshot.Missing = make(map[string]bool, len(latest.Missing)+2)
	for key, missing := range latest.Missing {
		snapshot.Missing[key] = missing
	}
	if latest.NetworkMissing {
		snapshot.Missing["agent_rx_bytes"], snapshot.Missing["agent_tx_bytes"] = true, true
	}
	snapshot.Timestamp = latest.Timestamp
	snapshot.CPUUsagePercent = latest.CPUUsagePercent
	snapshot.CPUIOWaitPercent = latest.CPUIOWaitPercent
	snapshot.CPUStealPercent = latest.CPUStealPercent
	snapshot.RAMUsedBytes = latest.RAMUsedBytes
	snapshot.RAMSwapUsedBytes = latest.RAMSwapUsedBytes
	snapshot.RXBytes = latest.RXBytes
	snapshot.TXBytes = latest.TXBytes
	snapshot.Disks = append([]agentparser.DiskTelemetry(nil), latest.Disks...)
	if dt > 0 {
		for i := range snapshot.Disks {
			snapshot.Disks[i].ReadBytes = uint64(float64(snapshot.Disks[i].ReadBytes) / dt)
			snapshot.Disks[i].WriteBytes = uint64(float64(snapshot.Disks[i].WriteBytes) / dt)
		}
	}
	snapshot.LoadAvg = latest.LoadAvg
	snapshot.Temperatures = latest.Temperatures
	if len(latest.Disks) > 0 {
		var totalUsed, totalTotal uint64
		for _, d := range latest.Disks {
			totalUsed += d.UsedBytes
			totalTotal += d.TotalBytes
		}
		snapshot.DiskUsedBytes = totalUsed
		snapshot.DiskTotalBytes = totalTotal
	}
	if data.AgentInfo != nil {
		snapshot.Metadata = data.AgentInfo
	}

	c.agents[agentID] = cloneSnapshot(snapshot)
	c.mu.Unlock() // release before the sliding-window writes (sync.Map is independent)

	// --- 2. Append to sliding-window cache (lock-free per window) -------------
	for _, s := range data.Metrics {
		tMs := s.Timestamp.UnixMilli()
		c.appendSample(windowKey(userID, agentID, "agent_sample_interval_seconds", ""), tMs, s.IntervalSeconds, s.Missing["agent_sample_interval_seconds"])

		c.appendSample(windowKey(userID, agentID, "agent_cpu_usage", ""), tMs, s.CPUUsagePercent, s.Missing["agent_cpu_usage"])
		c.appendSample(windowKey(userID, agentID, "agent_cpu_iowait", ""), tMs, s.CPUIOWaitPercent, s.Missing["agent_cpu_iowait"])
		c.appendSample(windowKey(userID, agentID, "agent_cpu_steal", ""), tMs, s.CPUStealPercent, s.Missing["agent_cpu_steal"])
		c.appendSample(windowKey(userID, agentID, "agent_ram_used", ""), tMs, float64(s.RAMUsedBytes), s.Missing["agent_ram_used"])
		c.appendSample(windowKey(userID, agentID, "agent_swap_used", ""), tMs, float64(s.RAMSwapUsedBytes), s.Missing["agent_swap_used"])
		if !s.NetworkMissing {
			c.appendSample(windowKey(userID, agentID, "agent_rx_bytes", ""), tMs, s.RXBytes, s.Missing["agent_rx_bytes"])
			c.appendSample(windowKey(userID, agentID, "agent_tx_bytes", ""), tMs, s.TXBytes, s.Missing["agent_tx_bytes"])
		}

		for _, disk := range s.Disks {
			c.appendSample(windowKey(userID, agentID, "agent_disk_used", disk.Path), tMs, float64(disk.UsedBytes), s.Missing["agent_disk_used"])
			c.appendSample(windowKey(userID, agentID, "agent_disk_read_bytes", disk.Path), tMs, float64(disk.ReadBytes), s.Missing["agent_disk_read_bytes"])
			c.appendSample(windowKey(userID, agentID, "agent_disk_write_bytes", disk.Path), tMs, float64(disk.WriteBytes), s.Missing["agent_disk_write_bytes"])

			usagePct := math.NaN()
			if disk.TotalBytes > 0 {
				usagePct = float64(disk.UsedBytes) / float64(disk.TotalBytes) * 100.0
			}
			c.appendSample(windowKey(userID, agentID, "agent_disk_usage", disk.Path), tMs, usagePct, s.Missing["agent_disk_usage"])
		}
	}
}

// Get retrieves the latest snapshot for an agent.
func (c *RealtimeCache) Get(agentID string) (*AgentSnapshot, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	s, ok := c.agents[agentID]
	if !ok {
		return nil, false
	}
	return cloneSnapshot(s), true
}

// Delete evicts the latest snapshot for an agent from memory.
func (c *RealtimeCache) Delete(agentID string) {
	c.evictNetwork(agentID, false)
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.agents, agentID)
}

// GetAll returns a shallow copy of all agent snapshots.
func (c *RealtimeCache) GetAll() map[string]*AgentSnapshot {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make(map[string]*AgentSnapshot, len(c.agents))
	for k, v := range c.agents {
		out[k] = cloneSnapshot(v)
	}
	return out
}

func max0(v float64) float64 {
	if v < 0 {
		return 0
	}
	return v
}

func cloneSnapshot(s *AgentSnapshot) *AgentSnapshot {
	if s == nil {
		return nil
	}
	out := *s
	if s.Missing != nil {
		out.Missing = make(map[string]bool, len(s.Missing))
		for k, v := range s.Missing {
			out.Missing[k] = v
		}
	}
	out.Disks = append([]agentparser.DiskTelemetry(nil), s.Disks...)
	if s.Metadata != nil {
		info := *s.Metadata
		out.Metadata = &info
	}
	if s.Temperatures != nil {
		out.Temperatures = make(map[string]float64, len(s.Temperatures))
		for k, v := range s.Temperatures {
			out.Temperatures[k] = v
		}
	}
	return &out
}

func (c *RealtimeCache) appendSample(key string, t int64, value float64, missing bool) {
	if missing {
		value = math.NaN()
	}
	c.appendPoint(key, t, value)
}
