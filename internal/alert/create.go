package alert

import (
	agentdata "certainstats/internal/agent_data"
	"certainstats/internal/base/alert"
	ctx "certainstats/internal/context"
	apiresponse "certainstats/internal/response"
	"certainstats/internal/store"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// CreateAlertHandler handles POST /api/alert
func CreateAlertHandler(s store.AlertsStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID, ok := r.Context().Value(ctx.UserIDKey).(string)
		if !ok {
			apiresponse.Error(w, http.StatusUnauthorized, "Unauthorized")
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

		newAlert := store.Alert{
			AlertID:  fmt.Sprintf("alert_%d_%s", time.Now().UnixMicro(), agentdata.GenerateRandomString(6)),
			UserID:   userID,
			Nickname: req.Nickname,
			Enabled:  req.Enabled,
			Trigger:  req.Trigger,
			Action:   req.Action,
		}

		for _, agentID := range req.Agents {
			newAlert.Agents = append(newAlert.Agents, alert.AgentState{
				AgentID: agentID,
				Status:  "ok",
			})
		}

		if err := s.AlertCreate(r.Context(), newAlert); err != nil {
			apiresponse.Error(w, http.StatusInternalServerError, "Database error")
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		apiresponse.JSON(w, http.StatusOK, newAlert)
	}
}
