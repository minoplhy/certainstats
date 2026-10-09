package ltstats

import "testing"

func TestProtocolVersionIsNotSoftwareVersion(t *testing.T) {
	raw := make([]byte, NetHeaderSize)
	raw[33] = 7
	p, e := (&LTstats{}).Parse(raw)
	if e != nil {
		t.Fatal(e)
	}
	if p.Runtime == nil || p.Runtime.AgentVersion != nil || p.Runtime.ProtocolVersion == nil || *p.Runtime.ProtocolVersion != "7" {
		t.Fatal(p.Runtime)
	}
}
