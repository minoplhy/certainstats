package auth

import (
	ctx "certainstats/internal/context"
	apiresponse "certainstats/internal/response"
	"certainstats/internal/store"
	"crypto/sha256"
	"encoding/hex"
	"net/http"

	"github.com/go-chi/chi/v5"
)

type SessionResponse struct {
	TokenPrefix     string `json:"token_prefix"`
	IsCurrent       bool   `json:"is_current"`
	IPAddress       string `json:"ip_address"`
	UserAgent       string `json:"user_agent"`
	CreatedAt       string `json:"created_at"`
	LastConnectedAt string `json:"last_connected_at"`
}

// hashTokenPrefix returns a unique, safe prefix/hash of the session token.
// This allows the frontend to reference and revoke sessions without revealing the raw token.
func hashTokenPrefix(token string) string {
	h := sha256.New()
	h.Write([]byte(token))
	hashStr := hex.EncodeToString(h.Sum(nil))
	if len(hashStr) > 8 {
		return hashStr[:8]
	}
	return hashStr
}

func ListSessionsHandler(sessions store.SessionStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := r.Context().Value(ctx.UserIDKey).(string)

		cookie, err := r.Cookie("session_token")
		currentToken := ""
		if err == nil {
			currentToken = cookie.Value
		}

		list, err := sessions.SessionListByUser(r.Context(), userID)
		if err != nil {
			apiresponse.Error(w, http.StatusInternalServerError, "Failed to retrieve sessions")
			return
		}

		res := make([]SessionResponse, 0, len(list))
		for _, s := range list {
			prefix := hashTokenPrefix(s.Token)
			res = append(res, SessionResponse{
				TokenPrefix:     prefix,
				IsCurrent:       s.Token == currentToken,
				IPAddress:       s.IPAddress,
				UserAgent:       s.UserAgent,
				CreatedAt:       s.CreatedAt.Format("2006-01-02T15:04:05Z"),
				LastConnectedAt: s.LastConnectedAt.Format("2006-01-02T15:04:05Z"),
			})
		}

		apiresponse.JSON(w, http.StatusOK, res)
	}
}

func EjectSessionHandler(sessions store.SessionStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := r.Context().Value(ctx.UserIDKey).(string)
		prefix := chi.URLParam(r, "prefix")
		if prefix == "" {
			apiresponse.Error(w, http.StatusBadRequest, "Missing session prefix")
			return
		}

		// Find the target session by checking the prefixes of all user's sessions
		list, err := sessions.SessionListByUser(r.Context(), userID)
		if err != nil {
			apiresponse.Error(w, http.StatusInternalServerError, "Database error")
			return
		}

		var targetToken string
		cookie, err := r.Cookie("session_token")
		currentToken := ""
		if err == nil {
			currentToken = cookie.Value
		}

		for _, s := range list {
			if hashTokenPrefix(s.Token) == prefix {
				targetToken = s.Token
				break
			}
		}

		if targetToken == "" {
			apiresponse.Error(w, http.StatusNotFound, "Session not found")
			return
		}

		// If the user ejects their own current session, clear their cookie too
		if targetToken == currentToken {
			ClearSessionCookie(w)
		}

		err = sessions.SessionDelete(r.Context(), targetToken)
		if err != nil {
			apiresponse.Error(w, http.StatusInternalServerError, "Failed to delete session")
			return
		}

		apiresponse.JSON(w, http.StatusOK, map[string]string{"status": "success", "message": "Session ejected"})
	}
}

func EjectOtherSessionsHandler(sessions store.SessionStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := r.Context().Value(ctx.UserIDKey).(string)

		cookie, err := r.Cookie("session_token")
		if err != nil {
			apiresponse.Error(w, http.StatusUnauthorized, "Unauthorized")
			return
		}

		err = sessions.SessionDeleteOther(r.Context(), userID, cookie.Value)
		if err != nil {
			apiresponse.Error(w, http.StatusInternalServerError, "Failed to revoke other sessions")
			return
		}

		apiresponse.JSON(w, http.StatusOK, map[string]string{"status": "success", "message": "Other sessions ejected"})
	}
}
