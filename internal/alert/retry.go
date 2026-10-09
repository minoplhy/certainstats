package alert

import (
	"certainstats/internal/alertdelivery"
	apiresponse "certainstats/internal/response"
	"certainstats/internal/store"
	"context"
	"database/sql"
	"errors"
	"log"
	"net/http"

	"github.com/go-chi/chi/v5"
)

// RetryAlertHandler preserves the original firing-notification retry endpoint.
func RetryAlertHandler(db store.AlertsStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, ok := historyUser(w, r)
		if !ok {
			return
		}
		id := chi.URLParam(r, "id")
		if id == "" {
			apiresponse.Error(w, 400, "Missing history ID")
			return
		}
		h, err := db.AlertHistoryGetByID(r.Context(), id, user)
		if err == sql.ErrNoRows {
			apiresponse.Error(w, 404, "Incident not found")
			return
		}
		if err != nil {
			apiresponse.Error(w, 500, "Failed to retrieve incident")
			return
		}
		latest, err := alertdelivery.LatestAttempt(r.Context(), db, user, id, "firing")
		if err != nil {
			apiresponse.Error(w, 500, "Failed to retrieve notification attempt")
			return
		}
		retryOf := ""
		if latest != nil {
			retryOf = latest.EventID
		} else if !h.Legacy || (h.NotifiedStatus != "failed" && h.NotifiedStatus != "unknown") {
			apiresponse.Error(w, 409, "No failed firing notification to retry")
			return
		}
		queueHistoryRetry(w, r, db, user, id, "firing", retryOf)
	}
}
func queueHistoryRetry(w http.ResponseWriter, r *http.Request, db store.AlertsStore, user, id, phase, retryOf string) {
	attempt, err := db.AlertAttemptQueue(r.Context(), user, id, phase, retryOf)
	if err != nil {
		if errors.Is(err, store.ErrAlertAttemptUnavailable) {
			apiresponse.Error(w, 409, "Notification is not eligible for retry")
		} else {
			apiresponse.Error(w, 500, "Failed to queue notification retry")
		}
		return
	}
	go func() {
		if err := alertdelivery.Dispatch(context.Background(), db, attempt.EventID); err != nil {
			log.Printf("Notification retry: %v", err)
		}
	}()
	apiresponse.JSON(w, 200, map[string]string{"status": "queued", "message": "Notification retry queued", "event_id": attempt.EventID})
}
