package networkservice

import (
	csctx "certainstats/internal/context"
	"certainstats/internal/metrics"
	nm "certainstats/internal/networkmonitor"
	"certainstats/internal/store"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
)

func TestHistoryMemoryMatchesTSDB(t *testing.T) {
	s, id := fixture(t)
	s.Cache = metrics.NewRealtimeCache()
	ctx := context.Background()
	for i, r := range []nm.Result{
		{Total: 2, Success: 1, Sum: 1000, Min: 1000, Max: 1000, Avg: 1000, Loss: 50},
		{Total: 8, Success: 8, Sum: 24000, Min: 3000, Max: 3000, Avg: 3000},
		{Total: 5, Success: 0, Loss: 100},
	} {
		r.LastProbeAt = time.Now().UnixMilli() + int64(i)
		r.SampleCount = int64(20 + i)
		if e := s.Accept(ctx, "owner", "node", "epoch", map[string]nm.Result{id: r}); e != nil {
			t.Fatal(e)
		}
	}
	m, e := s.Store.NetworkGet(ctx, "owner", id)
	if e != nil {
		t.Fatal(e)
	}
	end := time.Now().UnixMilli()
	start := end - 6*time.Hour.Milliseconds()
	raw, e := s.readSamples(ctx, *m, start, end)
	if e != nil {
		t.Fatal(e)
	}
	want := aggregate(raw, start, end)
	first, e := s.history(ctx, *m, start, end, false)
	if e != nil || !reflect.DeepEqual(want, first) {
		t.Fatal("cold mismatch", e)
	}
	before := s.Cache.NetworkStatistics()["tsdb_fallbacks"]
	second, e := s.history(ctx, *m, start, end, false)
	if e != nil || !reflect.DeepEqual(want, second) {
		t.Fatal("warm mismatch", e)
	}
	if s.Cache.NetworkStatistics()["tsdb_fallbacks"] != before {
		t.Fatal("cache hit read TSDB")
	}
	_, e = s.history(ctx, *m, end-48*time.Hour.Milliseconds(), end, false)
	if e != nil {
		t.Fatal(e)
	}
	if s.Cache.NetworkStatistics()["tsdb_fallbacks"] == before {
		t.Fatal("long request did not fall back")
	}
}
func TestHistoryResponseCoalescingOwnershipAndRevisions(t *testing.T) {
	s, id := fixture(t)
	s.Cache = metrics.NewRealtimeCache()
	request := func(owner string) *httptest.ResponseRecorder {
		r := historyRequest(context.Background(), owner, id, "hours=6")
		w := httptest.NewRecorder()
		s.History(w, r)
		return w
	}
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			w := request("owner")
			if w.Code != 200 {
				t.Errorf("%d %s", w.Code, w.Body)
			}
		}()
	}
	wg.Wait()
	if n := s.Cache.NetworkStatistics()["tsdb_fallbacks"].(uint64); n != 1 {
		t.Fatalf("identical builds performed %d reads", n)
	}
	if w := request("foreign"); w.Code != 404 {
		t.Fatal("ownership", w.Code)
	}
	r := nm.Result{Total: 1, Success: 1, Sum: 1000, Min: 1000, Max: 1000, Avg: 1000, LastProbeAt: time.Now().UnixMilli(), SampleCount: 1}
	if e := s.Accept(context.Background(), "owner", "node", "epoch", map[string]nm.Result{id: r}); e != nil {
		t.Fatal(e)
	}
	w := request("owner")
	var body struct {
		MonitorID string `json:"monitor_id"`
		Points    []nm.Point
	}
	if e := json.Unmarshal(w.Body.Bytes(), &body); e != nil {
		t.Fatal(e)
	}
	if body.MonitorID != id {
		t.Fatal("response is not for the requested monitor", body.MonitorID)
	}
	found := false
	for _, p := range body.Points {
		if p.Avg != nil {
			found = true
		}
	}
	if !found {
		t.Fatal("response not invalidated")
	}
	if e := s.Store.AgentDelete(context.Background(), "node", "owner"); e != nil {
		t.Fatal(e)
	}
	if w = request("owner"); w.Code != 200 {
		t.Fatal("archived history unavailable", w.Code)
	}
	revoked := &revokedStore{FullStore: s.Store}
	s.Store = revoked
	revoked.revoked.Store(true)
	if w = request("owner"); w.Code != 404 {
		t.Fatal("revoked monitor served cached history", w.Code)
	}
}
func TestHistoryFailedCommitDoesNotPopulate(t *testing.T) {
	s, id := fixture(t)
	s.Cache = metrics.NewRealtimeCache()
	s.TSDB = nil
	e := s.commit(context.Background(), nm.Batch{UserID: "owner", AgentID: "node", Timestamp: time.Now().UnixMilli(), Results: map[string]nm.Result{id: {Total: 1, Success: 1}}})
	if e == nil || s.Cache.NetworkStatistics()["entries"].(int) != 0 {
		t.Fatal("failed commit populated cache", e)
	}
}
func TestAggregateSparseFailuresAndLimit(t *testing.T) {
	start := int64(1000000)
	end := start + 180000
	points := aggregate([]metrics.NetworkSample{{Timestamp: start, Attempts: 2, Success: 1, Sum: 1000, Min: 1000, Max: 1000}, {Timestamp: start + 1000, Attempts: 8, Success: 8, Sum: 24000, Min: 3000, Max: 3000}, {Timestamp: start + 120000, Attempts: 4}}, start, end)
	if len(points) != 4 || *points[0].Loss != 10 || *points[0].Avg != 25.0/9 || *points[0].Min != 1 || *points[0].Max != 3 || points[1].Loss != nil || *points[2].Loss != 100 || points[2].Avg != nil || points[3].Avg != nil {
		t.Fatal(points)
	}
	if len(aggregate(nil, start, start+365*24*time.Hour.Milliseconds())) != 1000 {
		t.Fatal("point limit")
	}
}

