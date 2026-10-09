package sqlite

import (
	a "certainstats/internal/base/alert"
	"certainstats/internal/store"
	"context"
	"database/sql"
	"fmt"
	"sync"
	"testing"
)

func incidentFixture(t *testing.T) (*Store, store.Alert) {
	t.Helper()
	s := newTestStore(t)
	ctx := context.Background()
	if err := s.CreateUser(ctx, "owner", "owner", "hash", false); err != nil {
		t.Fatal(err)
	}
	if err := s.AgentProvision(ctx, "node", "owner", "token", "Original node", "ltstats"); err != nil {
		t.Fatal(err)
	}
	rule := store.Alert{AlertID: "rule", UserID: "owner", Nickname: "Original rule", Enabled: true, Trigger: a.Trigger{Type: a.TriggerTypeCPU, Operator: a.OpGreaterThan, Threshold: 90, Duration: "1m"}, Action: a.AlertAction{Type: a.DestWebhook, Destination: "https://example.com/notify"}, Agents: []a.AgentState{{AgentID: "node"}}}
	if err := s.AlertCreate(ctx, rule); err != nil {
		t.Fatal(err)
	}
	return s, rule
}
func openIncident(t *testing.T, s *Store, rule store.Alert, id string) {
	t.Helper()
	if err := s.AlertTrigger(context.Background(), rule, "node", "Original node", id, 95, "pending", "", "", ""); err != nil {
		t.Fatal(err)
	}
}
func finishAttempt(t *testing.T, s *Store, event a.HistoryEvent, status string) {
	t.Helper()
	if _, _, err := s.AlertAttemptStart(context.Background(), event.EventID); err != nil {
		t.Fatal(err)
	}
	if err := s.AlertAttemptComplete(context.Background(), event.EventID, status, "test result"); err != nil {
		t.Fatal(err)
	}
}

