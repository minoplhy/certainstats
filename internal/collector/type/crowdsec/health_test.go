package crowdsec

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"certainstats/internal/alertdelivery"
	"certainstats/internal/base/alert"
	"certainstats/internal/collector"
	"certainstats/internal/collector/type/crowdsec/wire"
	ctxkeys "certainstats/internal/context"
	"certainstats/internal/store"
	"certainstats/internal/store/sqlite"

	"github.com/go-chi/chi/v5"
)

// healthRig runs the CrowdSec type against a real main database, so rules and
// incidents are the Alerts page's own.
type healthRig struct {
	t      *testing.T
	typ    *Type
	db     *sqlite.Store
	live   map[string]bool
	events []string
}

func newHealthRig(t *testing.T) *healthRig {
	t.Helper()
	db, err := sqlite.New(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	for _, u := range []string{"u1", "u2"} {
		if err := db.CreateUser(context.Background(), u, u, "x", false); err != nil {
			t.Fatal(err)
		}
	}
	rig := &healthRig{t: t, db: db, live: map[string]bool{}}
	svc := collector.NewService(db, db)
	rig.typ = &Type{Store: openTest(t), Collectors: svc, Rules: db, Deliver: func(id string) { rig.events = append(rig.events, id) }}
	rig.typ.live = func(id string) (time.Time, bool) { return time.Now().Add(-time.Hour), rig.live[id] }
	if err := svc.Register(rig.typ); err != nil {
		t.Fatal(err)
	}
	return rig
}

func (r *healthRig) collector(id, user, name string) {
	r.t.Helper()
	c := store.Collector{CollectorID: id, UserID: user, Name: name, CreatedAt: 1}
	if err := r.db.CollectorCreate(context.Background(), c, "csc_"+id, collector.HashToken("csc_"+id), []string{wire.TypeName}); err != nil {
		r.t.Fatal(err)
	}
	r.live[id] = true
}

func (r *healthRig) report(id, user string, now int64, rep wire.Health) {
	r.t.Helper()
	if rep.Parts == nil && rep.Error == "" {
		rep.Parts = wire.Parts
	}
	if err := r.typ.Store.SaveReport(context.Background(), user, id, rep, now); err != nil {
		r.t.Fatal(err)
	}
}

func (r *healthRig) tick(now int64) {
	r.t.Helper()
	if err := r.typ.Evaluate(context.Background(), now); err != nil {
		r.t.Fatal(err)
	}
}

func (r *healthRig) rule(id, user string, trigger alert.TriggerType, threshold float64, target string, subjects ...alert.CrowdSecSubject) store.Alert {
	r.t.Helper()
	a := store.Alert{AlertID: id, UserID: user, Nickname: "rule " + id, Enabled: true, CrowdSec: subjects,
		Trigger: alert.Trigger{Type: trigger, Operator: alert.OpGreaterThan, Threshold: threshold, Duration: "0s"},
		Action:  alert.AlertAction{Type: alert.DestPreset, TargetID: target}}
	if err := r.db.AlertCreate(context.Background(), a); err != nil {
		r.t.Fatal(err)
	}
	return a
}

func (r *healthRig) open() []store.CrowdSecIncident {
	r.t.Helper()
	list, err := r.db.CrowdSecOpenIncidents(context.Background(), "")
	if err != nil {
		r.t.Fatal(err)
	}
	return list
}

func (r *healthRig) history(id string) *alert.AlertHistory {
	r.t.Helper()
	h, err := r.db.AlertHistoryGetByID(context.Background(), id, "u1")
	if err != nil {
		r.t.Fatal(err)
	}
	return h
}

// shared is a collector with one machine and a bouncer key used from two
// addresses. Unless healthy, the first entry's metrics carry no decision counts
// at all, as a firewall bouncer's do when its ipset fails.
func shared(now, metricsAt int64, healthy bool) wire.Health {
	brokenHeld := map[string]int64{}
	if healthy {
		brokenHeld = map[string]int64{"crowdsec": 1204, "CAPI": 30000}
	}
	return wire.Health{Active: map[string]int64{"crowdsec": 1300, "CAPI": 30000}, Components: []wire.Component{
		{Kind: wire.KindMachine, Name: "web-01", LastSeenMs: now, Validated: true},
		{Kind: wire.KindBouncer, Name: "fw@10.0.0.5", Type: "cs-firewall-bouncer", LastSeenMs: now, MetricsMs: metricsAt, Held: brokenHeld},
		{Kind: wire.KindBouncer, Name: "fw@10.0.0.6", Type: "cs-firewall-bouncer", LastSeenMs: now, MetricsMs: metricsAt, Held: map[string]int64{"crowdsec": 1204, "CAPI": 30000}},
	}}
}

func TestRuleOnASharedKeyFiresAndRecovers(t *testing.T) {
	rig := newHealthRig(t)
	ctx := context.Background()
	rig.collector("col_a", "u1", "edge")
	var got []string
	hook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		got = append(got, string(b))
	}))
	defer hook.Close()
	if err := rig.db.TargetCreate(ctx, alert.AlertTarget{TargetID: "tgt_1", UserID: "u1", Name: "Ops", Type: alert.DestWebhook, Destination: hook.URL}); err != nil {
		t.Fatal(err)
	}
	rig.rule("alert_1", "u1", "crowdsec_siblings", 10, "tgt_1", alert.CrowdSecSubject{CollectorID: "col_a", Scope: alert.ScopeGroup, Name: "fw"})
	now := time.Now().UnixMilli()

	rig.report("col_a", "u1", now, shared(now, now-2*minute, false))
	rig.tick(now)
	if len(rig.open()) != 0 {
		t.Fatal("one metrics report is not enough")
	}
	rig.report("col_a", "u1", now+minute, shared(now+minute, now-minute, false))
	rig.tick(now + minute)
	rig.tick(now + minute)
	open := rig.open()
	if len(open) != 1 || open[0].SubjectID != "col_a/bouncer/fw@10.0.0.5" || open[0].Trigger != "crowdsec_siblings" || len(rig.events) != 1 {
		t.Fatalf("one incident for the broken entry: %+v, %d events", open, len(rig.events))
	}
	h := rig.history(open[0].HistoryID)
	if h.SubjectKind != "crowdsec" || h.AgentID != "col_a" || h.AgentNickname != "fw@10.0.0.5 on edge" || h.Subject.Kind != "bouncer" ||
		!strings.Contains(h.Subject.Detail, "Holds 0 of the 31204 decisions") || !h.MonitoringAvailable || !h.RetryAvailable {
		t.Fatalf("incident in Alerts history: %+v", h)
	}
	// Delivery is the Alerts page's own, with the machine or bouncer named.
	if err := alertdelivery.Dispatch(ctx, rig.db, rig.events[0]); err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if len(got) != 1 || json.Unmarshal([]byte(got[0]), &payload) != nil || payload["nickname"] != "fw@10.0.0.5" || payload["collector"] != "edge" ||
		payload["subject_kind"] != "bouncer" || payload["status"] != "FIRING" || payload["trigger_type"] != "crowdsec_siblings" {
		t.Fatalf("webhook: %v", got)
	}
	// The popup shows it red, naming the rule.
	srv := rig.server()
	_, out := call(t, srv, "u1", "GET", "/crowdsec/components", "")
	var broken, good map[string]any
	for _, i := range out["items"].([]any) {
		switch i.(map[string]any)["name"] {
		case "fw@10.0.0.5":
			broken = i.(map[string]any)
		case "fw@10.0.0.6":
			good = i.(map[string]any)
		}
	}
	firing := broken["firing"].([]any)
	if broken["state"] != StateAlert || broken["group"] != "fw" || len(firing) != 1 || firing[0].(map[string]any)["rule"] != "rule alert_1" {
		t.Fatalf("popup row: %v", broken)
	}
	// Metrics with no decision counts reach the popup as an empty count, not as none.
	if held, ok := broken["held"].(map[string]any); !ok || len(held) != 0 {
		t.Fatalf("held of an entry whose metrics carry no counts: %v", broken)
	}
	if held := good["held"].(map[string]any); held["crowdsec"] != float64(1204) || good["active"].(map[string]any)["crowdsec"] != float64(1300) {
		t.Fatalf("per-origin held and active: %v", good)
	}

	// Fixed: one recovery.
	rig.report("col_a", "u1", now+2*minute, shared(now+2*minute, now+2*minute, true))
	rig.tick(now + 2*minute)
	rig.tick(now + 2*minute)
	if len(rig.open()) != 0 || len(rig.events) != 2 {
		t.Fatalf("recovery: %d open, %d events", len(rig.open()), len(rig.events))
	}
	if err := alertdelivery.Dispatch(ctx, rig.db, rig.events[1]); err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || !strings.Contains(got[1], `"status":"RESOLVED"`) {
		t.Fatalf("recovery webhook: %v", got)
	}
}

