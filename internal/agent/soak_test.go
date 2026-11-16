//go:build soak

package agent

import (
	parser "certainstats/internal/agent_parser"
	csctx "certainstats/internal/context"
	"certainstats/internal/metrics"
	"certainstats/internal/store"
	"certainstats/internal/store/sqlite"
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"
	"time"

	"github.com/prometheus/prometheus/tsdb"
)

// TestSoakIngestionMemory drives the real ingestion path with agent churn and
// checks that in-memory state stays bounded and the heap levels off.
//
//	go test -tags soak -run Soak -v ./internal/agent/
//	SOAK_CYCLES=500 go test -tags soak -run Soak -v ./internal/agent/
func TestSoakIngestionMemory(t *testing.T) {
	const agents, churnEvery, churnCount, checkpoints = 20, 100, 5, 10
	cycles := 1000
	if raw := os.Getenv("SOAK_CYCLES"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < checkpoints*2 {
			t.Fatalf("SOAK_CYCLES must be a number of at least %d", checkpoints*2)
		}
		cycles = n
	}
	ctx := context.Background()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "state.db")
	s, err := sqlite.New(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	counter, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer counter.Close()
	db, err := tsdb.Open(filepath.Join(dir, "tsdb"), nil, nil, tsdb.DefaultOptions(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := s.CreateUser(ctx, "user", "user", "hash", false); err != nil {
		t.Fatal(err)
	}
	cache := metrics.NewRealtimeCache()
	revoke := RevokeAgentHandler(s, db, cache, nil)

	live := make([]string, agents)
	provision := func(slot, gen int) {
		id := fmt.Sprintf("soak-%d-g%d", slot, gen)
		if err := s.AgentProvision(ctx, id, "user", "token-"+id, "", "ltstats"); err != nil {
			t.Fatal(err)
		}
		live[slot] = id
	}
	for i := range live {
		provision(i, 0)
	}

	type checkpoint struct {
		cycle                          int
		heapInuse                      uint64
		goroutines, ioStates, snapshot int
		journal                        int
	}
	var marks []checkpoint
	measure := func(cycle int) {
		runtime.GC()
		runtime.GC()
		var ms runtime.MemStats
		runtime.ReadMemStats(&ms)
		var rows int
		if err := counter.QueryRow("SELECT count(*) FROM ingestion_journal").Scan(&rows); err != nil {
			t.Fatal(err)
		}
		marks = append(marks, checkpoint{cycle, ms.HeapInuse, runtime.NumGoroutine(), IOStateCount(), cache.Len(), rows})
	}

	start := time.Now().Add(-time.Duration(cycles+1) * 30 * time.Second)
	gen := 1
	for c := 0; c < cycles; c++ {
		ts := start.Add(time.Duration(c) * 30 * time.Second)
		for _, id := range live {
			sample := parser.Telemetry{Timestamp: ts, IntervalSeconds: 30, SourceID: fmt.Sprintf("%s-%d", id, c),
				RXBytes: float64(1000 + c), TXBytes: float64(500 + c), CPUUsagePercent: float64(c % 100)}
			if err := Ingest(ctx, s, db, cache, &store.AgentIdentity{AgentID: id, UserID: "user", AgentType: "ltstats"}, &parser.ParsedData{Metrics: []parser.Telemetry{sample}}); err != nil {
				t.Fatalf("cycle %d ingest %s: %v", c, id, err)
			}
		}
		if (c+1)%churnEvery == 0 {
			for slot := 0; slot < churnCount; slot++ {
				r := httptest.NewRequest(http.MethodPost, "/?agent_id="+live[slot], nil)
				r = r.WithContext(context.WithValue(r.Context(), csctx.UserIDKey, "user"))
				w := httptest.NewRecorder()
				revoke(w, r)
				if w.Code != http.StatusOK {
					t.Fatalf("revoke %s: %d %s", live[slot], w.Code, w.Body.String())
				}
				provision(slot, gen)
			}
			gen++
			if _, err := s.IngestionPrune(ctx, time.Now()); err != nil {
				t.Fatal(err)
			}
			cache.EvictExpiredWindows()
		}
		if (c+1)%(cycles/checkpoints) == 0 {
			measure(c + 1)
		}
	}

	t.Logf("%8s %12s %10s %9s %9s %8s", "cycle", "heap_inuse", "goroutine", "io_state", "snapshot", "journal")
	for _, m := range marks {
		t.Logf("%8d %10.1fMi %10d %9d %9d %8d", m.cycle, float64(m.heapInuse)/(1<<20), m.goroutines, m.ioStates, m.snapshot, m.journal)
	}

	// Hard bounds: per-agent state never exceeds the live agents, the journal
	// never exceeds what one churn interval can add.
	for _, m := range marks {
		if m.ioStates > agents || m.snapshot > agents {
			t.Errorf("cycle %d: io_states=%d snapshots=%d exceed %d live agents", m.cycle, m.ioStates, m.snapshot, agents)
		}
		if m.journal > agents*churnEvery {
			t.Errorf("cycle %d: journal holds %d rows, bound is %d", m.cycle, m.journal, agents*churnEvery)
		}
	}
	if first, last := marks[1].goroutines, marks[len(marks)-1].goroutines; last > first+5 {
		t.Errorf("goroutines grew from %d to %d", first, last)
	}
	// Soft bound: the TSDB head legitimately keeps compressed samples, so allow
	// slack, but a leak per ingest or per churned agent shows up well past it.
	base, last := marks[1].heapInuse, marks[len(marks)-1].heapInuse
	allowed := base / 4
	if allowed < 16<<20 {
		allowed = 16 << 20
	}
	if last > base+allowed {
		t.Errorf("heap_inuse grew from %.1fMi to %.1fMi (allowed +%.1fMi)", float64(base)/(1<<20), float64(last)/(1<<20), float64(allowed)/(1<<20))
	}
}
