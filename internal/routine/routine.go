package routine

import (
	a "certainstats/internal/base/alert"
	log "certainstats/internal/logger"
	"certainstats/internal/metrics"
	"certainstats/internal/ws"
	"context"
	"crypto/rand"
	"os"
	"strconv"
	"time"
)

// Start runs the central timer loop for all background tasks
func (e *Routine) Start(ctx context.Context) {
	if e.beszelTicks == nil {
		e.beszelTicks = make(map[string]int)
	}
	// 1. Determine interval from environment
	interval := 60 * time.Second
	if env := os.Getenv("UPDATE_EVERY"); env != "" {
		if d, err := time.ParseDuration(env); err == nil {
			interval = d
		} else if i, err := strconv.Atoi(env); err == nil {
			interval = time.Duration(i) * time.Second
		}
	}

	log.Printf("[Timer] Central loop started with interval: %v", interval)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// UI Sync Ticker (Synchronized Pulse)
	uiTicker := time.NewTicker(10 * time.Second)
	defer uiTicker.Stop()

	// 2. Beszel Heartbeat Ticker (Syncs with UPDATE_EVERY by default, but allows override)
	beszelInterval := interval
	if env := os.Getenv("BESZEL_EVERY"); env != "" {
		if d, err := time.ParseDuration(env); err == nil {
			beszelInterval = d
		} else if i, err := strconv.Atoi(env); err == nil {
			beszelInterval = time.Duration(i) * time.Second
		}
	}
	log.Printf("[Timer] Beszel heartbeat loop started with interval: %v", beszelInterval)

	beszelTicker := time.NewTicker(beszelInterval)
	defer beszelTicker.Stop()

	// Track last runs for lower-frequency tasks
	lastCleanup := time.Now()

	for {
		select {
		case <-ctx.Done():
			return
		case <-beszelTicker.C:
			// Task 0: Beszel Heartbeats (Pulls)
			if e.WS != nil {
				e.WS.Range(func(token string, hub *ws.Hub) {
					var b [4]byte
					_, _ = rand.Read(b[:])
					reqID := uint32(b[0])<<24 | uint32(b[1])<<16 | uint32(b[2])<<8 | uint32(b[3])

					// Decide if we want full details (IncludeDetails: true)
					includeDetails := false
					e.beszelTicks[token]++
					if e.beszelTicks[token] >= 30 {
						includeDetails = true
						e.beszelTicks[token] = 0
					}

					_ = hub.Send(ws.HubRequest[ws.DataRequestOptions]{
						Action: ws.GetData,
						Data: ws.DataRequestOptions{
							CacheTimeMs:    60000,
							IncludeDetails: includeDetails,
						},
						Id: &reqID,
					})
				})
			}
		case <-uiTicker.C:
			// Task 1: Synchronized UI Pulse
			if e.Broadcaster != nil {
				e.PulseSync(ctx)
			}
		case <-ticker.C:
			// Task A: Alert Evaluation (Every Tick)
			e.EvaluateAll(ctx)

			// Task B: Agent Health Check (Every Tick)
			if n, err := e.Store.AgentMarkOffline(ctx, interval*2); err == nil && n > 0 {
				log.Debugf("[Timer] Marked %d agents as offline", n)
			}

			// Task C: Maintenance & Cleanup (Every Hour)
			if time.Since(lastCleanup) > 1*time.Hour {
				e.runCleanup(ctx)
				lastCleanup = time.Now()
			}
		}
	}
}

func (e *Routine) runCleanup(ctx context.Context) {
	log.Debugln("[Timer] Running hourly maintenance cleanup...")

	// 1. Purge expired web sessions
	if err := e.Store.SessionDeleteExpired(ctx); err != nil {
		log.Printf("[Timer] Session cleanup error: %v", err)
	} else {
		log.Debugln("[Timer] Expired sessions purged")
	}

	// 2. Evict expired windows from metrics cache to prevent OOM
	if e.Cache != nil {
		e.Cache.EvictExpiredWindows()
		log.Debugln("[Timer] Expired metrics cache windows evicted")
	}
}

