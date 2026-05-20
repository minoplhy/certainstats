package alert

import (
	ctx "certainstats/internal/context"
	apiresponse "certainstats/internal/response"

	"certainstats/internal/store"
	"database/sql"
	"net/http"

	"github.com/go-chi/chi/v5"
)

// GetAlertHandler handles GET /api/alert/{id}
func GetAlertHandler(s store.AlertsStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID, ok := r.Context().Value(ctx.UserIDKey).(string)
		if !ok {
			apiresponse.Error(w, http.StatusUnauthorized, "Unauthorized")
			return
		}

		alertID := chi.URLParam(r, "id")
		if alertID == "" {
			apiresponse.Error(w, http.StatusBadRequest, "Missing alert ID")
			return
		}

		a, err := s.AlertGetInfo(r.Context(), alertID, userID)
		if err != nil {
			if err == sql.ErrNoRows {
				apiresponse.Error(w, http.StatusNotFound, "Alert not found")
				return
			}
			apiresponse.Error(w, http.StatusInternalServerError, "Database error")
			return
		}

		w.Header().Set("Content-Type", "application/json")
		apiresponse.JSON(w, http.StatusOK, a)
	}
}
