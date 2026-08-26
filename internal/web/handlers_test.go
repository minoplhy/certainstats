package web

import (
	c "certainstats/internal/base/alert"
	ctx "certainstats/internal/context"
	"certainstats/internal/dashboard/accessrules"
	"certainstats/internal/store"
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

type mockWebStore struct {
	store.FullStore

	agentProvisionCalled bool
	provisionedType      string
	provisionedNick      string
	beszelSSHSaveCalled  bool
	savedPublicKey       string
	savedPrivateKey      string

	alertCreated store.Alert
	alertUpdated store.Alert
	targetCreated c.AlertTarget
	targetUpdated c.AlertTarget

	dashboardGetCalled bool
	dashboardSlug      string
	publicAgentsCalled bool
}

func (m *mockWebStore) AgentProvision(ctx context.Context, agentID, userID, token, nickname, agentType string) error {
	m.agentProvisionCalled = true
	m.provisionedType = agentType
	m.provisionedNick = nickname
	return nil
}

func (m *mockWebStore) BeszelSSHSave(ctx context.Context, ssh store.BeszelSSH, userID string) error {
	m.beszelSSHSaveCalled = true
	m.savedPublicKey = ssh.PublicKey
	m.savedPrivateKey = ssh.PrivateKey
	return nil
}

func (m *mockWebStore) AlertCreate(ctx context.Context, d store.Alert) error {
	m.alertCreated = d
	return nil
}

func (m *mockWebStore) AlertUpdate(ctx context.Context, d store.Alert, newAgents []string) error {
	m.alertUpdated = d
	return nil
}

func (m *mockWebStore) TargetCreate(ctx context.Context, t c.AlertTarget) error {
	m.targetCreated = t
	return nil
}

func (m *mockWebStore) TargetUpdate(ctx context.Context, t c.AlertTarget) error {
	m.targetUpdated = t
	return nil
}

func (m *mockWebStore) DashboardGetBySlug(ctx context.Context, slug string) (*store.Dashboard, error) {
	m.dashboardGetCalled = true
	m.dashboardSlug = slug
	return &store.Dashboard{
		DashboardID: "dash_1",
		Slug:        slug,
		Title:       "Production Status",
		AccessRules: accessrules.AccessRules{
			"public": accessrules.AccessRule{
				MaxDays: 7,
			},
		},
	}, nil
}

func (m *mockWebStore) DashboardGetPublicAgents(ctx context.Context, slug string, r accessrules.AccessRule) ([]store.PublicAgent, error) {
	m.publicAgentsCalled = true
	return []store.PublicAgent{
		{PublicID: "pub_1", Name: "Node-1"},
	}, nil
}

func TestAgentProvisionHandler_BeszelSSHAndRedirect(t *testing.T) {
	mock := &mockWebStore{}
	renderer, err := NewRenderer()
	if err != nil {
		t.Fatalf("failed to init renderer: %v", err)
	}

	handler := &WebHandler{
		Renderer:  renderer,
		Store:     mock,
		PanelPath: "",
	}

	t.Run("provisions beszel agent and generates SSH key with custom redirect", func(t *testing.T) {
		form := url.Values{
			"nickname":    {"web-prod-01"},
			"agent_type":  {"beszel"},
			"redirect_to": {"/agent/node-123"},
		}
		req := httptest.NewRequest("POST", "/agent/provision", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req = req.WithContext(context.WithValue(req.Context(), ctx.UserIDKey, "user-test"))

		rec := httptest.NewRecorder()
		handler.AgentProvisionHandler(rec, req)

		if rec.Code != http.StatusSeeOther {
			t.Fatalf("expected redirect 303, got %d", rec.Code)
		}
		if loc := rec.Header().Get("Location"); loc != "/agent/node-123" {
			t.Errorf("expected location /agent/node-123, got %q", loc)
		}
		if !mock.agentProvisionCalled {
			t.Errorf("expected AgentProvision to be called")
		}
		if mock.provisionedType != "beszel" {
			t.Errorf("expected agent_type beszel, got %q", mock.provisionedType)
		}
		if !mock.beszelSSHSaveCalled {
			t.Errorf("expected BeszelSSHSave to be called for beszel agent")
		}
		if !strings.HasPrefix(mock.savedPublicKey, "ssh-ed25519 ") {
			t.Errorf("expected valid ed25519 public key, got %q", mock.savedPublicKey)
		}
		if mock.savedPrivateKey == "" {
			t.Errorf("expected non-empty private key")
		}
	})

	t.Run("provisions ltstats agent without SSH and redirects to referer", func(t *testing.T) {
		mock.beszelSSHSaveCalled = false
		form := url.Values{
			"nickname":   {"lt-node-02"},
			"agent_type": {"ltstats"},
		}
		req := httptest.NewRequest("POST", "/agent/provision", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Referer", "/dashboards")
		req = req.WithContext(context.WithValue(req.Context(), ctx.UserIDKey, "user-test"))

		rec := httptest.NewRecorder()
		handler.AgentProvisionHandler(rec, req)

		if rec.Code != http.StatusSeeOther {
			t.Fatalf("expected redirect 303, got %d", rec.Code)
		}
		if loc := rec.Header().Get("Location"); loc != "/dashboards" {
			t.Errorf("expected location /dashboards, got %q", loc)
		}
		if mock.beszelSSHSaveCalled {
			t.Errorf("expected BeszelSSHSave NOT to be called for ltstats agent")
		}
		if mock.provisionedType != "ltstats" {
			t.Errorf("expected agent_type ltstats, got %q", mock.provisionedType)
		}
	})

	t.Run("defaults to beszel when agent_type is empty", func(t *testing.T) {
		mock.beszelSSHSaveCalled = false
		form := url.Values{
			"nickname": {"default-node"},
		}
		req := httptest.NewRequest("POST", "/agent/provision", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req = req.WithContext(context.WithValue(req.Context(), ctx.UserIDKey, "user-test"))

		rec := httptest.NewRecorder()
		handler.AgentProvisionHandler(rec, req)

		if mock.provisionedType != "beszel" {
			t.Errorf("expected default agent_type beszel, got %q", mock.provisionedType)
		}
		if !mock.beszelSSHSaveCalled {
			t.Errorf("expected BeszelSSHSave to be called for default beszel agent")
		}
	})
}

func TestPublicDashboardHandler_ContextCache(t *testing.T) {
	mock := &mockWebStore{}
	renderer, err := NewRenderer()
	if err != nil {
		t.Fatalf("failed to init renderer: %v", err)
	}

	handler := &WebHandler{
		Renderer:   renderer,
		Store:      mock,
		PanelPath:  "",
		PublicPath: "/dashboard",
		StaticPath: "/static",
	}

	slug := "test-status-cache"
	ctx.DashboardHTMLCache.Delete("html_dash_" + slug)

	// First request: Cache Miss -> calls DB and stores into ctx.DashboardHTMLCache
	r := chi.NewRouter()
	r.Get("/dashboard/{slug}", handler.PublicDashboardHandler)

	req1 := httptest.NewRequest("GET", "/dashboard/"+slug, nil)
	rec1 := httptest.NewRecorder()
	r.ServeHTTP(rec1, req1)

	if rec1.Code != http.StatusOK {
		t.Fatalf("expected 200 OK on first request, got %d", rec1.Code)
	}
	if !mock.dashboardGetCalled {
		t.Errorf("expected DB to be queried on first request")
	}

	// Verify cached entry in ctx.DashboardHTMLCache
	val, ok := ctx.DashboardHTMLCache.Load("html_dash_" + slug)
	if !ok {
		t.Fatalf("expected entry in ctx.DashboardHTMLCache")
	}
	entry := val.(*ctx.CacheEntry)
	if len(entry.Payload) == 0 {
		t.Fatalf("expected non-empty cached payload")
	}

	// Second request: Cache Hit -> served from ctx.DashboardHTMLCache without DB queries
	mock.dashboardGetCalled = false
	req2 := httptest.NewRequest("GET", "/dashboard/"+slug, nil)
	rec2 := httptest.NewRecorder()
	r.ServeHTTP(rec2, req2)

	if rec2.Code != http.StatusOK {
		t.Fatalf("expected 200 OK on cached request, got %d", rec2.Code)
	}
	if mock.dashboardGetCalled {
		t.Errorf("expected DB NOT to be queried on second cached request")
	}

	// Third request with Accept-Encoding: gzip -> serves pre-compressed gzip payload
	req3 := httptest.NewRequest("GET", "/dashboard/"+slug, nil)
	req3.Header.Set("Accept-Encoding", "gzip")
	rec3 := httptest.NewRecorder()
	r.ServeHTTP(rec3, req3)

	if rec3.Code != http.StatusOK {
		t.Fatalf("expected 200 OK on gzip request, got %d", rec3.Code)
	}
	if rec3.Header().Get("Content-Encoding") != "gzip" {
		t.Errorf("expected Content-Encoding gzip, got %q", rec3.Header().Get("Content-Encoding"))
	}

	// Invalidation test
	ctx.InvalidateDashboard(slug)
	if _, ok := ctx.DashboardHTMLCache.Load("html_dash_" + slug); ok {
		t.Errorf("expected cache entry to be purged after InvalidateDashboard")
	}
}
