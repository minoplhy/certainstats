package alert

import (
	"certainstats/internal/base/alert"
	ctx "certainstats/internal/context"
	apiresponse "certainstats/internal/response"
	"certainstats/internal/store"
	"database/sql"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
)

// EditAlertHandler handles PUT /api/alert/{id}
func EditAlertHandler(s store.AlertsStore) http.HandlerFunc {
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

		var req alert.AlertPayload
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			apiresponse.Error(w, http.StatusBadRequest, "Invalid request body")
			return
		}

		req.Nickname = strings.TrimSpace(req.Nickname)
		if req.Nickname == "" {
			apiresponse.Error(w, http.StatusBadRequest, "Alert name / nickname is required")
			return
		}

		if err := ParseTrigger(&req.Trigger); err != nil {
			apiresponse.Error(w, http.StatusBadRequest, err.Error())
			return
		}

		if err := ParseAction(&req.Action); err != nil {
			apiresponse.Error(w, http.StatusBadRequest, err.Error())
			return
		}

		// Verify target ownership if preset is selected
		if req.Action.Type == alert.DestPreset {
			_, err := s.TargetGetByID(r.Context(), req.Action.TargetID, userID)
			if err != nil {
				if err == sql.ErrNoRows {
					apiresponse.Error(w, http.StatusBadRequest, "Invalid or unauthorized preset target selected")
				} else {
					apiresponse.Error(w, http.StatusInternalServerError, "Database verification error")
				}
				return
			}
		}

		updatedAlert := store.Alert{
			AlertID:  alertID,
			UserID:   userID,
			Nickname: req.Nickname,
			Enabled:  req.Enabled,
			Trigger:  req.Trigger,
			Action:   req.Action,
		}

		if err := s.AlertUpdate(r.Context(), updatedAlert, req.Agents); err != nil {
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
