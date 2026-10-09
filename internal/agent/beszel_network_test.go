package agent

import (
	"certainstats/internal/agent_parser/registry"
	"certainstats/internal/metrics"
	nm "certainstats/internal/networkmonitor"
	"certainstats/internal/networkservice"
	"certainstats/internal/store/sqlite"
	"certainstats/internal/ws"
	"context"
	"github.com/prometheus/prometheus/tsdb"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// Optional real-agent interoperability check; ordinary tests require no downloads.
func TestRealBeszelNetworkMonitoring(t *testing.T) {
	binary := os.Getenv("BESZEL_TEST_AGENT_BINARY")
	if binary == "" {
		t.Skip("set BESZEL_TEST_AGENT_BINARY for the real-agent smoke test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	db, e := sqlite.New(filepath.Join(t.TempDir(), "state.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	tdb, e := tsdb.Open(filepath.Join(t.TempDir(), "tsdb"), nil, nil, tsdb.DefaultOptions(), nil)
	if e != nil {
		t.Fatal(e)
	}
	defer tdb.Close()
	db.CreateUser(ctx, "owner", "owner", "hash", false)
	db.AgentProvision(ctx, "real-node", "owner", "real-test-token", "Real Beszel", "beszel")
	key, e := GenerateAndSaveSSH(ctx, db, "real-node", "owner")
	if e != nil {
		t.Fatal(e)
	}
	r := registry.NewRegistry()
	db.Providers = r
	manager := ws.NewManager()
	defer manager.CloseAll()
	service := &networkservice.Service{Store: db, Registry: r, TSDB: tdb, WS: manager}
	hubServer := httptest.NewServer(BeszelWSHandler(db, tdb, manager, metrics.NewRealtimeCache(), service))
	defer hubServer.Close()
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }))
	defer target.Close()
	command := exec.CommandContext(ctx, binary)
	command.Env = append(os.Environ(), "KEY="+key, "TOKEN=real-test-token", "HUB_URL="+hubServer.URL, "DISABLE_SSH=true", "DATA_DIR="+t.TempDir(), "LOG_LEVEL=error")
	if e = command.Start(); e != nil {
		t.Fatal(e)
	}
	defer func() { cancel(); command.Wait() }()
	deadline := time.Now().Add(30 * time.Second)
	for {
		a, e := db.AgentGetByID(ctx, "real-node", "owner")
		if e == nil && a.AgentVersion != nil && a.IsOnline {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("real agent did not authenticate and report metadata")
		}
		time.Sleep(200 * time.Millisecond)
	}
	monitors, e := db.NetworkCreate(ctx, "owner", []string{"real-node"}, nm.Config{Target: target.URL, Protocol: "http", Interval: 60}, true)
	if e != nil {
		t.Fatal(e)
	}
	m := monitors[0]
	service.Changed("owner", "real-node", &nm.Operation{Action: 1, Config: m.Config, RunNow: true})
	for {
		latest, e := db.NetworkGet(ctx, "owner", m.ID)
		if e == nil && latest.Latest != nil && latest.Latest.SampleCount >= 3 && latest.Latest.Success > 0 {
			if latest.Latest.Loss1h != 0 {
				t.Fatal("unexpected local probe failure", latest.Latest)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("real agent did not report probe results")
		}
		// Request immediate probes to collect samples without waiting for the 60-second schedule.
		service.Changed("owner", "real-node", &nm.Operation{Action: 1, Config: m.Config, RunNow: true})
		if hub, ok := manager.GetHub("real-test-token"); ok {
			hub.SendTracked(ws.GetData, ws.DataRequestOptions{CacheTimeMs: 1000})
		}
		time.Sleep(300 * time.Millisecond)
	}
	config, state, e := db.NetworkConfigs(ctx, "owner", "real-node")
	if e != nil || len(config) != 1 || state.Desired != state.Ack {
		t.Fatal(config, state, e)
	}
	if e = db.NetworkArchive(ctx, "owner", m.ID); e != nil {
		t.Fatal(e)
	}
	service.Changed("owner", "real-node", &nm.Operation{Action: 2, Config: m.Config})
	deadline = time.Now().Add(5 * time.Second)
	for {
		_, state, e = db.NetworkConfigs(ctx, "owner", "real-node")
		if e == nil && state.Desired == state.Ack {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("real-agent removal did not synchronize")
		}
		time.Sleep(100 * time.Millisecond)
	}
}
