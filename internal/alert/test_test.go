package alert

import (
	basealert "certainstats/internal/base/alert"
	CSContext "certainstats/internal/context"
	"certainstats/internal/store"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAlertHandler_Tests(t *testing.T) {
	t.Run("test alert with alert_id using preset target", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
		defer ts.Close()

		mockStore := &mockAlertsStore{
			GetAlertInfoFunc: func(ctx context.Context, alertID string, userID string) (store.Alert, error) {
				return store.Alert{
					AlertID:  "alert-101",
					UserID:   "user-123",
					Nickname: "CPU High",
					Action: basealert.AlertAction{
						Type:     basealert.DestPreset,
						TargetID: "target-101",
					},
				}, nil
			},
			GetTargetByIDFunc: func(ctx context.Context, targetID string, userID string) (basealert.AlertTarget, error) {
				return basealert.AlertTarget{
					TargetID:    "target-101",
					UserID:      "user-123",
					Name:        "Discord Target",
					Type:        basealert.DestDiscord,
					Destination: ts.URL,
				}, nil
			},
		}

		handler := TestAlertHandler(mockStore)

		body, _ := json.Marshal(map[string]string{
			"alert_id": "alert-101",
		})
		req := httptest.NewRequest("POST", "/api/alerts/test", bytes.NewReader(body))
		req = req.WithContext(context.WithValue(req.Context(), CSContext.UserIDKey, "user-123"))

		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d. Body: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("test alert with direct action webhook", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
		defer ts.Close()

		mockStore := &mockAlertsStore{}
		handler := TestAlertHandler(mockStore)

		body, _ := json.Marshal(map[string]any{
			"action": map[string]any{
				"type":        "webhook",
				"destination": ts.URL,
			},
		})
		req := httptest.NewRequest("POST", "/api/alerts/test", bytes.NewReader(body))
		req = req.WithContext(context.WithValue(req.Context(), CSContext.UserIDKey, "user-123"))

		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d. Body: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("test alert with missing alert_id and action", func(t *testing.T) {
		mockStore := &mockAlertsStore{}
		handler := TestAlertHandler(mockStore)

		body, _ := json.Marshal(map[string]any{})
		req := httptest.NewRequest("POST", "/api/alerts/test", bytes.NewReader(body))
		req = req.WithContext(context.WithValue(req.Context(), CSContext.UserIDKey, "user-123"))

		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected status 400, got %d", rec.Code)
		}
	})

	t.Run("test alert with unknown target preset", func(t *testing.T) {
		mockStore := &mockAlertsStore{
			GetAlertInfoFunc: func(ctx context.Context, alertID string, userID string) (store.Alert, error) {
				return store.Alert{
					AlertID: "alert-bad",
					UserID:  "user-123",
					Action: basealert.AlertAction{
						Type:     basealert.DestPreset,
						TargetID: "target-unknown",
					},
				}, nil
			},
			GetTargetByIDFunc: func(ctx context.Context, targetID string, userID string) (basealert.AlertTarget, error) {
				return basealert.AlertTarget{}, errors.New("target not found")
			},
		}

		handler := TestAlertHandler(mockStore)

		body, _ := json.Marshal(map[string]string{
			"alert_id": "alert-bad",
		})
		req := httptest.NewRequest("POST", "/api/alerts/test", bytes.NewReader(body))
		req = req.WithContext(context.WithValue(req.Context(), CSContext.UserIDKey, "user-123"))

		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected status 400, got %d. Body: %s", rec.Code, rec.Body.String())
		}
	})
}

func TestTargetHandler_Tests(t *testing.T) {
	t.Run("test target by target_id", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
		defer ts.Close()

		mockStore := &mockAlertsStore{
			GetTargetByIDFunc: func(ctx context.Context, targetID string, userID string) (basealert.AlertTarget, error) {
				return basealert.AlertTarget{
					TargetID:    "target-555",
					UserID:      "user-123",
					Name:        "Ops Discord",
					Type:        basealert.DestDiscord,
					Destination: ts.URL,
				}, nil
			},
		}

		handler := TestTargetHandler(mockStore)

		body, _ := json.Marshal(map[string]string{
			"target_id": "target-555",
		})
		req := httptest.NewRequest("POST", "/api/alerts/targets/test", bytes.NewReader(body))
		req = req.WithContext(context.WithValue(req.Context(), CSContext.UserIDKey, "user-123"))

		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d. Body: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("test target direct destination", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
		defer ts.Close()

		mockStore := &mockAlertsStore{}
		handler := TestTargetHandler(mockStore)

		body, _ := json.Marshal(map[string]string{
			"type":        "webhook",
			"destination": ts.URL,
		})
		req := httptest.NewRequest("POST", "/api/alerts/targets/test", bytes.NewReader(body))
		req = req.WithContext(context.WithValue(req.Context(), CSContext.UserIDKey, "user-123"))

		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d. Body: %s", rec.Code, rec.Body.String())
		}
	})
}
