package networkmonitor

import (
	"math"
	"testing"
	"time"
)

func TestValidation(t *testing.T) {
	for _, c := range []Config{{Target: "https://user:pass@example.com", Protocol: "http"}, {Target: "file:///tmp/a", Protocol: "http"}, {Target: "127.0.0.1", Protocol: "dns"}, {Target: "example.com", Protocol: "smtp"}, {Target: "example.com", Protocol: "icmp", Interval: 59}, {Target: "example.com", Protocol: "icmp", Interval: 3601}, {Target: "example.com", Protocol: "dns", Server: "1.1.1.1:0"}} {
		if c.Normalize() == nil {
			t.Fatalf("accepted %+v", c)
		}
	}
	c := Config{Target: "EXAMPLE.COM", Protocol: "HTTP"}
	if e := c.Normalize(); e != nil || c.Target != "https://example.com" || c.Interval != 60 {
		t.Fatal(c, e)
	}
	r := Result{Total: 1, Success: 1, LastProbeAt: time.Now().UnixMilli(), Loss1h: math.NaN()}
	if r.Validate(time.Now()) == nil {
		t.Fatal("NaN accepted")
	}
	r.Loss1h = 0
	r.LastProbeAt = time.Now().Add(-time.Hour * 2).UnixMilli()
	if Fresh(r, 3600, time.Now()) {
		t.Fatal("stale fresh")
	}
}
