package auth

import (
	"certainstats/internal/store"
	"net/http"
)

func LogoutHandler(sessions store.SessionStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if cookie, err := r.Cookie("session_token"); err == nil {
			sessions.SessionDelete(r.Context(), cookie.Value) //nolint:errcheck
		}
		ClearSessionCookie(w)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"logged_out"}`))
	}
}
