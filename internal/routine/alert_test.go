package routine

import (
	basealert "certainstats/internal/base/alert"
	"certainstats/internal/store"
	"certainstats/internal/store/sqlite"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	_ "modernc.org/sqlite"
)

func TestRetryFailedAlerts(t *testing.T) {
	ctx := context.Background()

	// 1. Initialize SQLite store
	dbFile := t.TempDir() + "/test_routine.db"
	storeInst, err := sqlite.New(dbFile)
	if err != nil {
		t.Fatalf("failed to initialize test sqlite database: %v", err)
	}
	defer storeInst.Close()

	// 2. Create User, Agent, and Alert
	userID := "user-123"
	agentID := "agent-123"
	alertID := "alert-123"

	if err := storeInst.CreateUser(ctx, userID, "testuser", "hashed_password", false); err != nil {
		t.Fatalf("failed to create user: %v", err)
	}

	if err := storeInst.AgentProvision(ctx, agentID, userID, "test-token", "Node1", "ltstats"); err != nil {
		t.Fatalf("failed to provision agent: %v", err)
	}

	// Mock server for webhook action
	var webhookCalled bool
	var webhookStatus int = http.StatusOK
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		webhookCalled = true
		w.WriteHeader(webhookStatus)
	}))
	defer ts.Close()

	alertObj := store.Alert{
		AlertID:  alertID,
		UserID:   userID,
		Nickname: "High CPU Alert",
		Enabled:  true,
		Trigger: basealert.Trigger{
			Type:      basealert.TriggerTypeCPU,
			Operator:  basealert.OpGreaterThan,
			Threshold: 90.0,
			Duration:  "5m",
		},
		Action: basealert.AlertAction{
			Type:        basealert.DestWebhook,
			Destination: ts.URL,
		},
		Agents: []basealert.AgentState{
			{AgentID: agentID},
		},
	}

	if err := storeInst.AlertCreate(ctx, alertObj); err != nil {
		t.Fatalf("failed to create alert: %v", err)
	}

	// 3. Create Routine instance
	r := &Routine{
		Store: storeInst,
	}

	// 4. Trigger alert which fails (simulate a failed webhook)
	webhookStatus = http.StatusInternalServerError
	agentState := basealert.AgentState{
		AgentID: agentID,
		Status:  "ok",
	}
	agentInfo := store.AgentInfo{
		Nickname: "Node1",
		IsOnline: true,
	}

	err = r.TriggerAlert(ctx, alertObj, agentState, agentInfo, 95.5)
	if err != nil {
		t.Fatalf("TriggerAlert failed: %v", err)
	}

	// Verify that history log is created with status "failed" and agent is in "failed" state
	failedHistory, err := storeInst.AlertHistoryGetFailed(ctx)
	if err != nil {
		t.Fatalf("AlertHistoryGetFailed failed: %v", err)
	}
	if len(failedHistory) != 1 {
		t.Fatalf("expected 1 failed alert history entry, got %d", len(failedHistory))
	}
	if failedHistory[0].NotifiedStatus != "failed" {
		t.Errorf("expected history notified status 'failed', got %s", failedHistory[0].NotifiedStatus)
	}

	// Reset webhook check
	webhookCalled = false
	webhookStatus = http.StatusOK

	// 5. Run RetryFailedAlerts
	r.RetryFailedAlerts(ctx)

	// Verify that webhook was called and database status is updated to success / firing
	if !webhookCalled {
		t.Error("expected webhook to be called during retry, but it was not")
	}

	// Query alert history again: it should no longer be marked as failed
	failedHistory2, err := storeInst.AlertHistoryGetFailed(ctx)
	if err != nil {
		t.Fatalf("AlertHistoryGetFailed failed: %v", err)
	}
	if len(failedHistory2) != 0 {
		t.Errorf("expected 0 failed alert history entries, got %d", len(failedHistory2))
	}
}
