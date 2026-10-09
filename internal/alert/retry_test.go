package alert

import (
	basealert "certainstats/internal/base/alert"
	CSContext "certainstats/internal/context"
	"certainstats/internal/store"
	"certainstats/internal/store/sqlite"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
)

type mockAlertsStore struct {
	store.AlertsStore

	GetHistoryByIDFunc func(ctx context.Context, historyID string, userID string) (*basealert.AlertHistory, error)
	GetAlertInfoFunc   func(ctx context.Context, alertID string, userID string) (store.Alert, error)
	GetTargetByIDFunc  func(ctx context.Context, targetID string, userID string) (basealert.AlertTarget, error)
	UpdateHistoryFunc  func(ctx context.Context, historyID string, status string, errMsg string) error
	UpdateAgentFunc    func(ctx context.Context, alertID string, agentID string, status string, errMsg string) error
	GetFailedFunc      func(ctx context.Context) ([]*basealert.AlertHistory, error)
}

func (m *mockAlertsStore) AlertHistoryGetByID(ctx context.Context, historyID string, userID string) (*basealert.AlertHistory, error) {
	if m.GetHistoryByIDFunc != nil {
		return m.GetHistoryByIDFunc(ctx, historyID, userID)
	}
	return nil, errors.New("GetHistoryByIDFunc not implemented")
}

func (m *mockAlertsStore) AlertGetInfo(ctx context.Context, alertID string, userID string) (store.Alert, error) {
	if m.GetAlertInfoFunc != nil {
		return m.GetAlertInfoFunc(ctx, alertID, userID)
	}
	return store.Alert{}, errors.New("GetAlertInfoFunc not implemented")
}

func (m *mockAlertsStore) TargetGetByID(ctx context.Context, targetID string, userID string) (basealert.AlertTarget, error) {
	if m.GetTargetByIDFunc != nil {
		return m.GetTargetByIDFunc(ctx, targetID, userID)
	}
	return basealert.AlertTarget{}, errors.New("GetTargetByIDFunc not implemented")
}

func (m *mockAlertsStore) AlertHistoryUpdateStatus(ctx context.Context, historyID string, status string, errMsg string) error {
	if m.UpdateHistoryFunc != nil {
		return m.UpdateHistoryFunc(ctx, historyID, status, errMsg)
	}
	return errors.New("UpdateHistoryFunc not implemented")
}

func (m *mockAlertsStore) AlertAgentUpdateStatus(ctx context.Context, alertID string, agentID string, status string, errMsg string) error {
	if m.UpdateAgentFunc != nil {
		return m.UpdateAgentFunc(ctx, alertID, agentID, status, errMsg)
	}
	return errors.New("UpdateAgentFunc not implemented")
}

func (m *mockAlertsStore) AlertHistoryGetFailed(ctx context.Context) ([]*basealert.AlertHistory, error) {
	if m.GetFailedFunc != nil {
		return m.GetFailedFunc(ctx)
	}
	return nil, errors.New("GetFailedFunc not implemented")
}

