package collectoragent

import (
	"bufio"
	"context"
	"encoding/binary"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"certainstats/internal/collector"
	ctsdb "certainstats/internal/collector/tsdb"
	knotserver "certainstats/internal/collector/type/knot"
	ctxkeys "certainstats/internal/context"
	"certainstats/internal/security"
	"certainstats/internal/store/sqlite"

	"github.com/go-chi/chi/v5"
)

// fakeKnotCtl is a Knot control socket with one counter that the test raises.
type fakeKnotCtl struct {
	path    string
	mu      sync.Mutex
	queries uint64
	cmds    []string
	flags   map[string]string
}

// Item codes of Knot's control protocol: 16 + index.
const (
	kcCmd     = 16
	kcFlags   = 17
	kcSection = 19
	kcItem    = 20
	kcID      = 21
	kcZone    = 22
	kcType    = 25
	kcData    = 26
)

func knotItem(buf []byte, code byte, v string) []byte {
	buf = append(buf, code)
	buf = binary.BigEndian.AppendUint16(buf, uint16(len(v)))
	return append(buf, v...)
}

func (f *fakeKnotCtl) serve(c net.Conn) {
	defer c.Close()
	r := bufio.NewReader(c)
	if t, err := r.ReadByte(); err != nil || t != 1 {
		return
	}
	cmd, flags := "", ""
	for {
		code, err := r.ReadByte()
		if err != nil {
			return
		}
		if code < 16 {
			break // BLOCK ends the request
		}
		var n [2]byte
		io.ReadFull(r, n[:])
		v := make([]byte, binary.BigEndian.Uint16(n[:]))
		io.ReadFull(r, v)
		if code == kcCmd {
			cmd = string(v)
		}
		if code == kcFlags {
			flags = string(v)
		}
	}
	f.mu.Lock()
	f.cmds = append(f.cmds, cmd)
	if f.flags == nil {
		f.flags = map[string]string{}
	}
	f.flags[cmd] = flags
	q := f.queries
	f.mu.Unlock()
	var out []byte
	switch cmd {
	case "status":
		out = knotItem([]byte{1}, kcData, "3.4.2")
	case "stats":
		out = knotItem(knotItem(knotItem(knotItem([]byte{1}, kcSection, "mod-stats"), kcItem, "request-protocol"), kcID, "udp4"), kcData, strconv.FormatUint(q, 10))
		out = append(out, knotItem(knotItem(knotItem(knotItem([]byte{2}, kcSection, "mod-stats"), kcItem, "response-code"), kcID, "NXDOMAIN"), kcData, strconv.FormatUint(q/10, 10))...)
	case "zone-status":
		out = knotItem(knotItem(knotItem([]byte{1}, kcZone, "example.com."), kcType, "role"), kcData, "master")
		out = append(out, knotItem(knotItem(knotItem([]byte{2}, kcZone, "example.com."), kcType, "serial"), kcData, "2026111601")...)
	}
	c.Write(append(out, 3))
	io.Copy(io.Discard, r)
}

