package metrics

import (
	"container/list"
	"sort"
	"sync"
	"time"
)

const (
	// networkCacheLimit is the memory budget for recent network samples.
	networkCacheLimit = 64 << 20
	// Estimated bytes per window and per sample, for the budget.
	networkWindowOverhead = 256
	networkSampleSize     = 48
)

// NetworkSample keeps all five committed metrics together, in TSDB units.
type NetworkSample struct {
	Timestamp int64
	Attempts  float64
	Success   float64
	Sum       float64
	Min       float64
	Max       float64
}

// NetworkKey identifies one monitor's sample window.
type NetworkKey struct {
	Owner   string
	Agent   string
	Monitor string
}

type networkWindow struct {
	key     NetworkKey
	samples []NetworkSample
	// covered means every sample since coveredFrom is present, so empty
	// periods are known empty rather than missing.
	covered     bool
	coveredFrom int64
	revision    uint64
}

// networkCache is an LRU of recent sample windows within networkCacheLimit.
type networkCache struct {
	mu      sync.Mutex
	entries map[NetworkKey]*list.Element
	lru     list.List
	bytes   int
	limit   int
	// sequence advances on every change; windows record it as their revision.
	sequence uint64

	hits      uint64
	misses    uint64
	evictions uint64
	fallbacks uint64
}

func (c *RealtimeCache) networkInit() {
	if c.network.entries == nil {
		c.network.entries = make(map[NetworkKey]*list.Element)
		c.network.limit = networkCacheLimit
	}
}

func networkSize(w *networkWindow) int {
	keyBytes := len(w.key.Owner) + len(w.key.Agent) + len(w.key.Monitor)
	return networkWindowOverhead + keyBytes + cap(w.samples)*networkSampleSize
}

func (n *networkCache) remove(e *list.Element) {
	w := e.Value.(*networkWindow)
	n.bytes -= networkSize(w)
	delete(n.entries, w.key)
	n.lru.Remove(e)
	n.sequence++
}

func (n *networkCache) trim() {
	for n.bytes > n.limit && n.lru.Len() > 0 {
		n.evictions++
		n.remove(n.lru.Back())
	}
}

// NetworkRevision returns the window's data revision. Missing windows report
// the cache sequence, so any later insert or eviction changes the value.
func (c *RealtimeCache) NetworkRevision(key NetworkKey) uint64 {
	n := &c.network
	n.mu.Lock()
	defer n.mu.Unlock()
	c.networkInit()
	if e := n.entries[key]; e != nil {
		return e.Value.(*networkWindow).revision
	}
	return n.sequence
}

// UpdateNetwork is called only after a successful TSDB commit. Journal timestamps
// are unique sample identities, so replay replaces rather than adds a sample.
func (c *RealtimeCache) UpdateNetwork(key NetworkKey, sample NetworkSample) {
	n := &c.network
	n.mu.Lock()
	defer n.mu.Unlock()
	c.networkInit()

	e := n.entries[key]
	if e == nil {
		e = n.lru.PushFront(&networkWindow{key: key})
		n.entries[key] = e
		n.bytes += networkSize(e.Value.(*networkWindow))
	}
	w := e.Value.(*networkWindow)
	n.bytes -= networkSize(w)

	i := sort.Search(len(w.samples), func(i int) bool { return w.samples[i].Timestamp >= sample.Timestamp })
	if i < len(w.samples) && w.samples[i].Timestamp == sample.Timestamp {
		if w.samples[i] == sample {
			n.bytes += networkSize(w)
			return
		}
		w.samples[i] = sample
	} else {
		w.samples = append(w.samples, NetworkSample{})
		copy(w.samples[i+1:], w.samples[i:])
		w.samples[i] = sample
	}

	n.sequence++
	w.revision = n.sequence
	pruneNetwork(w, time.Now().UnixMilli()-windowTTL.Milliseconds())
	n.bytes += networkSize(w)
	n.lru.MoveToFront(e)
	n.trim()
}

func pruneNetwork(w *networkWindow, cutoff int64) {
	i := sort.Search(len(w.samples), func(i int) bool { return w.samples[i].Timestamp >= cutoff })
	if i == 0 {
		return
	}
	// Only removed observations reduce proven coverage. Empty time before
	// the oldest retained sample remains known empty, including the small
	// delay between resolving a relative 24h request and reading it.
	if w.covered {
		w.coveredFrom = max(w.coveredFrom, w.samples[i-1].Timestamp+1)
	}
	w.samples = append([]NetworkSample(nil), w.samples[i:]...)
}

