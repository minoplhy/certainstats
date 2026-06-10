package ws

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	"golang.org/x/net/websocket"
)

func TestAgentBroadcaster(t *testing.T) {
	// Start httptest server with websocket handler
	server := httptest.NewServer(websocket.Handler(func(ws *websocket.Conn) {
		// Keep connection open and read messages
		var msg string
		for {
			if err := websocket.Message.Receive(ws, &msg); err != nil {
				break
			}
		}
	}))
	defer server.Close()

	wsURL := "ws" + server.URL[4:]

	// Create broadcaster
	b := NewAgentBroadcaster()

	t.Run("user subscribe and broadcast", func(t *testing.T) {
		ws, err := websocket.Dial(wsURL, "", server.URL)
		if err != nil {
			t.Fatalf("failed to dial: %v", err)
		}
		defer ws.Close()

		b.SubscribeUser("user1", ws)

		activeUsers := b.GetActiveUserIDs()
		if len(activeUsers) != 1 || activeUsers[0] != "user1" {
			t.Errorf("expected active users to contain user1, got %+v", activeUsers)
		}

		// Try broadcasting
		b.BroadcastToUser("user1", UIUpdate{Type: "test", Data: "hello"})

		b.UnsubscribeUser("user1", ws)
		activeUsers = b.GetActiveUserIDs()
		if len(activeUsers) != 0 {
			t.Errorf("expected no active users, got %+v", activeUsers)
		}
	})

	t.Run("dash subscribe and broadcast", func(t *testing.T) {
		ws, err := websocket.Dial(wsURL, "", server.URL)
		if err != nil {
			t.Fatalf("failed to dial: %v", err)
		}
		defer ws.Close()

		b.SubscribeDash("dash1", ws)

		activeDashes := b.GetActiveDashIDs()
		if len(activeDashes) != 1 || activeDashes[0] != "dash1" {
			t.Errorf("expected active dashes to contain dash1, got %+v", activeDashes)
		}

		b.BroadcastToDash("dash1", UIUpdate{Type: "test", Data: "hello"})

		b.UnsubscribeDash("dash1", ws)
		activeDashes = b.GetActiveDashIDs()
		if len(activeDashes) != 0 {
			t.Errorf("expected no active dashes, got %+v", activeDashes)
		}
	})
}

func TestHubAndManager(t *testing.T) {
	var receivedMsg string
	messageChan := make(chan string, 1)

	// Set up server that reads and echoes back or posts to channel
	server := httptest.NewServer(websocket.Handler(func(ws *websocket.Conn) {
		var data []byte
		if err := websocket.Message.Receive(ws, &data); err == nil {
			messageChan <- string(data)
		}
	}))
	defer server.Close()

	wsURL := "ws" + server.URL[4:]

	t.Run("hub connection and communications", func(t *testing.T) {
		hub := NewHub()
		if hub.IsConnected() {
			t.Error("expected hub to be disconnected initially")
		}

		// Connect
		err := hub.Connect(wsURL, "test-token")
		if err != nil {
			t.Fatalf("failed to connect hub: %v", err)
		}
		defer hub.Close()

		if !hub.IsConnected() {
			t.Error("expected hub to be connected")
		}

		// Test Send
		testVal := map[string]string{"foo": "bar"}
		err = hub.Send(testVal)
		if err != nil {
			t.Fatalf("failed to send: %v", err)
		}

		// Wait for message in channel
		select {
		case receivedMsg = <-messageChan:
			// Unmarshal and verify it was CBOR encoded (or custom)
			// hub.Send uses cbor.Marshal(v)
		case <-time.After(1 * time.Second):
			t.Fatal("timed out waiting for message")
		}

		if len(receivedMsg) == 0 {
			t.Error("expected received message to be non-empty")
		}

		// Test deadlines
		err = hub.SetDeadline(time.Now().Add(5 * time.Second))
		if err != nil {
			t.Errorf("failed to set deadline: %v", err)
		}
		err = hub.SetReadDeadline(time.Now().Add(5 * time.Second))
		if err != nil {
			t.Errorf("failed to set read deadline: %v", err)
		}
		err = hub.SetWriteDeadline(time.Now().Add(5 * time.Second))
		if err != nil {
			t.Errorf("failed to set write deadline: %v", err)
		}
	})

	t.Run("manager tracking", func(t *testing.T) {
		mgr := NewManager()

		hub1 := NewHub()
		hub2 := NewHub()

		mgr.Register("token1", hub1)
		mgr.Register("token2", hub2)

		h, ok := mgr.GetHub("token1")
		if !ok || h != hub1 {
			t.Error("failed to retrieve hub1")
		}

		// Count hubs using Range
		count := 0
		mgr.Range(func(token string, hub *Hub) {
			count++
		})
		if count != 2 {
			t.Errorf("expected 2 hubs, got %d", count)
		}

		// Test registration replaces old hub
		ws, err := websocket.Dial(wsURL, "", server.URL)
		if err != nil {
			t.Fatalf("failed to dial: %v", err)
		}
		defer ws.Close()
		hub1.SetConn(ws)

		hub1Repl := NewHub()
		mgr.Register("token1", hub1Repl) // should close hub1

		if hub1.IsConnected() {
			t.Error("expected old hub1 connection to be closed")
		}

		// Test broadcast
		mgr.Broadcast(map[string]string{"ping": "pong"})

		// Test unregister
		mgr.Unregister("token1")
		_, ok = mgr.GetHub("token1")
		if ok {
			t.Error("expected token1 hub to be unregistered")
		}
	})
}

func TestHubDisconnectedErrors(t *testing.T) {
	hub := NewHub()

	if err := hub.Send("test"); err == nil || err.Error() != "not connected" {
		t.Errorf("expected 'not connected' error, got %v", err)
	}

	var data []byte
	if err := hub.Receive(&data); err == nil || err.Error() != "not connected" {
		t.Errorf("expected 'not connected' error, got %v", err)
	}

	if err := hub.SetDeadline(time.Now()); err == nil || err.Error() != "not connected" {
		t.Errorf("expected 'not connected' error, got %v", err)
	}

	if err := hub.SetReadDeadline(time.Now()); err == nil || err.Error() != "not connected" {
		t.Errorf("expected 'not connected' error, got %v", err)
	}

	if err := hub.SetWriteDeadline(time.Now()); err == nil || err.Error() != "not connected" {
		t.Errorf("expected 'not connected' error, got %v", err)
	}
}

func TestUIUpdateJSON(t *testing.T) {
	update := UIUpdate{
		Type: "status",
		Data: "online",
	}
	bytes, err := json.Marshal(update)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}
	expected := `{"type":"status","data":"online"}`
	if string(bytes) != expected {
		t.Errorf("expected %q, got %q", expected, string(bytes))
	}
}
