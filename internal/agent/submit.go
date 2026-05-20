package agent

import (
	agentdata "certainstats/internal/agent_data"
	agentparser "certainstats/internal/agent_parser"
	"certainstats/internal/agent_parser/registry"
	"certainstats/internal/metrics"
	apiresponse "certainstats/internal/response"

	"certainstats/internal/store"
	"io"
	"sync"

	"context"
	"log"
	"net/http"
	"time"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/tsdb"
)

// agentIOState tracks previous IO values per agent for normalization.
type agentIOState struct {
	lastTime  time.Time
	lastRX    float64
	lastTX    float64
}

var ioStates sync.Map // map[agentID]*agentIOState

// normalizeIOMetrics converts all throughput metrics in-place to IODelta (bytes in interval).
// This ensures TSDB and cache always receive the same format regardless of agent type.
func normalizeIOMetrics(agentID string, metrics []agentparser.Telemetry) {
	var state *agentIOState
	if val, ok := ioStates.Load(agentID); ok {
		state = val.(*agentIOState)
	} else {
		state = &agentIOState{}
		ioStates.Store(agentID, state)
	}

	for i := range metrics {
		m := &metrics[i]
		dt := 60.0 // reasonable fallback
		if !state.lastTime.IsZero() {
			elapsed := m.Timestamp.Sub(state.lastTime).Seconds()
			if elapsed > 0 {
				dt = elapsed
			}
		}

		// Normalize network
		switch m.NetworkIOType {
		case agentparser.IOCumulative:
			if state.lastRX > 0 || state.lastTX > 0 {
				rxDelta := m.RXBytes - state.lastRX
				txDelta := m.TXBytes - state.lastTX
				if rxDelta < 0 { rxDelta = 0 }
				if txDelta < 0 { txDelta = 0 }
				state.lastRX = m.RXBytes
				state.lastTX = m.TXBytes
				m.RXBytes = rxDelta
				m.TXBytes = txDelta
			} else {
				// First data point — store baseline, emit zero
				state.lastRX = m.RXBytes
				state.lastTX = m.TXBytes
				m.RXBytes = 0
				m.TXBytes = 0
			}
		case agentparser.IORate:
			m.RXBytes = m.RXBytes * dt
			m.TXBytes = m.TXBytes * dt
		case agentparser.IODelta:
			// already correct
		}

		// Normalize disk
		switch m.DiskIOType {
		case agentparser.IORate:
			for j := range m.Disks {
				m.Disks[j].ReadBytes = uint64(float64(m.Disks[j].ReadBytes) * dt)
				m.Disks[j].WriteBytes = uint64(float64(m.Disks[j].WriteBytes) * dt)
			}
		case agentparser.IODelta:
			// already correct
		}

		state.lastTime = m.Timestamp
	}
}

func SubmitHandler(agents store.AgentStore, tdb *tsdb.DB, parserRegistry *registry.Registry, cache *metrics.RealtimeCache) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// 1. Read Payload (Max 16KB)
		data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 16384))
		if err != nil {
			apiresponse.Error(w, http.StatusBadRequest, "payload too large or read error")
			return
		}

		agentType, token, err := parserRegistry.Detect(data)
		if err != nil {
			log.Printf("detection failed: %v", err)
			apiresponse.Error(w, http.StatusBadRequest, "invalid payload or token")
			return
		}

		identity, err := agents.AgentGetByToken(r.Context(), token)
		if err != nil {
			apiresponse.Error(w, http.StatusUnauthorized, "unauthorized")
			return
		}

		parsedData, err := parserRegistry.ParsePayload(agentType, data)
		if err != nil {
			log.Printf("invalid payload from %s: %s", agentType, err)
			apiresponse.Error(w, http.StatusBadRequest, "invalid payload")
			return
		}

		// 4. Force root-only disks for consistent telemetry
		if parsedData.AgentInfo != nil {
			var rootDiskSize uint64
			foundRoot := false

			for i := range parsedData.Metrics {
				var filteredDisks []agentparser.DiskTelemetry
				for _, d := range parsedData.Metrics[i].Disks {
					// We only care about the root disk
					if d.Path == "/" {
						filteredDisks = append(filteredDisks, d)
						if !foundRoot {
							rootDiskSize = d.TotalBytes
							foundRoot = true
						}
					}
				}
				parsedData.Metrics[i].Disks = filteredDisks
			}

			// Override the global disk size with the root disk size if found
			if foundRoot {
				parsedData.AgentInfo.DiskSize = rootDiskSize
			}
		}

		// 4. Update Agent State (SQLite) - Pure asynchronous execution, letting database/sql connection pool serialize the writes
		go func(agentID, userID string, info *agentparser.ParsedMetadata) {
			ctxBg := context.Background()
			var dbErr error
			if info != nil {
				dbErr = agents.AgentUpsertDetails(ctxBg, store.Agent{
					AgentID:      agentID,
					UserID:       userID,
					Uptime:       info.Uptime,
					LinuxVersion: info.LinuxVersion,
					CpuModel:     info.CpuModel,
					CpuCores:     info.CpuCores,
					RamSize:      info.RamSize,
					SwapSize:     info.SwapSize,
					DiskSize:     info.DiskSize,
				})
			} else {
				dbErr = agents.AgentUpdateHeartbeat(ctxBg, agentID, userID)
			}
			if dbErr != nil {
				log.Printf("submit background state update error for %s: %v", agentID, dbErr)
			}
		}(identity.AgentID, identity.UserID, parsedData.AgentInfo)

		// 5. Normalize IO metrics to delta bytes (in-place) before TSDB + cache
		if len(parsedData.Metrics) > 0 {
			normalizeIOMetrics(identity.AgentID, parsedData.Metrics)

			var batchRX, batchTX float64
			diskDeltas := make(map[string]*store.DiskDelta)
			for _, m := range parsedData.Metrics {
				batchRX += m.RXBytes
				batchTX += m.TXBytes
				for _, d := range m.Disks {
					if d.Path == "" {
						continue
					}
					if existing, ok := diskDeltas[d.Path]; ok {
						existing.ReadBytes += d.ReadBytes
						existing.WriteBytes += d.WriteBytes
					} else {
						diskDeltas[d.Path] = &store.DiskDelta{
							Path:       d.Path,
							ReadBytes:  d.ReadBytes,
							WriteBytes: d.WriteBytes,
						}
					}
				}
			}

			var disks []store.DiskDelta
			for _, dd := range diskDeltas {
				disks = append(disks, *dd)
			}

			_ = agents.AgentIncrementTraffic(r.Context(), identity.AgentID, identity.UserID, uint64(batchRX), uint64(batchTX), disks)
		}

		// 6. TSDB Write
		if len(parsedData.Metrics) > 0 {
			if err := WriteStatsToTSDB(r.Context(), tdb, identity, parsedData.Metrics); err != nil {
				log.Printf("tsdb write error for %s: %v", identity.AgentID, err)
				apiresponse.Error(w, http.StatusInternalServerError, "internal error")
				return
			}
		}

		// 7. Update Realtime Cache
		if cache != nil {
			cache.Update(identity.AgentID, parsedData)
		}

		w.WriteHeader(http.StatusOK)
		w.Write([]byte("1"))
	}
}

