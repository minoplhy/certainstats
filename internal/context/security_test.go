package context

import (
	"container/list"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestSharedResponseBudgetAndEviction(t *testing.T) {
	old := responseBudget
	responseBudget = &cacheBudget{items: make(map[cacheID]*list.Element), limit: 150}
	defer func() { responseBudget = old }()
	a, b := &ResponseCache{}, &ResponseCache{}
	entry := func() *CacheEntry {
		return &CacheEntry{Payload: make([]byte, 60), ExpiresAt: time.Now().Add(time.Minute)}
	}
	a.Store("one", entry())
	b.Store("two", entry())
	a.Load("one")
	b.Store("three", entry())
	if _, ok := b.Load("two"); ok {
		t.Fatal("least recently used entry survived")
	}
	if _, ok := a.Load("one"); !ok {
		t.Fatal("recently used entry evicted")
	}
	if responseBudget.bytes > responseBudget.limit {
		t.Fatal("budget exceeded")
	}
}
func TestCacheEncodingAndNoStore(t *testing.T) {
	entry := NewCacheEntry([]byte("payload"), time.Minute)
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Accept-Encoding", "gzip;q=0, zstd;q=0")
	w := httptest.NewRecorder()
	entry.Serve(w, req, "application/json", 200)
	if w.Header().Get("Content-Encoding") != "" || w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("Vary") != "Accept-Encoding" {
		t.Fatalf("wrong headers: %v", w.Header())
	}
}
func TestCoalescedFollowerUsesPublishedCache(t *testing.T) {
	key := t.Name()
	request := httptest.NewRequest("GET", "/", nil)
	finish, leader := BeginBuild(httptest.NewRecorder(), request, key, nil)
	if !leader {
		t.Fatal("no leader")
	}
	done := make(chan struct{})
	var followerStatus int
	go func() {
		w := httptest.NewRecorder()
		_, leader := BeginBuild(w, request, key, func(w http.ResponseWriter, r *http.Request) {
			if entry, ok := GetCacheEntry(&MetricsCache, key); ok {
				entry.Serve(w, r, "application/json", 200)
			}
		})
		if leader {
			followerStatus = 999
		} else {
			followerStatus = w.Code
		}
		close(done)
	}()
	time.Sleep(10 * time.Millisecond)
	MetricsCache.Store(key, NewCacheEntry([]byte(`{"ok":true}`), time.Minute))
	finish()
	<-done
	MetricsCache.Delete(key)
	if followerStatus != 200 {
		t.Fatalf("follower status %d", followerStatus)
	}
}
