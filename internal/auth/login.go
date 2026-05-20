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
	"time"

	"strings"

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

		token := generateSessionToken()
		duration := 24 * time.Hour
		if req.Remember {
			duration = 30 * 24 * time.Hour
		}
		expiresAt := time.Now().Add(duration)

		// Parse IP Address from headers or RemoteAddr
		ip := r.Header.Get("X-Forwarded-For")
		if ip == "" {
			ip = r.RemoteAddr
		}
		if idx := strings.Index(ip, ","); idx != -1 {
			ip = strings.TrimSpace(ip[:idx])
		}

		now := time.Now()
		if err := sessions.SessionCreate(r.Context(), store.Session{
			Token:           token,
			UserID:          user.UserID,
			ExpiresAt:       expiresAt,
			CreatedAt:       now,
			LastConnectedAt: now,
			IPAddress:       ip,
			UserAgent:       r.UserAgent(),
		}); err != nil {
			apiresponse.Error(w, http.StatusInternalServerError, "Failed to create session")
			log.Debugf("failed to created session: %s", err)
			return
		}

		http.SetCookie(w, &http.Cookie{
			Name:     "session_token",
			Value:    token,
			Path:     "/",
			Expires:  expiresAt,
			HttpOnly: true,
			SameSite: http.SameSiteLaxMode,
			// Secure: true, // enable in production (HTTPS)
		})

		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"success"}`))
	}
}
