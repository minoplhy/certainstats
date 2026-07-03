package alert

import (
	basealert "certainstats/internal/base/alert"
	CSContext "certainstats/internal/context"
	"certainstats/internal/store"
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

func TestRetryAlertHandler(t *testing.T) {
	t.Run("successful retry with webhook", func(t *testing.T) {
		// Mock HTTP webhook server to return 200 OK
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
		defer ts.Close()

		var updatedHistoryID, updatedHistoryStatus, updatedHistoryErrMsg string
		var updatedAlertID, updatedAgentID, updatedAgentStatus, updatedAgentErrMsg string

		mockStore := &mockAlertsStore{
			GetHistoryByIDFunc: func(ctx context.Context, historyID string, userID string) (*basealert.AlertHistory, error) {
				return &basealert.AlertHistory{
					HistoryID:     "history-123",
					AlertID:       "alert-123",
					AgentID:       "agent-123",
					AgentNickname: "TestNode",
					TriggerValue:  95.0,
					TriggeredAt:   time.Now(),
				}, nil
			},
			GetAlertInfoFunc: func(ctx context.Context, alertID string, userID string) (store.Alert, error) {
				return store.Alert{
					AlertID:  "alert-123",
					UserID:   "user-123",
					Nickname: "High CPU Usage",
					Trigger: basealert.Trigger{
						Type:      basealert.TriggerTypeCPU,
						Operator:  basealert.OpGreaterThan,
						Threshold: 90.0,
					},
					Action: basealert.AlertAction{
						Type:        basealert.DestWebhook,
						Destination: ts.URL,
					},
				}, nil
			},
			UpdateHistoryFunc: func(ctx context.Context, historyID string, status string, errMsg string) error {
				updatedHistoryID = historyID
				updatedHistoryStatus = status
				updatedHistoryErrMsg = errMsg
				return nil
			},
			UpdateAgentFunc: func(ctx context.Context, alertID string, agentID string, status string, errMsg string) error {
				updatedAlertID = alertID
				updatedAgentID = agentID
				updatedAgentStatus = status
				updatedAgentErrMsg = errMsg
				return nil
			},
		}

		handler := RetryAlertHandler(mockStore)

		req := httptest.NewRequest("POST", "/api/alerts/history/retry/history-123", nil)
		ctxVal := context.WithValue(req.Context(), CSContext.UserIDKey, "user-123")
		
		// Set URL parameter using chi routing context
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("id", "history-123")
		ctxVal = context.WithValue(ctxVal, chi.RouteCtxKey, rctx)
		req = req.WithContext(ctxVal)

		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d. Body: %s", rec.Code, rec.Body.String())
		}

		var resp map[string]string
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}
		if resp["status"] != "success" {
			t.Errorf("expected success status, got %q", resp["status"])
		}

		if updatedHistoryID != "history-123" || updatedHistoryStatus != "success" || updatedHistoryErrMsg != "" {
			t.Errorf("unexpected history status update: id=%s status=%s err=%s", updatedHistoryID, updatedHistoryStatus, updatedHistoryErrMsg)
		}
		if updatedAlertID != "alert-123" || updatedAgentID != "agent-123" || updatedAgentStatus != "firing" || updatedAgentErrMsg != "" {
			t.Errorf("unexpected agent status update: id=%s agent=%s status=%s err=%s", updatedAlertID, updatedAgentID, updatedAgentStatus, updatedAgentErrMsg)
		}
	})

	t.Run("failed retry with webhook", func(t *testing.T) {
		// Mock HTTP webhook server to return 500
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer ts.Close()

		var updatedHistoryID, updatedHistoryStatus, updatedHistoryErrMsg string
		var updatedAlertID, updatedAgentID, updatedAgentStatus, updatedAgentErrMsg string

		mockStore := &mockAlertsStore{
			GetHistoryByIDFunc: func(ctx context.Context, historyID string, userID string) (*basealert.AlertHistory, error) {
				return &basealert.AlertHistory{
					HistoryID:     "history-123",
					AlertID:       "alert-123",
					AgentID:       "agent-123",
					AgentNickname: "TestNode",
					TriggerValue:  95.0,
					TriggeredAt:   time.Now(),
				}, nil
			},
			GetAlertInfoFunc: func(ctx context.Context, alertID string, userID string) (store.Alert, error) {
				return store.Alert{
					AlertID:  "alert-123",
					UserID:   "user-123",
					Nickname: "High CPU Usage",
					Trigger: basealert.Trigger{
						Type:      basealert.TriggerTypeCPU,
						Operator:  basealert.OpGreaterThan,
						Threshold: 90.0,
					},
					Action: basealert.AlertAction{
						Type:        basealert.DestWebhook,
						Destination: ts.URL,
					},
				}, nil
			},
			UpdateHistoryFunc: func(ctx context.Context, historyID string, status string, errMsg string) error {
				updatedHistoryID = historyID
				updatedHistoryStatus = status
				updatedHistoryErrMsg = errMsg
				return nil
			},
			UpdateAgentFunc: func(ctx context.Context, alertID string, agentID string, status string, errMsg string) error {
				updatedAlertID = alertID
				updatedAgentID = agentID
				updatedAgentStatus = status
				updatedAgentErrMsg = errMsg
				return nil
			},
		}

		handler := RetryAlertHandler(mockStore)

		req := httptest.NewRequest("POST", "/api/alerts/history/retry/history-123", nil)
		ctxVal := context.WithValue(req.Context(), CSContext.UserIDKey, "user-123")
		
		// Set URL parameter using chi routing context
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("id", "history-123")
		ctxVal = context.WithValue(ctxVal, chi.RouteCtxKey, rctx)
		req = req.WithContext(ctxVal)

		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d. Body: %s", rec.Code, rec.Body.String())
		}

		var resp map[string]string
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}
		if resp["status"] != "failed" {
			t.Errorf("expected failed status, got %q", resp["status"])
		}

		if updatedHistoryID != "history-123" || updatedHistoryStatus != "failed" || updatedHistoryErrMsg == "" {
			t.Errorf("unexpected history status update: id=%s status=%s err=%s", updatedHistoryID, updatedHistoryStatus, updatedHistoryErrMsg)
		}
		if updatedAlertID != "alert-123" || updatedAgentID != "agent-123" || updatedAgentStatus != "failed" || updatedAgentErrMsg == "" {
			t.Errorf("unexpected agent status update: id=%s agent=%s status=%s err=%s", updatedAlertID, updatedAgentID, updatedAgentStatus, updatedAgentErrMsg)
		}
	})

	t.Run("failed retry with preset target missing", func(t *testing.T) {
		var updatedHistoryID, updatedHistoryStatus, updatedHistoryErrMsg string
		var updatedAlertID, updatedAgentID, updatedAgentStatus, updatedAgentErrMsg string

		mockStore := &mockAlertsStore{
			GetHistoryByIDFunc: func(ctx context.Context, historyID string, userID string) (*basealert.AlertHistory, error) {
				return &basealert.AlertHistory{
					HistoryID:     "history-456",
					AlertID:       "alert-456",
					AgentID:       "agent-456",
					AgentNickname: "TestNode2",
					TriggerValue:  95.0,
					TriggeredAt:   time.Now(),
				}, nil
			},
			GetAlertInfoFunc: func(ctx context.Context, alertID string, userID string) (store.Alert, error) {
				return store.Alert{
					AlertID:  "alert-456",
					UserID:   "user-123",
					Nickname: "High CPU Usage Preset",
					Trigger: basealert.Trigger{
						Type:      basealert.TriggerTypeCPU,
						Operator:  basealert.OpGreaterThan,
						Threshold: 90.0,
					},
					Action: basealert.AlertAction{
						Type:     basealert.DestPreset,
						TargetID: "trg_missing",
					},
				}, nil
			},
			GetTargetByIDFunc: func(ctx context.Context, targetID string, userID string) (basealert.AlertTarget, error) {
				return basealert.AlertTarget{}, errors.New("alert target not found")
			},
			UpdateHistoryFunc: func(ctx context.Context, historyID string, status string, errMsg string) error {
				updatedHistoryID = historyID
				updatedHistoryStatus = status
				updatedHistoryErrMsg = errMsg
				return nil
			},
			UpdateAgentFunc: func(ctx context.Context, alertID string, agentID string, status string, errMsg string) error {
				updatedAlertID = alertID
				updatedAgentID = agentID
				updatedAgentStatus = status
				updatedAgentErrMsg = errMsg
				return nil
			},
		}

		handler := RetryAlertHandler(mockStore)

		req := httptest.NewRequest("POST", "/api/alerts/history/retry/history-456", nil)
		ctxVal := context.WithValue(req.Context(), CSContext.UserIDKey, "user-123")
		
		// Set URL parameter using chi routing context
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("id", "history-456")
		ctxVal = context.WithValue(ctxVal, chi.RouteCtxKey, rctx)
		req = req.WithContext(ctxVal)

		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d. Body: %s", rec.Code, rec.Body.String())
		}

		var resp map[string]string
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}
		if resp["status"] != "failed" {
			t.Errorf("expected failed status, got %q", resp["status"])
		}

		if updatedHistoryID != "history-456" || updatedHistoryStatus != "failed" || updatedHistoryErrMsg != "alert target not found" {
			t.Errorf("unexpected history status update: id=%s status=%s err=%s", updatedHistoryID, updatedHistoryStatus, updatedHistoryErrMsg)
		}
		if updatedAlertID != "alert-456" || updatedAgentID != "agent-456" || updatedAgentStatus != "failed" || updatedAgentErrMsg != "alert target not found" {
			t.Errorf("unexpected agent status update: id=%s agent=%s status=%s err=%s", updatedAlertID, updatedAgentID, updatedAgentStatus, updatedAgentErrMsg)
		}
	})
}
