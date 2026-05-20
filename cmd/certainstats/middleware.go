package main

import (
	auth "certainstats/internal/auth"
	ctx "certainstats/internal/context"
	"certainstats/internal/store"
	"context"
	"database/sql"
	"errors"
	log "certainstats/internal/logger"
	"net/http"
	"time"
)

func requireAuth(sessions store.SessionStore, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie("session_token")
		if err != nil {
			http.Error(w, "Unauthorized — missing cookie", http.StatusUnauthorized)
			return
		}

		sess, err := sessions.SessionGet(r.Context(), cookie.Value)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				auth.ClearSessionCookie(w)
				http.Error(w, "Session expired or invalid", http.StatusUnauthorized)
				return
			}
			log.Printf("session: %s\n", err.Error())
			http.Error(w, "Internal error", http.StatusInternalServerError)
			return
		}

		if time.Now().After(sess.ExpiresAt) {
			sessions.SessionDelete(r.Context(), cookie.Value) //nolint:errcheck
			auth.ClearSessionCookie(w)
			http.Error(w, "Session expired", http.StatusUnauthorized)
			return
		}

		// Update activity timestamp if older than 5 minutes
		if time.Since(sess.LastConnectedAt) > 5*time.Minute {
			sessions.SessionUpdateActivity(r.Context(), sess.Token, time.Now()) //nolint:errcheck
		}

		ctx := context.WithValue(r.Context(), ctx.UserIDKey, sess.UserID)
		next.ServeHTTP(w, r.WithContext(ctx))
	}
}