func retryFixture(t *testing.T, destination string) (*sqlite.Store, store.Alert, string) {
	t.Helper()
	ctx := context.Background()
	db, err := sqlite.New(t.TempDir() + "/history.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err = db.CreateUser(ctx, "owner", "owner", "hash", false); err != nil {
		t.Fatal(err)
	}
	if err = db.AgentProvision(ctx, "node", "owner", "token", "Node", "ltstats"); err != nil {
		t.Fatal(err)
	}
	rule := store.Alert{AlertID: "rule", UserID: "owner", Nickname: "CPU", Enabled: true, Trigger: basealert.Trigger{Type: basealert.TriggerTypeCPU, Operator: basealert.OpGreaterThan, Threshold: 90}, Action: basealert.AlertAction{Type: basealert.DestWebhook, Destination: destination}, Agents: []basealert.AgentState{{AgentID: "node"}}}
	if err = db.AlertCreate(ctx, rule); err != nil {
		t.Fatal(err)
	}
	if err = db.AlertTrigger(ctx, rule, "node", "Node", "incident", 95, "pending", "", "", ""); err != nil {
		t.Fatal(err)
	}
	e, err := db.AlertAttemptQueue(ctx, "owner", "incident", "firing", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = db.AlertAttemptStart(ctx, e.EventID); err != nil {
		t.Fatal(err)
	}
	if err = db.AlertAttemptComplete(ctx, e.EventID, "failed", "first attempt failed"); err != nil {
		t.Fatal(err)
	}
	return db, rule, e.EventID
}
func retryRequest(handler http.HandlerFunc, id, user string) *httptest.ResponseRecorder {
	req := httptest.NewRequest("POST", "/retry/"+id, nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", id)
	req = req.WithContext(context.WithValue(context.WithValue(req.Context(), chi.RouteCtxKey, rctx), CSContext.UserIDKey, user))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	return w
}
func awaitDelivery(t *testing.T, db *sqlite.Store, phase string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		events, _, err := db.AlertHistoryEvents(context.Background(), "owner", "incident", 1, 100)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range events {
			if e.Phase == phase && e.Status == "success" {
				return
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("retry did not complete")
}
func TestRetryAlertHandler(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) }))
	defer ts.Close()
	db, _, eventID := retryFixture(t, ts.URL)
	if w := retryRequest(RetryAlertHandler(db), "incident", "other"); w.Code != 404 {
		t.Fatalf("ownership: %d", w.Code)
	}
	if w := retryRequest(RetryEventHandler(db), eventID, "other"); w.Code != 404 {
		t.Fatalf("event ownership: %d", w.Code)
	}
	w := retryRequest(RetryAlertHandler(db), "incident", "owner")
	if w.Code != 200 {
		t.Fatalf("retry: %d %s", w.Code, w.Body)
	}
	var response map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil || response["status"] != "queued" {
		t.Fatalf("response: %v %v", response, err)
	}
	awaitDelivery(t, db, "firing")
	if w = retryRequest(RetryAlertHandler(db), "incident", "owner"); w.Code != 409 {
		t.Fatalf("successful notification retried: %d", w.Code)
	}
	events, total, err := db.AlertHistoryEvents(context.Background(), "owner", "incident", 1, 100)
	if err != nil || total != 3 || events[1].Status != "failed" || events[2].Status != "success" || events[2].RetryOf != eventID {
		t.Fatalf("attempt log: %+v %v", events, err)
	}
}
func TestRetryRecoveryDoesNotRefire(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) }))
	defer ts.Close()
	db, rule, _ := retryFixture(t, ts.URL)
	ctx := context.Background()
	if err := db.AlertResolve(ctx, rule, "node"); err != nil {
		t.Fatal(err)
	}
	e, err := db.AlertAttemptQueue(ctx, "owner", "incident", "recovery", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = db.AlertAttemptStart(ctx, e.EventID); err != nil {
		t.Fatal(err)
	}
	if err = db.AlertAttemptComplete(ctx, e.EventID, "failed", "recovery failed"); err != nil {
		t.Fatal(err)
	}
	if w := retryRequest(RetryAlertHandler(db), "incident", "owner"); w.Code != 409 {
		t.Fatalf("stale firing retry: %d", w.Code)
	}
	if w := retryRequest(RetryEventHandler(db), e.EventID, "owner"); w.Code != 200 {
		t.Fatalf("recovery retry: %d %s", w.Code, w.Body)
	}
	awaitDelivery(t, db, "recovery")
	rules, err := db.AlertList(ctx, "owner")
	if err != nil || rules[0].Agents[0].Status != "ok" {
		t.Fatalf("recovery refired: %+v %v", rules, err)
	}
}
func TestHistoryAPI(t *testing.T) {
	db, _, eventID := retryFixture(t, "https://example.com")
	handler := HistoryAlertHandler(db)
	req := httptest.NewRequest("GET", "/history?limit=999&agent_id=node", nil)
	req = req.WithContext(context.WithValue(req.Context(), CSContext.UserIDKey, "owner"))
	w := httptest.NewRecorder()
	handler(w, req)
	var data struct {
		Limit, Total, TotalPages int
		Data                     []basealert.AlertHistory
	}
	if err := json.Unmarshal(w.Body.Bytes(), &data); err != nil || w.Code != 200 || data.Limit != 100 || data.Total != 1 {
		t.Fatalf("pagination: %s %v", w.Body, err)
	}
	if w := retryRequest(RetryEventHandler(db), eventID, ""); w.Code != 401 {
		t.Fatalf("unauthenticated: %d", w.Code)
	}
	if err := db.AlertDelete(context.Background(), "rule", "owner"); err != nil {
		t.Fatal(err)
	}
	if w := retryRequest(RetryAlertHandler(db), "incident", "owner"); w.Code != 409 {
		t.Fatalf("deleted rule retry: %d", w.Code)
	}
}
