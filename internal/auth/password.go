package auth

import (
	baseresponse "certainstats/internal/base/response"
	CSContext "certainstats/internal/context"
	apiresponse "certainstats/internal/response"

	"certainstats/internal/store"
	"encoding/json"
	"net/http"

	"golang.org/x/crypto/bcrypt"
)

func ChangePasswordHandler(users store.UserStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID, ok := r.Context().Value(CSContext.UserIDKey).(string)
		if !ok || userID == "" {
			apiresponse.Error(w, http.StatusUnauthorized, "Unauthorized")
			return
		}

		var req baseresponse.ChangePasswordRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			apiresponse.Error(w, http.StatusBadRequest, "Invalid request body")
			return
		}

		if req.NewPassword == "" {
			apiresponse.Error(w, http.StatusBadRequest, "New password cannot be empty")
			return
		}

		user, err := users.GetByID(r.Context(), userID)
		if err != nil {
			apiresponse.Error(w, http.StatusInternalServerError, "Database error")
			return
		}

		if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.OldPassword)); err != nil {
			apiresponse.Error(w, http.StatusUnauthorized, "Invalid old password")
			return
		}

		if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.NewPassword)); err == nil {
			apiresponse.Error(w, http.StatusBadRequest, "New password cannot be the same as old password")
			return
		}

		newHash, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcrypt.DefaultCost)
		if err != nil {
			apiresponse.Error(w, http.StatusInternalServerError, "Failed to hash password")
			return
		}

		if err := users.UpdatePassword(r.Context(), userID, string(newHash)); err != nil {
			apiresponse.Error(w, http.StatusInternalServerError, "Failed to update password")
			return
		}

		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"success"}`))
	}
}
