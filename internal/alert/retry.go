package alert

import (
	basealert "certainstats/internal/base/alert"
	ctx "certainstats/internal/context"
	"certainstats/internal/notifications"
	apiresponse "certainstats/internal/response"
	"certainstats/internal/store"
	"database/sql"
	"net/http"

	"github.com/go-chi/chi/v5"
)

// RetryAlertHandler handles POST /api/alerts/history/retry/{id}
func RetryAlertHandler(db store.AlertsStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID, ok := r.Context().Value(ctx.UserIDKey).(string)
		if !ok {
			apiresponse.Error(w, http.StatusUnauthorized, "Unauthorized")
			return
		}

		historyID := chi.URLParam(r, "id")
		if historyID == "" {
			apiresponse.Error(w, http.StatusBadRequest, "Missing history ID")
			return
		}

		// 1. Retrieve the alert history record
		history, err := db.AlertHistoryGetByID(r.Context(), historyID, userID)
		if err != nil {
			if err == sql.ErrNoRows {
				apiresponse.Error(w, http.StatusNotFound, "Alert history not found")
				return
			}
			apiresponse.Error(w, http.StatusInternalServerError, "Database error reading history")
			return
		}

		// 1.5. Reject retry if the notification has already succeeded
		if history.NotifiedStatus == "success" {
			apiresponse.Error(w, http.StatusBadRequest, "Notification has already been successfully delivered")
			return
		}

		// 2. Fetch the corresponding alert details
		alertVal, err := db.AlertGetInfo(r.Context(), history.AlertID, userID)
		if err != nil {
			apiresponse.Error(w, http.StatusInternalServerError, "Database error reading alert info")
			return
		}

		// 3. Construct Notification Context
		nctx := notifications.NotificationContext{
			AgentID:       history.AgentID,
			Nickname:      history.AgentNickname,
			TriggerType:   string(alertVal.Trigger.Type),
			Status:        "FIRING", // We are retrying a firing event
			Value:         history.TriggerValue,
			Operator:      string(alertVal.Trigger.Operator),
			Threshold:     alertVal.Trigger.Threshold,
			WentOfflineAt: &history.TriggeredAt,
		}

		// 4. Resolve destination / targets
		actionToDispatch := alertVal.Action
		var notifErr error
		if alertVal.Action.Type == basealert.DestPreset && alertVal.Action.TargetID != "" {
			target, err := db.TargetGetByID(r.Context(), alertVal.Action.TargetID, userID)
			if err == nil {
				actionToDispatch.Type = target.Type
				actionToDispatch.Destination = target.Destination
				if actionToDispatch.Payload == "" {
					actionToDispatch.Payload = target.Payload
				}
			} else {
				notifErr = err
			}
		}

		// 5. Dispatch notification
		if notifErr == nil {
			notifErr = notifications.DispatchNotification(actionToDispatch, nctx)
		}

		if notifErr != nil {
			// Update status to failed and store the error message
			_ = db.AlertHistoryUpdateStatus(r.Context(), historyID, "failed", notifErr.Error())
			_ = db.AlertAgentUpdateStatus(r.Context(), alertVal.AlertID, history.AgentID, "failed", notifErr.Error())

			apiresponse.JSON(w, http.StatusOK, map[string]string{
				"status":        "failed",
				"message":       "Notification dispatch failed",
				"error_message": notifErr.Error(),
			})
			return
		}

		// On success, update statuses in DB
		_ = db.AlertHistoryUpdateStatus(r.Context(), historyID, "success", "")
		_ = db.AlertAgentUpdateStatus(r.Context(), alertVal.AlertID, history.AgentID, "firing", "")

		apiresponse.JSON(w, http.StatusOK, map[string]string{
			"status":  "success",
			"message": "Notification retried successfully",
		})
	}
}