// GetNetwork returns a copy only for fully covered recent ranges. Once warmed,
// coverage advances with wall time because every subsequent commit updates it.
func (c *RealtimeCache) GetNetwork(key NetworkKey, start, end int64) ([]NetworkSample, bool) {
	n := &c.network
	n.mu.Lock()
	defer n.mu.Unlock()
	c.networkInit()

	e := n.entries[key]
	if e == nil {
		n.misses++
		return nil, false
	}
	now := time.Now().UnixMilli()
	w := e.Value.(*networkWindow)
	n.bytes -= networkSize(w)
	pruneNetwork(w, now-windowTTL.Milliseconds())
	n.bytes += networkSize(w)
	if !w.covered || start < w.coveredFrom || end-start > windowTTL.Milliseconds() || end > now {
		n.misses++
		return nil, false
	}

	n.hits++
	n.lru.MoveToFront(e)
	out := []NetworkSample{}
	for _, sample := range w.samples {
		if sample.Timestamp >= start && sample.Timestamp <= end {
			out = append(out, sample)
		}
	}
	return out, true
}

// WarmNetwork publishes a database snapshot only if no commit or eviction raced
// its read. Empty snapshots also establish coverage.
func (c *RealtimeCache) WarmNetwork(key NetworkKey, samples []NetworkSample, start int64, revision uint64) bool {
	n := &c.network
	n.mu.Lock()
	defer n.mu.Unlock()
	c.networkInit()

	e := n.entries[key]
	current := n.sequence
	if e != nil {
		current = e.Value.(*networkWindow).revision
	}
	if current != revision {
		return false
	}
	if e != nil {
		n.bytes -= networkSize(e.Value.(*networkWindow))
		n.lru.Remove(e)
	}

	w := &networkWindow{
		key:         key,
		samples:     append([]NetworkSample(nil), samples...),
		covered:     true,
		coveredFrom: start,
		revision:    revision,
	}
	pruneNetwork(w, time.Now().UnixMilli()-windowTTL.Milliseconds())
	n.entries[key] = n.lru.PushFront(w)
	n.bytes += networkSize(w)
	n.trim()
	return true
}

// NetworkFallback counts a history read that had to go to TSDB.
func (c *RealtimeCache) NetworkFallback() {
	c.network.mu.Lock()
	c.network.fallbacks++
	c.network.mu.Unlock()
}

// NetworkStatistics reports cache usage for maintenance logs.
func (c *RealtimeCache) NetworkStatistics() map[string]any {
	n := &c.network
	n.mu.Lock()
	defer n.mu.Unlock()
	return map[string]any{
		"bytes":          n.bytes,
		"entries":        len(n.entries),
		"hits":           n.hits,
		"misses":         n.misses,
		"evictions":      n.evictions,
		"tsdb_fallbacks": n.fallbacks,
	}
}

// evictNetwork removes an agent's windows when agentID is set, and prunes
// expired samples from every window when expired is set.
func (c *RealtimeCache) evictNetwork(agentID string, expired bool) {
	n := &c.network
	n.mu.Lock()
	defer n.mu.Unlock()

	cutoff := time.Now().UnixMilli() - windowTTL.Milliseconds()
	for _, e := range n.entries {
		w := e.Value.(*networkWindow)
		if agentID != "" && w.key.Agent == agentID {
			n.remove(e)
			continue
		}
		if !expired {
			continue
		}
		n.bytes -= networkSize(w)
		pruneNetwork(w, cutoff)
		n.bytes += networkSize(w)
		if len(w.samples) == 0 && !w.covered {
			n.evictions++
			n.remove(e)
		}
	}
}

// NetworkKeys returns identities for maintenance ownership reconciliation.
func (c *RealtimeCache) NetworkKeys() []NetworkKey {
	n := &c.network
	n.mu.Lock()
	defer n.mu.Unlock()
	keys := make([]NetworkKey, 0, len(n.entries))
	for key := range n.entries {
		keys = append(keys, key)
	}
	return keys
}
