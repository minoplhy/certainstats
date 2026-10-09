package routine

import (
	"certainstats/internal/agentmeta"
	response "certainstats/internal/base/response"
	csctx "certainstats/internal/context"
	"certainstats/internal/dashboard/accessrules"
	"certainstats/internal/metrics"
	nm "certainstats/internal/networkmonitor"
	"certainstats/internal/store"
	"certainstats/internal/store/sqlite"
	"certainstats/internal/ws"
	"certainstats/internal/ws/browserpb"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"golang.org/x/net/websocket"
	"google.golang.org/protobuf/proto"
)

// TestPulseCarriesOwnerNetworkReadings runs the real PulseSync against SQLite
// and checks the network part of each delivered frame.
func TestPulseCarriesOwnerNetworkReadings(t *testing.T) {
	for _, protobuf := range []bool{false, true} {
		t.Run(map[bool]string{false: "JSON", true: "Protobuf"}[protobuf], func(t *testing.T) {
			testPulseCarriesOwnerNetworkReadings(t, protobuf)
		})
	}
}

// pulseMonitor is the part of a delivered network snapshot the test checks.
type pulseMonitor struct {
	AgentID string
	State   string
	Latest  *struct {
		ResponseAvgMs *float64
		LossPct       float64
	}
}

func testPulseCarriesOwnerNetworkReadings(t *testing.T, protobuf bool) {
	ctx := context.Background()
	db, err := sqlite.New(t.TempDir() + "/state.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	monitors := map[string]nm.Monitor{}
	for _, owner := range []string{"one", "two"} {
		agentID := "agent-" + owner
		if err := db.CreateUser(ctx, owner, owner, "hash", false); err != nil {
			t.Fatal(err)
		}
		if err := db.AgentProvision(ctx, agentID, owner, "token-"+owner, owner, "beszel"); err != nil {
			t.Fatal(err)
		}
		runtime := agentmeta.Runtime{AgentVersion: agentmeta.String("0.21.0"), VersionSource: "test"}
		if err := db.AgentUpdateRuntime(ctx, agentID, owner, runtime); err != nil {
			t.Fatal(err)
		}
		if err := db.AgentUpdateHeartbeat(ctx, agentID, owner); err != nil {
			t.Fatal(err)
		}
		created, err := db.NetworkCreate(ctx, owner, []string{agentID}, nm.Config{Target: owner + ".example.com", Protocol: nm.ProtocolICMP}, true)
		if err != nil {
			t.Fatal(err)
		}
		monitors[owner] = created[0]
		if err := db.SessionCreate(ctx, store.Session{Token: "session-" + owner, UserID: owner, ExpiresAt: time.Now().Add(time.Hour), CreatedAt: time.Now(), LastConnectedAt: time.Now()}); err != nil {
			t.Fatal(err)
		}
	}

	// Only owner one has a committed reading: 2 ms average, 10% loss.
	reading := nm.Result{Avg: 2000, Loss: 10, Total: 10, Success: 9, Sum: 18000, SampleCount: 5, LastProbeAt: time.Now().UnixMilli()}
	batch, err := db.NetworkBegin(ctx, nm.Batch{ID: nm.ID(), UserID: "one", AgentID: "agent-one", Epoch: "epoch", Results: map[string]nm.Result{monitors["one"].ID: reading}})
	if err != nil {
		t.Fatal(err)
	}
	if err = db.NetworkComplete(ctx, *batch); err != nil {
		t.Fatal(err)
	}

	dash := store.Dashboard{DashboardID: "dash", UserID: "one", Slug: "public", Title: "Public", AccessRules: accessrules.AccessRules{"public": {AllowedFeatures: []string{"is_online"}, MaxDays: 1}}}
	if err := db.DashboardCreate(ctx, dash); err != nil {
		t.Fatal(err)
	}
	if err := db.DashboardAddAgents(ctx, dash, []response.CreateDashboardReqAgent{{AgentID: "agent-one"}}); err != nil {
		t.Fatal(err)
	}

	b := ws.NewAgentBroadcaster(protobuf)
	defer b.CloseAll()
	router := chi.NewRouter()
	for _, owner := range []string{"one", "two"} {
		router.Get("/"+owner, func(w http.ResponseWriter, r *http.Request) {
			r = r.WithContext(context.WithValue(r.Context(), csctx.UserIDKey, owner))
			ws.UIWebSocketHandler(b, db)(w, r)
		})
	}
	router.Get("/public/{id}", ws.PublicWebSocketHandler(db, b))
	server := httptest.NewServer(router)
	defer server.Close()

	dial := func(route, session string) *websocket.Conn {
		t.Helper()
		config, err := websocket.NewConfig("ws"+server.URL[4:]+route, server.URL)
		if err != nil {
			t.Fatal(err)
		}
		if protobuf {
			config.Protocol = []string{ws.BrowserProtocol}
		}
		if session != "" {
			config.Header.Set("Cookie", "session_token="+session)
		}
		conn, err := websocket.DialConfig(config)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { conn.Close() })
		return conn
	}
	one, two, public := dial("/one", "session-one"), dial("/two", "session-two"), dial("/public/dash", "")
	deadline := time.Now().Add(2 * time.Second)
	for len(b.GetActiveUserIDs()) != 2 || len(b.GetActiveDashIDs()) != 1 {
		if time.Now().After(deadline) {
			t.Fatal("subscriptions did not become active")
		}
		time.Sleep(time.Millisecond)
	}

	routine := &Routine{Store: db, Cache: metrics.NewRealtimeCache(), Broadcaster: b}
	routine.PulseSync(ctx)

	// readNetwork returns the frame's network monitors, or nil when the frame has none.
	readNetwork := func(conn *websocket.Conn) map[string]pulseMonitor {
		t.Helper()
		conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		var wire []byte
		if err := websocket.Message.Receive(conn, &wire); err != nil {
			t.Fatal(err)
		}
		if protobuf {
			var envelope browserpb.TelemetryEnvelope
			if err := proto.Unmarshal(wire, &envelope); err != nil {
				t.Fatal(err)
			}
			network := envelope.GetPulse().GetNetwork()
			if network == nil {
				return nil
			}
			out := map[string]pulseMonitor{}
			for id, m := range network.GetMonitors() {
				item := pulseMonitor{AgentID: m.GetAgentId(), State: m.GetState()}
				if latest := m.GetLatest(); latest != nil {
					item.Latest = &struct {
						ResponseAvgMs *float64
						LossPct       float64
					}{latest.GetResponseAvgMs().Value, latest.GetLossPct()}
				}
				out[id] = item
			}
			return out
		}
		var message struct {
			Network *map[string]struct {
				AgentID string `json:"agent_id"`
				State   string `json:"state"`
				Latest  *struct {
					ResponseAvgMs *float64 `json:"response_avg_ms"`
					LossPct       float64  `json:"loss_pct"`
				} `json:"latest"`
			} `json:"network"`
		}
		if err := json.Unmarshal(wire, &message); err != nil {
			t.Fatal(err)
		}
		if message.Network == nil {
			return nil
		}
		out := map[string]pulseMonitor{}
		for id, m := range *message.Network {
			item := pulseMonitor{AgentID: m.AgentID, State: m.State}
			if m.Latest != nil {
				item.Latest = &struct {
					ResponseAvgMs *float64
					LossPct       float64
				}{m.Latest.ResponseAvgMs, m.Latest.LossPct}
			}
			out[id] = item
		}
		return out
	}

	network := readNetwork(one)
	got, ok := network[monitors["one"].ID]
	if len(network) != 1 || !ok {
		t.Fatalf("owner one network: %+v", network)
	}
	if got.AgentID != "agent-one" || got.State != nm.StateActive {
		t.Fatalf("owner one monitor: %+v", got)
	}
	if got.Latest == nil || got.Latest.ResponseAvgMs == nil || *got.Latest.ResponseAvgMs != 2 || got.Latest.LossPct != 10 {
		t.Fatalf("owner one reading: %+v", got.Latest)
	}

	network = readNetwork(two)
	got, ok = network[monitors["two"].ID]
	if len(network) != 1 || !ok || got.Latest != nil || got.State != nm.StateWaiting {
		t.Fatalf("owner two network: %+v", network)
	}

	if network = readNetwork(public); network != nil {
		t.Fatalf("public dashboard received network readings: %+v", network)
	}
}
