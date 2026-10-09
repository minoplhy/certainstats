package ws

import (
	parser "certainstats/internal/agent_parser"
	"certainstats/internal/metrics"
	"certainstats/internal/ws/browserpb"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/net/websocket"
	"google.golang.org/protobuf/proto"
)

func sampleSnapshot() *metrics.AgentSnapshot {
	return &metrics.AgentSnapshot{
		AgentID: "private-agent", Timestamp: time.Date(2026, 10, 9, 0, 44, 15, 123456789, time.UTC),
		CPUUsagePercent: 0, CPUIOWaitPercent: 2.5, CPUStealPercent: 0.5,
		RAMUsedBytes: 123456789, RAMSwapUsedBytes: 0, DiskUsedBytes: 42, DiskTotalBytes: math.MaxUint64,
		Disks:   []parser.DiskTelemetry{{Path: "/💾/<script>", UsedBytes: 0, TotalBytes: math.MaxUint64, ReadBytes: 123, WriteBytes: 456}},
		LoadAvg: [3]float64{0, 0.5, 1.2}, Temperatures: map[string]float64{"CPU": 42.5},
		RXBytes: 1024.5, TXBytes: 2048, RXBps: 1024.5, TXBps: 2048, DiskReadBps: 12.3, DiskWriteBps: 45.6,
		Metadata: &parser.ParsedMetadata{Uptime: 1234, LinuxVersion: "Linux", CpuModel: "CPU", CpuCores: 8, RamSize: math.MaxUint64, SwapSize: 0, DiskSize: math.MaxUint64},
	}
}

type protocolFixture struct {
	Name          string         `json:"name"`
	Payload       string         `json:"payload"`
	Expected      map[string]any `json:"expected"`
	JSONBytes     int            `json:"json_bytes"`
	ProtobufBytes int            `json:"protobuf_bytes"`
}

