// Package networkservice provides provider-neutral network monitoring orchestration.
package networkservice

import (
	"certainstats/internal/agent_parser/registry"
	"certainstats/internal/agentmeta"
	"certainstats/internal/alertdelivery"
	log "certainstats/internal/logger"
	"certainstats/internal/metrics"
	nm "certainstats/internal/networkmonitor"
	"certainstats/internal/store"
	"certainstats/internal/ws"
	"context"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/tsdb"
)

const (
	// syncTimeout bounds one configuration request to an agent.
	syncTimeout = 5 * time.Second
	// probeTimeout bounds a configuration request that also runs an immediate probe.
	probeTimeout = 15 * time.Second
	// changeTimeout bounds the background work after a configuration change.
	changeTimeout = 30 * time.Second
)

const lockStripes = 256

// tsdbMetrics are the per-result series names, without the network_monitor_ prefix.
var tsdbMetrics = []string{"attempt_count", "success_count", "response_sum_us", "response_min_us", "response_max_us"}

// Service coordinates configuration sync, result ingestion, history and alerts.
type Service struct {
	Store    store.FullStore
	Registry *registry.Registry
	TSDB     *tsdb.DB
	Cache    *metrics.RealtimeCache
	WS       *ws.Manager

	// dataMu orders TSDB commits against history reads and cache publication.
	dataMu sync.RWMutex

	// builds coalesces identical history response builds by cache key.
	buildMu sync.Mutex
	builds  map[string]chan struct{}

	// ingestLocks serialize result ingestion per agent; syncLocks serialize
	// configuration requests per agent.
	ingestLocks [lockStripes]sync.Mutex
	syncLocks   [lockStripes]sync.Mutex
}

func lockIndex(agentID string) uint8 {
	var index uint8
	for _, b := range []byte(agentID) {
		index = index*31 + b
	}
	return index
}

// Sync sends the agent's full configuration when it has changed, or always
// when force is set, and records the acknowledgement.
func (s *Service) Sync(ctx context.Context, userID, agentID string, session nm.Session, force bool) error {
	lock := &s.syncLocks[lockIndex(agentID)]
	lock.Lock()
	defer lock.Unlock()

	agent, err := s.Store.AgentGetByID(ctx, agentID, userID)
	if err != nil {
		return err
	}
	caps := s.Registry.Capabilities(agent.AgentType, agent.Runtime())
	if !caps.Supports(agentmeta.FeatureConfigure) {
		return nil
	}
	adapter, ok := s.Registry.NetworkAdapter(agent.AgentType)
	if !ok {
		return nil
	}

	configs, state, err := s.Store.NetworkConfigs(ctx, userID, agentID)
	if err != nil {
		return err
	}
	if !force && state.Desired == state.Ack {
		return nil
	}
	// Never push a set the agent cannot apply; the generation stays pending.
	for _, c := range configs {
		if !caps.SupportsConfig(c.Protocol, c.Server) {
			return nil
		}
	}

	callCtx, cancel := context.WithTimeout(ctx, syncTimeout)
	defer cancel()
	_, err = adapter.Apply(callCtx, session, nm.Operation{Action: nm.ActionReplace, Configs: configs})
	message := ""
	if err != nil {
		message = err.Error()
	}
	if ackErr := s.Store.NetworkAck(context.WithoutCancel(ctx), userID, agentID, state.Desired, message); ackErr != nil {
		return ackErr
	}
	return err
}

// Changed pushes a committed change to a connected agent: op first, when
// given, then a full Sync to reconcile concurrent edits. Disconnected agents
// synchronize on reconnect.
func (s *Service) Changed(userID, agentID string, op *nm.Operation) {
	if s.WS == nil {
		return
	}
	ctx := context.Background()
	agent, err := s.Store.AgentGetByID(ctx, agentID, userID)
	if err != nil {
		log.Printf("[Network] Agent lookup failed for %s: %v", agentID, err)
		return
	}
	token, err := s.Store.AgentToken(ctx, agentID, userID)
	if err != nil {
		log.Printf("[Network] Agent token lookup failed for %s: %v", agentID, err)
		return
	}
	hub, ok := s.WS.GetHub(token)
	if !ok {
		return
	}

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), changeTimeout)
		defer cancel()
		if op != nil {
			s.applyIncremental(ctx, userID, agent, hub, *op)
		}
		if err := s.Sync(ctx, userID, agentID, hub, true); err != nil {
			log.Printf("[Network] Configuration synchronization failed for %s: %v", agentID, err)
		}
	}()
}

