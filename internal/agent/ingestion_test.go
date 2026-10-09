package agent

import (
	parser "certainstats/internal/agent_parser"
	"certainstats/internal/metrics"
	"certainstats/internal/store"
	"certainstats/internal/store/sqlite"
	"context"
	"encoding/json"
	"github.com/prometheus/prometheus/tsdb"
	"path/filepath"
	"testing"
	"time"
)

func TestIngestionRecoveryAndReplay(t *testing.T) {
	ctx := context.Background()
	s, err := sqlite.New(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.CreateUser(ctx, "user", "user", "hash", false); err != nil {
		t.Fatal(err)
	}
	if err := s.AgentProvision(ctx, "agent", "user", "token", "", "ltstats"); err != nil {
		t.Fatal(err)
	}
	db, err := tsdb.Open(t.TempDir(), nil, nil, tsdb.DefaultOptions(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	sample := parser.Telemetry{Timestamp: time.Now().Add(-time.Minute), IntervalSeconds: 30, SourceID: "sample-1", RXBytes: 300, TXBytes: 600, CPUUsagePercent: 42}
	data := parser.ParsedData{Metrics: []parser.Telemetry{sample}}
	payload, _ := json.Marshal(data)
	record := store.IngestionRecord{ID: "interrupted", AgentID: "agent", UserID: "user", Payload: payload}
	if err := s.IngestionBegin(ctx, record, 300, 600, nil); err != nil {
		t.Fatal(err)
	}
	if err := s.IngestionBegin(ctx, record, 300, 600, nil); err != nil {
		t.Fatal(err)
	}
	cache := metrics.NewRealtimeCache()
	if err := RecoverIngestion(ctx, s, db, cache); err != nil {
		t.Fatal(err)
	}
	if err := RecoverIngestion(ctx, s, db, cache); err != nil {
		t.Fatal(err)
	}
	agent, err := s.AgentGetByID(ctx, "agent", "user")
	if err != nil || agent.TotalRxBytes != 300 {
		t.Fatalf("counter repeated: %+v %v", agent, err)
	}
	snap, ok := cache.Get("agent")
	if !ok || snap.RXBps != 10 || !snap.Timestamp.Equal(sample.Timestamp) {
		t.Fatalf("incorrect snapshot %+v", snap)
	}
	sample.Timestamp = sample.Timestamp.Add(30 * time.Second)
	sample.SourceID = "sample-2"
	for i := 0; i < 2; i++ {
		data := &parser.ParsedData{Metrics: []parser.Telemetry{sample}}
		if err := Ingest(ctx, s, db, cache, &store.AgentIdentity{AgentID: "agent", UserID: "user"}, data); err != nil {
			t.Fatal(err)
		}
	}
	agent, _ = s.AgentGetByID(ctx, "agent", "user")
	if agent.TotalRxBytes != 600 {
		t.Fatalf("source replay duplicated counters: %d", agent.TotalRxBytes)
	}
}
func TestNormalizeVariableIntervalsAndResets(t *testing.T) {
	id := t.Name()
	samples := []parser.Telemetry{{Timestamp: time.Unix(100, 0), IntervalSeconds: 10, NetworkIOType: parser.IOCumulative, RXBytes: 100, TXBytes: 200}, {Timestamp: time.Unix(130, 0), NetworkIOType: parser.IOCumulative, RXBytes: 400, TXBytes: 800}, {Timestamp: time.Unix(150, 0), NetworkIOType: parser.IOCumulative, RXBytes: 50, TXBytes: 50}}
	normalizeIOMetrics(id, samples)
	if !samples[0].NetworkMissing || samples[1].IntervalSeconds != 30 || samples[1].RXBytes != 300 || !samples[2].NetworkMissing {
		t.Fatalf("bad normalization: %+v", samples)
	}
}
