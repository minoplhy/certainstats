package alert

import (
	"certainstats/internal/base/alert"
	ctx "certainstats/internal/context"
	"certainstats/internal/notifications"
	apiresponse "certainstats/internal/response"
	"certainstats/internal/store"
	"encoding/json"
	"net/http"
)

type TestAlertRequest struct {
	AlertID string             `json:"alert_id,omitempty"`
	Action  *alert.AlertAction `json:"action,omitempty"`
}

func TestAlertHandler(s store.AlertsStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID, ok := r.Context().Value(ctx.UserIDKey).(string)
		if !ok {
			apiresponse.Error(w, http.StatusUnauthorized, "Unauthorized")
			return
		}

		var req TestAlertRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			apiresponse.Error(w, http.StatusBadRequest, "Invalid request body")
			return
		}

		var action alert.AlertAction
		if req.AlertID != "" {
			alertVal, err := s.AlertGetInfo(r.Context(), req.AlertID, userID)
			if err != nil {
				apiresponse.Error(w, http.StatusNotFound, "Alert rule not found: "+err.Error())
				return
			}
			action = alertVal.Action
		} else if req.Action != nil {
			action = *req.Action
		} else {
			apiresponse.Error(w, http.StatusBadRequest, "Either alert_id or action configuration is required")
			return
		}

		if action.Type == alert.DestPreset && action.TargetID != "" {
			target, err := s.TargetGetByID(r.Context(), action.TargetID, userID)
			if err != nil {
				apiresponse.Error(w, http.StatusBadRequest, "Failed to resolve target preset: "+err.Error())
				return
			}
			action.Type = target.Type
			action.Destination = target.Destination
			if action.Payload == "" {
				action.Payload = target.Payload
			}
		}

		if action.Type == "" {
			apiresponse.Error(w, http.StatusBadRequest, "Notification destination type is not configured")
			return
		}

		// Dispatch a dummy notification
		err := notifications.DispatchNotification(action, notifications.NotificationContext{
			AgentID:     "agt_test123",
			Nickname:    "Test Node",
			TriggerType: "cpu_usage",
			Status:      "FIRING",
			Value:       99.9,
		})
		if err != nil {
			apiresponse.Error(w, http.StatusInternalServerError, "Test notification failed: "+err.Error())
			return
		}

		apiresponse.Success(w, "Test notification sent successfully", nil)
	}
}
