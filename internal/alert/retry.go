package alert

import (
	basealert "certainstats/internal/base/alert"
	ctx "certainstats/internal/context"
	"certainstats/internal/notifications"
	apiresponse "certainstats/internal/response"
	"certainstats/internal/store"
	"context"
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

		// 2. Mark status as pending immediately so UI reflects in-progress state
		_ = db.AlertHistoryUpdateStatus(r.Context(), historyID, "pending", "")

		// 3. Queue notification dispatch to background without blocking HTTP response
		go func(hID, aID, uID string, hist basealert.AlertHistory) {
			bgCtx := context.Background()

			alertVal, err := db.AlertGetInfo(bgCtx, hist.AlertID, uID)
			if err != nil {
				_ = db.AlertHistoryUpdateStatus(bgCtx, hID, "failed", "Database error reading alert info: "+err.Error())
				return
			}

			nctx := notifications.NotificationContext{
				AgentID:       hist.AgentID,
				Nickname:      hist.AgentNickname,
				TriggerType:   string(alertVal.Trigger.Type),
				Status:        "FIRING",
				Value:         hist.TriggerValue,
				Operator:      string(alertVal.Trigger.Operator),
				Threshold:     alertVal.Trigger.Threshold,
				WentOfflineAt: &hist.TriggeredAt,
			}

			actionToDispatch := alertVal.Action
			var notifErr error
			if alertVal.Action.Type == basealert.DestPreset && alertVal.Action.TargetID != "" {
				target, err := db.TargetGetByID(bgCtx, alertVal.Action.TargetID, uID)
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

			if notifErr == nil {
				notifErr = notifications.DispatchNotification(actionToDispatch, nctx)
			}

			if notifErr != nil {
				_ = db.AlertHistoryUpdateStatus(bgCtx, hID, "failed", notifErr.Error())
				_ = db.AlertAgentUpdateStatus(bgCtx, alertVal.AlertID, hist.AgentID, "failed", notifErr.Error())
			} else {
				_ = db.AlertHistoryUpdateStatus(bgCtx, hID, "success", "")
				_ = db.AlertAgentUpdateStatus(bgCtx, alertVal.AlertID, hist.AgentID, "firing", "")
			}
		}(historyID, history.AlertID, userID, *history)

		apiresponse.JSON(w, http.StatusOK, map[string]string{
			"status":  "queued",
			"message": "Notification retry queued in background",
		})
	}
}