// The store keeps an empty decision count apart from none, and remembers that a
// bouncer holds decisions (it sent decision or packet counts), so the held check
// can tell a failing set from a bouncer in live mode that never counts.
func TestHoldsDecisionsIsRemembered(t *testing.T) {
	rig := newHealthRig(t)
	rig.collector("col_a", "u1", "edge")
	now := time.Now().UnixMilli()
	send := func(at int64, fw map[string]int64) map[string]DBSignals {
		rig.report("col_a", "u1", at, wire.Health{Components: []wire.Component{
			{Kind: wire.KindBouncer, Name: "fw", LastSeenMs: at, MetricsMs: at, Held: fw},
			// A firewall bouncer whose set was broken from the start: packets, never decision counts.
			{Kind: wire.KindBouncer, Name: "fw-new", LastSeenMs: at, MetricsMs: at, Held: map[string]int64{}, Processed: 12134},
			{Kind: wire.KindBouncer, Name: "nginx", LastSeenMs: at, MetricsMs: at, Held: map[string]int64{}},
			{Kind: wire.KindBouncer, Name: "quiet", LastSeenMs: at},
		}})
		list, err := rig.typ.Store.Components(context.Background(), "u1")
		if err != nil {
			t.Fatal(err)
		}
		out := map[string]DBSignals{}
		for _, c := range list {
			out[c.Name] = c.db()
		}
		return out
	}
	got := send(now, map[string]int64{"crowdsec": 5})
	if !got["fw"].HoldsDecisions || !got["fw-new"].HoldsDecisions || got["nginx"].HoldsDecisions || got["nginx"].Held == nil || got["quiet"].Held != nil {
		t.Fatalf("first report: %+v", got)
	}
	got = send(now+minute, map[string]int64{})
	if fw := got["fw"]; !fw.HoldsDecisions || fw.Held == nil || len(fw.Held) != 0 {
		t.Fatalf("fw sent counts before, so an empty report is remembered as such: %+v", fw)
	}
	if got["nginx"].HoldsDecisions {
		t.Fatal("nginx never sent decision or packet counts")
	}
}

