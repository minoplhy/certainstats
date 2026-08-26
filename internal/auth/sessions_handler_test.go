package auth

import (
	ctx "certainstats/internal/context"
	"certainstats/internal/store"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
)

type mockSessionStore struct {
	store.SessionStore
	sessions      []store.Session
	deletedToken  string
	deletedOthers bool
}

func (m *mockSessionStore) SessionListByUser(ctx context.Context, userID string) ([]store.Session, error) {
	return m.sessions, nil
}

func (m *mockSessionStore) SessionDelete(ctx context.Context, token string) error {
	m.deletedToken = token
	return nil
}

func (m *mockSessionStore) SessionDeleteOther(ctx context.Context, userID string, currentToken string) error {
	m.deletedOthers = true
	return nil
}

func TestListSessionsHandler(t *testing.T) {
	tok1 := "tok_11111111111111111111111111111111"
	tok2 := "tok_22222222222222222222222222222222"

	mock := &mockSessionStore{
		sessions: []store.Session{
			{Token: tok1, UserID: "usr_1", IPAddress: "192.168.1.1", UserAgent: "Mozilla/5.0", CreatedAt: time.Now(), LastConnectedAt: time.Now()},
			{Token: tok2, UserID: "usr_1", IPAddress: "192.168.1.2", UserAgent: "Mozilla/5.0", CreatedAt: time.Now(), LastConnectedAt: time.Now()},
		},
	}

	handler := ListSessionsHandler(mock)

	req := httptest.NewRequest("GET", "/api/user/sessions", nil)
	req = req.WithContext(context.WithValue(req.Context(), ctx.UserIDKey, "usr_1"))
	req.AddCookie(&http.Cookie{Name: "session_token", Value: tok1})

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var res []SessionResponse
	if err := json.NewDecoder(rec.Body).Decode(&res); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if len(res) != 2 {
		t.Fatalf("expected 2 sessions, got %d", len(res))
	}

	if !res[0].IsCurrent {
		t.Errorf("expected session 1 to be marked as current")
	}
	if res[1].IsCurrent {
		t.Errorf("expected session 2 NOT to be marked as current")
	}
	if res[0].TokenPrefix != store.HashTokenPrefix(tok1) {
		t.Errorf("expected prefix %s, got %s", store.HashTokenPrefix(tok1), res[0].TokenPrefix)
	}
}

func TestEjectSessionHandler(t *testing.T) {
	tokCurrent := "tok_current_session_123456789012"
	tokOther := "tok_other_session_987654321098"

	mock := &mockSessionStore{
		sessions: []store.Session{
			{Token: tokCurrent, UserID: "usr_1"},
			{Token: tokOther, UserID: "usr_1"},
		},
	}

	r := chi.NewRouter()
	r.Delete("/api/user/session/{prefix}", EjectSessionHandler(mock))

	t.Run("ejects remote session without clearing cookie", func(t *testing.T) {
		mock.deletedToken = ""
		prefixOther := store.HashTokenPrefix(tokOther)

		req := httptest.NewRequest("DELETE", "/api/user/session/"+prefixOther, nil)
		req = req.WithContext(context.WithValue(req.Context(), ctx.UserIDKey, "usr_1"))
		req.AddCookie(&http.Cookie{Name: "session_token", Value: tokCurrent})

		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}
		if mock.deletedToken != tokOther {
			t.Fatalf("expected deleted token to be %s, got %s", tokOther, mock.deletedToken)
		}
	})

	t.Run("ejects current session and clears cookie", func(t *testing.T) {
		mock.deletedToken = ""
		prefixCurrent := store.HashTokenPrefix(tokCurrent)

		req := httptest.NewRequest("DELETE", "/api/user/session/"+prefixCurrent, nil)
		req = req.WithContext(context.WithValue(req.Context(), ctx.UserIDKey, "usr_1"))
		req.AddCookie(&http.Cookie{Name: "session_token", Value: tokCurrent})

		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}
		if mock.deletedToken != tokCurrent {
			t.Fatalf("expected deleted token to be %s, got %s", tokCurrent, mock.deletedToken)
		}

		cookies := rec.Result().Cookies()
		foundCleared := false
		for _, c := range cookies {
			if c.Name == "session_token" && c.MaxAge < 0 {
				foundCleared = true
			}
		}
		if !foundCleared {
			t.Errorf("expected session_token cookie to be cleared")
		}
	})
}

func TestEjectOtherSessionsHandler(t *testing.T) {
	mock := &mockSessionStore{}
	handler := EjectOtherSessionsHandler(mock)

	req := httptest.NewRequest("DELETE", "/api/user/sessions/other", nil)
	req = req.WithContext(context.WithValue(req.Context(), ctx.UserIDKey, "usr_1"))
	req.AddCookie(&http.Cookie{Name: "session_token", Value: "tok_my_session"})

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if !mock.deletedOthers {
		t.Errorf("expected SessionDeleteOther to be called")
	}
}