func TestIncidentSnapshotAndRemoval(t *testing.T) {
	for _, remove := range []string{"rule", "node"} {
		t.Run(remove, func(t *testing.T) {
			s, rule := incidentFixture(t)
			ctx := context.Background()
			openIncident(t, s, rule, "incident")
			event, err := s.AlertAttemptQueue(ctx, "owner", "incident", "firing", "")
			if err != nil {
				t.Fatal(err)
			}
			rule.Nickname = "Edited rule"
			rule.Trigger.Threshold = 50
			if err = s.AlertUpdate(ctx, rule, []string{"node"}); err != nil {
				t.Fatal(err)
			}
			h, err := s.AlertHistoryGetByID(ctx, "incident", "owner")
			if err != nil || h.Trigger.Threshold != 90 || h.AlertNickname != "Original rule" {
				t.Fatalf("snapshot: %+v %v", h, err)
			}
			if remove == "rule" {
				err = s.AlertDelete(ctx, "rule", "owner")
			} else {
				err = s.AgentDelete(ctx, "node", "owner")
			}
			if err != nil {
				t.Fatal(err)
			}
			h, err = s.AlertHistoryGetByID(ctx, "incident", "owner")
			if err != nil || h.ClosedAt == nil || h.ResolvedAt != nil || h.CloseReason != "monitoring_removed" || h.RetryAvailable {
				t.Fatalf("archive: %+v %v", h, err)
			}
			events, total, err := s.AlertHistoryEvents(ctx, "owner", "incident", 1, 100)
			if err != nil || total != 3 {
				t.Fatalf("events: %d %v", total, err)
			}
			skipped := false
			closed := false
			for _, e := range events {
				if e.EventID == event.EventID {
					skipped = e.Status == "skipped"
				}
				closed = closed || e.Kind == "monitoring_removed"
			}
			if !skipped || !closed {
				t.Fatalf("events: %+v", events)
			}
			count, err := s.AlertHistorySummary(ctx, "owner")
			if err != nil || count != 0 {
				t.Fatalf("count: %d %v", count, err)
			}
			nodes, err := s.AlertHistoryNodes(ctx, "owner")
			if err != nil || len(nodes) != 1 || nodes[0].Nickname != "Original node" {
				t.Fatalf("nodes: %+v %v", nodes, err)
			}
		})
	}
}
func TestAttemptClaimsRecoveryAndImmutability(t *testing.T) {
	s, rule := incidentFixture(t)
	ctx := context.Background()
	openIncident(t, s, rule, "incident")
	first, err := s.AlertAttemptQueue(ctx, "owner", "incident", "firing", "")
	if err != nil {
		t.Fatal(err)
	}
	finishAttempt(t, s, first, "failed")
	var wg sync.WaitGroup
	var mu sync.Mutex
	var claimed []a.HistoryEvent
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			e, err := s.AlertAttemptQueue(ctx, "owner", "incident", "firing", first.EventID)
			if err == nil {
				mu.Lock()
				claimed = append(claimed, e)
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if len(claimed) != 1 {
		t.Fatalf("claims: %d", len(claimed))
	}
	retry := claimed[0]
	if _, _, err = s.AlertAttemptStart(ctx, retry.EventID); err != nil {
		t.Fatal(err)
	}
	if _, _, err = s.AlertAttemptStart(ctx, retry.EventID); err == nil {
		t.Fatal("duplicate dispatch claim accepted")
	}
	if err = s.AlertResolve(ctx, rule, "node"); err != nil {
		t.Fatal(err)
	}
	if err = s.AlertAttemptComplete(ctx, retry.EventID, "success", ""); err != nil {
		t.Fatal(err)
	}
	if err = s.AlertAttemptComplete(ctx, first.EventID, "success", ""); err == nil {
		t.Fatal("completed failure mutated")
	}
	if _, err = s.AlertAttemptQueue(ctx, "owner", "incident", "firing", first.EventID); err == nil {
		t.Fatal("stale firing retry accepted")
	}
	recovery, err := s.AlertAttemptQueue(ctx, "owner", "incident", "recovery", "")
	if err != nil {
		t.Fatal(err)
	}
	finishAttempt(t, s, recovery, "failed")
	queued, err := s.AlertAttemptQueue(ctx, "owner", "incident", "recovery", recovery.EventID)
	if err != nil {
		t.Fatal(err)
	}
	finishAttempt(t, s, queued, "success")
	rules, err := s.AlertList(ctx, "owner")
	if err != nil || rules[0].Agents[0].Status != "ok" {
		t.Fatalf("late completion refired node: %+v %v", rules, err)
	}
	events, total, err := s.AlertHistoryEvents(ctx, "owner", "incident", 1, 100)
	if err != nil || total != 6 {
		t.Fatalf("events: %+v %d %v", events, total, err)
	}
	if events[1].Status != "failed" {
		t.Fatalf("failure not retained: %+v", events)
	}
}
func TestHistoryFilteringAndPagination(t *testing.T) {
	s, rule := incidentFixture(t)
	ctx := context.Background()
	for i := 0; i < 32; i++ {
		openIncident(t, s, rule, fmt.Sprintf("incident-%03d", i))
		if err := s.AlertResolve(ctx, rule, "node"); err != nil {
			t.Fatal(err)
		}
	}
	rows, total, err := s.AlertHistoryListFiltered(ctx, "owner", 2, 25, "Original", "resolved", "node")
	if err != nil || total != 32 || len(rows) != 7 {
		t.Fatalf("page: %d %d %v", len(rows), total, err)
	}
	if _, total, err = s.AlertHistoryListFiltered(ctx, "other", 1, 25, "", "all", "node"); err != nil || total != 0 {
		t.Fatal("history escaped ownership")
	}
	if _, _, err = s.AlertHistoryEvents(ctx, "other", rows[0].HistoryID, 1, 50); err != sql.ErrNoRows {
		t.Fatalf("events escaped ownership: %v", err)
	}
	if _, total, err = s.AlertHistoryListFiltered(ctx, "owner", 1, 25, "%", "all", ""); err != nil || total != 0 {
		t.Fatalf("search wildcard not literal: %d %v", total, err)
	}
	// Equal timestamps still have a deterministic secondary order.
	if _, err = s.db.Exec(`UPDATE alert_history SET triggered_at='2026-10-01 00:00:00'`); err != nil {
		t.Fatal(err)
	}
	rows, _, err = s.AlertHistoryListFiltered(ctx, "owner", 1, 25, "", "all", "")
	if err != nil || rows[0].HistoryID != "incident-031" {
		t.Fatalf("unstable order: %+v %v", rows, err)
	}
}
func TestIncidentMigrationAndInterruptedAttempts(t *testing.T) {
	s, rule := incidentFixture(t)
	ctx := context.Background()
	openIncident(t, s, rule, "legacy")
	recreateLegacyHistory(t, s)
	if err := s.migrate(); err != nil {
		t.Fatal(err)
	}
	h, err := s.AlertHistoryGetByID(ctx, "legacy", "owner")
	if err != nil || !h.Legacy || h.Trigger.Threshold != 90 || h.NotifiedStatus != "unknown" {
		t.Fatalf("legacy: %+v %v", h, err)
	}
	events, total, err := s.AlertHistoryEvents(ctx, "owner", "legacy", 1, 50)
	if err != nil || total != 1 || events[0].Kind != "firing" {
		t.Fatalf("fabricated attempts: %+v %v", events, err)
	}
	event, err := s.AlertAttemptQueue(ctx, "owner", "legacy", "firing", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = s.AlertAttemptStart(ctx, event.EventID); err != nil {
		t.Fatal(err)
	}
	if err = s.migrate(); err != nil {
		t.Fatal(err)
	}
	events, total, err = s.AlertHistoryEvents(ctx, "owner", "legacy", 1, 50)
	if err != nil || total != 2 || events[1].Status != "unknown" || !events[1].RetryAvailable {
		t.Fatalf("interrupted: %+v %v", events, err)
	}
	failed, err := s.AlertAttemptsFailed(ctx)
	if err != nil || len(failed) != 0 {
		t.Fatalf("unknown auto-retried: %+v %v", failed, err)
	}
	if err = s.migrate(); err != nil {
		t.Fatal(err)
	}
	if err = s.AlertDelete(ctx, "rule", "owner"); err != nil {
		t.Fatal(err)
	}
	h, err = s.AlertHistoryGetByID(ctx, "legacy", "owner")
	if err != nil || h.ClosedAt == nil {
		t.Fatalf("migration failed to preserve history: %+v %v", h, err)
	}
}

func recreateLegacyHistory(t *testing.T, s *Store) {
	t.Helper()
	// Recreate the previous table shape with an existing record and cascades.
	statements := []string{
		`DROP TRIGGER incident_remove_alerts`, `DROP TRIGGER incident_remove_agents`, `DROP TABLE alert_history_events`,
		`CREATE TABLE old_history AS SELECT history_id,alert_id,agent_id,user_id,triggered_at,resolved_at,trigger_value,notified_status,target_id,target_name,agent_nickname,alert_nickname,error_message FROM alert_history`,
		`DROP TABLE alert_history`,
		`CREATE TABLE alert_history(history_id TEXT PRIMARY KEY,alert_id TEXT NOT NULL REFERENCES alerts(alert_id) ON DELETE CASCADE,agent_id TEXT NOT NULL REFERENCES agents(agent_id) ON DELETE CASCADE,user_id TEXT DEFAULT '',triggered_at DATETIME NOT NULL,resolved_at DATETIME,trigger_value REAL NOT NULL,notified_status TEXT NOT NULL,target_id TEXT DEFAULT '',target_name TEXT DEFAULT '',agent_nickname TEXT DEFAULT '',alert_nickname TEXT DEFAULT '',error_message TEXT DEFAULT '')`,
		`INSERT INTO alert_history SELECT * FROM old_history`, `DROP TABLE old_history`,
	}
	for _, q := range statements {
		if _, err := s.db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
}

func TestIncidentMigrationFailureRollsBack(t *testing.T) {
	s, rule := incidentFixture(t)
	openIncident(t, s, rule, "legacy")
	recreateLegacyHistory(t, s)
	if _, err := s.db.Exec(`UPDATE alert_history SET user_id='missing-owner'`); err != nil {
		t.Fatal(err)
	}
	if err := s.migrateIncidentHistory(); err == nil {
		t.Fatal("invalid ownership unexpectedly migrated")
	}
	var migrated, count, cascade int
	if err := s.db.QueryRow(`SELECT count(*) FROM pragma_table_info('alert_history') WHERE name='trigger_snapshot'`).Scan(&migrated); err != nil {
		t.Fatal(err)
	}
	if err := s.db.QueryRow(`SELECT count(*) FROM alert_history`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if err := s.db.QueryRow(`SELECT count(*) FROM pragma_foreign_key_list('alert_history') WHERE on_delete='CASCADE'`).Scan(&cascade); err != nil {
		t.Fatal(err)
	}
	if migrated != 0 || count != 1 || cascade != 2 {
		t.Fatalf("original schema or records changed: migrated=%d count=%d cascades=%d", migrated, count, cascade)
	}
	if _, err := s.db.Exec(`UPDATE alert_history SET user_id='owner'`); err != nil {
		t.Fatal(err)
	}
	if err := s.migrateIncidentHistory(); err != nil {
		t.Fatal(err)
	}
}
