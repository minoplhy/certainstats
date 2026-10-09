package ws

import (
	csctx "certainstats/internal/context"
	nm "certainstats/internal/networkmonitor"
	"certainstats/internal/store"
	"certainstats/internal/ws/browserpb"
	"context"
	"encoding/json"
	"golang.org/x/net/websocket"
	"google.golang.org/protobuf/proto"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

type networkSessionStore struct {
	store.SessionStore
}

func (s *networkSessionStore) SessionGet(context.Context, string) (*store.Session, error) {
	expires := time.Now().Add(time.Hour)
	return &store.Session{UserID: "owner", ExpiresAt: expires}, nil
}

func TestReadOnlyUICombinedNetworkDelivery(t *testing.T) {
	for _, binary := range []bool{false, true} {
		t.Run(map[bool]string{false: "json", true: "protobuf"}[binary], func(t *testing.T) {
			b := NewAgentBroadcaster(binary)
			sessions := &networkSessionStore{}
			handler := UIWebSocketHandler(b, sessions)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				handler(w, r.WithContext(context.WithValue(r.Context(), csctx.UserIDKey, "owner")))
			}))
			defer server.Close()
			config, _ := websocket.NewConfig("ws"+server.URL[4:]+"/api/ws", server.URL)
			config.Header.Set("Cookie", "session_token=token")
			if binary {
				config.Protocol = []string{BrowserProtocol}
			}
			conn, err := websocket.DialConfig(config)
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			deadline := time.Now().Add(time.Second)
			for len(b.GetActiveUserIDs()) == 0 && time.Now().Before(deadline) {
				time.Sleep(time.Millisecond)
			}
			if len(b.GetActiveUserIDs()) != 1 {
				t.Fatal("owner connection not registered")
			}
			// Inbound application data cannot alter server-selected monitor delivery.
			if err := websocket.Message.Send(conn, `{"type":"network_subscribe","monitor_ids":[]}`); err != nil {
				t.Fatal(err)
			}
			pulse := &browserpb.TelemetryEnvelope{Pulse: &browserpb.TelemetryPulse{Agents: map[string]*browserpb.Snapshot{"node": {Available: proto.Bool(true)}}, Network: BrowserNetwork([]nm.Monitor{{Config: nm.Config{ID: "monitor"}, AgentID: "node", Enabled: true, State: "waiting"}})}}
			b.BroadcastToUser("owner", pulse)
			conn.SetReadDeadline(time.Now().Add(time.Second))
			var raw []byte
			if err := websocket.Message.Receive(conn, &raw); err != nil {
				t.Fatal(err)
			}
			if binary {
				var decoded browserpb.TelemetryEnvelope
				if err := proto.Unmarshal(raw, &decoded); err != nil {
					t.Fatal(err)
				}
				if decoded.GetPulse().GetNetwork().GetMonitors()["monitor"] == nil || decoded.GetPulse().GetAgents()["node"] == nil {
					t.Fatal("combined pulse lost a stream")
				}
			} else {
				var decoded struct {
					Data    map[string]any
					Network map[string]any
				}
				if err := json.Unmarshal(raw, &decoded); err != nil {
					t.Fatal(err)
				}
				if decoded.Data["node"] == nil || decoded.Network["monitor"] == nil {
					t.Fatal("combined pulse lost a stream")
				}
			}
			b.CloseSession("token")
			if err := websocket.Message.Receive(conn, &raw); err == nil {
				t.Fatal("revoked session remains connected")
			}
		})
	}
}

func TestPublicProtobufCannotCarryNetworkPulse(t *testing.T) {
	b := NewAgentBroadcaster(true)
	release := make(chan struct{})
	envelope := &browserpb.TelemetryEnvelope{Pulse: &browserpb.TelemetryPulse{Agents: map[string]*browserpb.Snapshot{"public-node": {Available: proto.Bool(true)}}, Network: BrowserNetwork([]nm.Monitor{{Config: nm.Config{ID: "private-monitor"}, AgentID: "private-node"}})}}
	server := httptest.NewServer(websocket.Server{Handshake: func(*websocket.Config, *http.Request) error { return nil }, Handler: func(conn *websocket.Conn) {
		defer conn.Close()
		b.SubscribeDash("dash", conn)
		defer b.UnsubscribeDash("dash", conn)
		b.BroadcastToDash("dash", envelope)
		<-release
	}})
	defer server.Close()
	defer close(release)
	conn, err := websocket.Dial("ws"+server.URL[4:], "", server.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	conn.SetReadDeadline(time.Now().Add(time.Second))
	var raw []byte
	if err := websocket.Message.Receive(conn, &raw); err != nil {
		t.Fatal(err)
	}
	var decoded browserpb.TelemetryEnvelope
	if err := proto.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.GetPulse().GetNetwork() != nil || decoded.GetPulse().GetAgents()["public-node"] == nil {
		t.Fatal("public transport leaked network or lost agents")
	}
	if envelope.Pulse.Network == nil {
		t.Fatal("public filtering mutated the owner pulse")
	}
}