// applyIncremental sends one operation and ingests its immediate probe result.
func (s *Service) applyIncremental(ctx context.Context, userID string, agent *store.Agent, hub *ws.Hub, op nm.Operation) {
	adapter, ok := s.Registry.NetworkAdapter(agent.AgentType)
	if !ok {
		return
	}
	lock := &s.syncLocks[lockIndex(agent.AgentID)]
	lock.Lock()
	defer lock.Unlock()

	timeout := syncTimeout
	if op.RunNow {
		timeout = probeTimeout
	}
	callCtx, cancel := context.WithTimeout(ctx, timeout)
	result, err := adapter.Apply(callCtx, hub, op)
	cancel()
	if err != nil {
		log.Printf("[Network] Incremental configuration failed for %s: %v", agent.AgentID, err)
		return
	}
	if result == nil {
		return
	}
	results := map[string]nm.Result{op.Config.ID: *result}
	if err = s.Accept(ctx, userID, agent.AgentID, hub.ConnectionEpoch, results); err != nil {
		log.Printf("[Network] Immediate probe ingestion failed for %s: %v", agent.AgentID, err)
	}
}

// Accept journals, commits and evaluates one connection's results. Pending
// batches for the agent are replayed first to keep sample order.
func (s *Service) Accept(ctx context.Context, userID, agentID, epoch string, results map[string]nm.Result) error {
	if len(results) > nm.MaxResultsPerBatch {
		return fmt.Errorf("monitor result limit exceeded")
	}
	lock := &s.ingestLocks[lockIndex(agentID)]
	lock.Lock()
	defer lock.Unlock()

	pending, err := s.Store.NetworkPending(ctx, agentID)
	if err != nil {
		return err
	}
	for _, b := range pending {
		if err = s.commit(ctx, b); err != nil {
			return err
		}
	}
	if len(results) == 0 {
		return nil
	}

	batch, err := s.Store.NetworkBegin(ctx, nm.Batch{
		ID:      nm.ID(),
		UserID:  userID,
		AgentID: agentID,
		Epoch:   epoch,
		Results: results,
	})
	if err != nil {
		return err
	}
	if len(batch.Results) == 0 {
		return nil
	}
	if err = s.commit(ctx, *batch); err != nil {
		return err
	}
	return s.Evaluate(ctx, agentID)
}

// commit writes a journaled batch to TSDB, then the sample cache, then marks
// it complete. Replaying a batch rewrites the same timestamps.
func (s *Service) commit(ctx context.Context, b nm.Batch) error {
	if s.TSDB == nil {
		return fmt.Errorf("network TSDB unavailable")
	}
	app := s.TSDB.Appender(ctx)
	defer app.Rollback()

	for monitorID, r := range b.Results {
		values := map[string]float64{
			"attempt_count":   float64(r.Total),
			"success_count":   float64(r.Success),
			"response_sum_us": float64(r.Sum),
			"response_min_us": float64(r.Min),
			"response_max_us": float64(r.Max),
		}
		// Min and max are meaningless when every probe failed.
		if r.Success == 0 {
			values["response_min_us"] = math.NaN()
			values["response_max_us"] = math.NaN()
		}
		for name, value := range values {
			series := labels.FromStrings(
				"__name__", "network_monitor_"+name,
				"user_id", b.UserID,
				"agent_id", b.AgentID,
				"monitor_id", monitorID,
			)
			if _, err := app.Append(0, series, b.Timestamp, value); err != nil {
				return err
			}
		}
	}

	s.dataMu.Lock()
	defer s.dataMu.Unlock()
	if err := app.Commit(); err != nil {
		return err
	}
	if s.Cache != nil {
		for monitorID, r := range b.Results {
			key := metrics.NetworkKey{Owner: b.UserID, Agent: b.AgentID, Monitor: monitorID}
			s.Cache.UpdateNetwork(key, metrics.NetworkSample{
				Timestamp: b.Timestamp,
				Attempts:  float64(r.Total),
				Success:   float64(r.Success),
				Sum:       float64(r.Sum),
				Min:       float64(r.Min),
				Max:       float64(r.Max),
			})
		}
	}
	return s.Store.NetworkComplete(ctx, b)
}

// Recover replays batches journaled before a restart, with their original timestamps.
func (s *Service) Recover(ctx context.Context) error {
	pending, err := s.Store.NetworkPending(ctx, "")
	if err != nil {
		return err
	}
	for _, b := range pending {
		if err = s.commit(ctx, b); err != nil {
			return err
		}
	}
	return nil
}

// Evaluate runs loss rules for one agent, or all agents when agentID is
// empty, and dispatches the queued notifications.
func (s *Service) Evaluate(ctx context.Context, agentID string) error {
	events, err := s.Store.NetworkEvaluate(ctx, agentID)
	if err != nil {
		return err
	}
	for _, eventID := range events {
		go alertdelivery.Dispatch(context.Background(), s.Store, eventID)
	}
	return nil
}
