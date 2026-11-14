package crowdsec

import (
	"strconv"
	"testing"

	"certainstats/internal/crowdsec/wire"

	"github.com/fxamacker/cbor/v2"
)

func bigAlert(i, decisions int) wire.Alert {
	a := wire.Alert{UUID: "a" + strconv.Itoa(i), Scenario: "crowdsecurity/ssh-bf"}
	for j := 0; j < decisions; j++ {
		a.Decisions = append(a.Decisions, wire.Decision{UUID: strconv.Itoa(j), Type: "ban", Scope: "Ip", Value: "203.0.113." + strconv.Itoa(j%250), Duration: "4h"})
	}
	return a
}

func TestFitKeepsBatchesWithinBudget(t *testing.T) {
	var alerts []wire.Alert
	for i := 0; i < 200; i++ {
		alerts = append(alerts, bigAlert(i, 50))
	}
	budget := 64 << 10
	for rest := alerts; len(rest) > 0; {
		n := fit(rest, budget)
		if n < 1 || n > len(rest) {
			t.Fatalf("fit returned %d of %d", n, len(rest))
		}
		if b, _ := cbor.Marshal(rest[:n]); n > 1 && len(b) > budget {
			t.Fatalf("batch of %d is %d bytes, over %d", n, len(b), budget)
		}
		rest = rest[n:]
	}
	if n := fit(alerts[:3], batchBudget); n != 3 {
		t.Fatalf("small batch split to %d", n)
	}
}

func TestCappedAlertFitsAFrame(t *testing.T) {
	a := bigAlert(0, maxDecisions)
	b, err := cbor.Marshal([]wire.Alert{a})
	if err != nil || len(b) > batchBudget {
		t.Fatalf("one alert with %d decisions is %d bytes (%v)", maxDecisions, len(b), err)
	}
}