// Go generates the shared wire fixtures; Node consumes the same bytes using
// the shipped decoder. Update deliberately with UPDATE_PROTOCOL_FIXTURES=1.
func TestBrowserProtocolFixtures(t *testing.T) {
	s := sampleSnapshot()
	full := BrowserSnapshot(s, nil)
	full.Available, full.IsOnline = proto.Bool(true), proto.Bool(false)
	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	var fullExpected map[string]any
	if err := json.Unmarshal(raw, &fullExpected); err != nil {
		t.Fatal(err)
	}
	fullExpected["available"], fullExpected["is_online"], fullExpected["uptime"] = true, false, uint32(1234)
	// Match the previous authenticated JSON serializer, including its decoded
	// numeric representation, but without the new top-level uptime field.
	oldFull := make(map[string]any, len(fullExpected))
	for key, value := range fullExpected {
		if key != "uptime" {
			oldFull[key] = value
		}
	}
	missingSource := &metrics.AgentSnapshot{Timestamp: s.Timestamp, Missing: map[string]bool{"agent_cpu_usage": true, "agent_rx_bytes": true}, CPUUsagePercent: 99, RXBytes: 99}
	missing := BrowserSnapshot(missingSource, map[string]struct{}{"agent_cpu_usage": {}, "agent_rx_bytes": {}})
	public := BrowserSnapshot(s, map[string]struct{}{"agent_cpu_usage": {}, "agent_disk_used": {}, "is_online": {}})
	public.IsOnline = proto.Bool(false)
	timestamp := "2026-10-09T00:44:15.123456789Z"
	cases := []struct {
		name     string
		agents   map[string]*browserpb.Snapshot
		expected map[string]any
	}{
		{"full", map[string]*browserpb.Snapshot{"private-agent": full}, map[string]any{"private-agent": fullExpected}},
		{"public", map[string]*browserpb.Snapshot{"public-agent": public}, map[string]any{"public-agent": map[string]any{"timestamp": timestamp, "is_online": false, "cpu_usage_percent": 0, "disk_used_bytes": 42, "disks": []any{map[string]any{"path": "/💾/<script>", "used_bytes": 0}}}}},
		{"missing", map[string]*browserpb.Snapshot{"public-agent": missing}, map[string]any{"public-agent": map[string]any{"timestamp": timestamp, "cpu_usage_percent": nil, "rx_bytes": nil, "rx_bps": nil}}},
		{"unavailable", map[string]*browserpb.Snapshot{"public-agent": {Available: proto.Bool(false), IsOnline: proto.Bool(false)}}, map[string]any{"public-agent": map[string]any{"available": false, "is_online": false}}},
		{"empty", map[string]*browserpb.Snapshot{}, map[string]any{}},
		{"empty_collections", map[string]*browserpb.Snapshot{"public-agent": {Disks: &browserpb.DiskList{}, Temperatures: &browserpb.Temperatures{}, LoadAvg: &browserpb.LoadAverage{}}}, map[string]any{"public-agent": map[string]any{"disks": []any{}, "temperatures": map[string]any{}, "load_avg": []any{}}}},
	}
	// A representative many-agent pulse compares the old JSON shape to the
	// new wire encoding. It is a sample measurement, not a capacity claim.
	many := map[string]*browserpb.Snapshot{}
	manyExpected := map[string]any{}
	for i := 0; i < 32; i++ {
		id := fmt.Sprintf("agent-%02d", i)
		many[id] = full
		manyExpected[id] = fullExpected
	}
	cases = append(cases, struct {
		name     string
		agents   map[string]*browserpb.Snapshot
		expected map[string]any
	}{"many_agents", many, manyExpected})
	fixtures := []protocolFixture{}
	for _, c := range cases {
		envelope := &browserpb.TelemetryEnvelope{Pulse: &browserpb.TelemetryPulse{Agents: c.agents}}
		wire, err := (proto.MarshalOptions{Deterministic: true}).Marshal(envelope)
		if err != nil {
			t.Fatal(err)
		}
		var decoded browserpb.TelemetryEnvelope
		if err := proto.Unmarshal(wire, &decoded); err != nil {
			t.Fatal(err)
		}
		if !proto.Equal(envelope, &decoded) {
			t.Fatalf("%s did not round trip", c.name)
		}
		oldData := c.expected
		if c.name == "full" || c.name == "many_agents" {
			oldData = make(map[string]any, len(c.expected))
			for key := range c.expected {
				oldData[key] = oldFull
			}
		}
		old, err := json.Marshal(map[string]any{"type": "agent_update", "data": oldData})
		if err != nil {
			t.Fatal(err)
		}
		fixtures = append(fixtures, protocolFixture{c.name, base64.StdEncoding.EncodeToString(wire), c.expected, len(old), len(wire)})
		t.Logf("%s: JSON %d bytes, Protobuf %d bytes", c.name, len(old), len(wire))
	}
	contents, err := json.MarshalIndent(fixtures, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	contents = append(contents, '\n')
	file := filepath.Join("..", "..", "web", "tests", "fixtures", "browser_protocol.json")
	if os.Getenv("UPDATE_PROTOCOL_FIXTURES") == "1" {
		if err := os.MkdirAll(filepath.Dir(file), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(file, contents, 0644); err != nil {
			t.Fatal(err)
		}
	}
	got, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(contents) {
		t.Fatal("shared browser fixtures need regeneration")
	}
}

func TestBrowserSnapshotPresence(t *testing.T) {
	s := sampleSnapshot()
	out := BrowserSnapshot(s, map[string]struct{}{})
	if out.AgentId != nil || out.Metadata != nil || out.CpuUsagePercent != nil || out.Disks != nil {
		t.Fatal("empty public policy leaked fields")
	}
	out = BrowserSnapshot(s, nil)
	if out.CpuUsagePercent.Value == nil || out.CpuUsagePercent.GetValue() != 0 {
		t.Fatal("measured zero lost presence")
	}
	if out.DiskTotalBytes.GetValue() != math.MaxUint64 {
		t.Fatal("integer precision lost")
	}
	s.Missing = map[string]bool{"agent_cpu_usage": true}
	s.RXBps = math.NaN()
	out = BrowserSnapshot(s, nil)
	if out.CpuUsagePercent == nil || out.CpuUsagePercent.Value != nil || out.RxBps.Value != nil {
		t.Fatal("unavailable value became zero")
	}
	// Editing generated messages must not mutate the cache's maps/slices.
	out.Disks.Items[0].Path = "changed"
	out.Temperatures.Values["CPU"] = 0
	if s.Disks[0].Path == "changed" || s.Temperatures["CPU"] != 42.5 {
		t.Fatal("wire model aliases cached snapshot")
	}
}

func TestBrowserProtocolNegotiation(t *testing.T) {
	for _, offered := range []string{"", "json", "certainstats.protobuf.v2"} {
		r := httptest.NewRequest("GET", "/api/ws", nil)
		r.Header.Set("Sec-WebSocket-Protocol", offered)
		w := httptest.NewRecorder()
		if requireBrowserProtocol(w, r, true) || w.Code != http.StatusUpgradeRequired {
			t.Fatalf("accepted %q", offered)
		}
	}
	r := httptest.NewRequest("GET", "/api/ws", nil)
	r.Header.Set("Sec-WebSocket-Protocol", "other, "+BrowserProtocol)
	if !requireBrowserProtocol(httptest.NewRecorder(), r, true) {
		t.Fatal("rejected supported offer")
	}
	config := &websocket.Config{Protocol: []string{"other", BrowserProtocol}}
	if err := selectBrowserProtocol(config, true); err != nil || len(config.Protocol) != 1 || config.Protocol[0] != BrowserProtocol {
		t.Fatal("protocol not selected")
	}
}

func TestBroadcasterBinaryDelivery(t *testing.T) {
	b := NewAgentBroadcaster(true)
	release := make(chan struct{})
	server := httptest.NewServer(websocket.Server{
		Handshake: func(c *websocket.Config, r *http.Request) error { return selectBrowserProtocol(c, true) },
		Handler: func(conn *websocket.Conn) {
			defer conn.Close()
			b.SubscribeUser("owner", conn)
			defer b.UnsubscribeUser("owner", conn)
			b.BroadcastToUser("owner", &browserpb.TelemetryEnvelope{Pulse: &browserpb.TelemetryPulse{Agents: map[string]*browserpb.Snapshot{"private-agent": BrowserSnapshot(sampleSnapshot(), nil)}}})
			<-release
		},
	})
	defer server.Close()
	defer close(release)
	config, err := websocket.NewConfig("ws"+server.URL[4:], server.URL)
	if err != nil {
		t.Fatal(err)
	}
	config.Protocol = []string{BrowserProtocol}
	conn, err := websocket.DialConfig(config)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	codec := websocket.Codec{Unmarshal: func(data []byte, payloadType byte, v any) error {
		if payloadType != websocket.BinaryFrame {
			t.Fatal("received a text frame")
		}
		return proto.Unmarshal(data, v.(*browserpb.TelemetryEnvelope))
	}}
	var got browserpb.TelemetryEnvelope
	if err := codec.Receive(conn, &got); err != nil {
		t.Fatal(err)
	}
	if got.Pulse.Agents["private-agent"].DiskTotalBytes.GetValue() != math.MaxUint64 {
		t.Fatal("binary payload changed")
	}
}

func TestBrowserMailboxRetainsLatestPulse(t *testing.T) {
	b := NewAgentBroadcaster(true)
	conn := &websocket.Conn{}
	writer := &browserWriter{queue: make(chan []byte, 1), done: make(chan struct{})}
	b.writers[conn] = writer
	for i := uint32(0); i < 100; i++ {
		b.sendTo([]*websocket.Conn{conn}, &browserpb.TelemetryEnvelope{Pulse: &browserpb.TelemetryPulse{Agents: map[string]*browserpb.Snapshot{"agent": {Uptime: proto.Uint32(i)}}}}, true)
	}
	if len(writer.queue) != 1 {
		t.Fatal("mailbox grew beyond one pulse")
	}
	var envelope browserpb.TelemetryEnvelope
	if err := proto.Unmarshal(<-writer.queue, &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Pulse.Agents["agent"].GetUptime() != 99 {
		t.Fatal("mailbox retained an obsolete pulse")
	}
	b.stopWriter(conn)
	if len(b.writers) != 0 {
		t.Fatal("writer not disposed")
	}
}
