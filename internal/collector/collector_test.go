package collector

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"certainstats/internal/collector/wire"
	ctxkeys "certainstats/internal/context"
	"certainstats/internal/security"
	"certainstats/internal/store/sqlite"

	"github.com/fxamacker/cbor/v2"
	"github.com/go-chi/chi/v5"
	"golang.org/x/net/websocket"
)

// fakeType records what reaches it.
type fakeType struct {
	mu   sync.Mutex
	msgs []string
}

func (f *fakeType) Name() string   { return "fake" }
func (f *fakeType) Label() string  { return "Fake" }
func (f *fakeType) Prefix() string { return "fk_" }
func (f *fakeType) OnConnect(_ context.Context, c *Conn) error {
	return c.Send("fk_config", map[string]int{"n": 1})
}
func (f *fakeType) OnMessage(_ context.Context, _ *Conn, msgType string, _ cbor.RawMessage) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.msgs = append(f.msgs, msgType)
	if msgType == "fk_fatal" {
		return context.Canceled
	}
	return nil
}
func (f *fakeType) OnDisconnect(*Conn)           {}
func (f *fakeType) CollectorRoutes(r chi.Router) {}
func (f *fakeType) DataRoutes(r chi.Router) {
	r.Get("/ping", func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte("pong")) })
}
func (f *fakeType) seen() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.msgs...)
}

type harness struct {
	t   *testing.T
	srv *httptest.Server
	svc *Service
	db  *sqlite.Store
	typ *fakeType
}

func newHarness(t *testing.T) *harness {
	db, err := sqlite.New(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	ctx := context.Background()
	for _, u := range []string{"u1", "u2"} {
		if err := db.CreateUser(ctx, u, u, "x", false); err != nil {
			t.Fatal(err)
		}
	}
	svc := NewService(db, db)
	svc.Limits = security.NewIPLimiter(1e6, 1e6) // tests dial often from one IP
	typ := &fakeType{}
	if err := svc.Register(typ); err != nil {
		t.Fatal(err)
	}
	r := chi.NewRouter()
	r.Route("/api/collectors", func(c chi.Router) {
		svc.Routes(c, func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				user := r.Header.Get("X-Test-User")
				if user == "" {
					http.Error(w, "no user", 401)
					return
				}
				next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxkeys.UserIDKey, user)))
			})
		})
	})
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	t.Cleanup(svc.Close)
	return &harness{t: t, srv: srv, svc: svc, db: db, typ: typ}
}

func (h *harness) api(user, method, path string, body any) (*http.Response, map[string]any) {
	h.t.Helper()
	var buf bytes.Buffer
	if body != nil {
		json.NewEncoder(&buf).Encode(body)
	}
	req, _ := http.NewRequest(method, h.srv.URL+"/api/collectors"+path, &buf)
	req.Header.Set("X-Test-User", user)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		h.t.Fatal(err)
	}
	defer resp.Body.Close()
	out := map[string]any{}
	json.NewDecoder(resp.Body).Decode(&out)
	return resp, out
}

func (h *harness) dial(token string) (*websocket.Conn, error) {
	url := "ws" + strings.TrimPrefix(h.srv.URL, "http") + wire.ConnectPath
	cfg, _ := websocket.NewConfig(url, h.srv.URL)
	cfg.Header.Set(wire.HeaderToken, token)
	cfg.Header.Set(wire.HeaderVersion, "1.2.3")
	return websocket.DialConfig(cfg)
}

func recv(t *testing.T, c *websocket.Conn) wire.Envelope {
	t.Helper()
	c.SetReadDeadline(time.Now().Add(5 * time.Second))
	var data []byte
	if err := websocket.Message.Receive(c, &data); err != nil {
		t.Fatalf("receive: %v", err)
	}
	var env wire.Envelope
	if err := cbor.Unmarshal(data, &env); err != nil {
		t.Fatal(err)
	}
	return env
}

func send(t *testing.T, c *websocket.Conn, msgType string, body any) {
	t.Helper()
	data, err := wire.Marshal(msgType, body)
	if err != nil {
		t.Fatal(err)
	}
	if err := websocket.Message.Send(c, data); err != nil {
		t.Fatal(err)
	}
}

func waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	for i := 0; i < 200; i++ {
		if ok() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func TestCollectorLifecycle(t *testing.T) {
	h := newHarness(t)
	resp, created := h.api("u1", "POST", "/", map[string]any{"name": "edge", "types": []string{"fake"}})
	if resp.StatusCode != 201 || created["token"] == nil {
		t.Fatalf("create: %d %v", resp.StatusCode, created)
	}
	id, token := created["collector_id"].(string), created["token"].(string)
	if resp, _ := h.api("u1", "POST", "/", map[string]any{"name": "x", "types": []string{"nope"}}); resp.StatusCode != 400 {
		t.Fatal("unknown type must be rejected")
	}
	if resp, _ := h.api("u2", "GET", "/"+id, nil); resp.StatusCode != 404 {
		t.Fatal("another owner must not see the collector")
	}
	if resp, _ := h.api("u1", "GET", "/fake/ping", nil); resp.StatusCode != 200 {
		t.Fatal("type data routes must win over the collector ID route")
	}

	if _, err := h.dial("csc_wrong"); err == nil {
		t.Fatal("a bad token must be refused before the upgrade")
	}
	c, err := h.dial(token)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	hello := recv(t, c)
	var hb wire.Hello
	cbor.Unmarshal(hello.Body, &hb)
	if hello.Type != wire.MsgHello || hb.CollectorID != id || len(hb.Types) != 1 || hb.Types[0] != "fake" {
		t.Fatalf("hello %+v", hb)
	}
	if env := recv(t, c); env.Type != "fk_config" {
		t.Fatalf("type config not sent: %s", env.Type)
	}
	send(t, c, wire.MsgPing, wire.Ping{Version: "1.2.4", Types: []string{"fake"}})
	send(t, c, "zz_unknown", nil)
	send(t, c, "fk_hello", nil)
	waitFor(t, "fake message", func() bool { return len(h.typ.seen()) == 1 && h.typ.seen()[0] == "fk_hello" })

	_, view := h.api("u1", "GET", "/"+id, nil)
	types := view["types"].([]any)
	if view["connected"] != true || view["version"] != "1.2.4" || types[0].(map[string]any)["reported_on"] != true {
		t.Fatalf("view %v", view)
	}

	// A second connection replaces the first.
	c2, err := h.dial(token)
	if err != nil {
		t.Fatal(err)
	}
	defer c2.Close()
	recv(t, c2)
	c.SetReadDeadline(time.Now().Add(5 * time.Second))
	var data []byte
	for websocket.Message.Receive(c, &data) == nil {
	}
	// Resetting the token closes the live connection and the old token stops working.
	if resp, _ := h.api("u1", "POST", "/"+id+"/token", nil); resp.StatusCode != 200 {
		t.Fatal("reset token")
	}
	recv(t, c2) // fk_config
	c2.SetReadDeadline(time.Now().Add(5 * time.Second))
	if err := websocket.Message.Receive(c2, &data); err == nil {
		t.Fatal("token reset must close the connection")
	}
	if _, err := h.dial(token); err == nil {
		t.Fatal("old token must stop working")
	}
	waitFor(t, "disconnect", func() bool { return h.svc.Conn(id) == nil })

	if resp, _ := h.api("u1", "DELETE", "/"+id, nil); resp.StatusCode != 204 {
		t.Fatal("remove")
	}
	if _, list := h.api("u1", "GET", "/", nil); len(list["items"].([]any)) != 0 {
		t.Fatal("removed collector must leave the list")
	}
}

func TestDisabledTypeAndFatalMessage(t *testing.T) {
	h := newHarness(t)
	_, created := h.api("u1", "POST", "/", map[string]any{"name": "edge", "types": []string{"fake"}})
	id, token := created["collector_id"].(string), created["token"].(string)
	if resp, _ := h.api("u1", "DELETE", "/"+id+"/fake", nil); resp.StatusCode != 204 {
		t.Fatal("disable type")
	}
	c, err := h.dial(token)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	var hb wire.Hello
	cbor.Unmarshal(recv(t, c).Body, &hb)
	if len(hb.Types) != 0 {
		t.Fatalf("disabled type offered: %v", hb.Types)
	}
	send(t, c, "fk_hello", nil)
	send(t, c, wire.MsgPing, wire.Ping{})
	time.Sleep(50 * time.Millisecond)
	if len(h.typ.seen()) != 0 {
		t.Fatal("frames for a disabled type must be ignored")
	}
	h.api("u1", "PUT", "/"+id+"/fake", nil)
	c2, err := h.dial(token)
	if err != nil {
		t.Fatal(err)
	}
	defer c2.Close()
	recv(t, c2)
	recv(t, c2)
	send(t, c2, "fk_fatal", nil)
	c2.SetReadDeadline(time.Now().Add(5 * time.Second))
	var data []byte
	if err := websocket.Message.Receive(c2, &data); err == nil {
		t.Fatal("a type error must close the connection")
	}
	oversized := bytes.Repeat([]byte{0}, wire.MaxFrameBytes+10)
	c3, _ := h.dial(token)
	defer c3.Close()
	recv(t, c3)
	recv(t, c3)
	websocket.Message.Send(c3, oversized)
	c3.SetReadDeadline(time.Now().Add(5 * time.Second))
	if err := websocket.Message.Receive(c3, &data); err == nil {
		t.Fatal("an oversized frame must close the connection")
	}
}

func TestConnectAttemptsAreLimited(t *testing.T) {
	h := newHarness(t)
	h.svc.Limits = security.NewIPLimiter(60, 2)
	codes := []int{}
	for i := 0; i < 3; i++ {
		req, _ := http.NewRequest("GET", h.srv.URL+wire.ConnectPath, nil)
		req.Header.Set(wire.HeaderToken, "csc_wrong")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		codes = append(codes, resp.StatusCode)
	}
	if codes[0] != 401 || codes[1] != 401 || codes[2] != 429 {
		t.Fatalf("status codes %v, want 401 401 429", codes)
	}
}
