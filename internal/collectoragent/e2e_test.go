package collectoragent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"certainstats/internal/collector"
	csserver "certainstats/internal/collector/type/crowdsec"
	cswire "certainstats/internal/collector/type/crowdsec/wire"
	ctxkeys "certainstats/internal/context"
	"certainstats/internal/security"
	"certainstats/internal/store/sqlite"

	"github.com/go-chi/chi/v5"
)

// fakeLAPI serves alerts that started inside the requested window, like CrowdSec's
// Local API reading since/until as durations before now.
type fakeLAPI struct {
	mu        sync.Mutex
	alerts    []map[string]any
	logins    atomic.Int32
	requests  atomic.Int32
	expireOne atomic.Bool
}

func (f *fakeLAPI) handler(t *testing.T) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/watchers/login", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		if body["machine_id"] != "certainstats" || body["password"] != "s3cret" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		f.logins.Add(1)
		json.NewEncoder(w).Encode(map[string]any{"code": 200, "token": "jwt-" + strconv.Itoa(int(f.logins.Load())), "expire": time.Now().Add(time.Hour).Format(time.RFC3339)})
	})
	mux.HandleFunc("GET /v1/alerts", func(w http.ResponseWriter, r *http.Request) {
		f.requests.Add(1)
		if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer jwt-") || f.expireOne.CompareAndSwap(true, false) {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		now := time.Now()
		window := func(key string) time.Time {
			v := r.URL.Query().Get(key)
			if v == "" {
				return now
			}
			d, err := time.ParseDuration(v)
			if err != nil {
				t.Errorf("bad %s %q", key, v)
			}
			return now.Add(-d)
		}
		since, until := window("since"), window("until")
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		f.mu.Lock()
		defer f.mu.Unlock()
		out := []map[string]any{}
		for _, a := range f.alerts {
			start, _ := cswire.ParseTime(a["start_at"].(string))
			if !start.Before(since.Add(-time.Second)) && !start.After(until.Add(time.Second)) && len(out) < limit {
				out = append(out, a)
			}
		}
		json.NewEncoder(w).Encode(out)
	})
	return mux
}

func lapiAlert(i int, start time.Time) map[string]any {
	ip := fmt.Sprintf("192.0.2.%d", i%250+1)
	return map[string]any{
		"id": i, "uuid": fmt.Sprintf("uuid-%d", i), "machine_id": "web-01", "created_at": start.Add(time.Second).Format(time.RFC3339),
		"scenario": "crowdsecurity/ssh-bf", "events_count": 5, "start_at": start.Format(time.RFC3339), "stop_at": start.Format(time.RFC3339),
		"events":    []any{map[string]any{"meta": []any{map[string]string{"key": "log", "value": "secret log line"}}}},
		"source":    map[string]any{"scope": "Ip", "value": ip, "ip": ip, "cn": "DE", "as_number": "3320", "as_name": "DTAG"},
		"decisions": []any{map[string]any{"type": "ban", "origin": "crowdsec", "scope": "Ip", "value": ip, "duration": "3h"}},
	}
}

type e2e struct {
	srv     *httptest.Server
	lapi    *fakeLAPI
	store   *csserver.Store
	svc     *collector.Service
	token   string
	id      string
	getenv  func(string) string
	cleanup func()
}

func newE2E(t *testing.T) *e2e {
	dir := t.TempDir()
	db, err := sqlite.New(filepath.Join(dir, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := db.CreateUser(context.Background(), "u1", "u1", "x", false); err != nil {
		t.Fatal(err)
	}
	cs, err := csserver.Open(filepath.Join(dir, "crowdsec.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cs.Close() })
	svc := collector.NewService(db, db)
	svc.Limits = security.NewIPLimiter(1e6, 1e6) // tests dial often from one IP
	if err := svc.Register(&csserver.Type{Store: cs, Collectors: svc}); err != nil {
		t.Fatal(err)
	}
	r := chi.NewRouter()
	r.Route("/api/collectors", func(c chi.Router) {
		svc.Routes(c, func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxkeys.UserIDKey, "u1")))
			})
		})
	})
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	t.Cleanup(svc.Close)

	resp, err := http.Post(srv.URL+"/api/collectors", "application/json", strings.NewReader(`{"name":"edge","types":["crowdsec"]}`))
	if err != nil {
		t.Fatal(err)
	}
	var created map[string]any
	json.NewDecoder(resp.Body).Decode(&created)
	resp.Body.Close()

	lapi := &fakeLAPI{}
	lapiSrv := httptest.NewServer(lapi.handler(t))
	t.Cleanup(lapiSrv.Close)
	creds := filepath.Join(dir, "crowdsec-lapi.yaml")
	os.WriteFile(creds, []byte("url: "+lapiSrv.URL+"/\nlogin: certainstats\npassword: \"s3cret\"\n"), 0o600)
	env := map[string]string{"HUB_URL": srv.URL, "TOKEN": created["token"].(string), "COLLECT_CROWDSEC": "true", "CROWDSEC_LAPI_CREDENTIALS": creds}
	return &e2e{srv: srv, lapi: lapi, store: cs, svc: svc, token: env["TOKEN"], id: created["collector_id"].(string), getenv: func(k string) string { return env[k] }}
}

