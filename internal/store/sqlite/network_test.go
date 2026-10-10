package sqlite

import (
	"certainstats/internal/agentmeta"
	a "certainstats/internal/base/alert"
	nm "certainstats/internal/networkmonitor"
	"certainstats/internal/store"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"
	"time"
)

func networkFixture(t *testing.T) (*Store, []nm.Monitor) {
	t.Helper()
	s := newTestStore(t)
	ctx := context.Background()
	if e := s.CreateUser(ctx, "owner", "owner", "hash", false); e != nil {
		t.Fatal(e)
	}
	if e := s.AgentProvision(ctx, "node", "owner", "token", "Node", "beszel"); e != nil {
		t.Fatal(e)
	}
	if e := s.AgentUpdateRuntime(ctx, "node", "owner", agentmeta.Runtime{AgentVersion: agentmeta.String("0.21.0"), VersionSource: "beszel_info"}); e != nil {
		t.Fatal(e)
	}
	if e := s.AgentUpdateHeartbeat(ctx, "node", "owner"); e != nil {
		t.Fatal(e)
	}
	m, e := s.NetworkCreate(ctx, "owner", []string{"node"}, nm.Config{Target: "example.com", Protocol: "icmp"}, true)
	if e != nil {
		t.Fatal(e)
	}
	return s, m
}
func saveNetwork(t *testing.T, s *Store, m nm.Monitor, epoch string, loss float64, count int64) {
	t.Helper()
	ctx := context.Background()
	r := nm.Result{Avg: 1000, Avg1h: 1000, Min: 500, Min1h: 500, Max: 1500, Max1h: 1500, Loss: loss, Loss1h: loss, Total: 20, Success: 18, Sum: 18000, SampleCount: count, LastProbeAt: time.Now().UnixMilli()}
	b, e := s.NetworkBegin(ctx, nm.Batch{ID: nm.ID(), UserID: m.UserID, AgentID: m.AgentID, Epoch: epoch, Results: map[string]nm.Result{m.ID: r}})
	if e != nil {
		t.Fatal(e)
	}
	if e = s.NetworkComplete(ctx, *b); e != nil {
		t.Fatal(e)
	}
}
func TestRuntimeVersionsAndCapabilities(t *testing.T) {
	s, _ := networkFixture(t)
	ctx := context.Background()
	if e := s.AgentUpdateRuntime(ctx, "node", "owner", agentmeta.Runtime{ProtocolVersion: agentmeta.String("7"), VersionSource: "test"}); e != nil {
		t.Fatal(e)
	}
	a, e := s.AgentGetByID(ctx, "node", "owner")
	if e != nil || *a.AgentVersion != "0.21.0" || *a.ProtocolVersion != "7" {
		t.Fatalf("metadata: %+v %v", a, e)
	}
	if e = s.AgentUpdateRuntime(ctx, "node", "owner", agentmeta.Runtime{AgentVersion: agentmeta.String("0.19.0")}); e != nil {
		t.Fatal(e)
	}
	_, e = s.NetworkCreate(ctx, "owner", []string{"node"}, nm.Config{Target: "new.example.com", Protocol: "icmp"}, true)
	var ce *nm.CapabilityError
	if !errors.As(e, &ce) {
		t.Fatalf("downgrade allowed: %v", e)
	}
}