// historyRequest builds a history GET for one monitor as the router delivers it.
func historyRequest(ctx context.Context, owner, monitorID, query string) *http.Request {
	r := httptest.NewRequest("GET", "/network-monitors/"+monitorID+"/history?"+query, nil)
	route := chi.NewRouteContext()
	route.URLParams.Add("id", monitorID)
	ctx = context.WithValue(ctx, chi.RouteCtxKey, route)
	return r.WithContext(context.WithValue(ctx, csctx.UserIDKey, owner))
}

type revokedStore struct {
	store.FullStore
	revoked atomic.Bool
}

func (s *revokedStore) NetworkGet(ctx context.Context, owner, id string) (*nm.Monitor, error) {
	if s.revoked.Load() {
		return nil, sql.ErrNoRows
	}
	return s.FullStore.NetworkGet(ctx, owner, id)
}

func TestHistoryRejectsPublicationAcrossConcurrentCommit(t *testing.T) {
	s, id := fixture(t)
	s.Cache = metrics.NewRealtimeCache()
	ctx := context.Background()
	m, e := s.Store.NetworkGet(ctx, "owner", id)
	if e != nil {
		t.Fatal(e)
	}
	r := historyRequest(ctx, "owner", id, "hours=6")
	timeRange, ok := metrics.ParsePrivateTimeRange(r)
	if !ok {
		t.Fatal("invalid range")
	}
	start, end := timeRange.StartMs, timeRange.EndMs
	oldKey := s.historyKey("owner", *m, start, end, true)
	var releases []func()
	for i := 0; i < 32; i++ {
		release, e := metrics.AcquirePrivateQuery(ctx)
		if e != nil {
			t.Fatal(e)
		}
		releases = append(releases, release)
	}
	defer func() {
		for _, release := range releases {
			release()
		}
	}()
	done := make(chan struct{})
	w := httptest.NewRecorder()
	go func() { s.History(w, r); close(done) }()
	deadline := time.Now().Add(2 * time.Second)
	for {
		s.buildMu.Lock()
		active := len(s.builds) > 0
		s.buildMu.Unlock()
		if active {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("build not started")
		}
		time.Sleep(time.Millisecond)
	}
	result := nm.Result{Total: 1, Success: 1, Sum: 1000, Min: 1000, Max: 1000, Avg: 1000, LastProbeAt: time.Now().UnixMilli(), SampleCount: 1}
	if e = s.Accept(ctx, "owner", "node", "epoch", map[string]nm.Result{id: result}); e != nil {
		t.Fatal(e)
	}
	for _, release := range releases {
		release()
	}
	releases = nil
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("build hung")
	}
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body)
	}
	if _, ok := csctx.GetCacheEntry(&csctx.MetricsCache, oldKey); ok {
		t.Fatal("stale revision published")
	}
	w = httptest.NewRecorder()
	s.History(w, r)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body)
	}
	newKey := s.historyKey("owner", *m, start, end, true)
	if _, ok := csctx.GetCacheEntry(&csctx.MetricsCache, newKey); !ok {
		t.Fatal("current revision not published")
	}
}
func TestHistoryWaiterCancellation(t *testing.T) {
	s := &Service{}
	r := httptest.NewRequest("GET", "/history", nil)
	finish, builder := s.beginHistory(httptest.NewRecorder(), r, "test")
	if !builder {
		t.Fatal("no builder")
	}
	defer finish()
	ctx, cancel := context.WithCancel(r.Context())
	cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, builder := s.beginHistory(httptest.NewRecorder(), r.WithContext(ctx), "test")
		if builder {
			t.Error("waiter became builder")
		}
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("canceled waiter blocked")
	}
}