func (e *Routine) EvaluateAll(ctx context.Context) {
	// 1. Fetch ALL enabled alerts, their mapped agents, and current agent info
	alerts, agentInfoMap, err := e.Store.GetActiveAlertsWithState(ctx)
	if err != nil {
		log.Println("Alert Engine Error:", err)
		return
	}

	for _, alert := range alerts {
		// Parse the duration (e.g. "5m")
		duration, err := time.ParseDuration(alert.Trigger.Duration)
		if err != nil {
			continue // Invalid duration, skip
		}

		for _, agentState := range alert.Agents {
			// 2. Map trigger type to actual TSDB metric name
			metricToQuery := string(alert.Trigger.Type)
			switch alert.Trigger.Type {
			case a.TriggerTypeRAM:
				metricToQuery = "agent_ram_used"
			case a.TriggerTypeDisk:
				metricToQuery = "agent_disk_used"
			case a.TriggerTypeCPU:
				metricToQuery = "agent_cpu_usage"
			case a.TriggerTypeCPUIOWait:
				metricToQuery = "agent_cpu_iowait"
			case a.TriggerTypeCPUSteal:
				metricToQuery = "agent_cpu_steal"
			case a.TriggerTypeSwap:
				metricToQuery = "agent_swap_used"
			case a.TriggerTypeNetRx:
				metricToQuery = "agent_rx_bytes"
			case a.TriggerTypeNetTx:
				metricToQuery = "agent_tx_bytes"
			case a.TriggerTypeDiskRead:
				metricToQuery = "agent_disk_read_bytes"
			case a.TriggerTypeDiskWrite:
				metricToQuery = "agent_disk_write_bytes"
			}

			// Fetch the aggregate metric for this agent over the duration
			avgValue, err := metrics.GetAverageMetric(ctx, e.TSDB, agentState.AgentID, metricToQuery, duration)
			if err != nil {
				log.Println("Alert Engine Error:", err)
				continue
			}

			isViolating := false
			info := agentInfoMap[agentState.AgentID]
			valToEvaluate := avgValue

			if alert.Trigger.Type == a.TriggerTypeDown {
				isViolating = !info.IsOnline // Agent is down if IsOnline is false
				valToEvaluate = 0
			} else {
				// Convert unit representation based on trigger type
				switch alert.Trigger.Type {
				case a.TriggerTypeRAM:
					if info.RamSize > 0 {
						valToEvaluate = (avgValue / float64(info.RamSize)) * 100.0
					}
				case a.TriggerTypeDisk:
					if info.DiskSize > 0 {
						valToEvaluate = (avgValue / float64(info.DiskSize)) * 100.0
					}
				case a.TriggerTypeSwap:
					if info.SwapSize > 0 {
						valToEvaluate = (avgValue / float64(info.SwapSize)) * 100.0
					}
				case a.TriggerTypeNetRx, a.TriggerTypeNetTx, a.TriggerTypeDiskRead, a.TriggerTypeDiskWrite:
					// Convert TSDB's delta bytes in average interval to KB/s rate (bytes/60 / 1024)
					valToEvaluate = (avgValue / 60.0) / 1024.0
				}

				isViolating = e.evaluate(valToEvaluate, alert.Trigger.Operator, alert.Trigger.Threshold)
			}

			// 4. Handle State Transitions
			if isViolating && (agentState.Status == "ok" || agentState.Status == "failed") {
				// STATE CHANGE: OK/FAILED -> FIRING
				e.TriggerAlert(ctx, alert, agentState, info, valToEvaluate)

			} else if !isViolating && (agentState.Status == "firing" || agentState.Status == "failed") {
				// STATE CHANGE: FIRING/FAILED -> OK
				e.ResolveAlert(ctx, alert, agentState, info)
			}
		}
	}
}

func (e *Routine) evaluate(value float64, op a.Operator, threshold float64) bool {
	switch op {
	case a.OpGreaterThan:
		return value > threshold
	case a.OpLessThan:
		return value < threshold
	case a.OpEquals:
		return value == threshold
	}
	return false
}

