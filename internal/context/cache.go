package context

import (
	"certainstats/internal/compress"
	"net/http"
	"strings"
	"sync"
	"time"
)

// NewCacheEntry constructs a CacheEntry with pre-compressed gzip and zstd payloads.
func NewCacheEntry(payload []byte, ttl time.Duration) *CacheEntry {
	return &CacheEntry{
		Payload:     payload,
		GzipPayload: compress.CompressGzip(payload),
		ZstdPayload: compress.CompressZstd(payload),
		ExpiresAt:   time.Now().Add(ttl),
	}
}

// GetCacheEntry checks a sync.Map cache, returns the entry if valid and unexpired, or purges it if expired.
func GetCacheEntry(m *sync.Map, key any) (*CacheEntry, bool) {
	if m == nil {
		return nil, false
	}
	val, ok := m.Load(key)
	if !ok {
		return nil, false
	}
	entry, ok := val.(*CacheEntry)
	if !ok || entry == nil {
		m.Delete(key)
		return nil, false
	}
	if time.Now().After(entry.ExpiresAt) {
		m.Delete(key)
		return nil, false
	}
	return entry, true
}

// Serve writes the CacheEntry to http.ResponseWriter with content negotiation and pre-compressed payloads.
func (e *CacheEntry) Serve(w http.ResponseWriter, r *http.Request, contentType string, status int) {
	if e == nil {
		return
	}

	w.Header().Set("Content-Type", contentType)
	if w.Header().Get("Cache-Control") == "" {
		w.Header().Set("Cache-Control", "public, max-age=60, must-revalidate")
	}

	ae := ""
	if r != nil {
		ae = r.Header.Get("Accept-Encoding")
	}

	if strings.Contains(ae, "zstd") && len(e.ZstdPayload) > 0 {
		w.Header().Set("Content-Encoding", "zstd")
		w.WriteHeader(status)
		_, _ = w.Write(e.ZstdPayload)
		return
	}
	if strings.Contains(ae, "gzip") && len(e.GzipPayload) > 0 {
		w.Header().Set("Content-Encoding", "gzip")
		w.WriteHeader(status)
		_, _ = w.Write(e.GzipPayload)
		return
	}

	w.WriteHeader(status)
	_, _ = w.Write(e.Payload)
}
