package alert

import (
	ctx "certainstats/internal/context"
	apiresponse "certainstats/internal/response"

	"certainstats/internal/store"
	"database/sql"
	"net/http"

	"github.com/go-chi/chi/v5"
)

// DeleteAlertHandler handles DELETE /api/alert/{id}
func DeleteAlertHandler(s store.AlertsStore) http.HandlerFunc {
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

		if err := s.AlertDelete(r.Context(), alertID, userID); err != nil {
			if err == sql.ErrNoRows {
				apiresponse.Error(w, http.StatusNotFound, "Alert not found")
				return
			}
			apiresponse.Error(w, http.StatusInternalServerError, "Database error")
			return
		}

		w.WriteHeader(http.StatusNoContent)
	}
}