func (e *e2e) once(t *testing.T) {
	t.Helper()
	cfg, mods, err := LoadConfig(e.getenv, "test")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if _, err := Session(ctx, cfg, mods, true); err != nil {
		t.Fatal(err)
	}
}

func (e *e2e) get(t *testing.T, path string) map[string]any {
	t.Helper()
	resp, err := http.Get(e.srv.URL + "/api/collectors" + path)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	out := map[string]any{}
	json.NewDecoder(resp.Body).Decode(&out)
	return out
}

func TestCollectorEndToEnd(t *testing.T) {
	e := newE2E(t)
	now := time.Now().Truncate(time.Second)
	for i := 0; i < 450; i++ {
		e.lapi.alerts = append(e.lapi.alerts, lapiAlert(i, now.Add(-time.Duration(450-i)*time.Minute)))
	}
	e.once(t)
	sum := e.get(t, "/crowdsec/summary?hours=24")
	if sum["alerts"] != float64(450) {
		t.Fatalf("backfill stored %v alerts, want 450", sum["alerts"])
	}
	if e.lapi.logins.Load() != 1 {
		t.Fatalf("JWT must be reused, logged in %d times", e.lapi.logins.Load())
	}
	st := e.get(t, "/"+e.id+"/crowdsec")
	if st["status"] != "ok" || st["machine"] != "certainstats" || st["cursor"].(float64) < float64(now.Add(-2*time.Minute).UnixMilli()) {
		t.Fatalf("state %v", st)
	}
	page := e.get(t, "/crowdsec/alerts?hours=24&limit=25")
	if !strings.Contains(fmt.Sprint(page), "DTAG") || strings.Contains(fmt.Sprint(page), "secret log line") {
		t.Fatal("alerts must arrive without their raw log lines")
	}

	// A restart re-reads the overlap; the server de-duplicates and nothing is added.
	e.lapi.expireOne.Store(true)
	e.once(t)
	if sum := e.get(t, "/crowdsec/summary?hours=24"); sum["alerts"] != float64(450) {
		t.Fatalf("restart duplicated alerts: %v", sum["alerts"])
	}
	if e.lapi.logins.Load() < 2 {
		t.Fatal("an expired JWT must trigger a new login")
	}

	e.lapi.mu.Lock()
	e.lapi.alerts = append(e.lapi.alerts, lapiAlert(1000, now))
	e.lapi.mu.Unlock()
	e.once(t)
	if sum := e.get(t, "/crowdsec/summary?hours=24"); sum["alerts"] != float64(451) {
		t.Fatalf("new alert not stored: %v", sum["alerts"])
	}
}

// TestBackfillGoTimeFormat feeds start_at the way CrowdSec's Local API writes it
// (Go's time.String()), with created_at hours later, over several days and batches.
func TestBackfillGoTimeFormat(t *testing.T) {
	e := newE2E(t)
	now := time.Now().UTC().Truncate(time.Second)
	var newest time.Time
	for i := 0; i < 450; i++ {
		start := now.Add(-time.Duration(450-i) * 12 * time.Minute) // about 90 hours back
		a := lapiAlert(i, start)
		a["start_at"] = start.String()
		a["stop_at"] = start.Add(time.Minute).String()
		a["created_at"] = start.Add(3 * time.Hour).Format(time.RFC3339) // stored late, as a long bucket is
		e.lapi.alerts = append(e.lapi.alerts, a)
		newest = start
	}
	e.once(t)
	if sum := e.get(t, "/crowdsec/summary?hours=168"); sum["alerts"] != float64(450) {
		t.Fatalf("backfill stored %v alerts, want 450", sum["alerts"])
	}
	// The cursor follows real start times, not created_at.
	st := e.get(t, "/"+e.id+"/crowdsec")
	if st["cursor"] != float64(newest.UnixMilli()) {
		t.Fatalf("cursor %v, want newest start %d", st["cursor"], newest.UnixMilli())
	}
	page := e.get(t, "/crowdsec/alerts?hours=168&sort=time") // newest first
	items, _ := page["items"].([]any)
	if len(items) == 0 {
		t.Fatal("no alerts listed")
	}
	if top := items[0].(map[string]any); top["started_at"] != float64(newest.UnixMilli()) || top["stopped_at"] != float64(newest.Add(time.Minute).UnixMilli()) {
		t.Fatalf("newest alert times: %v", top)
	}
	e.once(t)
	if sum := e.get(t, "/crowdsec/summary?hours=168"); sum["alerts"] != float64(450) {
		t.Fatalf("second pass duplicated alerts: %v", sum["alerts"])
	}
}

