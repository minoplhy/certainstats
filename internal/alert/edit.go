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

type EditAlertRequest struct {
	Nickname string            `json:"nickname"`
	Enabled  bool              `json:"enabled"`
	Trigger  alert.Trigger     `json:"trigger"`
	Action   alert.AlertAction `json:"action"`
	Agents   []string          `json:"agents"` // List of AgentIDs
}

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

		var req EditAlertRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			apiresponse.Error(w, http.StatusBadRequest, "Invalid request body")
			return
		}

		if strings.TrimSpace(req.Nickname) == "" {
			apiresponse.Error(w, http.StatusBadRequest, "Alert name / nickname is required")
			return
		}

		// Normalize: agent_down alerts don't have a threshold/operator
		if req.Trigger.Type == alert.TriggerTypeDown {
			req.Trigger.Operator = ""
			req.Trigger.Threshold = 0
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
