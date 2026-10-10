package networkservice

import (
	ap "certainstats/internal/agent_parser"
	"certainstats/internal/agent_parser/registry"
	"certainstats/internal/agentmeta"
	ctxkey "certainstats/internal/context"
	nm "certainstats/internal/networkmonitor"
	"certainstats/internal/store/sqlite"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/go-chi/chi/v5"
	"github.com/prometheus/prometheus/tsdb"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type futureProvider struct {
	ap.AgentParser
	calls *int
}

func (f *futureProvider) AgentType() string { return "future" }
func (f *futureProvider) ResolveCapabilities(r agentmeta.Runtime) agentmeta.Capabilities {
	caps := agentmeta.Capabilities{}
	for _, feature := range agentmeta.NetworkFeatures {
		caps[agentmeta.Key(feature)] = agentmeta.Capability{State: "supported"}
	}
	return agentmeta.Narrow(caps, r.ReportedCapabilities)
}
func (f *futureProvider) Apply(ctx context.Context, s nm.Session, op nm.Operation) (*nm.Result, error) {
	*f.calls++
	return nil, nil
}

type unusedSession struct{}

func (unusedSession) Request(context.Context, uint8, any) ([]byte, error) {
	return nil, errors.New("unused")
}
func fixture(t *testing.T) (*Service, string) {
	t.Helper()
	db, e := sqlite.New(filepath.Join(t.TempDir(), "state.db"))
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { db.Close() })
	tdb, e := tsdb.Open(filepath.Join(t.TempDir(), "tsdb"), nil, nil, tsdb.DefaultOptions(), nil)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { tdb.Close() })
	r := registry.NewRegistry()
	db.Providers = r
	ctx := context.Background()
	if e = db.CreateUser(ctx, "owner", "owner", "hash", false); e != nil {
		t.Fatal(e)
	}
	if e = db.AgentProvision(ctx, "node", "owner", "token", "Node", "beszel"); e != nil {
		t.Fatal(e)
	}
	db.AgentUpdateRuntime(ctx, "node", "owner", agentmeta.Runtime{AgentVersion: agentmeta.String("0.21.0")})
	db.AgentUpdateHeartbeat(ctx, "node", "owner")
	s := &Service{Store: db, Registry: r, TSDB: tdb}
	items, e := db.NetworkCreate(ctx, "owner", []string{"node"}, nm.Config{Target: "example.com", Protocol: "http"}, true)
	if e != nil {
		t.Fatal(e)
	}
	return s, items[0].ID
}
func TestFutureProviderAndSharedService(t *testing.T) {
	s, _ := fixture(t)
	calls := 0
	s.Registry.Register(&futureProvider{calls: &calls})
	ctx := context.Background()
	if e := s.Store.AgentProvision(ctx, "future-node", "owner", "future-token", "Future", "future"); e != nil {
		t.Fatal(e)
	}
	s.Store.AgentUpdateHeartbeat(ctx, "future-node", "owner")
	items, e := s.Store.NetworkCreate(ctx, "owner", []string{"future-node"}, nm.Config{Target: "internal.example", Protocol: "tcp"}, true)
	if e != nil {
		t.Fatal(e)
	}
	if e = s.Sync(ctx, "owner", "future-node", unusedSession{}, false); e != nil || calls != 1 {
		t.Fatal(calls, e)
	}
	if e = s.Sync(ctx, "owner", "future-node", unusedSession{}, false); e != nil || calls != 1 {
		t.Fatal(calls, e)
	}
	if e = s.Sync(ctx, "owner", "future-node", unusedSession{}, true); e != nil || calls != 2 {
		t.Fatal(calls, e)
	}
	r := nm.Result{Total: 4, Success: 4, Sum: 4000, Avg: 1000, Min: 1000, Max: 1000, LastProbeAt: time.Now().UnixMilli(), SampleCount: 4}
	if e = s.Accept(ctx, "owner", "future-node", "epoch", map[string]nm.Result{items[0].ID: r}); e != nil {
		t.Fatal(e)
	}
	m, e := s.Store.NetworkGet(ctx, "owner", items[0].ID)
	if e != nil || m.State != "active" || m.Latest == nil {
		t.Fatal(m, e)
	}
}
func TestHistoryWeightingAndReplay(t *testing.T) {
	s, id := fixture(t)
	ctx := context.Background()
	first := nm.Result{Total: 2, Success: 1, Sum: 1000, Avg: 1000, Min: 1000, Max: 1000, Loss: 50, LastProbeAt: time.Now().UnixMilli(), SampleCount: 2}
	if e := s.Accept(ctx, "owner", "node", "epoch", map[string]nm.Result{id: first}); e != nil {
		t.Fatal(e)
	}
	second := first
	second.Total = 8
	second.Success = 8
	second.Sum = 24000
	second.Min = 3000
	second.Max = 3000
	second.LastProbeAt++
	second.SampleCount += 8
	if e := s.Accept(ctx, "owner", "node", "epoch", map[string]nm.Result{id: second}); e != nil {
		t.Fatal(e)
	}
	m, e := s.Store.NetworkGet(ctx, "owner", id)
	if e != nil {
		t.Fatal(e)
	}
	now := time.Now().UnixMilli()
	points, e := s.history(ctx, *m, now-1000, now+10000, false)
	if e != nil || len(points) != 1 {
		t.Fatal(points, e)
	}
	p := points[0]
	if p.Avg == nil || *p.Avg != float64(25000)/9/1000 || p.Loss == nil || *p.Loss != 10 {
		t.Fatal(p)
	}
	second.LastProbeAt++
	second.Success = 0
	second.Sum = 0
	second.Loss = 100
	batch, e := s.Store.NetworkBegin(ctx, nm.Batch{ID: nm.ID(), UserID: "owner", AgentID: "node", Epoch: "epoch", Results: map[string]nm.Result{id: second}})
	if e != nil {
		t.Fatal(e)
	}
	if e = s.commit(ctx, *batch); e != nil {
		t.Fatal(e)
	}
	if e = s.commit(ctx, *batch); e != nil {
		t.Fatal("TSDB replay not idempotent", e)
	}
	pending, e := s.Store.NetworkPending(ctx, "")
	if e != nil || len(pending) != 0 {
		t.Fatal(pending, e)
	}
	if e = s.Recover(ctx); e != nil {
		t.Fatal(e)
	}
}
func TestHTTPContracts(t *testing.T) {
	s, id := fixture(t)
	router := chi.NewRouter()
	router.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxkey.UserIDKey, "owner")))
		})
	})
	router.Get("/network-monitors", s.List)
	router.Get("/network-monitors/targets", s.Targets)
	router.Post("/network-monitors", s.Create)
	router.Patch("/network-monitors/{id}", s.Update)
	router.Get("/network-monitors/{id}/history", s.History)
	router.Get("/network-monitors/{id}", s.Get)
	request := func(method, path, body string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest(method, path, strings.NewReader(body)))
		return w
	}
	w := request("GET", "/network-monitors?limit=10000", "")
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body)
	}
	var list struct {
		Limit int
		Items []nm.Monitor
	}
	json.Unmarshal(w.Body.Bytes(), &list)
	if list.Limit != 100 || len(list.Items) != 1 {
		t.Fatal(list)
	}
	for _, tc := range []struct {
		query         string
		status, count int
	}{
		{"target=EXAMPLE.COM&protocol=http", 200, 1},
		{"target=EXAMPLE.COM&protocol=dns", 200, 0},
		{"q=Node", 200, 1},
		{"protocol=smtp", 400, 0},
		{"target_exact=https%3A%2F%2Fexample.com&protocol=http", 200, 1},
		{"target_exact=example.com", 200, 0},
	} {
		w = request("GET", "/network-monitors?"+tc.query, "")
		if w.Code != tc.status {
			t.Fatalf("%s: %d %s", tc.query, w.Code, w.Body)
		}
		if w.Code == 200 {
			var out struct {
				Items []nm.Monitor
				Total int
			}
			if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
				t.Fatal(err)
			}
			if len(out.Items) != tc.count || out.Total != tc.count {
				t.Fatalf("%s: %s", tc.query, w.Body)
			}
		}
	}
	for _, tc := range []struct {
		query  string
		status int
		count  int
	}{
		{"", 200, 1},
		{"target=missing&target_exact=missing&q=missing", 200, 1},
		{"agent_id=foreign", 200, 0},
		{"protocol=dns", 200, 0},
		{"protocol=smtp", 400, 0},
		{"state=invalid", 400, 0},
	} {
		w = request("GET", "/network-monitors/targets?"+tc.query, "")
		if w.Code != tc.status {
			t.Fatalf("targets %s: %d %s", tc.query, w.Code, w.Body)
		}
		if w.Code == 200 {
			var out struct{ Items []string }
			if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
				t.Fatal(err)
			}
			if out.Items == nil || len(out.Items) != tc.count {
				t.Fatalf("targets %s: %s", tc.query, w.Body)
			}
		}
	}
	w = httptest.NewRecorder()
	s.Targets(w, httptest.NewRequest("GET", "/network-monitors/targets", nil))
	if w.Code != http.StatusUnauthorized {
		t.Fatal(w.Code, w.Body)
	}
	for _, body := range []string{`{"agent_ids":["node"],"target":"example.com","protocol":"http"}`, `{"agent_ids":["node"],"target":"example.com","protocol":"http","port":65536}`, `{"agent_ids":["node"],"target":"example.com","protocol":"http","unknown":true}`, `{"agent_ids":["node"],"target":"example.com","protocol":"http"} {}`} {
		w = request("POST", "/network-monitors", body)
		if w.Code != 400 && w.Code != 409 {
			t.Fatal(w.Code, w.Body)
		}
	}
	w = request("PATCH", "/network-monitors/"+id, `{"enabled":false}`)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body)
	}
	// History is one monitor per request, like /api/metrics; lists are never accepted.
	w = request("GET", "/network-monitors/"+id+"/history?hours=6", "")
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body)
	}
	w = request("GET", "/network-monitors/foreign/history?hours=6", "")
	if w.Code != 404 {
		t.Fatal(w.Code, w.Body)
	}
	w = request("GET", "/network-monitors/"+id+"/history?hours=0", "")
	if w.Code != 400 {
		t.Fatal(w.Code, w.Body)
	}
	w = request("GET", "/network-monitors/history?monitor_ids="+id, "")
	if w.Code == 200 {
		t.Fatal("comma-list history endpoint still served", w.Body)
	}
}