// A lone firewall bouncer whose metrics never carried decision counts fires a
// held rule: its packet counts show it holds decisions, and it holds none.
func TestHeldRuleFiresOnALoneFirewallWithNoCounts(t *testing.T) {
	rig := newHealthRig(t)
	rig.collector("col_a", "u1", "deltarune")
	rig.rule("alert_1", "u1", "crowdsec_held", 50, "", alert.CrowdSecSubject{CollectorID: "col_a", Scope: alert.ScopeAll})
	now := time.Now().UnixMilli()
	for i := int64(0); i < 2; i++ {
		at := now + i*15*minute
		rig.report("col_a", "u1", at, wire.Health{Active: map[string]int64{"crowdsec": 40, "CAPI": 15000}, Components: []wire.Component{
			{Kind: wire.KindBouncer, Name: "fw", Type: "crowdsec-firewall-bouncer", LastSeenMs: at, MetricsMs: at - minute, Held: map[string]int64{}, Processed: 12134},
		}})
		rig.tick(at)
	}
	open := rig.open()
	if len(open) != 1 || open[0].SubjectID != "col_a/bouncer/fw" {
		t.Fatalf("want one held incident on fw, got %+v", open)
	}
}

func TestIncidentsCloseQuietly(t *testing.T) {
	rig := newHealthRig(t)
	ctx := context.Background()
	rig.collector("col_a", "u1", "edge")
	now := time.Now().UnixMilli()
	stale := wire.Health{Components: []wire.Component{
		{Kind: wire.KindMachine, Name: "web-01", LastSeenMs: now - time.Hour.Milliseconds(), Validated: true},
		{Kind: wire.KindMachine, Name: "web-02", LastSeenMs: now - time.Hour.Milliseconds(), Validated: true},
	}}
	rule := rig.rule("alert_1", "u1", "crowdsec_heartbeat", 10, "", alert.CrowdSecSubject{CollectorID: "col_a", Scope: alert.ScopeAll})
	rig.report("col_a", "u1", now, stale)
	rig.tick(now)
	if len(rig.open()) != 2 {
		t.Fatalf("both stale machines: %+v", rig.open())
	}
	// A threshold above the silence fires nothing more and keeps nothing open.
	// The collector offline: incidents stay as they are.
	rig.live["col_a"] = false
	rig.tick(now)
	if len(rig.open()) != 2 {
		t.Fatal("an unjudged collector changes nothing")
	}
	rig.live["col_a"] = true

	// web-02 deleted from CrowdSec: its incident closes.
	rig.report("col_a", "u1", now+minute, wire.Health{Parts: []string{wire.PartMachines}, Components: stale.Components[:1]})
	rig.tick(now)
	open := rig.open()
	if len(open) != 1 || open[0].SubjectID != "col_a/machine/web-01" {
		t.Fatalf("after removal: %+v", open)
	}
	// Muting web-01 closes it, quietly.
	comps, _ := rig.typ.Store.Components(ctx, "u1")
	if err := rig.typ.Store.SetMuted(ctx, "u1", comps[0].ID, true); err != nil {
		t.Fatal(err)
	}
	events := len(rig.events)
	rig.tick(now)
	if len(rig.open()) != 0 || len(rig.events) != events {
		t.Fatalf("muting must close without a notification: %+v", rig.open())
	}
	if err := rig.typ.Store.SetMuted(ctx, "u1", comps[0].ID, false); err != nil {
		t.Fatal(err)
	}
	rig.tick(now)
	if len(rig.open()) != 1 {
		t.Fatal("unmuted, it fires again")
	}
	// The rule switched off closes it.
	rule.Enabled = false
	if err := rig.db.AlertUpdate(ctx, rule, nil); err != nil {
		t.Fatal(err)
	}
	rig.tick(now)
	if len(rig.open()) != 0 {
		t.Fatal("a disabled rule keeps nothing open")
	}
	h := rig.history(func() string {
		list, _, _ := rig.db.AlertHistoryListFiltered(ctx, "u1", 1, 10, "", "all", "")
		return list[0].HistoryID
	}())
	if h.ClosedAt == nil || h.CloseReason != "monitoring_removed" {
		t.Fatalf("closed, not resolved: %+v", h)
	}
	// Removing the collector removes what health kept about it.
	if err := rig.db.CollectorRevoke(ctx, "col_a", "u1"); err != nil {
		t.Fatal(err)
	}
	rig.tick(now)
	if comps, _ := rig.typ.Store.Components(ctx, "u1"); len(comps) != 0 {
		t.Fatalf("components after removal: %d", len(comps))
	}
}

