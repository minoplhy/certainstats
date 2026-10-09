package context

import (
	"container/list"
	"sync"
	"time"
)

// All dynamic response namespaces share one budget, including compressed bytes.
var responseBudget = &cacheBudget{items: make(map[cacheID]*list.Element), limit: 128 << 20}

type cacheID struct {
	namespace *ResponseCache
	key       any
}
type cacheItem struct {
	id    cacheID
	value *CacheEntry
	size  int
}
type cacheBudget struct {
	mu                      sync.Mutex
	items                   map[cacheID]*list.Element
	lru                     list.List
	bytes, limit            int
	hits, misses, evictions uint64
}
type ResponseCache struct{}

func (c *ResponseCache) Load(key any) (any, bool) {
	b := responseBudget
	b.mu.Lock()
	defer b.mu.Unlock()
	e := b.items[cacheID{c, key}]
	if e == nil {
		b.misses++
		return nil, false
	}
	v := e.Value.(cacheItem)
	if !time.Now().Before(v.value.ExpiresAt) {
		b.remove(e)
		return nil, false
	}
	b.hits++
	b.lru.MoveToFront(e)
	return v.value, true
}
func (b *cacheBudget) remove(e *list.Element) {
	v := e.Value.(cacheItem)
	delete(b.items, v.id)
	b.bytes -= v.size
	b.lru.Remove(e)
}
func (c *ResponseCache) Store(key, value any) {
	v, ok := value.(*CacheEntry)
	if !ok || v == nil {
		return
	}
	b := responseBudget
	b.mu.Lock()
	defer b.mu.Unlock()
	id := cacheID{c, key}
	if e := b.items[id]; e != nil {
		b.remove(e)
	}
	for e := b.lru.Back(); e != nil; {
		prev := e.Prev()
		if !time.Now().Before(e.Value.(cacheItem).value.ExpiresAt) {
			b.remove(e)
		}
		e = prev
	}
	size := cap(v.Payload) + cap(v.GzipPayload) + cap(v.ZstdPayload)
	if size > b.limit {
		return
	}
	for b.bytes+size > b.limit {
		b.evictions++
		b.remove(b.lru.Back())
	}
	b.items[id] = b.lru.PushFront(cacheItem{id, v, size})
	b.bytes += size
}
func (c *ResponseCache) Delete(key any) {
	b := responseBudget
	b.mu.Lock()
	defer b.mu.Unlock()
	if e := b.items[cacheID{c, key}]; e != nil {
		b.remove(e)
	}
}
func (c *ResponseCache) Range(fn func(any, any) bool) {
	b := responseBudget
	b.mu.Lock()
	var items []cacheItem
	for id, e := range b.items {
		if id.namespace == c {
			items = append(items, e.Value.(cacheItem))
		}
	}
	b.mu.Unlock()
	for _, v := range items {
		if !fn(v.id.key, v.value) {
			break
		}
	}
}

func CacheStatistics() map[string]any {
	b := responseBudget
	b.mu.Lock()
	defer b.mu.Unlock()
	return map[string]any{"bytes": b.bytes, "entries": len(b.items), "hits": b.hits, "misses": b.misses, "evictions": b.evictions}
}
func ExpireResponses() {
	b := responseBudget
	b.mu.Lock()
	defer b.mu.Unlock()
	for e := b.lru.Back(); e != nil; {
		previous := e.Prev()
		if !time.Now().Before(e.Value.(cacheItem).value.ExpiresAt) {
			b.remove(e)
		}
		e = previous
	}
}
