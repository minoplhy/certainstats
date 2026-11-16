package collectoragent

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	csserver "certainstats/internal/collector/type/crowdsec"

	_ "modernc.org/sqlite"
)

// crowdsecDB writes a SQLite file shaped like CrowdSec's database, with times in
// the two text forms its drivers produce. withMetrics is false for CrowdSec
// before 1.6.3, which has no metrics table.
func crowdsecDB(t *testing.T, now time.Time, withMetrics bool) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "crowdsec.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	sqliteTime := func(at time.Time) string { return at.UTC().Format("2006-01-02 15:04:05.999999999-07:00") }
	stmts := []string{
		`CREATE TABLE machines (id INTEGER PRIMARY KEY, created_at datetime, updated_at datetime, last_push datetime, last_heartbeat datetime,
			machine_id TEXT, password TEXT, ip_address TEXT, scenarios TEXT, version TEXT, is_validated bool, auth_type TEXT, osname TEXT, osfamily TEXT, osversion TEXT)`,
		`CREATE TABLE bouncers (id INTEGER PRIMARY KEY, created_at datetime, updated_at datetime, name TEXT, api_key TEXT, revoked bool, ip_address TEXT,
			type TEXT, version TEXT, last_pull datetime, auth_type TEXT, auto_created bool)`,
		`CREATE TABLE decisions (id INTEGER PRIMARY KEY, until datetime, scenario TEXT, type TEXT, scope TEXT, value TEXT, origin TEXT, simulated bool)`,
		fmt.Sprintf(`INSERT INTO machines (machine_id, last_heartbeat, ip_address, version, is_validated, osname, osversion, password) VALUES
			('web-01', '%s', '10.0.0.5', 'v1.8.2', 1, 'Debian', '13', 'secret-hash'),
			('web-02', '%s', '10.0.0.6', 'v1.7.6', 1, 'Ubuntu', '24.04', 'secret-hash'),
			('pending', NULL, '10.0.0.9', '', 0, NULL, NULL, 'secret-hash')`,
			sqliteTime(now.Add(-30*time.Second)), now.Add(-3*time.Hour).UTC().String()),
		fmt.Sprintf(`INSERT INTO bouncers (name, api_key, revoked, ip_address, type, version, last_pull) VALUES
			('fw-edge', 'key-hash', 0, '10.0.0.5', 'cs-firewall-bouncer', 'v0.0.38', '%s'),
			('old-nginx', 'key-hash', 1, '10.0.0.7', 'crowdsec-nginx-bouncer', 'v1.0', NULL),
			('fw-edge@10.0.0.8', 'key-hash', 0, '10.0.0.8', 'cs-firewall-bouncer', 'v0.0.38', '%[1]s')`, sqliteTime(now.Add(-8*time.Second))),
	}
	// 30 active bans from crowdsec on distinct IPs plus one duplicate, one expired,
	// one simulated, one captcha, one on a country, and a subscribed list.
	until, past := sqliteTime(now.Add(time.Hour)), sqliteTime(now.Add(-time.Hour))
	for i := 0; i < 30; i++ {
		stmts = append(stmts, fmt.Sprintf(`INSERT INTO decisions (until, scenario, type, scope, value, origin, simulated) VALUES ('%s', 'crowdsecurity/ssh-bf', 'ban', 'Ip', '192.0.2.%d', 'crowdsec', 0)`, until, i))
	}
	stmts = append(stmts,
		fmt.Sprintf(`INSERT INTO decisions (until, scenario, type, scope, value, origin, simulated) VALUES
			('%[1]s', 'crowdsecurity/http-probing', 'ban', 'Ip', '192.0.2.1', 'crowdsec', 0),
			('%[2]s', 'crowdsecurity/ssh-bf', 'ban', 'Ip', '198.51.100.1', 'crowdsec', 0),
			('%[1]s', 'crowdsecurity/ssh-bf', 'ban', 'Ip', '198.51.100.2', 'crowdsec', 1),
			('%[1]s', 'crowdsecurity/http-crawl', 'captcha', 'Ip', '198.51.100.3', 'crowdsec', 0),
			('%[1]s', 'manual', 'ban', 'Country', 'XX', 'cscli', 0),
			('%[1]s', 'firehol_level1', 'ban', 'Range', '203.0.113.0/24', 'lists', 0)`, until, past))
	if withMetrics {
		stmts = append(stmts,
			`CREATE TABLE metrics (id INTEGER PRIMARY KEY, generated_type TEXT, generated_by TEXT, received_at datetime, pushed_at datetime, payload TEXT)`,
			fmt.Sprintf(`INSERT INTO metrics (generated_type, generated_by, received_at, payload) VALUES
				('RC', 'fw-edge', '%s', '{"metrics":[{"items":[{"name":"active_decisions","unit":"ip","value":999,"labels":{"origin":"crowdsec","ip_type":"ipv4"}}]}]}'),
				('LP', 'web-01', '%s', '{"os":{"name":"Debian"}}'),
				('RC', 'fw-edge', '%s', '{"name":"fw-edge","type":"cs-firewall-bouncer","api_key":"must-not-leave","metrics":[{"meta":{"utc_now_timestamp":1,"window_size_seconds":900},"items":[
					{"name":"active_decisions","unit":"ip","value":28,"labels":{"origin":"crowdsec","ip_type":"ipv4"}},
					{"name":"active_decisions","unit":"ip","value":2,"labels":{"origin":"crowdsec","ip_type":"ipv6"}},
					{"name":"active_decisions","unit":"ip","value":1,"labels":{"origin":"lists:firehol_level1","ip_type":"ipv4"}},
					{"name":"dropped","unit":"packet","value":412,"labels":{"origin":"crowdsec","ip_type":"ipv4"}},
					{"name":"dropped","unit":"byte","value":90000,"labels":{"origin":"crowdsec","ip_type":"ipv4"}},
					{"name":"processed","unit":"packet","value":51000,"labels":{"ip_type":"ipv4"}}]}]}'),
				('RC', 'fw-edge@10.0.0.8', '%[3]s', '{"name":"fw-edge","type":"cs-firewall-bouncer","metrics":[{"meta":{"utc_now_timestamp":1,"window_size_seconds":900},"items":[
					{"name":"processed","unit":"packet","value":48000,"labels":{"ip_type":"ipv4"}}]}]}')`,
				sqliteTime(now.Add(-20*time.Minute)), sqliteTime(now.Add(-6*time.Minute)), sqliteTime(now.Add(-5*time.Minute))))
	}
	for _, stmt := range stmts {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("%v\n%s", err, stmt)
		}
	}
	return path
}