func TestKnotEndToEnd(t *testing.T) {
	dir := t.TempDir()
	db, err := sqlite.New(filepath.Join(dir, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := db.CreateUser(context.Background(), "u1", "u1", "x", false); err != nil {
		t.Fatal(err)
	}
	ks, err := knotserver.Open(filepath.Join(dir, "knot.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ks.Close() })
	tdb, err := ctsdb.Open(filepath.Join(dir, "collector_tsdb"), ctsdb.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { tdb.Close() })
	svc := collector.NewService(db, db)
	svc.Limits = security.NewIPLimiter(1e6, 1e6)
	if err := svc.Register(&knotserver.Type{Store: ks, TSDB: tdb, Collectors: svc}); err != nil {
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
	get := func(path string) map[string]any {
		t.Helper()
		resp, err := http.Get(srv.URL + "/api/collectors" + path)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		out := map[string]any{}
		json.NewDecoder(resp.Body).Decode(&out)
		return out
	}

	resp, err := http.Post(srv.URL+"/api/collectors", "application/json", strings.NewReader(`{"name":"ns1","types":["knot"]}`))
	if err != nil {
		t.Fatal(err)
	}
	var created map[string]any
	json.NewDecoder(resp.Body).Decode(&created)
	resp.Body.Close()

	sockDir, err := os.MkdirTemp("", "kne2e")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(sockDir) })
	fake := &fakeKnotCtl{path: filepath.Join(sockDir, "knot.sock"), queries: 12_000_000}
	ln, err := net.Listen("unix", fake.path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go fake.serve(c)
		}
	}()

	env := map[string]string{"HUB_URL": srv.URL, "TOKEN": created["token"].(string), "KNOT_SOCKET": fake.path}
	getenv := func(k string) string { return env[k] }
	if _, _, err := LoadConfig(getenv, "test"); err == nil || !strings.Contains(err.Error(), "COLLECT_KNOT=true") || !strings.Contains(err.Error(), "COLLECT_CROWDSEC=true") {
		t.Fatalf("with no switch the error must name each one: %v", err)
	}
	env["COLLECT_KNOT"] = "true"
	cfg, mods, err := LoadConfig(getenv, "test")
	if err != nil || len(mods) != 1 || mods[0].Name() != "knot" {
		t.Fatalf("config %v %v", mods, err)
	}
	if err := Check(context.Background(), cfg, mods); err != nil {
		t.Fatalf("check: %v", err)
	}
	once := func() {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if _, err := Session(ctx, cfg, mods, true); err != nil {
			t.Fatal(err)
		}
	}
	waitFor := func(what string, ok func() bool) {
		t.Helper()
		for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
			if ok() {
				return
			}
		}
		t.Fatalf("timed out waiting for %s", what)
	}

	once()
	waitFor("zones", func() bool {
		z := get("/knot/zones")
		zs, _ := z["zones"].([]any)
		return len(zs) == 1 && strings.Contains(strings.ToLower(jsonString(zs[0])), `"primary_serial":2026111601`)
	})
	waitFor("the first reading", func() bool {
		tot := get("/knot/totals?range=1h")
		return tot["collecting"] == true
	})
	// The first reading is a baseline: Knot's 12M lifetime queries are not counted.
	if tot := get("/knot/totals?range=1h"); tot["requests"] != float64(0) {
		t.Fatalf("baseline counted: %v", tot)
	}

	fake.mu.Lock()
	fake.queries += 250
	fake.mu.Unlock()
	time.Sleep(5 * time.Millisecond) // the next reading needs a later server timestamp
	once()
	waitFor("the increase", func() bool {
		tot := get("/knot/totals?range=1h")
		return tot["collecting"] == false && tot["requests"] == float64(250) && tot["nxdomain"] == float64(25)
	})
	over := get("/knot/")
	servers, _ := over["servers"].([]any)
	if len(servers) != 1 || !strings.Contains(jsonString(servers[0]), `"version":"3.4.2"`) || over["zone_count"] != float64(1) {
		t.Fatalf("overview %v", over)
	}
	if st := get("/" + created["collector_id"].(string) + "/knot"); st["status"] != "ok" || st["mod_stats"] != true {
		t.Fatalf("collector status %v", st)
	}
	if bad := get("/knot/totals?range=1h&collector=col_nope"); bad["error"] == nil {
		t.Fatal("an unknown collector was accepted")
	}
	fake.mu.Lock()
	defer fake.mu.Unlock()
	for _, c := range fake.cmds {
		if c != "status" && c != "stats" && c != "zone-status" {
			t.Fatalf("collector sent %q to Knot", c)
		}
	}
	if fake.flags["zone-status"] != "u" || fake.flags["stats"] != "F" {
		t.Fatalf("flags sent: %v", fake.flags)
	}
}

func jsonString(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}