func WriteStatsToTSDB(ctx context.Context, tdb *tsdb.DB, identity *store.AgentIdentity, metrics []agentparser.Telemetry) error {
	app := tdb.Appender(ctx)

	lbl := func(name string) labels.Labels {
		return labels.FromStrings(
			"__name__", name,
			"user_id", identity.UserID,
			"agent_id", identity.AgentID,
		)
	}

	now := time.Now()
	for i, s := range metrics {
		// Calculate timestamp based on position in batch
		// Stats are sent in chronological order, with the last one being 'now'
		ago := len(metrics) - 1 - i
		tMs := now.Add(time.Duration(-ago*agentdata.TIME_DIFF) * time.Second).UnixMilli()

		app.Append(0, lbl("agent_cpu_usage"), tMs, s.CPUUsagePercent)
		app.Append(0, lbl("agent_cpu_iowait"), tMs, s.CPUIOWaitPercent)
		app.Append(0, lbl("agent_cpu_steal"), tMs, s.CPUStealPercent)
		app.Append(0, lbl("agent_ram_used"), tMs, float64(s.RAMUsedBytes))
		app.Append(0, lbl("agent_swap_used"), tMs, float64(s.RAMSwapUsedBytes))

		// Multi-disk Support
		for _, disk := range s.Disks {
			diskLabels := labels.FromStrings(
				"__name__", "agent_disk_used",
				"user_id", identity.UserID,
				"agent_id", identity.AgentID,
				"path", disk.Path,
			)
			app.Append(0, diskLabels, tMs, float64(disk.UsedBytes))

			diskPctLabels := labels.FromStrings(
				"__name__", "agent_disk_usage",
				"user_id", identity.UserID,
				"agent_id", identity.AgentID,
				"path", disk.Path,
			)
			usagePct := 0.0
			if disk.TotalBytes > 0 {
				usagePct = (float64(disk.UsedBytes) / float64(disk.TotalBytes)) * 100.0
			}
			app.Append(0, diskPctLabels, tMs, usagePct)

			// Disk Activity (Read/Write)
			readLabels := labels.FromStrings(
				"__name__", "agent_disk_read_bytes",
				"user_id", identity.UserID,
				"agent_id", identity.AgentID,
				"path", disk.Path,
			)
			app.Append(0, readLabels, tMs, float64(disk.ReadBytes))

			writeLabels := labels.FromStrings(
				"__name__", "agent_disk_write_bytes",
				"user_id", identity.UserID,
				"agent_id", identity.AgentID,
				"path", disk.Path,
			)
			app.Append(0, writeLabels, tMs, float64(disk.WriteBytes))
		}

		app.Append(0, lbl("agent_rx_bytes"), tMs, s.RXBytes)
		app.Append(0, lbl("agent_tx_bytes"), tMs, s.TXBytes)
	}

	return app.Commit()
}
