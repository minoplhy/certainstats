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
		ServeStatic(sub, "/static")
	})

	// Test case 2: Mounted at subpath /dashboard
	r.Route("/dashboard", func(sub chi.Router) {
		ServeStatic(sub, "/static")
	})

	// Test request 1: /rasta/static/js/chart.js
	req1 := httptest.NewRequest("GET", "/rasta/static/js/chart.js", nil)
	w1 := httptest.NewRecorder()
	r.ServeHTTP(w1, req1)

	if w1.Code != http.StatusOK {
		t.Fatalf("Expected status 200 for /rasta/static/js/chart.js, got %d", w1.Code)
	}
	if !bytesContains(w1.Body.Bytes(), "CertainStatsChart") {
		t.Errorf("Response body for /rasta/static/js/chart.js does not contain 'CertainStatsChart'")
	}

	// Test request 2: /dashboard/static/css/styles.css
	req2 := httptest.NewRequest("GET", "/dashboard/static/css/styles.css", nil)
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)

	if w2.Code != http.StatusOK {
		t.Fatalf("Expected status 200 for /dashboard/static/css/styles.css, got %d", w2.Code)
	}
	if !bytesContains(w2.Body.Bytes(), "--accent-primary") {
		t.Errorf("Response body for /dashboard/static/css/styles.css does not contain '--accent-primary'")
	}
	if cc := w2.Header().Get("Cache-Control"); cc != "public, max-age=31536000, immutable" {
		t.Errorf("expected Cache-Control public, max-age=31536000, immutable, got %q", cc)
	}
}

func TestServeStatic_Compression(t *testing.T) {
	r := chi.NewRouter()
	ServeStatic(r, "/static")

	// 1. Plain request
	req1 := httptest.NewRequest("GET", "/static/css/styles.css", nil)
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
	req2 := httptest.NewRequest("GET", "/static/css/styles.css", nil)
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
	req3 := httptest.NewRequest("GET", "/static/css/styles.css", nil)
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
	ServeStatic(r, "/static")

	fpCSS := minify.AssetPath("css/styles.css")
	if fpCSS == "css/styles.css" {
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
