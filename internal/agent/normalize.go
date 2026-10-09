package agent

import (
	agentdata "certainstats/internal/agent_data"
	agentparser "certainstats/internal/agent_parser"
	"math"
	"os"
	"strconv"
	"sync"
	"time"
)

// Fixed stripes bound lock state while serializing all processing for an agent.
var ingestionLocks [256]sync.Mutex

func lockAgent(id string) func() {
	var h uint32 = 2166136261
	for _, c := range []byte(id) {
		h = (h ^ uint32(c)) * 16777619
	}
	m := &ingestionLocks[h%256]
	m.Lock()
	return m.Unlock
}

type agentIOState struct {
	lastTime       time.Time
	lastRX, lastTX float64
	baseline       bool
}

var ioStates sync.Map

func configuredCadence() float64 {
	for _, key := range []string{"UPDATE_EVERY", "BESZEL_EVERY"} {
		raw := os.Getenv(key)
		if duration, err := time.ParseDuration(raw); err == nil && duration > 0 {
			return duration.Seconds()
		}
		if n, err := strconv.ParseFloat(raw, 64); err == nil && n > 0 && !math.IsInf(n, 0) && !math.IsNaN(n) {
			return n
		}
	}
	return float64(agentdata.TIME_DIFF)
}
func normalizeIOMetrics(agentID string, samples []agentparser.Telemetry) {
	actual, _ := ioStates.LoadOrStore(agentID, &agentIOState{})
	state := actual.(*agentIOState)
	now := time.Now()
	for i := range samples {
		m := &samples[i]
		if m.Timestamp.IsZero() {
			m.Timestamp = now.Add(-time.Duration(len(samples)-1-i) * time.Duration(configuredCadence()*float64(time.Second)))
		}
		dt := m.IntervalSeconds
		if dt <= 0 && !state.lastTime.IsZero() {
			dt = m.Timestamp.Sub(state.lastTime).Seconds()
		}
		if dt <= 0 || math.IsInf(dt, 0) || math.IsNaN(dt) {
			dt = configuredCadence()
		}
		m.IntervalSeconds = dt
		switch m.NetworkIOType {
		case agentparser.IOCumulative:
			rx, tx := m.RXBytes, m.TXBytes
			if state.baseline {
				if rx < state.lastRX || tx < state.lastTX {
					m.NetworkMissing = true
				}
				m.RXBytes = math.Max(0, rx-state.lastRX)
				m.TXBytes = math.Max(0, tx-state.lastTX)
			} else {
				m.RXBytes = 0
				m.TXBytes = 0
				m.NetworkMissing = true
			}
			state.lastRX = rx
			state.lastTX = tx
			state.baseline = true
		case agentparser.IORate:
			m.RXBytes *= dt
			m.TXBytes *= dt
		}
		if m.DiskIOType == agentparser.IORate {
			for j := range m.Disks {
				m.Disks[j].ReadBytes = uint64(float64(m.Disks[j].ReadBytes) * dt)
				m.Disks[j].WriteBytes = uint64(float64(m.Disks[j].WriteBytes) * dt)
			}
		}
		if m.NetworkMissing {
			m.RXBytes, m.TXBytes = 0, 0
		}
		m.NetworkIOType = agentparser.IODelta
		m.DiskIOType = agentparser.IODelta
		state.lastTime = m.Timestamp
	}
}
