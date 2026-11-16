package ws

import (
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"

	"golang.org/x/net/websocket"
)

func managerLen(m *Manager) int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.agents)
}

func TestManagerReleasesHubs(t *testing.T) {
	const n = 500
	m := NewManager()
	hubs := make([]*Hub, n)
	for i := range hubs {
		hubs[i] = NewHub()
		hubs[i].AgentID = fmt.Sprintf("agent-%d", i)
		m.Register(fmt.Sprintf("token-%d", i), hubs[i])
	}
	// Reconnects replace the hub in place instead of adding entries.
	for i := range hubs {
		replacement := NewHub()
		replacement.AgentID = hubs[i].AgentID
		m.Register(fmt.Sprintf("token-%d", i), replacement)
		// The old reader's cleanup must not remove the replacement.
		m.UnregisterHub(fmt.Sprintf("token-%d", i), hubs[i])
		hubs[i] = replacement
	}
	if got := managerLen(m); got != n {
		t.Fatalf("manager holds %d hubs, want %d", got, n)
	}
	for i := range hubs {
		if i%2 == 0 {
			m.UnregisterHub(fmt.Sprintf("token-%d", i), hubs[i])
		} else {
			m.CloseAgent(hubs[i].AgentID)
		}
	}
	if got := managerLen(m); got != 0 {
		t.Fatalf("manager holds %d hubs after every disconnect", got)
	}
}

func TestBrowserSlotsReleased(t *testing.T) {
	const keys, perKey = 100, 5
	var releases []func()
	for k := 0; k < keys; k++ {
		for j := 0; j < perKey; j++ {
			release, ok := browserSlot(httptest.NewRecorder(), fmt.Sprintf("viewer-%d", k), perKey)
			if !ok {
				t.Fatalf("slot %d/%d refused", k, j)
			}
			releases = append(releases, release)
		}
	}
	if _, ok := browserSlot(httptest.NewRecorder(), "viewer-0", perKey); ok {
		t.Fatal("per-key limit not enforced")
	}
	for _, release := range releases {
		release()
	}
	connectionMu.Lock()
	counts := len(connectionCounts)
	connectionMu.Unlock()
	if counts != 0 || len(browserConnections) != 0 {
		t.Fatalf("after release: %d keys tracked, %d global slots held", counts, len(browserConnections))
	}
}

// dialPair returns the server side of a live websocket connection; the
// server handler blocks until the test ends.
func dialPair(t *testing.T) *websocket.Conn {
	t.Helper()
	conns := make(chan *websocket.Conn, 1)
	release := make(chan struct{})
	server := httptest.NewServer(websocket.Handler(func(c *websocket.Conn) {
		conns <- c
		<-release
	}))
	client, err := websocket.Dial("ws"+strings.TrimPrefix(server.URL, "http"), "", server.URL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		client.Close()
		close(release)
		server.Close()
	})
	return <-conns
}

func writerCount(b *AgentBroadcaster) int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.writers)
}

// CloseDash drops the dashboard's set before the handler unsubscribes; the
// writer goroutine must still stop.
func TestBroadcasterWritersReleasedAfterCloseDash(t *testing.T) {
	b := NewAgentBroadcaster(false)
	const n = 20
	var conns []*websocket.Conn
	for i := 0; i < n; i++ {
		c := dialPair(t)
		conns = append(conns, c)
		if i%2 == 0 {
			b.SubscribeDash("dash", c)
		} else {
			b.SubscribeUser("user", c)
		}
	}
	if got := writerCount(b); got != n {
		t.Fatalf("%d writers, want %d", got, n)
	}
	b.CloseDash("dash")
	for i, c := range conns {
		if i%2 == 0 {
			b.UnsubscribeDash("dash", c)
		} else {
			b.UnsubscribeUser("user", c)
		}
	}
	if got := writerCount(b); got != 0 {
		t.Fatalf("%d writers left after every connection unsubscribed", got)
	}
}
