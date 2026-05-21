package main

import (
	auth "certainstats/internal/auth"
	ctx "certainstats/internal/context"
	log "certainstats/internal/logger"
	apiresponse "certainstats/internal/response"
	"certainstats/internal/store"
	"context"
	"database/sql"
	"errors"
	"net/http"
	"time"
)

func requireAuth(sessions store.SessionStore, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie("session_token")
		if err != nil {
			apiresponse.Error(w, http.StatusUnauthorized, "Unauthorized")
			return
		}

		sess, err := sessions.SessionGet(r.Context(), cookie.Value)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				auth.ClearSessionCookie(w)
				apiresponse.Error(w, http.StatusUnauthorized, "Unauthorized")
				return
			}
			log.Printf("session: %s\n", err.Error())
			apiresponse.Error(w, http.StatusInternalServerError, "Internal Error")
			return
		}

		if time.Now().After(sess.ExpiresAt) {
			sessions.SessionDelete(r.Context(), cookie.Value) //nolint:errcheck
			auth.ClearSessionCookie(w)
			apiresponse.Error(w, http.StatusUnauthorized, "Unauthorized")
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
