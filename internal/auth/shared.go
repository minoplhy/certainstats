package auth

import (
	"certainstats/internal/store"
	"context"
	"errors"
	"net/http"
)

func ValidatePassword(password, confirm string) error {
	if password == "" {
		return errors.New("password cannot be empty")
	}
	if len([]byte(password)) > 72 {
		return errors.New("password cannot exceed 72 bytes")
	}
	if password != confirm {
		return errors.New("passwords do not match")
	}
	return nil
}
func CreateInitialUser(ctx context.Context, users store.UserStore, id, username, hash string) error {
	return users.CreateInitialUser(ctx, id, username, hash)
}
func PersistPassword(r *http.Request, users store.UserStore, id, oldHash, newHash string) error {
	token := ""
	if c, e := r.Cookie("session_token"); e == nil {
		token = c.Value
	}
	return users.ChangePasswordAndRevoke(r.Context(), id, oldHash, newHash, token)
}
