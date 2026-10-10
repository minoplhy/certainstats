package sqlite

import (
	"certainstats/internal/agentmeta"
	a "certainstats/internal/base/alert"
	nm "certainstats/internal/networkmonitor"
	"certainstats/internal/store"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"testing"
)

func groupAgent(t *testing.T, s *Store, id string, supported bool) {
	t.Helper()
	ctx := context.Background()
	if err := s.AgentProvision(ctx, id, "owner", "token-"+id, id, "beszel"); err != nil {
		t.Fatal(err)
	}
	version := "0.21.0"
	if !supported {
		version = "0.19.0"
	}
	if err := s.AgentUpdateRuntime(ctx, id, "owner", agentmeta.Runtime{AgentVersion: agentmeta.String(version), VersionSource: "test"}); err != nil {
		t.Fatal(err)
	}
}

func groupEdit(t *testing.T, s *Store, target string, ids ...string) store.NetworkGroupEdit {
	t.Helper()
	g, err := s.NetworkGroup(context.Background(), "owner", target)
	if err != nil {
		t.Fatal(err)
	}
	return store.NetworkGroupEdit{Target: target, Revision: g.Revision, Config: g.Items[0].Config, AgentIDs: ids}
}

func TestNetworkGroupMembershipAndHistory(t *testing.T) {
	s, initial := networkFixture(t)
	ctx := context.Background()
	groupAgent(t, s, "second", true)
	groupAgent(t, s, "third", true)
	second, err := s.NetworkCreate(ctx, "owner", []string{"second"}, initial[0].Config, false)
	if err != nil {
		t.Fatal(err)
	}
	saveNetwork(t, s, initial[0], "epoch", 10, 3)
	rule := store.Alert{AlertID: "group-rule", UserID: "owner", Nickname: "Loss", Enabled: true, MonitorIDs: []string{initial[0].ID, second[0].ID}, Trigger: a.Trigger{Type: a.TriggerTypeNetworkLoss, Operator: a.OpGreaterThan, Threshold: 5, Duration: "1h"}, Action: a.AlertAction{Type: a.DestWebhook, Destination: "https://example.com/notify"}}
	if err := s.AlertCreate(ctx, rule); err != nil {
		t.Fatal(err)
	}
	if _, err := s.NetworkEvaluate(ctx, "node"); err != nil {
		t.Fatal(err)
	}
	edit := groupEdit(t, s, "example.com", "third", "second", "node") // new agent intentionally first
	edit.Config.Interval = 300
	change, err := s.NetworkEditGroup(ctx, "owner", edit)
	if err != nil || len(change.Items) != 3 {
		t.Fatalf("change=%+v err=%v", change, err)
	}
	for _, m := range change.Items {
		if m.Interval != 300 || (m.AgentID == "second") == m.Enabled {
			t.Fatalf("state/settings %+v", m)
		}
		if m.AgentID == "node" && (m.ID != initial[0].ID || m.Latest == nil) {
			t.Fatal("schedule lost history")
		}
	}
	if _, err := s.NetworkEditGroup(ctx, "owner", edit); !errors.Is(err, nm.ErrConflict) {
		t.Fatal("stale revision", err)
	}
	edit = groupEdit(t, s, "example.com", "second", "third")
	edit.Config.Target = "renamed.example.com"
	change, err = s.NetworkEditGroup(ctx, "owner", edit)
	if err != nil {
		t.Fatal(err)
	}
	old, err := s.NetworkGet(ctx, "owner", initial[0].ID)
	if err != nil || old.ArchivedAt == nil || old.Latest == nil {
		t.Fatal("removed history", old, err)
	}
	oldSecond, err := s.NetworkGet(ctx, "owner", second[0].ID)
	if err != nil || oldSecond.ArchivedAt == nil || oldSecond.ReplacementID == "" {
		t.Fatal("replacement", oldSecond, err)
	}
	var membership string
	if err := s.db.QueryRow(`SELECT monitor_id FROM alert_monitors WHERE alert_id = ? AND monitor_id = ?`, "group-rule", oldSecond.ReplacementID).Scan(&membership); err != nil {
		t.Fatal("alert transfer", err)
	}
	var open int
	if err := s.db.QueryRow(`SELECT count(*) FROM alert_history WHERE monitor_id = ? AND closed_at IS NULL AND resolved_at IS NULL`, initial[0].ID).Scan(&open); err != nil || open != 0 {
		t.Fatal("removed incident open", open, err)
	}
	for _, m := range change.Items {
		if m.Target != edit.Config.Target || m.Latest != nil {
			t.Fatal(m)
		}
	}
	if _, err := s.NetworkGroup(ctx, "owner", "example.com"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal(err)
	}
	edit = groupEdit(t, s, "renamed.example.com", "second", "third")
	enabled := true
	edit.Enabled = &enabled
	change, err = s.NetworkEditGroup(ctx, "owner", edit)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range change.Items {
		if !m.Enabled {
			t.Fatal("not resumed", m)
		}
		_, sync, err := s.NetworkConfigs(ctx, "owner", m.AgentID)
		if err != nil || sync.Desired == 0 {
			t.Fatal("sync", sync, err)
		}
	}
	enabled = false
	edit = groupEdit(t, s, "renamed.example.com", "second", "third")
	edit.Enabled = &enabled
	change, err = s.NetworkEditGroup(ctx, "owner", edit)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range change.Items {
		if m.Enabled {
			t.Fatal("not paused")
		}
	}
}

