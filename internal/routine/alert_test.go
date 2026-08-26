package routine

import (
	basealert "certainstats/internal/base/alert"
	"certainstats/internal/store"
	"certainstats/internal/store/sqlite"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

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

func TestAlertLifecycle_DownAndResolve(t *testing.T) {
	ctx := context.Background()

	// 1. Initialize SQLite store
	dbFile := t.TempDir() + "/test_lifecycle.db"
	storeInst, err := sqlite.New(dbFile)
	if err != nil {
		t.Fatalf("failed to initialize test sqlite database: %v", err)
	}
	defer storeInst.Close()

	// 2. Setup User, Agent, and Preset Target
	userID := "usr-ops-99"
	agentID := "agt-srv-01"
	alertID := "alt-down-01"
	targetID := "tgt-discord-01"

	if err := storeInst.CreateUser(ctx, userID, "sysadmin", "password123", false); err != nil {
		t.Fatalf("failed to create user: %v", err)
	}

	if err := storeInst.AgentProvision(ctx, agentID, userID, "tok-srv-01", "Node-Alpha", "beszel"); err != nil {
		t.Fatalf("failed to provision agent: %v", err)
	}

	// Mock server capturing Discord / Webhook dispatches
	var lastRequestBody []byte
	var dispatchCount int
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		dispatchCount++
		body, _ := io.ReadAll(r.Body)
		lastRequestBody = body
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	// Create Preset Alert Target (Discord type pointing to mock test server)
	tgt := basealert.AlertTarget{
		TargetID:    targetID,
		UserID:      userID,
		Name:        "Ops Discord Alert Channel",
		Type:        basealert.DestDiscord,
		Destination: ts.URL,
		CreatedAt:   time.Now(),
	}
	if err := storeInst.TargetCreate(ctx, tgt); err != nil {
		t.Fatalf("failed to create target: %v", err)
	}

	// Create Alert using Preset destination
	alertObj := store.Alert{
		AlertID:  alertID,
		UserID:   userID,
		Nickname: "Node Offline Sentinel",
		Enabled:  true,
		Trigger: basealert.Trigger{
			Type:     basealert.TriggerTypeDown,
			Duration: "1s",
		},
		Action: basealert.AlertAction{
			Type:     basealert.DestPreset,
			TargetID: targetID,
		},
		Agents: []basealert.AgentState{
			{AgentID: agentID, Status: "ok"},
		},
	}
	if err := storeInst.AlertCreate(ctx, alertObj); err != nil {
		t.Fatalf("failed to create alert: %v", err)
	}

	r := &Routine{
		Store: storeInst,
	}

	// Initial State: Set agent online
	if err := storeInst.AgentUpdateHeartbeat(ctx, agentID, userID); err != nil {
		t.Fatalf("AgentUpdateHeartbeat failed: %v", err)
	}

	// Healthy check: should not fire
	r.EvaluateAll(ctx)
	if dispatchCount != 0 {
		t.Fatalf("expected 0 dispatches while agent is online, got %d", dispatchCount)
	}

	// =========================================================================
	// PHASE 1: Simulate DOWN Event (Agent goes offline)
	// =========================================================================
	t.Log("--- PHASE 1: Simulating Node Down (Offline) Event ---")
	// Mark agent offline (olderThan -1s marks agents with last_seen <= now offline)
	offlineIDs, err := storeInst.AgentMarkOffline(ctx, -1*time.Second)
	if err != nil {
		t.Fatalf("AgentMarkOffline failed: %v", err)
	}
	if len(offlineIDs) == 0 {
		t.Fatalf("expected agent %s to be marked offline", agentID)
	}

	// Reset dispatch metrics
	dispatchCount = 0
	lastRequestBody = nil

	// Run EvaluateAll
	r.EvaluateAll(ctx)

	// Assertions for DOWN event
	if dispatchCount != 1 {
		t.Fatalf("expected 1 notification dispatch on node down, got %d", dispatchCount)
	}

	downPayload := string(lastRequestBody)
	t.Logf("Down Notification Payload Received: %s", downPayload)

	if !strings.Contains(downPayload, "Alert Triggered") && !strings.Contains(downPayload, "OFFLINE") {
		t.Errorf("expected FIRING/OFFLINE alert message in payload, got: %s", downPayload)
	}
	if !strings.Contains(downPayload, "Node-Alpha") {
		t.Errorf("expected agent nickname Node-Alpha in payload, got: %s", downPayload)
	}

	// Check DB State: agent should be 'firing'
	activeAlerts, _, err := storeInst.GetActiveAlertsWithState(ctx)
	if err != nil {
		t.Fatalf("GetActiveAlertsWithState failed: %v", err)
	}
	if len(activeAlerts) != 1 || len(activeAlerts[0].Agents) != 1 {
		t.Fatalf("unexpected active alerts count: %v", activeAlerts)
	}
	if activeAlerts[0].UserID != userID {
		t.Errorf("expected active alert UserID %s, got %s", userID, activeAlerts[0].UserID)
	}
	if activeAlerts[0].Agents[0].Status != "firing" {
		t.Errorf("expected agent status 'firing', got '%s'", activeAlerts[0].Agents[0].Status)
	}

	// Check DB History: 1 history entry, notified_status = success, resolved_at = NULL
	histories, totalHist, err := storeInst.AlertHistoryListPaginated(ctx, userID, 1, 10, "", "")
	if err != nil {
		t.Fatalf("AlertHistoryListPaginated failed: %v", err)
	}
	if totalHist != 1 || len(histories) != 1 {
		t.Fatalf("expected 1 history entry, got total=%d len=%d", totalHist, len(histories))
	}
	if histories[0].NotifiedStatus != "success" {
		t.Errorf("expected history notified_status 'success', got '%s'", histories[0].NotifiedStatus)
	}
	if histories[0].ResolvedAt != nil {
		t.Errorf("expected history resolved_at to be nil while node is down, got %v", histories[0].ResolvedAt)
	}
	if histories[0].TargetID != targetID {
		t.Errorf("expected target_id %s, got %s", targetID, histories[0].TargetID)
	}

	// =========================================================================
	// PHASE 2: Simulate RESOLVE Event (Agent comes back online)
	// =========================================================================
	t.Log("--- PHASE 2: Simulating Node Resolve (Online / Recovered) Event ---")
	// Agent sends heartbeat, returning to online state
	if err := storeInst.AgentUpdateHeartbeat(ctx, agentID, userID); err != nil {
		t.Fatalf("AgentUpdateHeartbeat failed: %v", err)
	}

	// Reset dispatch metrics
	dispatchCount = 0
	lastRequestBody = nil

	// Run EvaluateAll
	r.EvaluateAll(ctx)

	// Assertions for RESOLVED event
	if dispatchCount != 1 {
		t.Fatalf("expected 1 notification dispatch on node resolve, got %d", dispatchCount)
	}

	resolvePayload := string(lastRequestBody)
	t.Logf("Resolve Notification Payload Received: %s", resolvePayload)

	if !strings.Contains(resolvePayload, "Alert Resolved") && !strings.Contains(resolvePayload, "ONLINE") && !strings.Contains(resolvePayload, "RECOVERED") {
		t.Errorf("expected RESOLVED message in payload, got: %s", resolvePayload)
	}
	if !strings.Contains(resolvePayload, "Node-Alpha") {
		t.Errorf("expected agent nickname Node-Alpha in payload, got: %s", resolvePayload)
	}

	// Check DB State: agent status should now be 'ok'
	activeAlerts, _, err = storeInst.GetActiveAlertsWithState(ctx)
	if err != nil {
		t.Fatalf("GetActiveAlertsWithState failed: %v", err)
	}
	if activeAlerts[0].Agents[0].Status != "ok" {
		t.Errorf("expected agent status 'ok' after resolve, got '%s'", activeAlerts[0].Agents[0].Status)
	}

	// Check DB History: resolved_at is now populated
	histories, _, err = storeInst.AlertHistoryListPaginated(ctx, userID, 1, 10, "", "")
	if err != nil {
		t.Fatalf("AlertHistoryListPaginated failed: %v", err)
	}
	if histories[0].ResolvedAt == nil {
		t.Error("expected history resolved_at to be populated after resolve, but was nil")
	}

	// =========================================================================
	// PHASE 3: Ensure Retry Engine does not send stale FIRING alerts
	// =========================================================================
	t.Log("--- PHASE 3: Verifying Stale Retry Protection ---")
	dispatchCount = 0
	r.RetryFailedAlerts(ctx)
	if dispatchCount != 0 {
		t.Errorf("expected 0 notifications sent during retry of resolved alert, got %d", dispatchCount)
	}
}
