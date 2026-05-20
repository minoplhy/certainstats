package agent

import (
	ctx "certainstats/internal/context"
	api_response "certainstats/internal/response"
	"certainstats/internal/store"
	"net/http"
)

func ListAgentsManagementHandler(agents store.AgentStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := r.Context().Value(ctx.UserIDKey).(string)

		list, err := agents.AgentListManagement(r.Context(), userID)
		if err != nil {
			api_response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}

		w.Header().Set("Content-Type", "application/json")
		api_response.JSON(w, http.StatusOK, list)
	}
}
