package sqlite

import (
	response "certainstats/internal/base/response"
	"certainstats/internal/dashboard/accessrules"
	"certainstats/internal/store"
	"context"
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestDashboardOwnershipAndVersion(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	for _, id := range []string{"one", "two"} {
		if err := s.CreateUser(ctx, id, id, "hash", false); err != nil {
			t.Fatal(err)
		}
		if err := s.AgentProvision(ctx, "agent-"+id, id, "token-"+id, "", "ltstats"); err != nil {
			t.Fatal(err)
		}
	}
	d := store.Dashboard{DashboardID: "dash", UserID: "one", Slug: "slug", Title: "Test", AccessRules: accessrules.AccessRules{"public": {AllowedFeatures: []string{"is_online"}, MaxDays: 1}}}
	if err := s.DashboardCreate(ctx, d); err != nil {
		t.Fatal(err)
	}
	own := []response.CreateDashboardReqAgent{{AgentID: "agent-one"}}
	if err := s.DashboardAddAgents(ctx, d, own); err != nil {
		t.Fatal(err)
	}
	before, err := s.DashboardGetBySlug(ctx, d.Slug)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.DashboardDelete(ctx, d.DashboardID, "two"); err == nil {
		t.Fatal("unauthorized delete succeeded")
	}
	identities, err := s.DashboardGetAgents(ctx, d.DashboardID, d.UserID)
	if err != nil || len(identities) != 1 {
		t.Fatalf("membership lost: %v %+v", err, identities)
	}
	other := d
	other.UserID = "two"
	if err := s.DashboardAddAgents(ctx, other, nil); err == nil {
		t.Fatal("unauthorized add succeeded")
	}
	if err := s.DashboardUpdate(ctx, d, []response.CreateDashboardReqAgent{{AgentID: "agent-two"}}); err == nil {
		t.Fatal("cross-owner membership succeeded")
	}
	after, _ := s.DashboardGetBySlug(ctx, d.Slug)
	if before.Version != after.Version {
		t.Fatal("unauthorized mutation changed version")
	}
	d.Title = "Updated"
	if err := s.DashboardUpdate(ctx, d, own); err != nil {
		t.Fatal(err)
	}
	after, _ = s.DashboardGetBySlug(ctx, d.Slug)
	if after.Version <= before.Version {
		t.Fatal("version did not increase")
	}
}
func TestConcurrentInitialSetup(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	var wg sync.WaitGroup
	success := make(chan bool, 20)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			success <- s.CreateInitialUser(ctx, fmt.Sprint(i), fmt.Sprint(i), "hash") == nil
		}(i)
	}
	wg.Wait()
	close(success)
	n := 0
	for ok := range success {
		if ok {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("created %d administrators", n)
	}
}
func TestAtomicPasswordRevocation(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if err := s.CreateUser(ctx, "user", "user", "old", false); err != nil {
		t.Fatal(err)
	}
	for _, token := range []string{"current", "other"} {
		if err := s.SessionCreate(ctx, store.Session{Token: token, UserID: "user", ExpiresAt: time.Now().Add(time.Hour), CreatedAt: time.Now(), LastConnectedAt: time.Now()}); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.ChangePasswordAndRevoke(ctx, "user", "old", "new", "current"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SessionGet(ctx, "other"); err == nil {
		t.Fatal("other session survived")
	}
	if _, err := s.SessionGet(ctx, "current"); err != nil {
		t.Fatal(err)
	}
	if err := s.ChangePasswordAndRevoke(ctx, "user", "old", "unexpected", "current"); err == nil {
		t.Fatal("stale password write succeeded")
	}
}