// PulseSync gathers all cached snapshots and broadcasts them to active UI sessions
func (e *Routine) PulseSync(ctx context.Context) {
	if e.Cache == nil || e.Broadcaster == nil {
		return
	}

	// 1. Get all snapshots once
	allSnaps := e.Cache.GetAll()
	if len(allSnaps) == 0 {
		return
	}

	// 2. Pulse Admins
	activeUsers := e.Broadcaster.GetActiveUserIDs()
	for _, userID := range activeUsers {
		e.Broadcaster.BroadcastToUser(userID, ws.UIUpdate{
			Type: "agent_update",
			Data: allSnaps,
		})
	}

	// 3. Pulse Dashboards (Public)
	activeDashes := e.Broadcaster.GetActiveDashIDs()
	for _, dashID := range activeDashes {
		dash, agents, err := e.Store.DashboardGetPulseConfig(ctx, dashID)
		if err != nil {
			continue
		}

		rule, ok := dash.AccessRules["public"]
		if !ok {
			continue
		}

		filteredData := make(map[string]any)
		for _, agentID := range agents {
			if snap, ok := allSnaps[agentID.AgentID]; ok {
				filteredData[agentID.PublicAgentID] = e.filterSnapshot(snap, rule.MetricSet())
			}
		}

		if len(filteredData) > 0 {
			e.Broadcaster.BroadcastToDash(dashID, ws.UIUpdate{
				Type: "agent_update",
				Data: filteredData,
			})
		}
	}
}

func (e *Routine) filterSnapshot(snap *metrics.AgentSnapshot, allowed map[string]struct{}) map[string]any {
	out := make(map[string]any)
	out["Timestamp"] = snap.Timestamp

	_, allowedUptime := allowed["uptime"]
	_, allowedCPUUsage := allowed["agent_cpu_usage"]
	_, allowedCPUIOWait := allowed["agent_cpu_iowait"]
	_, allowedCPUSteal := allowed["agent_cpu_steal"]
	_, allowedRAMUsed := allowed["agent_ram_used"]
	_, allowedSWAPUsed := allowed["agent_swap_used"]
	_, allowedDiskUsed := allowed["agent_disk_used"]
	_, allowedDiskReadBytes := allowed["agent_disk_read_bytes"]
	_, allowedDiskWriteBytes := allowed["agent_disk_write_bytes"]
	_, allowedRXBytes := allowed["agent_rx_bytes"]
	_, allowedTXBytes := allowed["agent_tx_bytes"]

	if allowedUptime && snap.Metadata != nil {
		out["Uptime"] = snap.Metadata.Uptime
	}

	if allowedCPUUsage {
		out["CPUUsagePercent"] = snap.CPUUsagePercent
	}

	if allowedCPUIOWait {
		out["CPUIOWaitPercent"] = snap.CPUIOWaitPercent
	}
	if allowedCPUSteal {
		out["CPUStealPercent"] = snap.CPUStealPercent
	}

	if allowedRAMUsed {
		out["RAMUsedBytes"] = snap.RAMUsedBytes
	}

	if allowedSWAPUsed {
		out["RAMSwapUsedBytes"] = snap.RAMSwapUsedBytes
	}

	if allowedDiskUsed {
		out["DiskUsedBytes"] = snap.DiskUsedBytes
		out["DiskTotalBytes"] = snap.DiskTotalBytes
		out["Disks"] = snap.Disks
	}
	if allowedDiskReadBytes {
		out["DiskReadBps"] = snap.DiskReadBps
	}
	if allowedDiskWriteBytes {
		out["DiskWriteBps"] = snap.DiskWriteBps
	}
	if allowedRXBytes {
		out["RXBytes"] = snap.RXBytes
		out["RXBps"] = snap.RXBps
	}
	if allowedTXBytes {
		out["TXBytes"] = snap.TXBytes
		out["TXBps"] = snap.TXBps
	}

	return out
}
