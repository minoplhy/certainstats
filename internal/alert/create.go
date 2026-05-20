package alert

import (
	agentdata "certainstats/internal/agent_data"
	"certainstats/internal/base/alert"
	ctx "certainstats/internal/context"
	apiresponse "certainstats/internal/response"

	"certainstats/internal/store"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

type CreateAlertRequest struct {
	Enabled bool              `json:"enabled"`
	Trigger alert.Trigger     `json:"trigger"`
	Action  alert.AlertAction `json:"action"`
	Agents  []string          `json:"agents"` // List of AgentIDs
}

// CreateAlertHandler handles POST /api/alert
func CreateAlertHandler(s store.AlertsStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID, ok := r.Context().Value(ctx.UserIDKey).(string)
		if !ok {
			apiresponse.Error(w, http.StatusUnauthorized, "Unauthorized")
			return
		}

		var req CreateAlertRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			apiresponse.Error(w, http.StatusBadRequest, "Invalid request body")
			return
		}

		// Normalize: agent_down alerts don't have a threshold/operator
		if req.Trigger.Type == alert.TriggerTypeDown {
			req.Trigger.Operator = ""
			req.Trigger.Threshold = 0
		}

		newAlert := store.Alert{
			AlertID: fmt.Sprintf("alert_%d_%s", time.Now().UnixMicro(), agentdata.GenerateRandomString(6)),
			UserID:  userID,
			Enabled: req.Enabled,
			Trigger: req.Trigger,
			Action:  req.Action,
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
