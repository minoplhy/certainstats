package agent

import (
	parser "certainstats/internal/agent_parser"
	csctx "certainstats/internal/context"
	"certainstats/internal/metrics"
	"certainstats/internal/store"
	"certainstats/internal/store/sqlite"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/prometheus/prometheus/tsdb"
)

func ioStateLen() int { return IOStateCount() }

// Revoking an agent must drop its per-agent traffic state.
func TestRevokeForgetsIOState(t *testing.T) {
	const n = 100
	ctx := context.Background()
	s, err := sqlite.New(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	db, err := tsdb.Open(t.TempDir(), nil, nil, tsdb.DefaultOptions(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := s.CreateUser(ctx, "user", "user", "hash", false); err != nil {
		t.Fatal(err)
	}
	cache := metrics.NewRealtimeCache()
	start := ioStateLen()
	now := time.Now().Add(-time.Minute)
	for i := 0; i < n; i++ {
		id := fmt.Sprintf("leak-agent-%d", i)
		if err := s.AgentProvision(ctx, id, "user", "token-"+id, "", "ltstats"); err != nil {
			t.Fatal(err)
		}
		sample := parser.Telemetry{Timestamp: now, IntervalSeconds: 30, SourceID: "s-" + id, RXBytes: 1, TXBytes: 1}
		if err := Ingest(ctx, s, db, cache, &store.AgentIdentity{AgentID: id, UserID: "user", AgentType: "ltstats"}, &parser.ParsedData{Metrics: []parser.Telemetry{sample}}); err != nil {
			t.Fatal(err)
		}
	}
	if got := ioStateLen() - start; got != n {
		t.Fatalf("ioStates grew by %d, want %d", got, n)
	}
	revoke := RevokeAgentHandler(s, db, cache, nil)
	for i := 0; i < n; i++ {
		r := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/?agent_id=leak-agent-%d", i), nil)
		r = r.WithContext(context.WithValue(r.Context(), csctx.UserIDKey, "user"))
		w := httptest.NewRecorder()
		revoke(w, r)
		if w.Code != http.StatusOK {
			t.Fatalf("revoke %d: %d %s", i, w.Code, w.Body.String())
		}
	}
	if got := ioStateLen(); got != start {
		t.Fatalf("ioStates holds %d entries after revoking all agents, want %d", got, start)
	}
	if got := len(cache.GetAll()); got != 0 {
		t.Fatalf("realtime cache holds %d snapshots after revoke", got)
	}
}