func TestNetworkListFilters(t *testing.T) {
	s, _ := networkFixture(t)
	ctx := context.Background()
	for _, cfg := range []nm.Config{
		{Target: "https://api.example.com/health", Protocol: "http"},
		{Target: "api.example.com", Protocol: "tcp", Port: 443},
		{Target: "https://api.example.com/health_100%25", Protocol: "http"},
	} {
		if _, err := s.NetworkCreate(ctx, "owner", []string{"node"}, cfg, true); err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct {
		name, user string
		filter     store.NetworkListFilter
		count      int
	}{
		{"case insensitive hostname inside URL", "owner", store.NetworkListFilter{Target: "API.EXAMPLE.COM"}, 3},
		{"protocol and target", "owner", store.NetworkListFilter{Target: "api.example.com", Protocol: "tcp"}, 1},
		{"literal wildcards", "owner", store.NetworkListFilter{Target: "health_100%"}, 1},
		{"legacy node query", "owner", store.NetworkListFilter{Query: "Node"}, 4},
		{"node filter", "owner", store.NetworkListFilter{AgentID: "other"}, 0},
		{"owner isolation", "foreign", store.NetworkListFilter{Target: "example.com"}, 0},
		{"exact target excludes URL matches", "owner", store.NetworkListFilter{TargetExact: "api.example.com"}, 1},
		{"exact URL path", "owner", store.NetworkListFilter{TargetExact: "https://api.example.com/health"}, 1},
		{"exact literal wildcards", "owner", store.NetworkListFilter{TargetExact: "https://api.example.com/health_100%25"}, 1},
		{"exact target combines with search", "owner", store.NetworkListFilter{TargetExact: "api.example.com", Target: "health"}, 0},
		{"exact target combines with protocol", "owner", store.NetworkListFilter{TargetExact: "api.example.com", Protocol: "http"}, 0},
		{"exact target owner isolation", "foreign", store.NetworkListFilter{TargetExact: "api.example.com"}, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			items, total, err := s.NetworkList(ctx, tc.user, tc.filter, 1, 1)
			if err != nil || total != tc.count || len(items) != min(tc.count, 1) {
				t.Fatalf("items=%d total=%d err=%v", len(items), total, err)
			}
			if total > 1 {
				next, count, err := s.NetworkList(ctx, tc.user, tc.filter, 2, 1)
				if err != nil || count != total || len(next) != 1 || next[0].ID == items[0].ID {
					t.Fatalf("invalid pagination: %+v %d %v", next, count, err)
				}
			}
		})
	}
}
func TestNetworkTargets(t *testing.T) {
	s, initial := networkFixture(t)
	ctx := context.Background()
	create := func(target, protocol string, enabled bool) {
		t.Helper()
		if _, err := s.NetworkCreate(ctx, "owner", []string{"node"}, nm.Config{Target: target, Protocol: protocol, Port: 443}, enabled); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.AgentProvision(ctx, "second", "owner", "token2", "Second", "beszel"); err != nil {
		t.Fatal(err)
	}
	if err := s.AgentUpdateRuntime(ctx, "second", "owner", agentmeta.Runtime{AgentVersion: agentmeta.String("0.21.0"), VersionSource: "test"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.NetworkCreate(ctx, "owner", []string{"second"}, initial[0].Config, true); err != nil {
		t.Fatal(err)
	}
	create("https://example.com/health", "http", true)
	create("https://example.com/login", "http", false)
	for i := range 26 {
		create(fmt.Sprintf("z%02d.example.com", i), "icmp", true)
	}
	for _, tc := range []struct {
		name, user string
		filter     store.NetworkListFilter
		want       []string
	}{
		{"distinct sorted HTTP targets", "owner", store.NetworkListFilter{Protocol: "http"}, []string{"https://example.com/health", "https://example.com/login"}},
		{"ignores target and search filters", "owner", store.NetworkListFilter{Protocol: "http", Target: "missing", TargetExact: "example.com", Query: "missing"}, []string{"https://example.com/health", "https://example.com/login"}},
		{"paused targets", "owner", store.NetworkListFilter{State: nm.StatePaused}, []string{"https://example.com/login"}},
		{"active targets", "owner", store.NetworkListFilter{Protocol: "http", State: nm.StateActive}, []string{"https://example.com/health"}},
		{"missing node", "owner", store.NetworkListFilter{AgentID: "other"}, []string{}},
		{"another owner", "foreign", store.NetworkListFilter{}, []string{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := s.NetworkTargets(ctx, tc.user, tc.filter)
			if err != nil || !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("targets=%v want=%v err=%v", got, tc.want, err)
			}
		})
	}
	targets, err := s.NetworkTargets(ctx, "owner", store.NetworkListFilter{AgentID: "node"})
	if err != nil || len(targets) != 29 || targets[0] != "example.com" {
		t.Fatalf("targets=%v err=%v", targets, err)
	}
	if err := s.NetworkArchive(ctx, "owner", initial[0].ID); err != nil {
		t.Fatal(err)
	}
	targets, err = s.NetworkTargets(ctx, "owner", store.NetworkListFilter{State: nm.StateArchived})
	if err != nil || !reflect.DeepEqual(targets, []string{"example.com"}) {
		t.Fatalf("archived targets=%v err=%v", targets, err)
	}
}

func TestNetworkArchiveAndReplacement(t *testing.T) {
	s, items := networkFixture(t)
	ctx := context.Background()
	m := items[0]
	_, e := s.NetworkCreate(ctx, "owner", []string{"node"}, m.Config, true)
	if !errors.Is(e, nm.ErrConflict) {
		t.Fatal(e)
	}
	saveNetwork(t, s, m, "epoch", 10, 3)
	cfg := m.Config
	cfg.Interval = 90
	changed, e := s.NetworkUpdate(ctx, "owner", m.ID, cfg, true)
	if e != nil || changed.ID != m.ID || changed.Latest == nil {
		t.Fatalf("interval edit %+v %v", changed, e)
	}
	cfg.Target = "new.example.com"
	changed, e = s.NetworkUpdate(ctx, "owner", m.ID, cfg, true)
	if e != nil || changed.ID == m.ID || changed.ReplacesID != m.ID || changed.Latest != nil {
		t.Fatalf("replacement %+v %v", changed, e)
	}
	old, e := s.NetworkGet(ctx, "owner", m.ID)
	if e != nil || old.ArchivedAt == nil || old.Latest == nil {
		t.Fatal(old, e)
	}
	if e = s.AgentDelete(ctx, "node", "owner"); e != nil {
		t.Fatal(e)
	}
	old, e = s.NetworkGet(ctx, "owner", changed.ID)
	if e != nil || old.ArchivedAt == nil {
		t.Fatal(old, e)
	}
	if _, e = s.NetworkGet(ctx, "other", m.ID); e == nil {
		t.Fatal("foreign archive visible")
	}
	if e = s.migrateNetworkMonitors(); e != nil {
		t.Fatal(e)
	}
}
func TestNetworkBatchDedupReplayAndCertificate(t *testing.T) {
	s, items := networkFixture(t)
	ctx := context.Background()
	m := items[0]
	r := nm.Result{Total: 3, Success: 3, Sum: 3000, Avg: 1000, Min: 1000, Max: 1000, SampleCount: 3, LastProbeAt: time.Now().UnixMilli(), Cert: &nm.Cert{Issuer: "Test", Expires: time.Now().Add(time.Hour).UnixMilli()}}
	makeBatch := func(epoch string) *nm.Batch {
		t.Helper()
		b, e := s.NetworkBegin(ctx, nm.Batch{ID: nm.ID(), UserID: "owner", AgentID: "node", Epoch: epoch, Results: map[string]nm.Result{m.ID: r, "foreign": r}})
		if e != nil {
			t.Fatal(e)
		}
		return b
	}
	b := makeBatch("first")
	if len(b.Results) != 1 {
		t.Fatal(b)
	}
	pending, e := s.NetworkPending(ctx, "node")
	if e != nil || len(pending) != 1 {
		t.Fatal(pending, e)
	}
	if e = s.NetworkComplete(ctx, *b); e != nil {
		t.Fatal(e)
	}
	if e = s.NetworkComplete(ctx, *b); e != nil {
		t.Fatal(e)
	}
	if len(makeBatch("first").Results) != 0 {
		t.Fatal("duplicate accepted")
	}
	original := r
	r.LastProbeAt--
	r.SampleCount--
	if len(makeBatch("first").Results) != 0 {
		t.Fatal("out-of-order observation accepted")
	}
	r = original
	r.Cert = nil
	next := makeBatch("second")
	if len(next.Results) != 1 {
		t.Fatal("reconnect not accepted")
	}
	if e = s.NetworkComplete(ctx, *next); e != nil {
		t.Fatal(e)
	}
	saved, e := s.NetworkGet(ctx, "owner", m.ID)
	if e != nil || saved.Latest.Cert == nil || saved.Latest.Cert.Issuer != "Test" {
		t.Fatal(saved, e)
	}
	pending, e = s.NetworkPending(ctx, "")
	if e != nil || len(pending) != 0 {
		t.Fatal(pending, e)
	}
}
func TestNetworkIndependentIncidentsAndClosure(t *testing.T) {
	s, items := networkFixture(t)
	ctx := context.Background()
	second, e := s.NetworkCreate(ctx, "owner", []string{"node"}, nm.Config{Target: "second.example.com", Protocol: "icmp"}, true)
	if e != nil {
		t.Fatal(e)
	}
	m := items[0]
	rule := store.Alert{AlertID: "network-rule", UserID: "owner", Nickname: "Loss", Enabled: true, MonitorIDs: []string{m.ID, second[0].ID}, Trigger: a.Trigger{Type: a.TriggerTypeNetworkLoss, Operator: a.OpGreaterThan, Threshold: 5, Duration: "1h"}, Action: a.AlertAction{Type: a.DestWebhook, Destination: "https://example.com/notify"}}
	if e = s.AlertCreate(ctx, rule); e != nil {
		t.Fatal(e)
	}
	saveNetwork(t, s, m, "epoch", 10, 2)
	events, e := s.NetworkEvaluate(ctx, "node")
	if e != nil || len(events) != 0 {
		t.Fatal(events, e)
	}
	saveNetwork(t, s, m, "epoch", 10, 3)
	saveNetwork(t, s, second[0], "epoch", 20, 3)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, e := s.NetworkEvaluate(ctx, "node"); e != nil {
				t.Error(e)
			}
		}()
	}
	wg.Wait()
	history, total, e := s.AlertHistoryListFiltered(ctx, "owner", 1, 25, "", "firing", "")
	if e != nil || total != 2 {
		t.Fatal(total, e)
	}
	h := history[0]
	if h.MonitorID == "" || h.SubjectKind != "network_monitor" || h.Monitor.Target == "" {
		t.Fatal(h)
	}
	eventList, _, e := s.AlertHistoryEvents(ctx, "owner", h.HistoryID, 1, 100)
	if e != nil || len(eventList) != 2 {
		t.Fatal(eventList, e)
	}
	var notification a.HistoryEvent
	for _, event := range eventList {
		if event.Kind == "notification" {
			notification = event
		}
	}
	finishAttempt(t, s, notification, "failed")
	retry, e := s.AlertAttemptQueue(ctx, "owner", h.HistoryID, "firing", notification.EventID)
	if e != nil {
		t.Fatal(e)
	}
	var recovering nm.Monitor
	if h.MonitorID == m.ID {
		recovering = m
	} else {
		recovering = second[0]
	}
	saveNetwork(t, s, recovering, "epoch", 5, 4)
	events, e = s.NetworkEvaluate(ctx, "node")
	if e != nil || len(events) != 1 {
		t.Fatal(events, e)
	}
	if _, _, e = s.AlertAttemptStart(ctx, retry.EventID); !errors.Is(e, ErrAttemptUnavailable) {
		t.Fatal("obsolete firing retry", e)
	}
	finishAttempt(t, s, a.HistoryEvent{EventID: events[0]}, "failed")
	recoveryRetry, e := s.AlertAttemptQueue(ctx, "owner", h.HistoryID, "recovery", events[0])
	if e != nil {
		t.Fatal(e)
	}
	finishAttempt(t, s, recoveryRetry, "success")
	saved, e := s.AlertHistoryGetByID(ctx, h.HistoryID, "owner")
	if e != nil || saved.ResolvedAt == nil || saved.RecoveryDelivery != "success" {
		t.Fatal(saved, e)
	}
	if e = s.NetworkArchive(ctx, "owner", m.ID); e != nil {
		t.Fatal(e)
	}
	if e = s.NetworkArchive(ctx, "owner", second[0].ID); e != nil {
		t.Fatal(e)
	}
	_, count, e := s.AlertHistoryListFiltered(ctx, "owner", 1, 25, "", "firing", "")
	if e != nil || count != 0 {
		t.Fatal(count, e)
	}
	var state string
	for _, id := range rule.MonitorIDs {
		if e = s.db.QueryRow(`SELECT status FROM alert_monitors WHERE alert_id=? AND monitor_id=?`, rule.AlertID, id).Scan(&state); e != nil || state != "ok" {
			t.Fatal(state, e)
		}
	}
	raw, _ := json.Marshal(saved)
	if string(raw) == "" {
		t.Fatal("serialization")
	}
}

