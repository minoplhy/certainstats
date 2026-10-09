package routine

import (
	a "certainstats/internal/base/alert"
	csctx "certainstats/internal/context"
	log "certainstats/internal/logger"
	"certainstats/internal/metrics"
	"certainstats/internal/security"
	"certainstats/internal/ws"
	"certainstats/internal/ws/browserpb"
	"context"
	"crypto/rand"
	"os"
	"strconv"
	"time"

	"google.golang.org/protobuf/proto"
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
			// Task 1: Agent Health Check (Mark offline agents before evaluation)
			if offlineIDs, err := e.Store.AgentMarkOffline(ctx, 3*time.Minute); err == nil && len(offlineIDs) > 0 {
				log.Debugf("[Timer] Marked %d agents as offline", len(offlineIDs))
				for _, id := range offlineIDs {
					e.Cache.Delete(id)
				}
			}

			// Task 2: Alert Evaluation (Every Tick)
			e.EvaluateAll(ctx)

			// Task 3: Retry Failed Alerts (Every Tick)
			e.RetryFailedAlerts(ctx)

			// Task C: Maintenance & Cleanup (Every Hour)
			if time.Since(lastCleanup) > 1*time.Hour {
				e.runCleanup(ctx)
				lastCleanup = time.Now()
			}
		}
	}
}

func (e *Routine) runCleanup(ctx context.Context) {
	csctx.ExpireResponses()
	log.Printf("[Maintenance] response_cache=%v builds=%v public_limits=%v", csctx.CacheStatistics(), csctx.BuildStatistics(), security.PublicLimits.Statistics())
	{
		pending, err := e.Store.IngestionPending(ctx)
		if err != nil {
			log.Printf("journal status error: %v", err)
		} else {
			log.Printf("[Maintenance] ingestion_journal_pending=%d", len(pending))
		}
	}

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
			isViolating := false
			info := agentInfoMap[agentState.AgentID]
			var valToEvaluate float64

			if alert.Trigger.Type == a.TriggerTypeDown {
				isViolating = !info.IsOnline // Agent is down if IsOnline is false
				valToEvaluate = 0
			} else {
				if e.TSDB == nil {
					continue
				}

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

				valToEvaluate = avgValue

				// Convert unit representation based on trigger type
				switch alert.Trigger.Type {
				case a.TriggerTypeRAM:
					if info.RamSize == 0 {
						continue
					}
					if info.RamSize > 0 {
						valToEvaluate = (avgValue / float64(info.RamSize)) * 100.0
					}
				case a.TriggerTypeDisk:
					if info.DiskSize == 0 {
						continue
					}
					if info.DiskSize > 0 {
						valToEvaluate = (avgValue / float64(info.DiskSize)) * 100.0
					}
				case a.TriggerTypeSwap:
					if info.SwapSize == 0 {
						continue
					}
					if info.SwapSize > 0 {
						valToEvaluate = (avgValue / float64(info.SwapSize)) * 100.0
					}
				case a.TriggerTypeNetRx, a.TriggerTypeNetTx, a.TriggerTypeDiskRead, a.TriggerTypeDiskWrite:
					// Convert TSDB's delta bytes in average interval to KB/s rate (bytes/60 / 1024)
					valToEvaluate = avgValue / 1024.0
				}

				isViolating = e.evaluate(valToEvaluate, alert.Trigger.Operator, alert.Trigger.Threshold)
			}

			// 4. Handle State Transitions
			if isViolating && agentState.Status == "ok" {
				// STATE CHANGE: OK -> FIRING
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
	if allSnaps == nil {
		allSnaps = make(map[string]*metrics.AgentSnapshot)
	}

	// 2. Pulse Admins
	activeUsers := e.Broadcaster.GetActiveUserIDs()
	for _, userID := range activeUsers {
		ownedSnaps := make(map[string]*browserpb.Snapshot)
		agents, err := e.Store.AgentList(ctx, userID)
		if err != nil {
			continue
		}
		for _, a := range agents {
			item := ws.BrowserSnapshot(allSnaps[a.AgentID], nil)
			item.IsOnline = proto.Bool(a.IsOnline)
			item.Available = proto.Bool(allSnaps[a.AgentID] != nil)
			ownedSnaps[a.AgentID] = item
		}
		e.Broadcaster.BroadcastToUser(userID, &browserpb.TelemetryEnvelope{
			Pulse: &browserpb.TelemetryPulse{Agents: ownedSnaps},
		})
	}

	// 3. Pulse Dashboards (Public)
	activeDashes := e.Broadcaster.GetActiveDashIDs()
	for _, dashID := range activeDashes {
		dash, agents, err := e.Store.DashboardGetPulseConfig(ctx, dashID)
		if err != nil {
			e.Broadcaster.CloseDash(dashID)
			continue
		}

		rule, ok := dash.AccessRules["public"]
		if !ok || rule.IsEmpty() {
			e.Broadcaster.CloseDash(dashID)
			continue
		}

		filteredData := make(map[string]*browserpb.Snapshot)
		allowed := rule.MetricSet()
		for feature := range rule.FeatureSet() {
			allowed[feature] = struct{}{}
		}
		for _, agent := range agents {
			if snap := allSnaps[agent.AgentID]; snap != nil {
				filteredData[agent.PublicAgentID] = ws.BrowserSnapshot(snap, allowed)
			}
			if _, ok := allowed["is_online"]; ok {
				info, err := e.Store.AgentGetByID(ctx, agent.AgentID, dash.UserID)
				if err == nil {
					item := filteredData[agent.PublicAgentID]
					if item == nil {
						item = &browserpb.Snapshot{Available: proto.Bool(false)}
						filteredData[agent.PublicAgentID] = item
					}
					item.IsOnline = proto.Bool(info.IsOnline)
				}
			}
		}
		// Empty full pulses still establish freshness, including empty dashboards.
		e.Broadcaster.BroadcastToDash(dashID, &browserpb.TelemetryEnvelope{
			Pulse: &browserpb.TelemetryPulse{Agents: filteredData},
		})
	}
}
