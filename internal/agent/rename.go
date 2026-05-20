package agent

import (
	a "certainstats/internal/base/agent"
	ctx "certainstats/internal/context"
	apiresponse "certainstats/internal/response"

	"certainstats/internal/store"
	"database/sql"
	"encoding/json"
	"net/http"
	"strings"
)

func RenameAgentHandler(agent store.AgentStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {

		userID := r.Context().Value(ctx.UserIDKey).(string)

		var req a.RenameRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.AgentID == "" {
			apiresponse.Error(w, http.StatusBadRequest, "Invalid request body")
			return
		}

		req.Nickname = strings.TrimSpace(req.Nickname)
		if req.Nickname == "" {
			apiresponse.Error(w, http.StatusBadRequest, "Nickname cannot be empty")
			return
		}
		if len(req.Nickname) > 64 {
			apiresponse.Error(w, http.StatusBadRequest, "Nickname too long (max 64 chars)")
			return
		}

		err := agent.AgentRename(r.Context(), req.AgentID, userID, req.Nickname)

		if err != nil {
			apiresponse.Error(w, http.StatusInternalServerError, "Database error")
			return
		}
		if err == sql.ErrNoRows {
			apiresponse.Error(w, http.StatusNotFound, "Agent not found or unauthorized")
			return
		}

		w.Header().Set("Content-Type", "application/json")
		apiresponse.JSON(w, http.StatusOK, map[string]string{"status": "ok", "nickname": req.Nickname})
	}
}
