package beszel

import (
	"certainstats/internal/agentmeta"
	nm "certainstats/internal/networkmonitor"
	"context"
	"github.com/fxamacker/cbor/v2"
	"testing"
)

func TestCapabilityVersions(t *testing.T) {
	p := &BeszelStats{}
	for _, tc := range []struct {
		v             string
		basic, extras bool
	}{{"", false, false}, {"nonsense", false, false}, {"0.19.9", false, false}, {"0.20.0", true, false}, {"0.21.0", true, true}, {"v1.0.0", true, true}} {
		c := p.ResolveCapabilities(agentmeta.Runtime{AgentVersion: agentmeta.String(tc.v)})
		if c.Supports("icmp") != tc.basic || c.Supports("custom_dns_server") != tc.extras {
			t.Fatal(tc, c)
		}
	}
	d := agentmeta.Declaration{agentmeta.Key("icmp"): false}
	c := p.ResolveCapabilities(agentmeta.Runtime{AgentVersion: agentmeta.String("0.21.0"), ReportedCapabilities: &d})
	if c.Supports("icmp") || !c.Supports("http") {
		t.Fatal(c)
	}
}

type wireSession struct{ t *testing.T }

func (s wireSession) Request(ctx context.Context, action uint8, req any) ([]byte, error) {
	if action != 7 {
		s.t.Fatal(action)
	}
	raw, _ := cbor.Marshal(req)
	var m map[uint64]cbor.RawMessage
	cbor.Unmarshal(raw, &m)
	var config map[uint64]any
	cbor.Unmarshal(m[1], &config)
	if config[0] != "id" || config[5] != "1.1.1.1" {
		s.t.Fatal(config)
	}
	return cbor.Marshal(map[uint64]any{0: map[uint64]any{8: int64(123), 10: int64(3), 11: int64(2)}})
}
func TestMonitorWire(t *testing.T) {
	p := &BeszelStats{}
	r, e := p.Apply(context.Background(), wireSession{t}, nm.Operation{Action: 1, Config: nm.Config{ID: "id", Target: "example.com", Protocol: "dns", Interval: 30, Server: "1.1.1.1"}, RunNow: true})
	if e != nil || r.LastProbeAt != 123 || r.Total != 3 || r.Success != 2 {
		t.Fatal(r, e)
	}
	raw, _ := cbor.Marshal(map[uint64]any{0: map[uint64]any{}, 1: map[uint64]any{10: "0.21.0"}, 6: map[string]any{"id": map[uint64]any{8: int64(123), 10: int64(3), 11: int64(2)}}})
	parsed, e := p.Parse(raw)
	if e != nil || parsed.NetworkResults["id"].Total != 3 || *parsed.Runtime.AgentVersion != "0.21.0" {
		t.Fatal(parsed, e)
	}
}

func TestNewGPUFormatDoesNotBlockTelemetry(t *testing.T) {
	raw, e := cbor.Marshal(map[uint64]any{0: map[uint64]any{22: map[string]any{"gpu": map[uint64]any{0: "Example GPU", 1: 50.0}}}, 1: map[uint64]any{10: "0.21.0"}, 6: map[string]any{"monitor": map[uint64]any{8: int64(123), 10: int64(1), 11: int64(1)}}})
	if e != nil {
		t.Fatal(e)
	}
	parsed, e := (&BeszelStats{}).Parse(raw)
	if e != nil || parsed.NetworkResults["monitor"].Total != 1 {
		t.Fatal(parsed, e)
	}
}
