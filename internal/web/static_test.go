package web

import (
	"certainstats/internal/minify"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
)

func TestServeStatic_CustomPathPrefixes(t *testing.T) {
	r := chi.NewRouter()

	// Test case 1: Mounted at subpath /rasta
	r.Route("/rasta", func(sub chi.Router) {
		ServeStatic(sub, "/static", PublicStaticAreas()...)
	})

	// Test case 2: Mounted at subpath /dashboard
	r.Route("/dashboard", func(sub chi.Router) {
		ServeStatic(sub, "/static", PublicStaticAreas()...)
	})

	// Test request 1: /rasta/static/shared/js/chart.js
	req1 := httptest.NewRequest("GET", "/rasta/static/shared/js/chart.js", nil)
	w1 := httptest.NewRecorder()
	r.ServeHTTP(w1, req1)

	if w1.Code != http.StatusOK {
		t.Fatalf("Expected status 200 for /rasta/static/shared/js/chart.js, got %d", w1.Code)
	}
	if !bytesContains(w1.Body.Bytes(), "CertainStatsChart") {
		t.Errorf("Response body for /rasta/static/shared/js/chart.js does not contain 'CertainStatsChart'")
	}

	// Test request 2: /dashboard/static/shared/css/base.css
	req2 := httptest.NewRequest("GET", "/dashboard/static/shared/css/base.css", nil)
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)

	if w2.Code != http.StatusOK {
		t.Fatalf("Expected status 200 for /dashboard/static/shared/css/base.css, got %d", w2.Code)
	}
	if !bytesContains(w2.Body.Bytes(), "--accent-primary") {
		t.Errorf("Response body for /dashboard/static/shared/css/base.css does not contain '--accent-primary'")
	}
	if cc := w2.Header().Get("Cache-Control"); cc != "public, max-age=31536000, immutable" {
		t.Errorf("expected Cache-Control public, max-age=31536000, immutable, got %q", cc)
	}
}

func TestServeStatic_Compression(t *testing.T) {
	r := chi.NewRouter()
	ServeStatic(r, "/static", PublicStaticAreas()...)

	// 1. Plain request
	req1 := httptest.NewRequest("GET", "/static/shared/css/base.css", nil)
	rec1 := httptest.NewRecorder()
	r.ServeHTTP(rec1, req1)
	if rec1.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec1.Code)
	}
	if rec1.Header().Get("Content-Type") != "text/css; charset=utf-8" {
		t.Errorf("expected text/css, got %q", rec1.Header().Get("Content-Type"))
	}
	if rec1.Header().Get("Content-Encoding") != "" {
		t.Errorf("expected no encoding, got %q", rec1.Header().Get("Content-Encoding"))
	}

	// 2. Gzip request
	req2 := httptest.NewRequest("GET", "/static/shared/css/base.css", nil)
	req2.Header.Set("Accept-Encoding", "gzip")
	rec2 := httptest.NewRecorder()
	r.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec2.Code)
	}
	if rec2.Header().Get("Content-Encoding") != "gzip" {
		t.Errorf("expected gzip encoding, got %q", rec2.Header().Get("Content-Encoding"))
	}

	// 3. Zstd request
	req3 := httptest.NewRequest("GET", "/static/shared/css/base.css", nil)
	req3.Header.Set("Accept-Encoding", "zstd, gzip")
	rec3 := httptest.NewRecorder()
	r.ServeHTTP(rec3, req3)
	if rec3.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec3.Code)
	}
	if rec3.Header().Get("Content-Encoding") != "zstd" {
		t.Errorf("expected zstd encoding, got %q", rec3.Header().Get("Content-Encoding"))
	}
}

func TestServeStatic_FingerprintedPath(t *testing.T) {
	r := chi.NewRouter()
	ServeStatic(r, "/static", PublicStaticAreas()...)

	fpCSS := minify.AssetPath("shared/css/base.css")
	if fpCSS == "shared/css/base.css" {
		t.Fatalf("expected fingerprinted CSS path, got %q", fpCSS)
	}

	req := httptest.NewRequest("GET", "/static/"+fpCSS, nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200 for %s, got %d", fpCSS, rec.Code)
	}
	if rec.Header().Get("Content-Type") != "text/css; charset=utf-8" {
		t.Errorf("expected text/css, got %q", rec.Header().Get("Content-Type"))
	}
	if !bytesContains(rec.Body.Bytes(), "--accent-primary") {
		t.Errorf("expected CSS content in fingerprinted response")
	}
}

func TestServeStatic_PublicRouterOnlySeesSharedAndPublic(t *testing.T) {
	r := chi.NewRouter()
	ServeStatic(r, "/static", PublicStaticAreas()...)

	for path, want := range map[string]int{
		"/static/" + minify.AssetPath("shared/js/telemetry.js"):        http.StatusOK,
		"/static/" + minify.AssetPath("public/js/public_dashboard.js"): http.StatusOK,
		"/static/" + minify.AssetPath("admin/js/admin_agents.js"):      http.StatusNotFound,
		"/static/admin/js/admin_agents.js":                             http.StatusNotFound,
		"/static/" + minify.AssetPath("auth/js/setup.js"):              http.StatusNotFound,
		"/static/shared/../admin/js/admin_agents.js":                   http.StatusNotFound,
	} {
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
		if rec.Code != want {
			t.Errorf("GET %s: expected %d, got %d", path, want, rec.Code)
		}
	}
}

func TestServeStatic_PanelGatesAdminOnSession(t *testing.T) {
	signedIn := false
	r := chi.NewRouter()
	ServeStatic(r, "/static", PanelStaticAreas(func(*http.Request) bool { return signedIn })...)
	get := func(path string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest("GET", "/static/"+minify.AssetPath(path), nil))
		return rec
	}

	for _, path := range []string{"shared/css/base.css", "auth/js/setup.js"} {
		if rec := get(path); rec.Code != http.StatusOK {
			t.Errorf("signed out %s: expected 200, got %d", path, rec.Code)
		}
	}
	if rec := get("public/js/public_dashboard.js"); rec.Code != http.StatusNotFound {
		t.Errorf("panel served a public asset: %d", rec.Code)
	}
	rec := get("admin/js/admin_agents.js")
	if rec.Code != http.StatusNotFound || rec.Header().Get("Cache-Control") != "private, no-store" {
		t.Errorf("signed out admin asset: expected 404 private, no-store; got %d %q", rec.Code, rec.Header().Get("Cache-Control"))
	}

	signedIn = true
	rec = get("admin/js/admin_agents.js")
	if rec.Code != http.StatusOK || rec.Header().Get("Cache-Control") != "private, max-age=31536000, immutable" {
		t.Errorf("signed in admin asset: expected 200 private; got %d %q", rec.Code, rec.Header().Get("Cache-Control"))
	}
}

func TestServeStatic_NeedsAnArea(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("ServeStatic without areas must panic rather than serve everything")
		}
	}()
	ServeStatic(chi.NewRouter(), "/static")
}

func bytesContains(b []byte, substr string) bool {
	return len(b) > 0 && (string(b) != "") && (indexOf(string(b), substr) != -1)
}

func indexOf(s, substr string) int {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}