func TestNetworkCapabilityDowngradeSuspendsEvaluationAndRetry(t *testing.T) {
	s, items := networkFixture(t)
	ctx := context.Background()
	m := items[0]
	rule := store.Alert{AlertID: "downgrade-rule", UserID: "owner", Enabled: true, MonitorIDs: []string{m.ID}, Trigger: a.Trigger{Type: a.TriggerTypeNetworkLoss, Operator: a.OpGreaterThan, Threshold: 5, Duration: "1h"}, Action: a.AlertAction{Type: a.DestWebhook, Destination: "https://example.com/notify"}}
	if e := s.AlertCreate(ctx, rule); e != nil {
		t.Fatal(e)
	}
	saveNetwork(t, s, m, "epoch", 10, 3)
	events, e := s.NetworkEvaluate(ctx, "node")
	if e != nil || len(events) != 1 {
		t.Fatal(events, e)
	}
	finishAttempt(t, s, a.HistoryEvent{EventID: events[0]}, "failed")
	hs, _, e := s.AlertHistoryListFiltered(ctx, "owner", 1, 25, "", "firing", "")
	if e != nil || len(hs) != 1 {
		t.Fatal(hs, e)
	}
	if e = s.AgentUpdateRuntime(ctx, "node", "owner", agentmeta.Runtime{AgentVersion: agentmeta.String("0.19.0")}); e != nil {
		t.Fatal(e)
	}
	saveNetwork(t, s, m, "epoch", 0, 4)
	events, e = s.NetworkEvaluate(ctx, "node")
	if e != nil || len(events) != 0 {
		t.Fatal("downgrade recovered incident", events, e)
	}
	hs, _, e = s.AlertHistoryListFiltered(ctx, "owner", 1, 25, "", "firing", "")
	if e != nil || len(hs) != 1 || hs[0].RetryAvailable || hs[0].MonitoringAvailable {
		t.Fatal(hs, e)
	}
	if _, e = s.AlertAttemptQueue(ctx, "owner", hs[0].HistoryID, "firing", ""); !errors.Is(e, ErrAttemptUnavailable) {
		t.Fatal("retry allowed after downgrade", e)
	}
	if e = s.AlertCreate(ctx, store.Alert{AlertID: "unsupported-rule", UserID: "owner", Enabled: true, MonitorIDs: rule.MonitorIDs, Trigger: rule.Trigger, Action: rule.Action}); e == nil {
		t.Fatal("unsupported monitor rule created")
	}
	if e = s.AgentUpdateRuntime(ctx, "node", "owner", agentmeta.Runtime{AgentVersion: agentmeta.String("0.21.0")}); e != nil {
		t.Fatal(e)
	}
	events, e = s.NetworkEvaluate(ctx, "node")
	if e != nil || len(events) != 1 {
		t.Fatal("upgrade did not resume evaluation", events, e)
	}
}