func TestNetworkGroupPolicyAndRollback(t *testing.T) {
	s, initial := networkFixture(t)
	ctx := context.Background()
	groupAgent(t, s, "second", true)
	groupAgent(t, s, "unsupported", false)
	groupAgent(t, s, "full", true)
	for i := range nm.MaxMonitorsPerAgent {
		if _, err := s.NetworkCreate(ctx, "owner", []string{"full"}, nm.Config{Target: fmt.Sprintf("limit-%d.example", i), Protocol: "icmp"}, true); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.NetworkCreate(ctx, "owner", []string{"second"}, nm.Config{Target: "occupied.example", Protocol: "icmp"}, true); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		mutate func(*store.NetworkGroupEdit)
	}{
		{"unsupported", func(e *store.NetworkGroupEdit) { e.AgentIDs = []string{"node", "unsupported"} }},
		{"foreign agent", func(e *store.NetworkGroupEdit) { e.AgentIDs = []string{"second", "foreign"} }},
		{"agent limit", func(e *store.NetworkGroupEdit) { e.AgentIDs = []string{"node", "full"} }},
		{"duplicate", func(e *store.NetworkGroupEdit) { e.AgentIDs = []string{"node", "node"} }},
		{"empty", func(e *store.NetworkGroupEdit) { e.AgentIDs = nil }},
		{"invalid", func(e *store.NetworkGroupEdit) { e.Config.Interval = 5 }},
		{"destination", func(e *store.NetworkGroupEdit) { e.Config.Target = "occupied.example" }},
		{"stale", func(e *store.NetworkGroupEdit) { e.Revision = "old" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before, _ := s.NetworkGroup(ctx, "owner", "example.com")
			_, beforeSync, _ := s.NetworkConfigs(ctx, "owner", "node")
			edit := groupEdit(t, s, "example.com", "node", "second")
			edit.Config.Interval = 300
			tc.mutate(&edit)
			if _, err := s.NetworkEditGroup(ctx, "owner", edit); err == nil {
				t.Fatal("accepted invalid edit")
			}
			after, err := s.NetworkGroup(ctx, "owner", "example.com")
			_, afterSync, _ := s.NetworkConfigs(ctx, "owner", "node")
			if err != nil || after.Revision != before.Revision || afterSync.Desired != beforeSync.Desired {
				t.Fatal("partial commit", after, err)
			}
		})
	}
	if _, err := s.NetworkGroup(ctx, "foreign", "example.com"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("foreign read", err)
	}
	edit := groupEdit(t, s, "example.com", "node")
	if _, err := s.NetworkEditGroup(ctx, "foreign", edit); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("foreign edit", err)
	}
	c := initial[0].Config
	c.Interval = 300
	if _, err := s.NetworkCreate(ctx, "owner", []string{"second"}, c, true); !errors.Is(err, nm.ErrConflict) {
		t.Fatal("mixed create", err)
	}
	c = initial[0].Config
	if _, err := s.NetworkCreate(ctx, "owner", []string{"node"}, c, true); !errors.Is(err, nm.ErrConflict) {
		t.Fatal("duplicate", err)
	}
	second, err := s.NetworkCreate(ctx, "owner", []string{"second"}, c, true)
	if err != nil {
		t.Fatal(err)
	}
	c.Interval = 300
	if _, err := s.NetworkUpdate(ctx, "owner", initial[0].ID, c, true); !errors.Is(err, nm.ErrConflict) {
		t.Fatal("mixed update", err)
	}
	c.Interval = 60
	if _, err := s.NetworkUpdate(ctx, "owner", second[0].ID, c, false); err != nil {
		t.Fatal("independent pause", err)
	}
	// Lost capabilities must not prevent removing or pausing an existing prober.
	if err := s.AgentUpdateRuntime(ctx, "node", "owner", agentmeta.Runtime{AgentVersion: agentmeta.String("0.19.0"), VersionSource: "test"}); err != nil {
		t.Fatal(err)
	}
	edit = groupEdit(t, s, "example.com", "node", "second")
	enabled := false
	edit.Enabled = &enabled
	if _, err := s.NetworkEditGroup(ctx, "owner", edit); err != nil {
		t.Fatal("unsupported pause", err)
	}
	edit = groupEdit(t, s, "example.com", "second")
	if _, err := s.NetworkEditGroup(ctx, "owner", edit); err != nil {
		t.Fatal("unsupported removal", err)
	}
}

func TestNetworkGroupAcrossPagesAndInconsistentData(t *testing.T) {
	s, initial := networkFixture(t)
	ctx := context.Background()
	ids := []string{"node"}
	for i := range 26 {
		id := fmt.Sprintf("node-%02d", i)
		groupAgent(t, s, id, true)
		ids = append(ids, id)
	}
	if _, err := s.NetworkCreate(ctx, "owner", ids[1:], initial[0].Config, true); err != nil {
		t.Fatal(err)
	}
	group, err := s.NetworkGroup(ctx, "owner", "example.com")
	if err != nil || len(group.Items) != 27 {
		t.Fatal("paginated group", group, err)
	}
	if _, err := s.db.Exec(`UPDATE network_monitors SET interval_seconds = 300 WHERE monitor_id = ?`, initial[0].ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.NetworkGroup(ctx, "owner", "example.com"); !errors.Is(err, nm.ErrConflict) {
		t.Fatal("mixed group", err)
	}
}
