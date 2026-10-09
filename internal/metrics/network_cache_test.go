package metrics

import (
	"sync"
	"testing"
	"time"
)

func TestNetworkCoverageReplayAndEviction(t *testing.T) {
	c := NewRealtimeCache()
	key := NetworkKey{"owner", "node", "monitor"}
	now := time.Now().UnixMilli()
	start := now - 3600000
	p := NetworkSample{Timestamp: now - 1000, Attempts: 4, Success: 3, Sum: 6000, Min: 1000, Max: 3000}
	c.UpdateNetwork(key, p)
	rev := c.NetworkRevision(key)
	if _, ok := c.GetNetwork(key, start, now); ok {
		t.Fatal("unwarmed cache claimed coverage")
	}
	c.UpdateNetwork(key, p)
	if c.NetworkRevision(key) != rev {
		t.Fatal("replay advanced revision")
	}
	if !c.WarmNetwork(key, []NetworkSample{p}, start, rev) {
		t.Fatal("warm failed")
	}
	out, ok := c.GetNetwork(key, start, now)
	if !ok || len(out) != 1 || out[0] != p {
		t.Fatal(out, ok)
	}
	out[0].Sum = 0
	out, _ = c.GetNetwork(key, start, now)
	if out[0].Sum != p.Sum {
		t.Fatal("reader changed cache")
	}
	p.Timestamp++
	c.UpdateNetwork(key, p)
	if c.WarmNetwork(key, nil, start, rev) {
		t.Fatal("stale snapshot accepted")
	}
	other := NetworkKey{"owner", "node", "empty"}
	rev = c.NetworkRevision(other)
	c.WarmNetwork(other, nil, start, rev)
	if out, ok = c.GetNetwork(other, start, now); !ok || len(out) != 0 {
		t.Fatal("empty coverage lost")
	}
	if _, ok = c.GetNetwork(key, start-24*time.Hour.Milliseconds(), now); ok {
		t.Fatal("old range covered")
	}
	c.network.mu.Lock()
	c.network.limit = networkSize(c.network.entries[other].Value.(*networkWindow))
	c.network.trim()
	c.network.mu.Unlock()
	if _, ok = c.GetNetwork(key, start, now); ok {
		t.Fatal("LRU eviction failed")
	}
	if c.NetworkStatistics()["evictions"].(uint64) == 0 {
		t.Fatal("no eviction recorded")
	}
	c.Delete("node")
	if _, ok = c.GetNetwork(other, start, now); ok {
		t.Fatal("deleted node retained")
	}
}
func TestNetworkConcurrentReadsWrites(t *testing.T) {
	c := NewRealtimeCache()
	key := NetworkKey{"owner", "node", "monitor"}
	now := time.Now().UnixMilli()
	c.WarmNetwork(key, nil, now-3600000, c.NetworkRevision(key))
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				c.UpdateNetwork(key, NetworkSample{Timestamp: now - 1000 + int64(j), Attempts: float64(i + 1)})
				c.GetNetwork(key, now-3600000, time.Now().UnixMilli())
				c.NetworkRevision(key)
				c.NetworkStatistics()
			}
		}(i)
	}
	wg.Wait()
}