func TestNetworkLiveCompleteOwnedCommitted(t *testing.T) {
	s, initial := networkFixture(t)
	ctx := context.Background()
	if err := s.AgentProvision(ctx, "second", "owner", "token-second", "Second", "beszel"); err != nil {
		t.Fatal(err)
	}
	if err := s.AgentUpdateRuntime(ctx, "second", "owner", agentmeta.Runtime{AgentVersion: agentmeta.String("0.21.0"), VersionSource: "beszel_info"}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 100; i++ {
		if _, err := s.NetworkCreate(ctx, "owner", []string{"second"}, nm.Config{Target: fmt.Sprintf("host-%d.example.com", i), Protocol: "icmp"}, true); err != nil {
			t.Fatal(err)
		}
	}
	m := initial[0]
	pending, err := s.NetworkBegin(ctx, nm.Batch{ID: nm.ID(), UserID: "owner", AgentID: m.AgentID, Epoch: "epoch", Results: map[string]nm.Result{m.ID: {LastProbeAt: time.Now().UnixMilli(), Total: 1, Success: 1}}})
	if err != nil {
		t.Fatal(err)
	}
	agents, err := s.AgentList(ctx, "owner")
	if err != nil {
		t.Fatal(err)
	}
	live, err := s.NetworkLive(ctx, "owner", agents)
	if err != nil || len(live) != 101 {
		t.Fatalf("complete live set: %d %v", len(live), err)
	}
	for _, v := range live {
		if v.ID == m.ID && v.Latest != nil {
			t.Fatal("uncommitted reading visible")
		}
	}
	if err := s.NetworkComplete(ctx, *pending); err != nil {
		t.Fatal(err)
	}
	live, err = s.NetworkLive(ctx, "owner", agents)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, v := range live {
		if v.ID == m.ID {
			found = v.Latest != nil
		}
	}
	if !found {
		t.Fatal("committed reading missing")
	}
	foreign, err := s.NetworkLive(ctx, "foreign", nil)
	if err != nil || len(foreign) != 0 {
		t.Fatal("owner isolation", err)
	}
	if err := s.NetworkArchive(ctx, "owner", m.ID); err != nil {
		t.Fatal(err)
	}
	live, err = s.NetworkLive(ctx, "owner", agents)
	if err != nil || len(live) != 100 {
		t.Fatal("archived monitor in live set", err)
	}
	archived, err := s.NetworkGet(ctx, "owner", m.ID)
	if err != nil || archived.Latest == nil {
		t.Fatal("archive no longer queryable", err)
	}
}
