package sqlite

import (
	"certainstats/internal/store"
	"context"
	"encoding/binary"
	"testing"
	"time"
)

func newJournalStore(t *testing.T) (*Store, context.Context) {
	t.Helper()
	ctx := context.Background()
	s := newTestStore(t)
	if err := s.CreateUser(ctx, "user", "user", "hash", false); err != nil {
		t.Fatal(err)
	}
	if err := s.AgentProvision(ctx, "agent", "user", "token", "", "ltstats"); err != nil {
		t.Fatal(err)
	}
	return s, ctx
}

func journalRows(t *testing.T, s *Store) int {
	t.Helper()
	var n int
	if err := s.db.QueryRow("SELECT count(*) FROM ingestion_journal").Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestIngestionCompleteStampsCompletionTime(t *testing.T) {
	s, ctx := newJournalStore(t)
	r := store.IngestionRecord{ID: "b1", AgentID: "agent", UserID: "user", Payload: []byte(`{"x":1}`)}
	if err := s.IngestionBegin(ctx, r, 0, 0, nil); err != nil {
		t.Fatal(err)
	}
	before := time.Now().UnixMilli()
	if err := s.IngestionComplete(ctx, "b1"); err != nil {
		t.Fatal(err)
	}
	got, err := s.IngestionGet(ctx, "b1")
	if err != nil || !got.Complete || len(got.Payload) != 8 {
		t.Fatalf("unexpected record %+v %v", got, err)
	}
	ms := int64(binary.BigEndian.Uint64(got.Payload))
	if ms < before || ms > time.Now().UnixMilli() {
		t.Fatalf("stamp %d outside completion window", ms)
	}
}

func TestIngestionPruneWindow(t *testing.T) {
	s, ctx := newJournalStore(t)
	for _, id := range []string{"done", "pending"} {
		if err := s.IngestionBegin(ctx, store.IngestionRecord{ID: id, AgentID: "agent", UserID: "user", Payload: []byte(`{}`)}, 0, 0, nil); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.IngestionComplete(ctx, "done"); err != nil {
		t.Fatal(err)
	}
	// Legacy completed row from before completion stamps.
	if _, err := s.db.Exec("INSERT INTO ingestion_journal(batch_id,agent_id,user_id,payload,complete) VALUES('legacy','agent','user',X'',1)"); err != nil {
		t.Fatal(err)
	}

	n, err := s.IngestionPrune(ctx, time.Now().Add(-time.Hour))
	if err != nil || n != 1 {
		t.Fatalf("past cutoff pruned %d %v, want only the legacy row", n, err)
	}
	if _, err := s.IngestionGet(ctx, "done"); err != nil {
		t.Fatalf("recent completed row removed: %v", err)
	}

	n, err = s.IngestionPrune(ctx, time.Now().Add(time.Hour))
	if err != nil || n != 1 {
		t.Fatalf("future cutoff pruned %d %v, want 1", n, err)
	}
	pending, err := s.IngestionPending(ctx)
	if err != nil || len(pending) != 1 || pending[0].ID != "pending" {
		t.Fatalf("pending row not kept: %+v %v", pending, err)
	}
}

func TestIngestionPruneChunks(t *testing.T) {
	s, ctx := newJournalStore(t)
	const total = journalPruneChunk*2 + 7
	tx, err := s.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	old := completionStamp(time.Now().Add(-48 * time.Hour))
	for i := 0; i < total; i++ {
		if _, err := tx.Exec("INSERT INTO ingestion_journal(batch_id,agent_id,user_id,payload,complete) VALUES(?,?,?,?,1)", "b"+time.Duration(i).String(), "agent", "user", old); err != nil {
			t.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	n, err := s.IngestionPrune(ctx, time.Now().Add(-24*time.Hour))
	if err != nil || n != total {
		t.Fatalf("pruned %d %v, want %d", n, err, total)
	}
	if left := journalRows(t, s); left != 0 {
		t.Fatalf("%d rows left", left)
	}
}

func TestReclaimSpaceSwitchesToIncremental(t *testing.T) {
	s, ctx := newJournalStore(t)
	for i := 0; i < 2; i++ {
		if err := s.ReclaimSpace(ctx); err != nil {
			t.Fatalf("reclaim %d: %v", i, err)
		}
		var mode int
		if err := s.db.QueryRow("PRAGMA auto_vacuum").Scan(&mode); err != nil {
			t.Fatal(err)
		}
		if mode != 2 {
			t.Fatalf("auto_vacuum=%d, want 2", mode)
		}
	}
}
