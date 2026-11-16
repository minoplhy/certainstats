package security

import (
	"fmt"
	"testing"
	"time"
)

func TestLimiterPrunesStaleClients(t *testing.T) {
	l := NewIPLimiter(60, 10)
	for i := 0; i < 10000; i++ {
		l.Allow(fmt.Sprintf("10.0.%d.%d", i/256, i%256), false)
	}
	l.mu.Lock()
	for _, c := range l.entries {
		c.seen = time.Now().Add(-11 * time.Minute)
	}
	l.mu.Unlock()
	l.Allow("192.0.2.1", false)
	l.mu.Lock()
	got := len(l.entries)
	l.mu.Unlock()
	if got != 1 {
		t.Fatalf("%d clients tracked after stale prune, want 1", got)
	}
}

func TestLimiterCappedUnderFreshFlood(t *testing.T) {
	l := NewIPLimiter(60, 10)
	for i := 0; i < 15000; i++ {
		l.Allow(fmt.Sprintf("10.%d.%d.%d", i>>16, (i>>8)&255, i&255), false)
	}
	l.mu.Lock()
	got, overflow := len(l.entries), l.overflowRequests
	l.mu.Unlock()
	if got > 10000 {
		t.Fatalf("%d clients tracked, cap is 10000", got)
	}
	if overflow != 5000 {
		t.Fatalf("overflow=%d, want 5000", overflow)
	}
}
