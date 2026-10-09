package agent

import (
	csctx "certainstats/internal/context"
	"certainstats/internal/metrics"
	"certainstats/internal/store/sqlite"
	"context"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/tsdb"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"
)

func TestRevokePreservesArchivedNetworkHistory(t *testing.T) {
	ctx := context.Background()
	store, e := sqlite.New(filepath.Join(t.TempDir(), "state.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer store.Close()
	if e = store.CreateUser(ctx, "owner", "owner", "hash", false); e != nil {
		t.Fatal(e)
	}
	if e = store.AgentProvision(ctx, "node", "owner", "token", "Node", "beszel"); e != nil {
		t.Fatal(e)
	}
	db, e := tsdb.Open(t.TempDir(), nil, nil, tsdb.DefaultOptions(), nil)
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	now := time.Now().UnixMilli()
	app := db.Appender(ctx)
	for _, name := range []string{"agent_cpu_usage", "network_monitor_attempt_count"} {
		if _, e = app.Append(0, labels.FromStrings("__name__", name, "agent_id", "node", "user_id", "owner"), now, 1); e != nil {
			t.Fatal(e)
		}
	}
	if e = app.Commit(); e != nil {
		t.Fatal(e)
	}
	cache := metrics.NewRealtimeCache()
	key := metrics.NetworkKey{Owner: "owner", Agent: "node", Monitor: "monitor"}
	cache.UpdateNetwork(key, metrics.NetworkSample{Timestamp: now, Attempts: 1})
	r := httptest.NewRequest("DELETE", "/agent?agent_id=node", nil)
	r = r.WithContext(context.WithValue(ctx, csctx.UserIDKey, "owner"))
	w := httptest.NewRecorder()
	RevokeAgentHandler(store, db, cache)(w, r)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body)
	}
	if cache.NetworkStatistics()["entries"].(int) != 0 {
		t.Fatal("node samples retained")
	}
	q, e := db.Querier(now-1, now+1)
	if e != nil {
		t.Fatal(e)
	}
	defer q.Close()
	for _, tc := range []struct {
		name string
		want bool
	}{{"agent_cpu_usage", false}, {"network_monitor_attempt_count", true}} {
		set := q.Select(ctx, false, nil, labels.MustNewMatcher(labels.MatchEqual, "__name__", tc.name))
		found := false
		for set.Next() {
			it := set.At().Iterator(nil)
			if it.Next() != 0 {
				found = true
			}
		}
		if e = set.Err(); e != nil {
			t.Fatal(e)
		}
		if found != tc.want {
			t.Fatalf("%s presence %t", tc.name, found)
		}
	}
}
