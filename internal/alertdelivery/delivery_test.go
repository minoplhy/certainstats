package alertdelivery

import (
	a "certainstats/internal/base/alert"
	"certainstats/internal/store"
	"certainstats/internal/store/sqlite"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestDispatchUsesOriginalConditionAndTarget(t *testing.T) {
	ctx := context.Background()
	db, err := sqlite.New(t.TempDir() + "/delivery.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	payloads := make(chan map[string]any, 3)
	var newTargetCalls atomic.Int32
	original := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var p map[string]any
		if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
			t.Error(err)
		}
		payloads <- p
		w.WriteHeader(200)
	}))
	defer original.Close()
	changed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { newTargetCalls.Add(1); w.WriteHeader(200) }))
	defer changed.Close()
	if err = db.CreateUser(ctx, "owner", "owner", "hash", false); err != nil {
		t.Fatal(err)
	}
	if err = db.AgentProvision(ctx, "node", "owner", "token", "Original node", "ltstats"); err != nil {
		t.Fatal(err)
	}
	for _, target := range []a.AlertTarget{{TargetID: "original", UserID: "owner", Name: "Original", Type: a.DestWebhook, Destination: original.URL}, {TargetID: "changed", UserID: "owner", Name: "Changed", Type: a.DestWebhook, Destination: changed.URL}} {
		if err = db.TargetCreate(ctx, target); err != nil {
			t.Fatal(err)
		}
	}
	rule := store.Alert{AlertID: "rule", UserID: "owner", Nickname: "CPU", Enabled: true, Trigger: a.Trigger{Type: a.TriggerTypeCPU, Operator: a.OpGreaterThan, Threshold: 90}, Action: a.AlertAction{Type: a.DestPreset, TargetID: "original"}, Agents: []a.AgentState{{AgentID: "node"}}}
	if err = db.AlertCreate(ctx, rule); err != nil {
		t.Fatal(err)
	}
	if err = db.AlertTrigger(ctx, rule, "node", "Original node", "incident", 95, "pending", "original", "Original", ""); err != nil {
		t.Fatal(err)
	}
	e, err := db.AlertAttemptQueue(ctx, "owner", "incident", "firing", "")
	if err != nil {
		t.Fatal(err)
	}
	rule.Action.TargetID = "changed"
	rule.Trigger.Threshold = 50
	if err = db.AlertUpdate(ctx, rule, []string{"node"}); err != nil {
		t.Fatal(err)
	}
	if err = Dispatch(ctx, db, e.EventID); err != nil {
		t.Fatal(err)
	}
	p := <-payloads
	if p["threshold"] != float64(90) || p["nickname"] != "Original node" || p["status"] != "FIRING" || newTargetCalls.Load() != 0 {
		t.Fatalf("snapshot payload: %v new target calls %d", p, newTargetCalls.Load())
	}
	if err = db.AlertResolve(ctx, rule, "node"); err != nil {
		t.Fatal(err)
	}
	recovery, err := db.AlertAttemptQueue(ctx, "owner", "incident", "recovery", "")
	if err != nil {
		t.Fatal(err)
	}
	if err = Dispatch(ctx, db, recovery.EventID); err != nil {
		t.Fatal(err)
	}
	p = <-payloads
	if p["status"] != "RESOLVED" || p["threshold"] != float64(90) || newTargetCalls.Load() != 0 {
		t.Fatalf("recovery payload: %v", p)
	}
	if err = db.TargetDelete(ctx, "original", "owner"); err != nil {
		t.Fatal(err)
	}
	h, err := db.AlertHistoryGetByID(ctx, "incident", "owner")
	if err != nil || h.RetryAvailable {
		t.Fatalf("missing original target retryable: %+v %v", h, err)
	}
}