func (e *e2e) with(extra map[string]string) {
	base := e.getenv
	e.getenv = func(k string) string {
		if v, ok := extra[k]; ok {
			return v
		}
		return base(k)
	}
}

// waitHealth polls the server's record of the collector's health until want accepts it.
func (e *e2e) waitHealth(t *testing.T, want func(*csserver.CollectorHealth) bool) *csserver.CollectorHealth {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		h, err := e.store.Health(context.Background(), "u1", e.id)
		if err != nil {
			t.Fatal(err)
		}
		if want(h) {
			return h
		}
		if time.Now().After(deadline) {
			t.Fatalf("health never matched: %+v", h)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func (e *e2e) components(t *testing.T) map[string]*csserver.Component {
	t.Helper()
	list, err := e.store.Components(context.Background(), "u1")
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]*csserver.Component{}
	for _, c := range list {
		out[c.Kind+":"+c.Name] = c
	}
	return out
}

func TestHealthIsOffUnlessSwitchedOn(t *testing.T) {
	e := newE2E(t)
	// A path that fails loudly if anything tries to open it.
	e.with(map[string]string{"CROWDSEC_DB": t.TempDir()})
	e.once(t)
	// cs_status proves the session ran and was read by the server.
	e.waitState(t, func(st map[string]any) bool { return st["status"] == "ok" })
	h, err := e.store.Health(context.Background(), "u1", e.id)
	if err != nil || h.ReportedMs != 0 || h.Error != "" {
		t.Fatalf("no health report may be sent without CROWDSEC_HEALTH=true: %+v %v", h, err)
	}
	if len(e.components(t)) != 0 {
		t.Fatal("no components without the switch")
	}
	out := e.get(t, "/crowdsec/components")
	if cols, _ := out["collectors"].([]any); len(cols) != 1 || len(out["items"].([]any)) != 0 {
		t.Fatalf("components view: %v", out)
	}
}

func TestHealthReadsCrowdSecDatabase(t *testing.T) {
	e := newE2E(t)
	now := time.Now()
	path := crowdsecDB(t, now, true)
	if err := os.Chmod(path, 0o640); err != nil {
		t.Fatal(err)
	}
	before, _ := os.Stat(path)
	e.with(map[string]string{"CROWDSEC_HEALTH": "true", "CROWDSEC_DB": path})
	e.once(t)
	h := e.waitHealth(t, func(h *csserver.CollectorHealth) bool { return h.ReportedMs > 0 })
	if h.Error != "" || strings.Join(h.Parts, ",") != "machines,bouncers,metrics,decisions" {
		t.Fatalf("report: %+v", h)
	}
	// Distinct active bans on IPs and ranges, by origin; a list is named.
	if h.Active["crowdsec"] != 30 || h.Active["lists:firehol_level1"] != 1 || len(h.Active) != 2 {
		t.Fatalf("active decisions %v", h.Active)
	}
	comps := e.components(t)
	if len(comps) != 6 {
		t.Fatalf("want 3 machines and 3 bouncers, got %d", len(comps))
	}
	near := func(got int64, want time.Time) bool { d := got - want.UnixMilli(); return d > -2000 && d < 2000 }
	web1 := comps["machine:web-01"]
	if web1 == nil || web1.Version != "v1.8.2" || web1.IP != "10.0.0.5" || web1.OS != "Debian 13" {
		t.Fatalf("web-01 %+v", web1)
	}
	if !strings.Contains(string(web1.Signals[csserver.SourceDB]), `"validated":true`) {
		t.Fatalf("web-01 signals %s", web1.Signals[csserver.SourceDB])
	}
	var sig csserver.DBSignals
	read := func(c *csserver.Component) csserver.DBSignals {
		sig = csserver.DBSignals{}
		if err := jsonUnmarshal(c.Signals[csserver.SourceDB], &sig); err != nil {
			t.Fatal(err)
		}
		return sig
	}
	if !near(read(web1).LastSeenMs, now.Add(-30*time.Second)) {
		t.Fatalf("web-01 heartbeat %d", sig.LastSeenMs)
	}
	// The second machine's time is in Go's String() form.
	if !near(read(comps["machine:web-02"]).LastSeenMs, now.Add(-3*time.Hour)) {
		t.Fatalf("web-02 heartbeat %d", sig.LastSeenMs)
	}
	if read(comps["machine:pending"]); sig.LastSeenMs != 0 || sig.Validated {
		t.Fatalf("pending machine %+v", sig)
	}
	fw := comps["bouncer:fw-edge"]
	if fw.Type != "cs-firewall-bouncer" || fw.Version != "v0.0.38" {
		t.Fatalf("fw-edge %+v", fw)
	}
	// The newest metrics row wins; only counts leave the host.
	if read(fw); !near(sig.LastSeenMs, now.Add(-8*time.Second)) || !near(sig.MetricsMs, now.Add(-5*time.Minute)) ||
		sig.Held["crowdsec"] != 30 || sig.Held["lists:firehol_level1"] != 1 || sig.Dropped != 412 || sig.Processed != 51000 {
		t.Fatalf("fw-edge signals %+v", sig)
	}
	if strings.Contains(fmt.Sprint(e.get(t, "/crowdsec/components")), "must-not-leave") || strings.Contains(string(fw.Signals[csserver.SourceDB]), "must-not-leave") {
		t.Fatal("the raw metrics payload must not leave the host")
	}
	if !sig.HoldsDecisions {
		t.Fatal("fw-edge sent decision counts")
	}
	// Without usage metrics there are no counts; with metrics that carry none
	// (a firewall bouncer whose ipset fails) the counts are there and empty, and
	// its packet counts mark it as a bouncer that holds decisions.
	if read(comps["bouncer:old-nginx"]); !sig.Revoked || sig.LastSeenMs != 0 || sig.Held != nil {
		t.Fatalf("old-nginx %+v", sig)
	}
	if read(comps["bouncer:fw-edge@10.0.0.8"]); sig.Held == nil || len(sig.Held) != 0 || sig.Processed != 48000 || !sig.HoldsDecisions {
		t.Fatalf("fw-edge@10.0.0.8 must report holding nothing: %+v", sig)
	}

	// Nothing about the file changed, and no journal or lock file was left behind.
	after, _ := os.Stat(path)
	if !after.ModTime().Equal(before.ModTime()) || after.Mode() != before.Mode() || after.Size() != before.Size() {
		t.Fatalf("the database file was touched: %v %v -> %v %v", before.ModTime(), before.Mode(), after.ModTime(), after.Mode())
	}
	entries, _ := os.ReadDir(filepath.Dir(path))
	if len(entries) != 1 {
		t.Fatalf("files were created beside the database: %v", entries)
	}
}

func TestHealthPartsAndOlderCrowdSec(t *testing.T) {
	now := time.Now()

	// Before CrowdSec 1.6.3 there is no metrics table: that part is dropped, the rest works.
	e := newE2E(t)
	e.with(map[string]string{"CROWDSEC_HEALTH": "true", "CROWDSEC_DB": crowdsecDB(t, now, false)})
	e.once(t)
	h := e.waitHealth(t, func(h *csserver.CollectorHealth) bool { return h.ReportedMs > 0 })
	if h.Error != "" || strings.Join(h.Parts, ",") != "machines,bouncers,decisions" || len(e.components(t)) != 6 {
		t.Fatalf("without a metrics table: %+v", h)
	}

	// The host limits what is read.
	e = newE2E(t)
	e.with(map[string]string{"CROWDSEC_HEALTH": "TRUE", "CROWDSEC_DB": crowdsecDB(t, now, true), "CROWDSEC_HEALTH_READ": "machines"})
	e.once(t)
	h = e.waitHealth(t, func(h *csserver.CollectorHealth) bool { return h.ReportedMs > 0 })
	if strings.Join(h.Parts, ",") != "machines" || len(h.Active) != 0 || len(e.components(t)) != 3 {
		t.Fatalf("machines only: %+v, %d components", h, len(e.components(t)))
	}

	e = newE2E(t)
	e.with(map[string]string{"CROWDSEC_HEALTH": "true", "CROWDSEC_DB": crowdsecDB(t, now, true), "CROWDSEC_HEALTH_READ": "machines,logs"})
	e.once(t)
	h = e.waitHealth(t, func(h *csserver.CollectorHealth) bool { return h.ReportedMs > 0 })
	if !strings.Contains(h.Error, `unknown part "logs"`) {
		t.Fatalf("an unknown part must be named: %+v", h)
	}
}

func TestHealthDatabaseUnreadable(t *testing.T) {
	e := newE2E(t)
	missing := filepath.Join(t.TempDir(), "nope", "crowdsec.db")
	e.with(map[string]string{"CROWDSEC_HEALTH": "true", "CROWDSEC_DB": missing})
	e.once(t)
	h := e.waitHealth(t, func(h *csserver.CollectorHealth) bool { return h.ReportedMs > 0 })
	if !strings.Contains(h.Error, "cannot read CrowdSec's machines table") {
		t.Fatalf("report: %+v", h)
	}
	if _, err := os.Stat(missing); !os.IsNotExist(err) {
		t.Fatal("a missing database must never be created")
	}
	// Alerts keep flowing and the collector is not marked as failing.
	if st := e.waitState(t, func(st map[string]any) bool { return st["status"] != "" }); st["status"] != "ok" {
		t.Fatalf("health must not fail the collector: %v", st)
	}
	// --check warns, and passes.
	cfg, mods, err := LoadConfig(e.getenv, "test")
	if err != nil {
		t.Fatal(err)
	}
	if err := Check(context.Background(), cfg, mods); err != nil {
		t.Fatalf("an unreadable health database must not fail --check: %v", err)
	}
}

func jsonUnmarshal(raw []byte, v any) error { return json.Unmarshal(raw, v) }