func TestHTTPIntervalPolicy(t *testing.T) {
	s, id := fixture(t)
	router := chi.NewRouter()
	router.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxkey.UserIDKey, "owner")))
		})
	})
	router.Post("/network-monitors", s.Create)
	router.Patch("/network-monitors/{id}", s.Update)
	request := func(method, path, body string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest(method, path, strings.NewReader(body)))
		return w
	}
	for i, tc := range []struct {
		name             string
		field            string
		status, interval int
	}{
		{"omitted", "", 201, 60},
		{"zero", `,"interval_seconds":0`, 201, 60},
		{"below minimum", `,"interval_seconds":59`, 400, 0},
		{"minimum", `,"interval_seconds":60`, 201, 60},
		{"maximum", `,"interval_seconds":3600`, 201, 3600},
		{"negative", `,"interval_seconds":-1`, 400, 0},
		{"above maximum", `,"interval_seconds":3601`, 400, 0},
		{"integer overflow", `,"interval_seconds":65596`, 400, 0},
	} {
		t.Run("create/"+tc.name, func(t *testing.T) {
			body := fmt.Sprintf(`{"agent_ids":["node"],"target":"target%d.example","protocol":"http"%s}`, i, tc.field)
			w := request("POST", "/network-monitors", body)
			if w.Code != tc.status {
				t.Fatalf("status %d: %s", w.Code, w.Body)
			}
			if w.Code == 201 {
				var out struct{ Items []nm.Monitor }
				if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
					t.Fatal(err)
				}
				if len(out.Items) != 1 || int(out.Items[0].Interval) != tc.interval {
					t.Fatalf("unexpected response: %s", w.Body)
				}
			}
		})
	}
	ctx := context.Background()
	result := nm.Result{Total: 4, Success: 4, Sum: 4000, Avg: 1000, Min: 1000, Max: 1000, LastProbeAt: time.Now().UnixMilli(), SampleCount: 4}
	if err := s.Accept(ctx, "owner", "node", "epoch", map[string]nm.Result{id: result}); err != nil {
		t.Fatal(err)
	}
	for _, body := range []string{`{"port":443}`, `{"enabled":false}`, `{"enabled":true}`} {
		w := request("PATCH", "/network-monitors/"+id, body)
		if w.Code != 200 {
			t.Fatalf("status %d: %s", w.Code, w.Body)
		}
		m, err := s.Store.NetworkGet(ctx, "owner", id)
		if err != nil || m.Interval != 60 || m.ID != id || m.Latest == nil {
			t.Fatalf("interval changed unexpectedly: %+v %v", m, err)
		}
	}
	for _, interval := range []int{-1, 0, 1, 30, 59, 3601, 65596} {
		w := request("PATCH", "/network-monitors/"+id, fmt.Sprintf(`{"interval_seconds":%d}`, interval))
		if w.Code != 400 {
			t.Fatalf("interval %d: status %d: %s", interval, w.Code, w.Body)
		}
	}
	for _, interval := range []int{60, 3600} {
		w := request("PATCH", "/network-monitors/"+id, fmt.Sprintf(`{"interval_seconds":%d}`, interval))
		if w.Code != 200 {
			t.Fatalf("interval %d: status %d: %s", interval, w.Code, w.Body)
		}
		m, err := s.Store.NetworkGet(ctx, "owner", id)
		if err != nil || int(m.Interval) != interval || m.ID != id || m.Latest == nil || m.Latest.LastProbeAt != result.LastProbeAt {
			t.Fatalf("interval edit lost identity or reading: %+v %v", m, err)
		}
	}
}

func TestLatestHTTPUnitsAndUnavailableLatency(t *testing.T) {
	latest := &nm.Latest{Result: nm.Result{Avg: 1500, Min: 1000, Max: 2000, Avg1h: 2500, Min1h: 1000, Max1h: 4000, Success: 2, Total: 3, SampleCount: 3, Loss1h: 10}}
	out := monitorJSON(nm.Monitor{Latest: latest})
	if out.Latest.Avg == nil || *out.Latest.Avg != 1.5 || *out.Latest.Avg1h != 2.5 {
		t.Fatal(out.Latest)
	}
	latest.Success = 0
	latest.Loss1h = 100
	out = monitorJSON(nm.Monitor{Latest: latest})
	raw, e := json.Marshal(out)
	if e != nil {
		t.Fatal(e)
	}
	if out.Latest.Avg != nil || out.Latest.Min1h != nil || !strings.Contains(string(raw), `"response_avg_ms":null`) || strings.Contains(string(raw), `response_avg_us`) {
		t.Fatal(string(raw))
	}
}