func TestRulesValidateSubjects(t *testing.T) {
	rig := newHealthRig(t)
	ctx := context.Background()
	rig.collector("col_a", "u1", "edge")
	rig.collector("col_b", "u2", "theirs")
	bad := []store.Alert{
		{AlertID: "a1", UserID: "u1", Nickname: "x", Trigger: alert.Trigger{Type: "crowdsec_pull", Threshold: 60}, Action: alert.AlertAction{Type: alert.DestPreset}},
		{AlertID: "a2", UserID: "u1", Nickname: "x", Trigger: alert.Trigger{Type: "crowdsec_pull", Threshold: 60}, Action: alert.AlertAction{Type: alert.DestPreset},
			CrowdSec: []alert.CrowdSecSubject{{CollectorID: "col_b", Scope: alert.ScopeAll}}},
		{AlertID: "a3", UserID: "u1", Nickname: "x", Trigger: alert.Trigger{Type: alert.TriggerTypeCPU, Threshold: 60}, Action: alert.AlertAction{Type: alert.DestPreset},
			CrowdSec: []alert.CrowdSecSubject{{CollectorID: "col_a", Scope: alert.ScopeAll}}},
	}
	for _, a := range bad {
		if err := rig.db.AlertCreate(ctx, a); err == nil {
			t.Errorf("%s must be refused", a.AlertID)
		}
	}
	rig.rule("a4", "u1", "crowdsec_pull", 60, "", alert.CrowdSecSubject{CollectorID: "col_a", Scope: alert.ScopeGroup, Name: "fw"},
		alert.CrowdSecSubject{CollectorID: "col_a", Scope: alert.ScopeMachine, Name: "web/01"})
	got, err := rig.db.AlertGetInfo(ctx, "a4", "u1")
	if err != nil || len(got.CrowdSec) != 2 || got.CrowdSec[0].Key() != "col_a/group/fw" || got.CrowdSec[1].Key() != "col_a/machine/web/01" {
		t.Fatalf("subjects saved: %+v %v", got.CrowdSec, err)
	}
	if s, ok := alert.ParseCrowdSecSubject("col_a/machine/web/01"); !ok || s.Name != "web/01" {
		t.Fatalf("a name may contain a slash: %+v", s)
	}
	for _, v := range []string{"col_a", "col_a/all/x", "col_a/group/", "/all", "col_a/nope/x"} {
		if _, ok := alert.ParseCrowdSecSubject(v); ok {
			t.Errorf("%q must not parse", v)
		}
	}
}

