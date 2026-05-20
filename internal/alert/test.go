package alert

import (
	"certainstats/internal/base/alert"
	"certainstats/internal/notifications"
	apiresponse "certainstats/internal/response"

	"certainstats/internal/store"
	"encoding/json"
	"net/http"
)

type TestAlertRequest struct {
	Action alert.AlertAction `json:"action"`
}

func TestAlertHandler(s store.AlertsStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req TestAlertRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			apiresponse.Error(w, http.StatusBadRequest, "Invalid request body")
			return
		}

		// Dispatch a dummy notification
		err := notifications.DispatchNotification(req.Action, notifications.NotificationContext{
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