func TestRelative24HourRangeWarmsAndHits(t *testing.T) {
	s, id := fixture(t)
	s.Cache = metrics.NewRealtimeCache()
	m, e := s.Store.NetworkGet(context.Background(), "owner", id)
	if e != nil {
		t.Fatal(e)
	}
	end := time.Now().UnixMilli() - 10
	start := end - (24 * time.Hour).Milliseconds()
	if _, e = s.history(context.Background(), *m, start, end, true); e != nil {
		t.Fatal(e)
	}
	before := s.Cache.NetworkStatistics()["tsdb_fallbacks"]
	end = time.Now().UnixMilli()
	start = end - (24 * time.Hour).Milliseconds()
	if _, e = s.history(context.Background(), *m, start, end, true); e != nil {
		t.Fatal(e)
	}
	if before != s.Cache.NetworkStatistics()["tsdb_fallbacks"] {
		t.Fatal("24h empty coverage did not hit")
	}
}

func TestRecoveryCacheDeduplicatesJournalTimestamp(t *testing.T) {
	s, id := fixture(t)
	s.Cache = metrics.NewRealtimeCache()
	ctx := context.Background()
	result := nm.Result{Total: 3, Success: 2, Sum: 4000, Min: 1000, Max: 3000, Avg: 2000, LastProbeAt: time.Now().UnixMilli(), SampleCount: 3}
	batch, e := s.Store.NetworkBegin(ctx, nm.Batch{ID: nm.ID(), UserID: "owner", AgentID: "node", Epoch: "epoch", Results: map[string]nm.Result{id: result}})
	if e != nil {
		t.Fatal(e)
	}
	if e = s.Recover(ctx); e != nil {
		t.Fatal(e)
	}
	key := metrics.NetworkKey{Owner: "owner", Agent: "node", Monitor: id}
	revision := s.Cache.NetworkRevision(key)
	if e = s.commit(ctx, *batch); e != nil {
		t.Fatal(e)
	}
	if s.Cache.NetworkRevision(key) != revision {
		t.Fatal("duplicate replay advanced revision")
	}
	m, e := s.Store.NetworkGet(ctx, "owner", id)
	if e != nil {
		t.Fatal(e)
	}
	end := time.Now().UnixMilli()
	start := end - time.Hour.Milliseconds()
	if _, e = s.history(ctx, *m, start, end, false); e != nil {
		t.Fatal(e)
	}
	samples, ok := s.Cache.GetNetwork(key, start, end)
	if !ok || len(samples) != 1 || samples[0].Timestamp != batch.Timestamp || samples[0].Attempts != 3 {
		t.Fatal(samples, ok)
	}
	bad := *batch
	bad.Timestamp -= int64(48 * time.Hour / time.Millisecond)
	if e = s.commit(ctx, bad); e == nil {
		t.Fatal("out of order append succeeded")
	}
	if s.Cache.NetworkRevision(key) != revision {
		t.Fatal("failed TSDB write advanced revision")
	}
}
