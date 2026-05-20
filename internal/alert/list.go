package alert

import (
	ctx "certainstats/internal/context"
	apiresponse "certainstats/internal/response"

	"certainstats/internal/store"
	"log"
	"net/http"
)

// ListAlertsHandler handles GET /api/alerts
func ListAlertsHandler(s store.AlertsStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID, ok := r.Context().Value(ctx.UserIDKey).(string)
		if !ok {
			apiresponse.Error(w, http.StatusUnauthorized, "Unauthorized")
			return
		}

		alerts, err := s.AlertList(r.Context(), userID)
		if err != nil {
			apiresponse.Error(w, http.StatusInternalServerError, "Database error")
			log.Println(err)

			return
		}

		w.Header().Set("Content-Type", "application/json")
		apiresponse.JSON(w, http.StatusOK, alerts)
	}
}
