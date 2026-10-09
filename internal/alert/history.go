package alert

import (
	"certainstats/internal/alertdelivery"
	ctx "certainstats/internal/context"
	apiresponse "certainstats/internal/response"
	"certainstats/internal/store"
	"database/sql"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
)

func historyPagination(r *http.Request, defaultLimit int) (int, int) {
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	if page > 1000000 {
		page = 1000000
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit < 1 {
		limit = defaultLimit
	}
	if limit > 100 {
		limit = 100
	}
	return page, limit
}
func historyUser(w http.ResponseWriter, r *http.Request) (string, bool) {
	user, ok := r.Context().Value(ctx.UserIDKey).(string)
	if !ok || user == "" {
		apiresponse.Error(w, http.StatusUnauthorized, "Unauthorized")
		return "", false
	}
	return user, true
}
func HistoryAlertHandler(db store.AlertsStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, ok := historyUser(w, r)
		if !ok {
			return
		}
		page, limit := historyPagination(r, 25)
		status := r.URL.Query().Get("status")
		switch status {
		case "", "all", "firing", "resolved", "closed":
		default:
			apiresponse.Error(w, 400, "Unknown incident status")
			return
		}
		history, total, err := db.AlertHistoryListFiltered(r.Context(), user, page, limit, r.URL.Query().Get("q"), status, r.URL.Query().Get("agent_id"))
		if err != nil {
			apiresponse.Error(w, 500, "Failed to retrieve incident history")
			return
		}
		apiresponse.JSON(w, 200, map[string]any{"data": history, "total": total, "page": page, "limit": limit, "total_pages": (total + limit - 1) / limit})
	}
}
func HistorySummaryHandler(db store.AlertsStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, ok := historyUser(w, r)
		if !ok {
			return
		}
		count, err := db.AlertHistorySummary(r.Context(), user)
		if err != nil {
			apiresponse.Error(w, 500, "Failed to retrieve incident summary")
			return
		}
		nodes, err := db.AlertHistoryNodes(r.Context(), user)
		if err != nil {
			apiresponse.Error(w, 500, "Failed to retrieve history nodes")
			return
		}
		apiresponse.JSON(w, 200, map[string]any{"firing": count, "nodes": nodes})
	}
}
func HistoryEventsHandler(db store.AlertsStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, ok := historyUser(w, r)
		if !ok {
			return
		}
		page, limit := historyPagination(r, 50)
		events, total, err := db.AlertHistoryEvents(r.Context(), user, chi.URLParam(r, "id"), page, limit)
		if err == sql.ErrNoRows {
			apiresponse.Error(w, 404, "Incident not found")
			return
		}
		if err != nil {
			apiresponse.Error(w, 500, "Failed to retrieve incident events")
			return
		}
		apiresponse.JSON(w, 200, map[string]any{"data": events, "total": total, "page": page, "limit": limit, "total_pages": (total + limit - 1) / limit})
	}
}
func RetryEventHandler(db store.AlertsStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, ok := historyUser(w, r)
		if !ok {
			return
		}
		id := chi.URLParam(r, "id")
		// Resolve ownership before exposing even an event's existence.
		h, err := db.AlertAttemptIncident(r.Context(), id)
		if err != nil {
			if err == sql.ErrNoRows {
				apiresponse.Error(w, 404, "Notification attempt not found")
			} else {
				apiresponse.Error(w, 500, "Failed to retrieve notification attempt")
			}
			return
		}
		if h.UserID != user {
			apiresponse.Error(w, 404, "Notification attempt not found")
			return
		}
		for _, phase := range []string{"firing", "recovery"} {
			latest, err := alertdelivery.LatestAttempt(r.Context(), db, user, h.HistoryID, phase)
			if err != nil {
				apiresponse.Error(w, 500, "Failed to retrieve attempt")
				return
			}
			if latest != nil && latest.EventID == id {
				queueHistoryRetry(w, r, db, user, h.HistoryID, phase, id)
				return
			}
		}
		apiresponse.Error(w, 409, "Only the latest failed or unknown attempt can be retried")
	}
}
