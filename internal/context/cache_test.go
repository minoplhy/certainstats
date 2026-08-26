package context

import (
	base "certainstats/internal/base"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestCacheEntry_GetAndServe(t *testing.T) {
	var cache sync.Map

	payload := []byte(strings.Repeat(`{"agent_id":"node-1","metric":"cpu_usage","value":42.5},`, 10))
	entry := NewCacheEntry(payload, 50*time.Millisecond)
	cache.Store("test_key", entry)

	t.Run("hit valid unexpired cache", func(t *testing.T) {
		got, hit := GetCacheEntry(&cache, "test_key")
		if !hit || got == nil {
			t.Fatalf("expected cache hit")
		}
		if string(got.Payload) != string(payload) {
			t.Errorf("expected payload %s, got %s", payload, got.Payload)
		}
	})

	t.Run("serves plain payload when no compression requested", func(t *testing.T) {
		got, _ := GetCacheEntry(&cache, "test_key")
		req := httptest.NewRequest("GET", "/test", nil)
		rec := httptest.NewRecorder()

		got.Serve(rec, req, "application/json", http.StatusOK)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}
		if rec.Header().Get("Content-Type") != "application/json" {
			t.Errorf("expected Content-Type application/json, got %q", rec.Header().Get("Content-Type"))
		}
		if rec.Header().Get("Content-Encoding") != "" {
			t.Errorf("expected empty Content-Encoding, got %q", rec.Header().Get("Content-Encoding"))
		}
		if strings.TrimSpace(rec.Body.String()) != string(payload) {
			t.Errorf("expected body %s, got %s", payload, rec.Body.String())
		}
	})

	t.Run("serves gzip when requested", func(t *testing.T) {
		got, _ := GetCacheEntry(&cache, "test_key")
		req := httptest.NewRequest("GET", "/test", nil)
		req.Header.Set("Accept-Encoding", "gzip, deflate")
		rec := httptest.NewRecorder()

		got.Serve(rec, req, "application/json", http.StatusOK)

		if rec.Header().Get("Content-Encoding") != "gzip" {
			t.Errorf("expected gzip encoding, got %q", rec.Header().Get("Content-Encoding"))
		}
	})

	t.Run("serves zstd when requested", func(t *testing.T) {
		got, _ := GetCacheEntry(&cache, "test_key")
		req := httptest.NewRequest("GET", "/test", nil)
		req.Header.Set("Accept-Encoding", "zstd, gzip")
		rec := httptest.NewRecorder()

		got.Serve(rec, req, "application/json", http.StatusOK)

		if rec.Header().Get("Content-Encoding") != "zstd" {
			t.Errorf("expected zstd encoding, got %q", rec.Header().Get("Content-Encoding"))
		}
	})

	t.Run("cleans up expired cache entry", func(t *testing.T) {
		time.Sleep(60 * time.Millisecond)
		got, hit := GetCacheEntry(&cache, "test_key")
		if hit || got != nil {
			t.Fatalf("expected cache miss after expiration")
		}
		if _, exists := cache.Load("test_key"); exists {
			t.Errorf("expected expired key to be deleted from map")
		}
	})
}

func TestInvalidateDashboard(t *testing.T) {
	slug := "demo-dashboard"
	DashboardCache.Store(slug, NewCacheEntry([]byte(`{}`), DefaultCacheTTL))
	DashboardHTMLCache.Store("html_dash_"+slug, NewCacheEntry([]byte(`<html></html>`), DefaultCacheTTL))
	MetricsCache.Store("pub_"+slug+"_agent_cpu", NewCacheEntry([]byte(`[]`), DefaultCacheTTL))

	InvalidateDashboard(slug)

	if _, ok := DashboardCache.Load(slug); ok {
		t.Errorf("expected DashboardCache entry to be deleted")
	}
	if _, ok := DashboardHTMLCache.Load("html_dash_" + slug); ok {
		t.Errorf("expected DashboardHTMLCache entry to be deleted")
	}
	if _, ok := MetricsCache.Load("pub_" + slug + "_agent_cpu"); ok {
		t.Errorf("expected MetricsCache entry to be deleted")
	}
}

func TestInvalidateAgent(t *testing.T) {
	agentID := "agent-12345"
	PublicAgentCache.Store("dash_pub1", &PublicAgentCacheEntry{
		Agent: base.FindAgentByPublicID{RealAgentID: agentID},
	})
	MetricsCache.Store("priv_user1_"+agentID+"_cpu_1", NewCacheEntry([]byte(`[]`), DefaultCacheTTL))

	InvalidateAgent(agentID)

	if _, ok := PublicAgentCache.Load("dash_pub1"); ok {
		t.Errorf("expected PublicAgentCache entry to be deleted")
	}
	if _, ok := MetricsCache.Load("priv_user1_" + agentID + "_cpu_1"); ok {
		t.Errorf("expected MetricsCache entry to be deleted")
	}
}