func (r *healthRig) server() *httptest.Server {
	router := chi.NewRouter()
	router.Route("/api/collectors", func(c chi.Router) {
		r.typ.Collectors.Routes(c, func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				next.ServeHTTP(w, req.WithContext(context.WithValue(req.Context(), ctxkeys.UserIDKey, req.Header.Get("X-User"))))
			})
		})
	})
	srv := httptest.NewServer(router)
	r.t.Cleanup(srv.Close)
	return srv
}

func call(t *testing.T, srv *httptest.Server, user, method, path, body string) (int, map[string]any) {
	t.Helper()
	req, _ := http.NewRequest(method, srv.URL+"/api/collectors"+path, strings.NewReader(body))
	req.Header.Set("X-User", user)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	out := map[string]any{}
	json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

func TestComponentRoutes(t *testing.T) {
	rig := newHealthRig(t)
	rig.collector("col_a", "u1", "edge")
	rig.collector("col_b", "u2", "other")
	rig.collector("col_c", "u1", "quiet")
	srv := rig.server()
	now := time.Now().UnixMilli()
	rig.report("col_a", "u1", now, shared(now, now, true))
	rig.report("col_b", "u2", now, wire.Health{Components: []wire.Component{{Kind: wire.KindBouncer, Name: "theirs", LastSeenMs: now}}})
	rig.tick(now)

	code, out := call(t, srv, "u1", "GET", "/crowdsec/components", "")
	if code != 200 || len(out["items"].([]any)) != 3 || strings.Contains(jsonText(out), "theirs") {
		t.Fatalf("components: %d %v", code, out)
	}
	byName := map[string]map[string]any{}
	for _, c := range out["collectors"].([]any) {
		byName[c.(map[string]any)["name"].(string)] = c.(map[string]any)
	}
	if byName["edge"]["health"] != healthOn || byName["quiet"]["health"] != healthOff {
		t.Fatalf("collectors: %v", out["collectors"])
	}

	code, out = call(t, srv, "u1", "GET", "/crowdsec/subjects", "")
	cols := out["collectors"].([]any)
	if code != 200 || len(cols) != 2 {
		t.Fatalf("subjects: %d %v", code, out)
	}
	edge := cols[0].(map[string]any)
	groups := edge["groups"].([]any)
	if edge["name"] != "edge" || jsonText(edge["machines"]) != `["web-01"]` || len(groups) != 1 ||
		jsonText(groups[0]) != `{"entries":["fw@10.0.0.5","fw@10.0.0.6"],"name":"fw"}` || jsonText(edge["bouncers"]) != `[]` {
		t.Fatalf("edge subjects: %v", edge)
	}

	id := ""
	for _, i := range rigItems(t, srv) {
		if i["name"] == "web-01" {
			id = jsonText(i["id"])
		}
	}
	if code, _ := call(t, srv, "u2", "PUT", "/crowdsec/components/"+id+"/mute", ""); code != 404 {
		t.Fatalf("another owner muted it: %d", code)
	}
	if code, _ := call(t, srv, "u1", "PUT", "/crowdsec/components/"+id+"/mute", ""); code != 204 {
		t.Fatalf("mute: %d", code)
	}
	for _, i := range rigItems(t, srv) {
		if i["name"] == "web-01" && i["state"] != StateMuted {
			t.Fatalf("muted row %v", i)
		}
	}
}

func rigItems(t *testing.T, srv *httptest.Server) []map[string]any {
	_, out := call(t, srv, "u1", "GET", "/crowdsec/components", "")
	var list []map[string]any
	for _, i := range out["items"].([]any) {
		list = append(list, i.(map[string]any))
	}
	return list
}

func TestPruneHealth(t *testing.T) {
	rig := newHealthRig(t)
	ctx := context.Background()
	rig.collector("col_a", "u1", "edge")
	rig.collector("col_b", "u2", "other")
	now := time.Now().UnixMilli()
	for id, user := range map[string]string{"col_a": "u1", "col_b": "u2"} {
		rig.report(id, user, now, wire.Health{Components: []wire.Component{{Kind: wire.KindMachine, Name: "m", LastSeenMs: now, Validated: true}}})
	}
	if err := rig.typ.Store.pruneHealth(ctx, []string{"u1"}); err != nil {
		t.Fatal(err)
	}
	if comps, _ := rig.typ.Store.Components(ctx, "u2"); len(comps) != 0 {
		t.Fatal("a deleted account's components must go")
	}
	if comps, _ := rig.typ.Store.Components(ctx, "u1"); len(comps) != 1 {
		t.Fatal("the remaining account keeps its components")
	}
}
