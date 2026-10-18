package auth

import (
	"certainstats/internal/security"
	"certainstats/internal/store"
	"context"
	"database/sql"
	"errors"
	"net/http"
	"time"

	"golang.org/x/crypto/bcrypt"
)

var ErrUnauthenticated = errors.New("unauthenticated")

// dummyHash keeps an unknown username as slow as a wrong password.
var dummyHash, _ = bcrypt.GenerateFromPassword([]byte("certainstats-dummy-password"), bcrypt.DefaultCost)

// CheckCredentials returns the user only when the username and password match.
func CheckCredentials(ctx context.Context, users store.UserStore, username, password string) (*store.User, error) {
	user, err := users.GetByUsername(ctx, username)
	if errors.Is(err, sql.ErrNoRows) {
		bcrypt.CompareHashAndPassword(dummyHash, []byte(password))
		return nil, ErrUnauthenticated
	}
	if err != nil {
		return nil, err
	}
	if bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)) != nil {
		return nil, ErrUnauthenticated
	}
	return user, nil
}

func Authenticate(r *http.Request, sessions store.SessionStore) (*store.Session, error) {
	cookie, err := r.Cookie("session_token")
	if err != nil {
		return nil, ErrUnauthenticated
	}
	sess, err := sessions.SessionGet(r.Context(), cookie.Value)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrUnauthenticated
	}
	if err != nil {
		return nil, err
	}
	if !time.Now().Before(sess.ExpiresAt) || time.Since(sess.LastConnectedAt) > 15*24*time.Hour {
		if err := sessions.SessionDelete(r.Context(), cookie.Value); err != nil {
			return nil, err
		}
		return nil, ErrUnauthenticated
	}
	if time.Since(sess.LastConnectedAt) > 5*time.Minute {
		if err := sessions.SessionUpdateActivity(r.Context(), sess.Token, time.Now()); err != nil {
			return nil, err
		}
	}
	return sess, nil
}
func CreateBrowserSession(w http.ResponseWriter, r *http.Request, sessions store.SessionStore, userID string, remember bool) error {
	now := time.Now()
	duration := 24 * time.Hour
	if remember {
		duration = 30 * 24 * time.Hour
	}
	expires := now.Add(duration)
	token := GenerateSessionToken()
	if err := sessions.SessionCreate(r.Context(), store.Session{Token: token, UserID: userID, ExpiresAt: expires, CreatedAt: now, LastConnectedAt: now, IPAddress: security.ClientIP(r), UserAgent: r.UserAgent()}); err != nil {
		return err
	}
	SetSessionCookie(w, r, token, expires)
	return nil
}
