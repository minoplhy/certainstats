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

		var nickname *string
		if req.Nickname != nil {
			trimmed := strings.TrimSpace(*req.Nickname)
			if trimmed == "" {
				apiresponse.Error(w, http.StatusBadRequest, "Nickname cannot be empty")
				return
			}
			if len(trimmed) > 64 {
				apiresponse.Error(w, http.StatusBadRequest, "Nickname too long (max 64 chars)")
				return
			}
			nickname = &trimmed
		}

		var note *string
		if req.Note != nil {
			trimmedNote := strings.TrimSpace(*req.Note)
			if len(trimmedNote) > 10000 {
				apiresponse.Error(w, http.StatusBadRequest, "Note too long (max 10000 chars)")
				return
			}
			note = &trimmedNote
		}

		if nickname == nil && note == nil {
			apiresponse.Error(w, http.StatusBadRequest, "Either nickname or note must be specified")
			return
		}

		err := agent.AgentUpdate(r.Context(), req.AgentID, userID, nickname, note)

		if err != nil {
			if err == sql.ErrNoRows {
				apiresponse.Error(w, http.StatusNotFound, "Agent not found or unauthorized")
				return
			}
			apiresponse.Error(w, http.StatusInternalServerError, "Database error")
			return
		}

		w.Header().Set("Content-Type", "application/json")
		respData := map[string]any{"status": "ok"}
		if nickname != nil {
			respData["nickname"] = *nickname
		}
		if note != nil {
			respData["note"] = *note
		}
		apiresponse.JSON(w, http.StatusOK, respData)
	}
}
