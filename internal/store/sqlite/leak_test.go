package sqlite

import (
	"context"
	"fmt"
	"testing"
)

func tokenCacheLen(s *Store) int {
	n := 0
	s.cache.Range(func(any, any) bool { n++; return true })
	return n
}

// Deleting agents must evict their cached token identities.
func TestTokenCacheReleasedOnAgentDelete(t *testing.T) {
	const n = 500
	ctx := context.Background()
	s := newTestStore(t)
	if err := s.CreateUser(ctx, "user", "user", "hash", false); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < n; i++ {
		id := fmt.Sprintf("agent-%d", i)
		if err := s.AgentProvision(ctx, id, "user", "token-"+id, "", "ltstats"); err != nil {
			t.Fatal(err)
		}
		if _, err := s.AgentGetByToken(ctx, "token-"+id); err != nil {
			t.Fatal(err)
		}
	}
	if got := tokenCacheLen(s); got != n {
		t.Fatalf("token cache holds %d, want %d", got, n)
	}
	for i := 0; i < n; i++ {
		if err := s.AgentDelete(ctx, fmt.Sprintf("agent-%d", i), "user"); err != nil {
			t.Fatal(err)
		}
	}
	if got := tokenCacheLen(s); got != 0 {
		t.Fatalf("token cache holds %d after deleting every agent", got)
	}
}
