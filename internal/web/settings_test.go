package web

import (
	ctx "certainstats/internal/context"
	"certainstats/internal/store"
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
)

type mockSettingsStore struct {
	store.FullStore
	sessions      []store.Session
	deletedToken  string
	deletedOthers bool
	user          *store.User
	updatedPass   string
}

func (m *mockSettingsStore) SessionListByUser(ctx context.Context, userID string) ([]store.Session, error) {
	return m.sessions, nil
}

func (m *mockSettingsStore) SessionDelete(ctx context.Context, token string) error {
	m.deletedToken = token
	return nil
}

func (m *mockSettingsStore) SessionDeleteOther(ctx context.Context, userID string, currentToken string) error {
	m.deletedOthers = true
	return nil
}

func (m *mockSettingsStore) GetByID(ctx context.Context, userID string) (*store.User, error) {
	if m.user != nil {
		return m.user, nil
	}
	return &store.User{}, nil
}

func (m *mockSettingsStore) UpdatePassword(ctx context.Context, userID, newPasswordHash string) error {
	m.updatedPass = newPasswordHash
	return nil
}

func TestSettingsHandler(t *testing.T) {
	renderer, err := NewRenderer()
	if err != nil {
		t.Fatalf("failed to init renderer: %v", err)
	}

	tok := "tok_12345678901234567890123456789012"
	mock := &mockSettingsStore{
		sessions: []store.Session{
			{Token: tok, UserID: "usr_1", IPAddress: "127.0.0.1", UserAgent: "Mozilla/5.0", CreatedAt: time.Now(), LastConnectedAt: time.Now()},
		},
	}

	h := &WebHandler{
		Renderer:  renderer,
		Store:     mock,
		PanelPath: "",
	}

	req := httptest.NewRequest("GET", "/settings", nil)
	req = req.WithContext(context.WithValue(req.Context(), ctx.UserIDKey, "usr_1"))
	req.AddCookie(&http.Cookie{Name: "session_token", Value: tok})

	rec := httptest.NewRecorder()
	h.SettingsHandler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestSessionEjectHandler(t *testing.T) {
	renderer, err := NewRenderer()
	if err != nil {
		t.Fatalf("failed to init renderer: %v", err)
	}

	tokCurrent := "tok_current_session_111111111111"
	tokOther := "tok_other_session_222222222222"

	mock := &mockSettingsStore{
		sessions: []store.Session{
			{Token: tokCurrent, UserID: "usr_1"},
			{Token: tokOther, UserID: "usr_1"},
		},
	}

	h := &WebHandler{
		Renderer:  renderer,
		Store:     mock,
		PanelPath: "",
	}

	t.Run("ejects remote session and redirects to /settings", func(t *testing.T) {
		mock.deletedToken = ""
		prefixOther := store.HashTokenPrefix(tokOther)

		form := url.Values{"token_prefix": {prefixOther}}
		req := httptest.NewRequest("POST", "/settings/sessions/eject", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req = req.WithContext(context.WithValue(req.Context(), ctx.UserIDKey, "usr_1"))
		req.AddCookie(&http.Cookie{Name: "session_token", Value: tokCurrent})

		rec := httptest.NewRecorder()
		h.SessionEjectHandler(rec, req)

		if rec.Code != http.StatusSeeOther {
			t.Fatalf("expected 303 redirect, got %d", rec.Code)
		}
		if loc := rec.Header().Get("Location"); loc != "/settings" {
			t.Fatalf("expected location /settings, got %q", loc)
		}
		if mock.deletedToken != tokOther {
			t.Fatalf("expected deleted token %s, got %s", tokOther, mock.deletedToken)
		}
	})

	t.Run("ejects current session, clears cookie, and redirects to /login", func(t *testing.T) {
		mock.deletedToken = ""
		prefixCurrent := store.HashTokenPrefix(tokCurrent)

		form := url.Values{"token_prefix": {prefixCurrent}}
		req := httptest.NewRequest("POST", "/settings/sessions/eject", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req = req.WithContext(context.WithValue(req.Context(), ctx.UserIDKey, "usr_1"))
		req.AddCookie(&http.Cookie{Name: "session_token", Value: tokCurrent})

		rec := httptest.NewRecorder()
		h.SessionEjectHandler(rec, req)

		if rec.Code != http.StatusSeeOther {
			t.Fatalf("expected 303 redirect, got %d", rec.Code)
		}
		if loc := rec.Header().Get("Location"); loc != "/login" {
			t.Fatalf("expected location /login, got %q", loc)
		}
		if mock.deletedToken != tokCurrent {
			t.Fatalf("expected deleted token %s, got %s", tokCurrent, mock.deletedToken)
		}

		cookies := rec.Result().Cookies()
		foundCleared := false
		for _, c := range cookies {
			if c.Name == "session_token" && c.MaxAge < 0 {
				foundCleared = true
			}
		}
		if !foundCleared {
			t.Errorf("expected session_token cookie to be cleared on self eject")
		}
	})
}

func TestSessionEjectOtherHandler(t *testing.T) {
	renderer, _ := NewRenderer()
	mock := &mockSettingsStore{}
	h := &WebHandler{
		Renderer:  renderer,
		Store:     mock,
		PanelPath: "",
	}

	req := httptest.NewRequest("POST", "/settings/sessions/eject-other", nil)
	req = req.WithContext(context.WithValue(req.Context(), ctx.UserIDKey, "usr_1"))
	req.AddCookie(&http.Cookie{Name: "session_token", Value: "tok_my_sess"})

	rec := httptest.NewRecorder()
	h.SessionEjectOtherHandler(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("expected 303 redirect, got %d", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/settings" {
		t.Fatalf("expected location /settings, got %q", loc)
	}
	if !mock.deletedOthers {
		t.Errorf("expected SessionDeleteOther to be called")
	}
}

func TestPasswordChangeHandler(t *testing.T) {
	renderer, _ := NewRenderer()
	hashed, _ := bcrypt.GenerateFromPassword([]byte("oldPassword123"), bcrypt.DefaultCost)
	mock := &mockSettingsStore{
		user: &store.User{UserID: "usr_1", PasswordHash: string(hashed)},
	}
	h := &WebHandler{
		Renderer:  renderer,
		Store:     mock,
		PanelPath: "",
	}

	t.Run("successfully changes password", func(t *testing.T) {
		form := url.Values{
			"old_password":     {"oldPassword123"},
			"new_password":     {"newPassword456"},
			"confirm_password": {"newPassword456"},
		}
		req := httptest.NewRequest("POST", "/settings/password", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req = req.WithContext(context.WithValue(req.Context(), ctx.UserIDKey, "usr_1"))

		rec := httptest.NewRecorder()
		h.PasswordChangeHandler(rec, req)

		if rec.Code != http.StatusSeeOther {
			t.Fatalf("expected 303, got %d", rec.Code)
		}
		if err := bcrypt.CompareHashAndPassword([]byte(mock.updatedPass), []byte("newPassword456")); err != nil {
			t.Errorf("expected password to match newPassword456")
		}
	})

	t.Run("fails on wrong old password", func(t *testing.T) {
		form := url.Values{
			"old_password":     {"wrongPass"},
			"new_password":     {"newPassword456"},
			"confirm_password": {"newPassword456"},
		}
		req := httptest.NewRequest("POST", "/settings/password", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req = req.WithContext(context.WithValue(req.Context(), ctx.UserIDKey, "usr_1"))

		rec := httptest.NewRecorder()
		h.PasswordChangeHandler(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d", rec.Code)
		}
	})
}
