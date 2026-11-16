package routine

import (
	"bytes"
	"certainstats/internal/metrics"
	"log"
	"os"
	"strings"
	"testing"
)

func captureLog(t *testing.T, fn func()) string {
	t.Helper()
	var buf bytes.Buffer
	log.SetOutput(&buf)
	defer log.SetOutput(os.Stderr)
	fn()
	return buf.String()
}

func TestMemoryLogOffByDefault(t *testing.T) {
	e := &Routine{Cache: metrics.NewRealtimeCache(), MemoryStats: func() map[string]int { return map[string]int{"io_states": 3} }}
	if out := captureLog(t, e.maybeLogMemory); out != "" {
		t.Fatalf("memory logged while MemoryLog is off: %q", out)
	}
}

func TestMemoryLogOptIn(t *testing.T) {
	e := &Routine{MemoryLog: true, Cache: metrics.NewRealtimeCache(), MemoryStats: func() map[string]int { return map[string]int{"io_states": 3} }}
	out := captureLog(t, e.maybeLogMemory)
	if strings.Count(out, "[Maintenance] memory ") != 1 {
		t.Fatalf("want exactly one memory line, got %q", out)
	}
	for _, field := range []string{"heap_inuse=", "goroutines=", "realtime_agents=0", "io_states=3"} {
		if !strings.Contains(out, field) {
			t.Fatalf("memory line missing %s: %q", field, out)
		}
	}
}
