package routine

import (
	parser "certainstats/internal/agent_parser"
	response "certainstats/internal/base/response"
	csctx "certainstats/internal/context"
	"certainstats/internal/dashboard/accessrules"
	"certainstats/internal/metrics"
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

func TestPulseIsolationAndRevocation(t *testing.T) {
	for _, protobuf := range []bool{false, true} {
		t.Run(map[bool]string{false: "JSON", true: "Protobuf"}[protobuf], func(t *testing.T) { testPulseIsolationAndRevocation(t, protobuf) })
	}
}

func testPulseIsolationAndRevocation(t *testing.T, protobuf bool) {
	ctx := context.Background()
	db, err := sqlite.New(t.TempDir() + "/state.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	cache := metrics.NewRealtimeCache()
	b := ws.NewAgentBroadcaster(protobuf)
	defer b.CloseAll()
	for _, owner := range []string{"one", "two"} {
		if err := db.CreateUser(ctx, owner, owner, "hash", false); err != nil {
			t.Fatal(err)
		}
		if err := db.AgentProvision(ctx, "agent-"+owner, owner, "token-"+owner, owner, "ltstats"); err != nil {
			t.Fatal(err)
		}
		if err := db.SessionCreate(ctx, store.Session{Token: "session-" + owner, UserID: owner, ExpiresAt: time.Now().Add(time.Hour), CreatedAt: time.Now(), LastConnectedAt: time.Now()}); err != nil {
			t.Fatal(err)
		}
		cache.Update(owner, "agent-"+owner, &parser.ParsedData{Metrics: []parser.Telemetry{{Timestamp: time.Now(), CPUUsagePercent: 5, Disks: []parser.DiskTelemetry{{Path: "/secret", UsedBytes: 10, TotalBytes: 100, ReadBytes: 99, WriteBytes: 999}}}}})
	}
	dash := store.Dashboard{DashboardID: "dash", UserID: "one", Slug: "public", Title: "Public", AccessRules: accessrules.AccessRules{"public": {AllowedFeatures: []string{"is_online"}, AllowedMetrics: []string{"agent_disk_used"}, MaxDays: 1}}}
	agents := []response.CreateDashboardReqAgent{{AgentID: "agent-one"}}
	if err := db.DashboardCreate(ctx, dash); err != nil {
		t.Fatal(err)
	}
	if err := db.DashboardAddAgents(ctx, dash, agents); err != nil {
		t.Fatal(err)
	}
	identities, err := db.DashboardGetAgents(ctx, "dash", "one")
	if err != nil || len(identities) != 1 {
		t.Fatalf("identities: %v", err)
	}
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
	dial := func(route, cookie string) *websocket.Conn {
		t.Helper()
		config, err := websocket.NewConfig("ws"+server.URL[4:]+route, server.URL)
		if err != nil {
			t.Fatal(err)
		}
		if protobuf {
			config.Protocol = []string{ws.BrowserProtocol}
		}
		if cookie != "" {
			config.Header.Set("Cookie", "session_token="+cookie)
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
	routine := &Routine{Store: db, Cache: cache, Broadcaster: b}
	routine.PulseSync(ctx)
	read := func(conn *websocket.Conn) map[string]json.RawMessage {
		t.Helper()
		conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		var data map[string]json.RawMessage
		codec := websocket.Codec{Unmarshal: func(wire []byte, kind byte, _ any) error {
			if protobuf {
				if kind != websocket.BinaryFrame {
					t.Error("expected binary frame")
				}
				var envelope browserpb.TelemetryEnvelope
				if err := proto.Unmarshal(wire, &envelope); err != nil {
					return err
				}
				data = make(map[string]json.RawMessage)
				for id, s := range envelope.GetPulse().GetAgents() {
					raw, err := json.Marshal(s)
					if err != nil {
						return err
					}
					data[id] = raw
				}
				return nil
			}
			if kind != websocket.TextFrame {
				t.Error("expected text frame")
			}
			var message struct {
				Type string
				Data map[string]json.RawMessage
			}
			if err := json.Unmarshal(wire, &message); err != nil {
				return err
			}
			if message.Type != "agent_update" {
				t.Error("unexpected message type")
			}
			data = message.Data
			return nil
		}}
		if err := codec.Receive(conn, new(any)); err != nil {
			t.Fatal(err)
		}
		return data
	}
	for owner, conn := range map[string]*websocket.Conn{"one": one, "two": two} {
		pulse := read(conn)
		if len(pulse) != 1 || pulse["agent-"+owner] == nil {
			t.Fatalf("owner %s received another owner's data", owner)
		}
	}
	pulse := read(public)
	var item map[string]json.RawMessage
	if len(pulse) != 1 || json.Unmarshal(pulse[identities[0].PublicAgentID], &item) != nil {
		t.Fatal("public identity leaked")
	}
	for _, field := range []string{"agent_id", "metadata", "cpu_usage_percent", "CPUUsagePercent"} {
		if item[field] != nil {
			t.Fatalf("public field leaked: %s", field)
		}
	}
	var disks []map[string]json.RawMessage
	if protobuf {
		var list struct{ Items []map[string]json.RawMessage }
		json.Unmarshal(item["disks"], &list)
		disks = list.Items
	} else {
		json.Unmarshal(item["Disks"], &disks)
	}
	if len(disks) != 1 {
		t.Fatal("missing disk")
	}
	for _, field := range []string{"total_bytes", "read_bytes", "write_bytes"} {
		if disks[0][field] != nil {
			t.Fatalf("nested field leaked: %s", field)
		}
	}
	// Configuration revocation must close the Protobuf socket, too.
	dash.AccessRules = accessrules.AccessRules{"public": {}}
	if err := db.DashboardUpdate(ctx, dash, agents); err != nil {
		t.Fatal(err)
	}
	routine.PulseSync(ctx)
	public.SetReadDeadline(time.Now().Add(2 * time.Second))
	var wire []byte
	if err := websocket.Message.Receive(public, &wire); err == nil {
		t.Fatal("revoked public socket remained open")
	}
	// A session revocation affects only that session's sockets.
	read(one) // Drain the final authorized pulse before revoking the session.
	b.CloseSession("session-one")
	one.SetReadDeadline(time.Now().Add(2 * time.Second))
	if err := websocket.Message.Receive(one, &wire); err == nil {
		t.Fatal("revoked session remained open")
	}
	read(two) // Owner two still receives the pulse generated above.
}
