package ws

import (
	"certainstats/internal/ws/browserpb"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"golang.org/x/net/websocket"
	"google.golang.org/protobuf/proto"
)

func TestProtobufFlag(t *testing.T) {
	for _, v := range []string{"", "false", "TRUE", "1", " true", "true ", "true"} {
		if ProtobufEnabled(v) != (v == "true") {
			t.Fatalf("flag %q", v)
		}
	}
}

func TestJSONNegotiation(t *testing.T) {
	for _, offer := range []string{"", BrowserProtocol, "json", "certainstats.protobuf.v2"} {
		r := httptest.NewRequest("GET", "/api/ws", nil)
		if offer != "" {
			r.Header.Set("Sec-WebSocket-Protocol", offer)
		}
		if requireBrowserProtocol(httptest.NewRecorder(), r, false) != (offer == "") {
			t.Fatalf("offer %q", offer)
		}
		config := &websocket.Config{}
		if offer != "" {
			config.Protocol = []string{offer}
		}
		if (selectBrowserProtocol(config, false) == nil) != (offer == "") {
			t.Fatalf("selection %q", offer)
		}
	}
}

func TestLegacyJSONFixtures(t *testing.T) {
	full := BrowserSnapshot(sampleSnapshot(), nil)
	full.Available, full.IsOnline = proto.Bool(true), proto.Bool(false)
	// Owner JSON preserves the original snapshot field names and metadata casing.
	raw, _ := json.Marshal(sampleSnapshot())
	var expected map[string]any
	json.Unmarshal(raw, &expected)
	expected["available"], expected["is_online"] = true, false
	actual, _ := json.Marshal(legacySnapshot(full, false))
	var decoded map[string]any
	json.Unmarshal(actual, &decoded)
	actual, _ = json.Marshal(decoded)
	want, _ := json.Marshal(expected)
	if string(actual) != string(want) {
		t.Fatalf("owner JSON mismatch\ngot %s\nwant %s", actual, want)
	}
	s := sampleSnapshot()
	s.Missing = map[string]bool{"agent_cpu_usage": true, "agent_disk_used": true}
	public := BrowserSnapshot(s, map[string]struct{}{"agent_cpu_usage": {}, "agent_disk_used": {}})
	actual, _ = json.Marshal(legacySnapshot(public, true))
	want = []byte(`{"CPUUsagePercent":null,"DiskUsedBytes":null,"Disks":[{"path":"/💾/<script>","used_bytes":null}],"Timestamp":"2026-10-09T00:44:15.123456789Z"}`)
	// Compare parsed values because encoding/json escapes HTML characters.
	var a, b any
	json.Unmarshal(actual, &a)
	json.Unmarshal(want, &b)
	ar, _ := json.Marshal(a)
	br, _ := json.Marshal(b)
	if string(ar) != string(br) {
		t.Fatalf("public JSON: %s", actual)
	}
	raw, _ = marshalLegacyJSON(&browserpb.TelemetryEnvelope{Pulse: &browserpb.TelemetryPulse{}}, true)
	if string(raw) != `{"type":"agent_update","data":{}}` {
		t.Fatalf("empty pulse: %s", raw)
	}
}

func TestLegacyJSONPresence(t *testing.T) {
	for _, public := range []bool{false, true} {
		field := "cpu_usage_percent"
		if public {
			field = "CPUUsagePercent"
		}
		cases := []struct {
			name     string
			snapshot *browserpb.Snapshot
			want     string
		}{
			{"denied", &browserpb.Snapshot{}, `{}`},
			{"unavailable metric", &browserpb.Snapshot{CpuUsagePercent: &browserpb.FloatValue{}}, `{"` + field + `":null}`},
			{"measured zero", &browserpb.Snapshot{CpuUsagePercent: &browserpb.FloatValue{Value: proto.Float64(0)}}, `{"` + field + `":0}`},
			{"unavailable agent", &browserpb.Snapshot{Available: proto.Bool(false), IsOnline: proto.Bool(false)}, `{"available":false,"is_online":false}`},
		}
		for _, c := range cases {
			raw, err := json.Marshal(legacySnapshot(c.snapshot, public))
			if err != nil || string(raw) != c.want {
				t.Fatalf("public=%v %s: %s (%v)", public, c.name, raw, err)
			}
		}
	}
}

func TestBroadcasterJSONTextDelivery(t *testing.T) {
	for _, public := range []bool{false, true} {
		t.Run(map[bool]string{false: "owner", true: "public"}[public], func(t *testing.T) {
			b := NewAgentBroadcaster(false)
			release := make(chan struct{})
			server := httptest.NewServer(websocket.Server{Handshake: func(c *websocket.Config, r *http.Request) error { return selectBrowserProtocol(c, false) }, Handler: func(conn *websocket.Conn) {
				defer conn.Close()
				envelope := &browserpb.TelemetryEnvelope{Pulse: &browserpb.TelemetryPulse{Agents: map[string]*browserpb.Snapshot{"id": {Available: proto.Bool(false), IsOnline: proto.Bool(false)}}}}
				if public {
					b.SubscribeDash("dash", conn)
					defer b.UnsubscribeDash("dash", conn)
					b.BroadcastToDash("dash", envelope)
				} else {
					b.SubscribeUser("owner", conn)
					defer b.UnsubscribeUser("owner", conn)
					b.BroadcastToUser("owner", envelope)
				}
				<-release
			}})
			defer server.Close()
			defer close(release)
			config, _ := websocket.NewConfig("ws"+server.URL[4:], server.URL)
			conn, err := websocket.DialConfig(config)
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			conn.SetReadDeadline(time.Now().Add(2 * time.Second))
			codec := websocket.Codec{Unmarshal: func(data []byte, kind byte, v any) error {
				if kind != websocket.TextFrame {
					t.Error("expected text frame")
				}
				if string(data) != `{"type":"agent_update","data":{"id":{"available":false,"is_online":false}}}` {
					t.Errorf("JSON %s", data)
				}
				return nil
			}}
			if err := codec.Receive(conn, new(any)); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestJSONMailboxRetainsLatestPulse(t *testing.T) {
	b := NewAgentBroadcaster(false)
	conn := &websocket.Conn{}
	writer := &browserWriter{queue: make(chan []byte, 1), done: make(chan struct{})}
	b.writers[conn] = writer
	for i := uint32(0); i < 100; i++ {
		b.sendTo([]*websocket.Conn{conn}, &browserpb.TelemetryEnvelope{Pulse: &browserpb.TelemetryPulse{Agents: map[string]*browserpb.Snapshot{"id": {Uptime: proto.Uint32(i)}}}}, true)
	}
	if len(writer.queue) != 1 {
		t.Fatal("mailbox grew")
	}
	if got := string(<-writer.queue); got != `{"type":"agent_update","data":{"id":{"Uptime":99}}}` {
		t.Fatal(got)
	}
	b.stopWriter(conn)
}
