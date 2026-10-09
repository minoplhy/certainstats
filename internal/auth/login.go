package auth

import (
	baseresponse "certainstats/internal/base/response"
	log "certainstats/internal/logger"
	apiresponse "certainstats/internal/response"

	"certainstats/internal/store"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"

	"golang.org/x/crypto/bcrypt"
)

func LoginHandler(users store.UserStore, sessions store.SessionStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req baseresponse.LoginRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			apiresponse.Error(w, http.StatusBadRequest, "Invalid request body")
			return
		}

		user, err := users.GetByUsername(r.Context(), req.Username)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				apiresponse.Error(w, http.StatusUnauthorized, "Invalid credentials")
				return
			}
			apiresponse.Error(w, http.StatusInternalServerError, "Database error")
			return
		}

		if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password)); err != nil {
			apiresponse.Error(w, http.StatusUnauthorized, "Invalid credentials")
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
