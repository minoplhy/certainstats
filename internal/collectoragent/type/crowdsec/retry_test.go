package crowdsec

import (
	"errors"
	"testing"
	"time"

	"certainstats/internal/collector/type/crowdsec/wire"
)

func TestRetryDelay(t *testing.T) {
	poll := time.Minute
	want := []time.Duration{5 * time.Second, 10 * time.Second, 20 * time.Second, 40 * time.Second, time.Minute, time.Minute}
	for i, w := range want {
		if got := retryDelay(i+1, poll); got != w {
			t.Fatalf("retryDelay(%d) = %s, want %s", i+1, got, w)
		}
	}
	if got := retryDelay(1, 3*time.Second); got != 3*time.Second {
		t.Fatalf("delay must not exceed the poll interval: %s", got)
	}
}

func TestStartupGrace(t *testing.T) {
	start := time.Date(2026, 11, 15, 3, 44, 0, 0, time.UTC)
	now := start
	newRunner := func(once bool) (*runner, *[]wire.Status) {
		var sent []wire.Status
		m := &Module{Now: func() time.Time { return now }, started: start}
		send := func(msgType string, body any) error {
			if msgType == wire.MsgStatus {
				sent = append(sent, body.(wire.Status))
			}
			return nil
		}
		return &runner{m: m, send: send, once: once, cfg: wire.Config{PollSeconds: 60}}, &sent
	}
	down := errors.New("cannot reach the Local API at http://crowdsec:8080: connection refused")

	r, sent := newRunner(false)
	now = start.Add(30 * time.Second)
	r.reachErr(down)
	if len(*sent) != 0 || r.nextWait() != 5*time.Second {
		t.Fatalf("inside the grace: sent %v, next wait %s", *sent, r.nextWait())
	}
	r.reachErr(down)
	if len(*sent) != 0 || r.nextWait() != 10*time.Second {
		t.Fatalf("second failure: sent %v, next wait %s", *sent, r.nextWait())
	}

	now = start.Add(startGrace + time.Second)
	r.reachErr(down)
	if len(*sent) != 1 || (*sent)[0].Reachable {
		t.Fatalf("after the grace the error must be reported: %v", *sent)
	}

	r.reachErr(nil)
	if r.failures != 0 || !r.m.reached || r.nextWait() != time.Minute || !(*sent)[len(*sent)-1].Reachable {
		t.Fatalf("success must reset: failures %d, sent %v", r.failures, *sent)
	}

	// Once reached, a later outage is reported at once, even inside the grace.
	r2, sent2 := newRunner(false)
	r2.m.reached = true
	now = start.Add(10 * time.Second)
	r2.reachErr(down)
	if len(*sent2) != 1 {
		t.Fatalf("outage after reaching must be reported: %v", *sent2)
	}

	// --once reports at once.
	r3, sent3 := newRunner(true)
	r3.reachErr(down)
	if len(*sent3) != 1 {
		t.Fatalf("--once must report at once: %v", *sent3)
	}
}
