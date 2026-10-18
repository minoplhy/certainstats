package auth

import (
	baseresponse "certainstats/internal/base/response"
	log "certainstats/internal/logger"
	apiresponse "certainstats/internal/response"

	"certainstats/internal/store"
	"encoding/json"
	"errors"
	"net/http"
)

func LoginHandler(users store.UserStore, sessions store.SessionStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req baseresponse.LoginRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			apiresponse.Error(w, http.StatusBadRequest, "Invalid request body")
			return
		}

		user, err := CheckCredentials(r.Context(), users, req.Username, req.Password)
		if errors.Is(err, ErrUnauthenticated) {
			apiresponse.Error(w, http.StatusUnauthorized, "Invalid credentials")
			return
		}
		if err != nil {
			apiresponse.Error(w, http.StatusInternalServerError, "Database error")
			return
		}

		if err := CreateBrowserSession(w, r, sessions, user.UserID, req.Remember); err != nil {
			apiresponse.Error(w, 500, "Failed to create session")
			log.Printf("session creation: %v", err)
			return
		}

		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"success"}`))
	}
}
