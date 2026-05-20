package agent

import (
	agentdata "certainstats/internal/agent_data"
	ctx "certainstats/internal/context"
	api_response "certainstats/internal/response"
	"certainstats/internal/store"
	"certainstats/internal/ws"
	"net/http"

	"github.com/go-chi/chi/v5"
)

func ResetAgentTokenHandler(agent store.AgentStore, wsManager *ws.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := r.Context().Value(ctx.UserIDKey).(string)
		agentID := chi.URLParam(r, "id")
		if agentID == "" {
			api_response.Error(w, http.StatusBadRequest, "Missing agent_id")
			return
		}

		ag, err := agent.AgentGetByID(r.Context(), agentID, userID)
		if err != nil {
			api_response.Error(w, http.StatusNotFound, "Agent not found")
			return
		}

		newToken := agentdata.GenerateDeviceToken(ag.AgentType)

		err = agent.AgentResetToken(r.Context(), agentID, userID, newToken)
		if err != nil {
			api_response.Error(w, http.StatusInternalServerError, "Failed to reset token")
			return
		}

		api_response.JSON(w, http.StatusOK, map[string]string{
			"status":  "success",
			"message": "Agent token reset successfully",
			"token":   newToken,
		})
	}
}
