package auth

import (
	"bytes"
	"context"
	CSContext "certainstats/internal/context"
	"certainstats/internal/store"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

type mockUserStore struct {
	store.UserStore
	GetUserFunc     func(ctx context.Context, userID string) (*store.User, error)
	UpdatePassFunc  func(ctx context.Context, userID string, passwordHash string) error
}

func (m *mockUserStore) GetByID(ctx context.Context, userID string) (*store.User, error) {
	if m.GetUserFunc != nil {
		return m.GetUserFunc(ctx, userID)
	}
	return nil, errors.New("GetUserFunc not implemented")
}

func (m *mockUserStore) UpdatePassword(ctx context.Context, userID string, passwordHash string) error {
	if m.UpdatePassFunc != nil {
		return m.UpdatePassFunc(ctx, userID, passwordHash)
	}
	return errors.New("UpdatePassFunc not implemented")
}

func TestChangePasswordHandler(t *testing.T) {
	oldPassword := "oldsecret123"
	newPassword := "newsecret456"

	// Pre-generate a bcrypt hash for the old password
	oldHashBytes, err := bcrypt.GenerateFromPassword([]byte(oldPassword), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("failed to generate bcrypt hash: %v", err)
	}
	oldHash := string(oldHashBytes)

	t.Run("successful password change", func(t *testing.T) {
		var updatedHash string
		var updatedUserID string

		mockStore := &mockUserStore{
			GetUserFunc: func(ctx context.Context, userID string) (*store.User, error) {
				return &store.User{
					UserID:       "user-123",
					Username:     "testuser",
					PasswordHash: oldHash,
				}, nil
			},
			UpdatePassFunc: func(ctx context.Context, userID string, passwordHash string) error {
				updatedUserID = userID
				updatedHash = passwordHash
				return nil
			},
		}

		handler := ChangePasswordHandler(mockStore)

		payload := map[string]string{
			"old_password": oldPassword,
			"new_password": newPassword,
		}
		body, _ := json.Marshal(payload)

		req := httptest.NewRequest("POST", "/api/change-password", bytes.NewBuffer(body))
		// Inject userID into context
		ctx := context.WithValue(req.Context(), CSContext.UserIDKey, "user-123")
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("expected status 200, got %d. Body: %s", rec.Code, rec.Body.String())
		}

		if updatedUserID != "user-123" {
			t.Errorf("expected updated user ID to be %q, got %q", "user-123", updatedUserID)
		}

		// Verify the updated hash matches the new password
		if err := bcrypt.CompareHashAndPassword([]byte(updatedHash), []byte(newPassword)); err != nil {
			t.Errorf("updated password hash does not match new password: %v", err)
		}
	})

	t.Run("unauthorized - no user ID in context", func(t *testing.T) {
		mockStore := &mockUserStore{}
		handler := ChangePasswordHandler(mockStore)

		req := httptest.NewRequest("POST", "/api/change-password", bytes.NewBufferString("{}"))
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Errorf("expected status 401, got %d", rec.Code)
		}
	})

	t.Run("invalid request body JSON", func(t *testing.T) {
		mockStore := &mockUserStore{}
		handler := ChangePasswordHandler(mockStore)

		req := httptest.NewRequest("POST", "/api/change-password", bytes.NewBufferString("{invalid}"))
		ctx := context.WithValue(req.Context(), CSContext.UserIDKey, "user-123")
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Errorf("expected status 400, got %d", rec.Code)
		}
	})

	t.Run("empty new password", func(t *testing.T) {
		mockStore := &mockUserStore{}
		handler := ChangePasswordHandler(mockStore)

		payload := map[string]string{
			"old_password": oldPassword,
			"new_password": "",
		}
		body, _ := json.Marshal(payload)

		req := httptest.NewRequest("POST", "/api/change-password", bytes.NewBuffer(body))
		ctx := context.WithValue(req.Context(), CSContext.UserIDKey, "user-123")
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Errorf("expected status 400, got %d", rec.Code)
		}
	})

	t.Run("database error on user lookup", func(t *testing.T) {
		mockStore := &mockUserStore{
			GetUserFunc: func(ctx context.Context, userID string) (*store.User, error) {
				return nil, errors.New("db error")
			},
		}
		handler := ChangePasswordHandler(mockStore)

		payload := map[string]string{
			"old_password": oldPassword,
			"new_password": newPassword,
		}
		body, _ := json.Marshal(payload)

		req := httptest.NewRequest("POST", "/api/change-password", bytes.NewBuffer(body))
		ctx := context.WithValue(req.Context(), CSContext.UserIDKey, "user-123")
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusInternalServerError {
			t.Errorf("expected status 500, got %d", rec.Code)
		}
	})

	t.Run("invalid old password", func(t *testing.T) {
		mockStore := &mockUserStore{
			GetUserFunc: func(ctx context.Context, userID string) (*store.User, error) {
				return &store.User{
					UserID:       "user-123",
					Username:     "testuser",
					PasswordHash: oldHash,
				}, nil
			},
		}
		handler := ChangePasswordHandler(mockStore)

		payload := map[string]string{
			"old_password": "wrong_password",
			"new_password": newPassword,
		}
		body, _ := json.Marshal(payload)

		req := httptest.NewRequest("POST", "/api/change-password", bytes.NewBuffer(body))
		ctx := context.WithValue(req.Context(), CSContext.UserIDKey, "user-123")
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Errorf("expected status 401, got %d", rec.Code)
		}
	})

	t.Run("new password same as old password", func(t *testing.T) {
		mockStore := &mockUserStore{
			GetUserFunc: func(ctx context.Context, userID string) (*store.User, error) {
				return &store.User{
					UserID:       "user-123",
					Username:     "testuser",
					PasswordHash: oldHash,
				}, nil
			},
		}
		handler := ChangePasswordHandler(mockStore)

		payload := map[string]string{
			"old_password": oldPassword,
			"new_password": oldPassword,
		}
		body, _ := json.Marshal(payload)

		req := httptest.NewRequest("POST", "/api/change-password", bytes.NewBuffer(body))
		ctx := context.WithValue(req.Context(), CSContext.UserIDKey, "user-123")
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Errorf("expected status 400, got %d", rec.Code)
		}
	})

	t.Run("failed database update", func(t *testing.T) {
		mockStore := &mockUserStore{
			GetUserFunc: func(ctx context.Context, userID string) (*store.User, error) {
				return &store.User{
					UserID:       "user-123",
					Username:     "testuser",
					PasswordHash: oldHash,
				}, nil
			},
			UpdatePassFunc: func(ctx context.Context, userID string, passwordHash string) error {
				return errors.New("db write error")
			},
		}
		handler := ChangePasswordHandler(mockStore)

		payload := map[string]string{
			"old_password": oldPassword,
			"new_password": newPassword,
		}
		body, _ := json.Marshal(payload)

		req := httptest.NewRequest("POST", "/api/change-password", bytes.NewBuffer(body))
		ctx := context.WithValue(req.Context(), CSContext.UserIDKey, "user-123")
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusInternalServerError {
			t.Errorf("expected status 500, got %d", rec.Code)
		}
	})
}
