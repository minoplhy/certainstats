package ws

import (
	"bytes"
	nm "certainstats/internal/networkmonitor"
	"certainstats/internal/ws/browserpb"
	"encoding/base64"
	"encoding/json"
	"google.golang.org/protobuf/proto"
	"os"
	"testing"
	"time"
)

func TestNetworkPulseContracts(t *testing.T) {
	now := time.Unix(1700000000, 0).UTC()
	monitors := []nm.Monitor{
		{Config: nm.Config{ID: "zero"}, AgentID: "node", Enabled: true, State: "active", Sync: nm.Sync{Desired: 2, Ack: 1, LastAttempt: &now}, Latest: &nm.Latest{Result: nm.Result{Success: 1, Total: 1, SampleCount: 1, LastProbeAt: now.UnixMilli(), Cert: &nm.Cert{Expires: now.UnixMilli() + 10000, Issuer: "issuer"}}}},
		{Config: nm.Config{ID: "failed"}, AgentID: "node", State: "paused", Latest: &nm.Latest{Result: nm.Result{Total: 1, Loss: 100, Loss1h: 100}}},
		{Config: nm.Config{ID: "waiting"}, AgentID: "node", State: "waiting"},
	}
	pulse := BrowserNetwork(monitors)
	if pulse.Monitors["zero"].Latest.ResponseAvgMs.GetValue() != 0 || pulse.Monitors["zero"].Latest.ResponseAvgMs == nil {
		t.Fatal("lost measured zero")
	}
	if pulse.Monitors["failed"].Latest.ResponseAvgMs != nil || pulse.Monitors["waiting"].Latest != nil {
		t.Fatal("unavailable readings became measured values")
	}
	envelope := &browserpb.TelemetryEnvelope{Pulse: &browserpb.TelemetryPulse{Agents: map[string]*browserpb.Snapshot{"node": {Available: proto.Bool(true)}}, Network: pulse}}
	wire, err := proto.MarshalOptions{Deterministic: true}.Marshal(envelope)
	if err != nil {
		t.Fatal(err)
	}
	var decoded browserpb.TelemetryEnvelope
	if err := proto.Unmarshal(wire, &decoded); err != nil || !proto.Equal(envelope, &decoded) {
		t.Fatal("wire mismatch", err)
	}
	owner, err := marshalLegacyJSON(envelope, false)
	if err != nil {
		t.Fatal(err)
	}
	public, err := marshalLegacyJSON(envelope, true)
	if err != nil || bytes.Contains(public, []byte(`"network"`)) {
		t.Fatal("network leaked to public JSON", err)
	}
	empty, _ := marshalLegacyJSON(&browserpb.TelemetryEnvelope{Pulse: &browserpb.TelemetryPulse{Network: BrowserNetwork(nil)}}, false)
	if !bytes.Contains(empty, []byte(`"network":{}`)) {
		t.Fatal("lost complete empty pulse")
	}
	absent, _ := marshalLegacyJSON(&browserpb.TelemetryEnvelope{Pulse: &browserpb.TelemetryPulse{}}, false)
	if bytes.Contains(absent, []byte(`"network"`)) {
		t.Fatal("absent pulse became empty")
	}
	var expected map[string]any
	if err := json.Unmarshal(owner, &expected); err != nil {
		t.Fatal(err)
	}
	fixture, _ := json.MarshalIndent(map[string]any{"payload": base64.StdEncoding.EncodeToString(wire), "expected": expected["network"]}, "", "  ")
	fixture = append(fixture, '\n')
	file := "../../web/tests/fixtures/network_protocol.json"
	if os.Getenv("UPDATE_PROTOCOL_FIXTURES") == "1" {
		if err := os.WriteFile(file, fixture, 0644); err != nil {
			t.Fatal(err)
		}
	}
	got, err := os.ReadFile(file)
	if err != nil || !bytes.Equal(got, fixture) {
		t.Fatal("network fixture needs regeneration", err)
	}
}
