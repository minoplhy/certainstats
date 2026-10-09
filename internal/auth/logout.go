package auth

import (
	"certainstats/internal/store"
	"net/http"
)

func LogoutHandler(sessions store.SessionStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if cookie, err := r.Cookie("session_token"); err == nil {
			if err := sessions.SessionDelete(r.Context(), cookie.Value); err != nil {
				http.Error(w, "Logout failed; retry", 500)
				return
			}
		}
		ClearSessionCookie(w)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"logged_out"}`))
	}
}