func TestWindowNarrowing(t *testing.T) {
	e := newE2E(t)
	now := time.Now().Truncate(time.Second)
	// More alerts than one page, packed into the last few hours.
	for i := 0; i < 2500; i++ {
		e.lapi.alerts = append(e.lapi.alerts, lapiAlert(i, now.Add(-time.Duration(2500-i)*5*time.Second)))
	}
	e.once(t)
	if sum := e.get(t, "/crowdsec/summary?hours=24"); sum["alerts"] != float64(2500) {
		t.Fatalf("narrowing lost alerts: stored %v of 2500", sum["alerts"])
	}
}

func TestLAPIDownIsReported(t *testing.T) {
	e := newE2E(t)
	creds := e.getenv("CROWDSEC_LAPI_CREDENTIALS")
	os.WriteFile(creds, []byte("url: http://127.0.0.1:1\nlogin: certainstats\npassword: s3cret\n"), 0o600)
	e.once(t)
	// cs_status is not acknowledged, so the collector can exit before the server reads it.
	st := e.waitState(t, func(st map[string]any) bool { return st["status"] == "error" })
	if msg, _ := st["error"].(string); !strings.Contains(msg, "cannot reach the Local API") {
		t.Fatalf("state %v", st)
	}
}

// waitState polls the collector's CrowdSec state until want accepts it.
func (e *e2e) waitState(t *testing.T, want func(map[string]any) bool) map[string]any {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		st := e.get(t, "/"+e.id+"/crowdsec")
		if want(st) {
			return st
		}
		if time.Now().After(deadline) {
			t.Fatalf("state never matched: %v", st)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestConfigAndCheck(t *testing.T) {
	e := newE2E(t)
	env := map[string]string{"HUB_URL": e.srv.URL, "TOKEN": e.token}
	if _, _, err := LoadConfig(func(k string) string { return env[k] }, "test"); err == nil || !strings.Contains(err.Error(), "COLLECT_CROWDSEC=true") {
		t.Fatalf("no type switched on must name the switch: %v", err)
	}
	cfg, mods, err := LoadConfig(e.getenv, "test")
	if err != nil {
		t.Fatal(err)
	}
	if err := Check(context.Background(), cfg, mods); err != nil {
		t.Fatalf("check: %v", err)
	}
	cfg.Token = "csc_bad"
	if err := Check(context.Background(), cfg, mods); err == nil || !strings.Contains(err.Error(), "TOKEN") {
		t.Fatalf("bad token must be named: %v", err)
	}
	cfg.Token = e.token
	os.WriteFile(e.getenv("CROWDSEC_LAPI_CREDENTIALS"), []byte("url: x\n"), 0o600)
	_, mods, _ = LoadConfig(e.getenv, "test")
	if err := Check(context.Background(), cfg, mods); err == nil || !strings.Contains(err.Error(), "login and password") {
		t.Fatalf("broken credentials must be named: %v", err)
	}
}

func TestResync(t *testing.T) {
	e := newE2E(t)
	now := time.Now().Truncate(time.Second)
	e.lapi.alerts = append(e.lapi.alerts, lapiAlert(1, now.Add(-48*time.Hour)), lapiAlert(2, now.Add(-time.Minute)))
	cfg, mods, _ := LoadConfig(e.getenv, "test")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); Session(ctx, cfg, mods, false) }()
	deadline := time.Now().Add(10 * time.Second)
	for e.get(t, "/crowdsec/summary?hours=72")["alerts"] != float64(2) && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	before := e.lapi.requests.Load()
	resp, err := http.Post(e.srv.URL+"/api/collectors/"+e.id+"/crowdsec/resync", "application/json", bytes.NewReader([]byte(fmt.Sprintf(`{"since_ms":%d}`, now.Add(-72*time.Hour).UnixMilli()))))
	if err != nil || resp.StatusCode != http.StatusAccepted {
		t.Fatalf("resync: %v %v", err, resp.Status)
	}
	for e.lapi.requests.Load() == before && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if e.lapi.requests.Load() == before {
		t.Fatal("resync did not trigger a read")
	}
	cancel()
	<-done
}
