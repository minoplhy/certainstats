package ws

import (
	"context"
	"testing"
	"time"
)

func TestCorrelationAndDisconnect(t *testing.T) {
	h := NewHub()
	id, ch := h.track(context.Background(), SyncNetworkMonitors)
	if _, ok := h.Route(AgentResponse{Id: &id, Data: []byte{0xa0}}); ok {
		t.Fatal("sync response parsed as telemetry")
	}
	if _, ok := <-ch; !ok {
		t.Fatal("lost response")
	}
	if _, ok := h.Route(AgentResponse{Id: &id}); ok {
		t.Fatal("duplicate routed")
	}
	stats, _ := h.track(context.Background(), GetData)
	if _, ok := h.Route(AgentResponse{Id: &stats, SystemData: []byte{0xa0}}); !ok {
		t.Fatal("stats missing")
	}
	_, pending := h.track(context.Background(), CheckFingerprint)
	h.Close()
	select {
	case _, ok := <-pending:
		if ok {
			t.Fatal("not cancelled")
		}
	case <-time.After(time.Second):
		t.Fatal("pending waiter leaked")
	}
}
func TestOldConnectionUnregister(t *testing.T) {
	m := NewManager()
	old, newer := NewHub(), NewHub()
	m.Register("token", old)
	m.Register("token", newer)
	m.UnregisterHub("token", old)
	if h, ok := m.GetHub("token"); !ok || h != newer {
		t.Fatal("new connection removed")
	}
}
