package main

import (
	auth "certainstats/internal/auth"
	ctx "certainstats/internal/context"
	log "certainstats/internal/logger"
	apiresponse "certainstats/internal/response"
	"certainstats/internal/store"
	"context"
	"errors"
	"net/http"
)

func requireAuth(sessions store.SessionStore, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "private, no-store")
		sess, err := auth.Authenticate(r, sessions)
		if err != nil {
			if errors.Is(err, auth.ErrUnauthenticated) {
				auth.ClearSessionCookie(w)
				apiresponse.Error(w, http.StatusUnauthorized, "Unauthorized")
			} else {
				log.Printf("session: %v", err)
				apiresponse.Error(w, 500, "Internal error")
			}
			return
		}

		ctx := context.WithValue(r.Context(), ctx.UserIDKey, sess.UserID)
		next.ServeHTTP(w, r.WithContext(ctx))
	}
}
