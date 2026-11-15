package crowdsec

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"certainstats/internal/base/alert"
	"certainstats/internal/collector"
	ctxkeys "certainstats/internal/context"
	"certainstats/internal/crowdsec/wire"
	"certainstats/internal/notifications"
	"certainstats/internal/store"
	"certainstats/internal/store/sqlite"

	"github.com/go-chi/chi/v5"
)

const minute = int64(60000)

func allParts() map[string]bool {
	set := map[string]bool{}
	for _, p := range wire.Parts {
		set[p] = true
	}
	return set
}

func component(kind, name string, sig DBSignals) *Component {
	raw, _ := json.Marshal(sig)
	return &Component{ID: 1, Kind: kind, Name: name, Signals: map[string]json.RawMessage{SourceDB: raw}}
}

func finding(c *Component, check string) *Finding {
	for i := range c.Findings {
		if c.Findings[i].Check == check {
			return &c.Findings[i]
		}
	}
	return nil
}

func TestQuietChecks(t *testing.T) {
	now := int64(1_800_000_000_000)
	ec := evalContext{Parts: allParts(), Now: now}

	m := component(wire.KindMachine, "web-01", DBSignals{LastSeenMs: now - 4*minute, Validated: true})
	m.evaluate(ec)
	if len(m.Findings) != 0 {
		t.Fatalf("a heartbeat 4 minutes old is healthy by default: %v", m.Findings)
	}
	m = component(wire.KindMachine, "web-01", DBSignals{LastSeenMs: now - 12*minute, Validated: true})
	m.evaluate(ec)
	f := finding(m, "heartbeat")
	if f == nil || f.Level != LevelAlert || f.Detail != "No heartbeat for 12m" || f.SinceMs != now {
		t.Fatalf("stale machine: %+v", f)
	}
	// Since is kept while the finding lasts.
	ec.Now = now + minute
	m.evaluate(ec)
	if f := finding(m, "heartbeat"); f == nil || f.SinceMs != now {
		t.Fatalf("since must not move: %+v", f)
	}
	if got := m.state(true, ""); got != StateAlert {
		t.Fatalf("state %q", got)
	}
	if got := m.state(false, ""); got != StateUnknown {
		t.Fatalf("an unjudged component is unknown, got %q", got)
	}

	// Level and threshold are configuration, not code.
	ec.Now = now
	ec.Settings = map[string]CheckSetting{"heartbeat": {Level: LevelCaution, Values: map[string]int{"minutes": 30}}}
	m.evaluate(ec)
	if len(m.Findings) != 0 {
		t.Fatalf("12 minutes is under a 30 minute threshold: %v", m.Findings)
	}
	ec.Settings = map[string]CheckSetting{"heartbeat": {Level: LevelCaution, Values: map[string]int{"minutes": 10}}}
	m.evaluate(ec)
	if f := finding(m, "heartbeat"); f == nil || f.Level != LevelCaution || m.state(true, "") != StateCaution {
		t.Fatalf("caution level: %+v", f)
	}
	ec.Settings = map[string]CheckSetting{"heartbeat": {Level: LevelOff}}
	m.evaluate(ec)
	if len(m.Findings) != 0 {
		t.Fatal("a check switched off finds nothing")
	}
	// A setting saved before a parameter existed, or out of range, falls back to the default.
	ec.Settings = map[string]CheckSetting{"heartbeat": {Level: "loud", Values: map[string]int{"minutes": -4}}}
	m.evaluate(ec)
	if f := finding(m, "heartbeat"); f == nil || f.Level != LevelAlert {
		t.Fatalf("defaults: %+v", f)
	}
	ec.Settings = nil

	// Not judged: muted, the collector's own login, never seen, not validated, revoked, part not read.
	stale := DBSignals{LastSeenMs: now - time.Hour.Milliseconds()*5, Validated: true}
	for name, tc := range map[string]struct {
		c    *Component
		ec   evalContext
		want string
	}{
		"muted":       {c: func() *Component { c := component(wire.KindMachine, "a", stale); c.Muted = true; return c }(), ec: ec, want: StateMuted},
		"self":        {c: component(wire.KindMachine, "certainstats", stale), ec: evalContext{Parts: allParts(), Now: now, Self: "certainstats"}, want: StateSelf},
		"never":       {c: component(wire.KindMachine, "a", DBSignals{Validated: true}), ec: ec, want: StateNever},
		"unvalidated": {c: component(wire.KindMachine, "a", DBSignals{LastSeenMs: stale.LastSeenMs}), ec: ec, want: StateOK},
		"revoked":     {c: component(wire.KindBouncer, "a", DBSignals{LastSeenMs: stale.LastSeenMs, Revoked: true}), ec: ec, want: StateOK},
		"unread part": {c: component(wire.KindMachine, "a", stale), ec: evalContext{Parts: map[string]bool{wire.PartBouncers: true}, Now: now}, want: StateOK},
	} {
		tc.c.evaluate(tc.ec)
		if len(tc.c.Findings) != 0 {
			t.Errorf("%s: must not be judged: %v", name, tc.c.Findings)
		}
		if got := tc.c.state(true, tc.ec.Self); got != tc.want {
			t.Errorf("%s: state %q, want %q", name, got, tc.want)
		}
	}

	b := component(wire.KindBouncer, "fw", DBSignals{LastSeenMs: now - 59*minute})
	b.evaluate(ec)
	if len(b.Findings) != 0 {
		t.Fatalf("a pull 59 minutes old is healthy by default: %v", b.Findings)
	}
	b = component(wire.KindBouncer, "fw", DBSignals{LastSeenMs: now - 3*time.Hour.Milliseconds()})
	b.evaluate(ec)
	if f := finding(b, "pull"); f == nil || f.Level != LevelAlert || f.Detail != "No pull for 3h" {
		t.Fatalf("stale bouncer: %+v", f)
	}
}

func TestHeldIsACaution(t *testing.T) {
	now := int64(1_800_000_000_000)
	ec := evalContext{Parts: allParts(), Now: now, Active: map[string]int64{"crowdsec": 100, "CAPI": 40000, "cscli": 3}}
	sig := DBSignals{LastSeenMs: now, MetricsMs: now - minute, Held: map[string]int64{"crowdsec": 10, "cscli": 0}}
	b := component(wire.KindBouncer, "fw", sig)

	b.evaluate(ec)
	if len(b.Findings) != 0 {
		t.Fatalf("one bad report is not enough: %v", b.Findings)
	}
	b.evaluate(ec) // the same report again counts once
	if len(b.Findings) != 0 {
		t.Fatalf("the same report must not be counted twice: %v", b.Findings)
	}
	sig.MetricsMs = now
	b.Signals[SourceDB], _ = json.Marshal(sig)
	out := b.evaluate(ec)
	f := finding(b, "held")
	if f == nil || f.Level != LevelCaution || !strings.Contains(f.Detail, "Holds 10 of 100 crowdsec decisions, may not be applying them") {
		t.Fatalf("second bad report: %+v", f)
	}
	if b.state(true, "") != StateCaution {
		t.Fatalf("state %q", b.state(true, ""))
	}
	// CAPI is not reported by the bouncer and cscli has too few active decisions: neither is compared.
	for _, o := range out {
		if o.Check.Name == "held" && (o.Finding == nil || strings.Contains(o.Finding.Detail, "CAPI") || strings.Contains(o.Finding.Detail, "cscli")) {
			t.Fatalf("outcome %+v", o.Finding)
		}
	}

	// A good report clears it.
	sig.MetricsMs, sig.Held = now+minute, map[string]int64{"crowdsec": 98}
	b.Signals[SourceDB], _ = json.Marshal(sig)
	b.evaluate(ec)
	if len(b.Findings) != 0 {
		t.Fatalf("recovered: %v", b.Findings)
	}

	// Metrics that are old, or the decisions part not being read, mean no judgement.
	sig.MetricsMs, sig.Held = now-3*time.Hour.Milliseconds(), map[string]int64{"crowdsec": 0}
	old := component(wire.KindBouncer, "fw", sig)
	old.evaluate(evalContext{Parts: allParts(), Now: now, Active: ec.Active, Settings: map[string]CheckSetting{"held": {Level: LevelAlert, Values: map[string]int{"reports": 1}}}})
	if finding(old, "held") != nil {
		t.Fatal("stale metrics must not be judged")
	}
	sig.MetricsMs = now
	unread := component(wire.KindBouncer, "fw", sig)
	unread.evaluate(evalContext{Parts: map[string]bool{wire.PartBouncers: true, wire.PartMetrics: true}, Now: now, Active: ec.Active,
		Settings: map[string]CheckSetting{"held": {Level: LevelAlert, Values: map[string]int{"reports": 1}}}})
	if finding(unread, "held") != nil {
		t.Fatal("held needs the decisions part")
	}
}

// TestCheckRegistrySeam registers a check fed by a second signal source, the way
// a later source such as bouncer log parsing would, and needs no other change.
func TestCheckRegistrySeam(t *testing.T) {
	RegisterCheck(&Check{
		Name: "apply_errors", Label: "Bouncer apply errors", Kind: wire.KindBouncer, DefaultLevel: LevelAlert,
		Params: []Param{{Key: "errors", Label: "Errors", Min: 1, Max: 100, Default: 1}},
		Eval: func(in evalInput) verdict {
			var log struct {
				Errors int `json:"errors"`
			}
			raw, ok := in.Component.Signals["log"]
			if !ok || json.Unmarshal(raw, &log) != nil {
				return verdict{}
			}
			return verdict{Judged: true, Bad: log.Errors >= in.Values["errors"], Detail: "ipset errors in the bouncer log"}
		},
	})
	defer delete(checks, "apply_errors")
	now := int64(1_800_000_000_000)
	b := component(wire.KindBouncer, "fw", DBSignals{LastSeenMs: now})
	b.Signals["log"] = json.RawMessage(`{"errors":3}`)
	b.evaluate(evalContext{Parts: allParts(), Now: now})
	if f := finding(b, "apply_errors"); f == nil || f.Level != LevelAlert {
		t.Fatalf("a registered check must run: %v", b.Findings)
	}
	var names []string
	for _, c := range Checks() {
		names = append(names, c.Name)
	}
	if strings.Join(names, ",") != "heartbeat,apply_errors,held,pull" {
		t.Fatalf("registry order %v", names)
	}
	if err := checks["apply_errors"].validate(CheckSetting{Level: LevelAlert, Values: map[string]int{"errors": 500}}); err == nil {
		t.Fatal("out of range value must be rejected")
	}
}

type stubTargets map[string]alert.AlertTarget

func (s stubTargets) TargetGetByID(_ context.Context, id, user string) (alert.AlertTarget, error) {
	t, ok := s[id]
	if !ok || t.UserID != user {
		return alert.AlertTarget{}, sql.ErrNoRows
	}
	return t, nil
}

func (s stubTargets) TargetList(_ context.Context, user string) ([]alert.AlertTarget, error) {
	var list []alert.AlertTarget
	for _, t := range s {
		if t.UserID == user {
			list = append(list, t)
		}
	}
	return list, nil
}

type healthRig struct {
	t      *testing.T
	typ    *Type
	db     *sqlite.Store
	live   map[string]bool
	sent   []notifications.NotificationContext
	failTo error
}

func newHealthRig(t *testing.T) *healthRig {
	t.Helper()
	dir := t.TempDir()
	db, err := sqlite.New(filepath.Join(dir, "state.db"))
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
	rig.typ = &Type{Store: openTest(t), Collectors: svc,
		Targets: stubTargets{"tgt_1": {TargetID: "tgt_1", UserID: "u1", Name: "Ops", Type: alert.DestWebhook, Destination: "https://example.com/hook"}}}
	rig.typ.live = func(id string) (time.Time, bool) { return time.Now().Add(-time.Hour), rig.live[id] }
	rig.typ.Notify = func(_ alert.AlertAction, n notifications.NotificationContext) error {
		if rig.failTo != nil {
			return rig.failTo
		}
		rig.sent = append(rig.sent, n)
		return nil
	}
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
	r.typ.Deliver(context.Background())
}

func (r *healthRig) events() []*HealthEvent {
	r.t.Helper()
	list, err := r.typ.Store.events(context.Background(), `1 = 1`)
	if err != nil {
		r.t.Fatal(err)
	}
	return list
}

func TestIncidentsAndNotifications(t *testing.T) {
	rig := newHealthRig(t)
	ctx := context.Background()
	rig.collector("col_a", "u1", "edge")
	now := time.Now().UnixMilli()
	if err := rig.typ.Store.SaveHealthSettings(ctx, "u1", "col_a", "tgt_1", nil); err != nil {
		t.Fatal(err)
	}
	fresh := func(at int64) wire.Health {
		return wire.Health{Active: map[string]int64{"crowdsec": 100}, Components: []wire.Component{
			{Kind: wire.KindMachine, Name: "web-01", LastSeenMs: at, Validated: true, Version: "v1.8.2"},
			{Kind: wire.KindBouncer, Name: "fw", LastSeenMs: at, MetricsMs: at, Held: map[string]int64{"crowdsec": 100}},
		}}
	}
	rig.report("col_a", "u1", now, fresh(now))
	rig.tick(now)
	if len(rig.events()) != 0 || len(rig.sent) != 0 {
		t.Fatal("healthy components open nothing")
	}

	// The bouncer stops pulling, and holds almost nothing on two reports.
	rep := fresh(now)
	rep.Components[1].LastSeenMs = now - 2*time.Hour.Milliseconds()
	rep.Components[1].Held = map[string]int64{"crowdsec": 1}
	rep.Components[1].MetricsMs = now + minute/2 // a new metrics push, not the healthy one again
	rig.report("col_a", "u1", now+minute, rep)
	rig.tick(now + minute)
	rep.Components[1].MetricsMs = now + minute
	rig.report("col_a", "u1", now+2*minute, rep)
	rig.tick(now + 2*minute)
	rig.tick(now + 2*minute)

	ev := rig.events()
	if len(ev) != 1 || ev[0].Check != "pull" || ev[0].Firing != notifySent || ev[0].ResolvedMs != 0 {
		t.Fatalf("exactly one incident, for the pull check: %+v", ev)
	}
	if len(rig.sent) != 1 {
		t.Fatalf("one notification, got %d", len(rig.sent))
	}
	n := rig.sent[0]
	if n.Status != "FIRING" || n.Nickname != "fw" || n.Collector != "edge" || n.SubjectKind != wire.KindBouncer || n.TriggerType != "crowdsec_pull" ||
		n.TriggerLabel != "CrowdSec: Bouncer pull" || !strings.HasPrefix(n.Detail, "No pull for") {
		t.Fatalf("notification %+v", n)
	}
	// The size comparison is a caution: shown, but no incident and no notification.
	comps, _ := rig.typ.Store.Components(ctx, "u1")
	var fw *Component
	for _, c := range comps {
		if c.Name == "fw" {
			fw = c
		}
	}
	if f := finding(fw, "held"); f == nil || f.Level != LevelCaution {
		t.Fatalf("held must be found as a caution: %v", fw.Findings)
	}

	// Recovery is announced once.
	rig.report("col_a", "u1", now+3*minute, fresh(now+3*minute))
	rig.tick(now + 3*minute)
	rig.tick(now + 3*minute)
	ev = rig.events()
	if len(ev) != 1 || ev[0].ResolvedMs == 0 || ev[0].Recovery != notifySent || len(rig.sent) != 2 || rig.sent[1].Status != "RESOLVED" || rig.sent[1].ResolvedAt == nil {
		t.Fatalf("recovery: %+v sent %d", ev[0], len(rig.sent))
	}

	// Raising the caution to alert makes it an incident.
	rep = fresh(now + 4*minute)
	rep.Components[1].Held = map[string]int64{"crowdsec": 2}
	rig.report("col_a", "u1", now+4*minute, rep)
	if err := rig.typ.Store.SaveHealthSettings(ctx, "u1", "col_a", "tgt_1", map[string]CheckSetting{"held": {Level: LevelAlert, Values: map[string]int{"reports": 1}}}); err != nil {
		t.Fatal(err)
	}
	rig.tick(now + 4*minute)
	if ev = rig.events(); len(ev) != 2 || ev[1].Check != "held" || rig.sent[len(rig.sent)-1].TriggerType != "crowdsec_held" {
		t.Fatalf("held at alert level: %+v", ev)
	}
	// Muting ends it quietly.
	if err := rig.typ.Store.SetMuted(ctx, "u1", fw.ID, true); err != nil {
		t.Fatal(err)
	}
	sent := len(rig.sent)
	rig.tick(now + 4*minute)
	if ev = rig.events(); ev[1].ResolvedMs == 0 || ev[1].Recovery != notifyNone || len(rig.sent) != sent {
		t.Fatalf("muting must close without a notification: %+v", ev[1])
	}
}

func TestNothingIsJudgedWithoutFreshReports(t *testing.T) {
	rig := newHealthRig(t)
	rig.collector("col_a", "u1", "edge")
	now := time.Now().UnixMilli()
	stale := wire.Health{Components: []wire.Component{{Kind: wire.KindMachine, Name: "web-01", LastSeenMs: now - time.Hour.Milliseconds(), Validated: true}}}

	// Collector offline.
	rig.live["col_a"] = false
	rig.report("col_a", "u1", now, stale)
	rig.tick(now)
	// The database could not be read.
	rig.live["col_a"] = true
	rig.report("col_a", "u1", now, wire.Health{Error: "cannot read CrowdSec's machines table"})
	rig.tick(now)
	// The last report is old: health was switched off, or the collector hangs.
	rig.report("col_a", "u1", now, stale)
	rig.tick(now + reportFresh.Milliseconds() + minute)
	if len(rig.events()) != 0 {
		t.Fatalf("no incident without a fresh, clean report from a connected collector: %+v", rig.events())
	}
	// An error keeps what was known.
	if comps, _ := rig.typ.Store.Components(context.Background(), "u1"); len(comps) != 1 {
		t.Fatalf("components after an error report: %d", len(comps))
	}
	rig.tick(now)
	if len(rig.events()) != 1 {
		t.Fatal("a fresh report from a connected collector is judged")
	}
}

func TestRemovalAndRetries(t *testing.T) {
	rig := newHealthRig(t)
	ctx := context.Background()
	rig.collector("col_a", "u1", "edge")
	now := time.Now().UnixMilli()
	rig.typ.Store.SaveHealthSettings(ctx, "u1", "col_a", "tgt_1", nil)
	two := wire.Health{Components: []wire.Component{
		{Kind: wire.KindMachine, Name: "web-01", LastSeenMs: now - time.Hour.Milliseconds(), Validated: true},
		{Kind: wire.KindMachine, Name: "web-02", LastSeenMs: now - time.Hour.Milliseconds(), Validated: true},
		{Kind: wire.KindBouncer, Name: "fw", LastSeenMs: now},
	}}
	rig.report("col_a", "u1", now, two)

	// A target that keeps failing is tried a bounded number of times.
	rig.failTo = errors.New("webhook returned non-2xx status code: 500")
	for i := 0; i < maxNotifyTries+3; i++ {
		rig.tick(now)
	}
	ev := rig.events()
	if len(ev) != 2 || ev[0].Firing != notifyFailed || ev[0].FiringTries != maxNotifyTries || !strings.Contains(ev[0].Error, "500") {
		t.Fatalf("retry cap: %+v", ev[0])
	}
	rig.failTo = nil

	// web-02 is deleted from CrowdSec: gone here too, its incident closed quietly.
	// A report that did not read bouncers leaves the bouncer alone.
	rig.report("col_a", "u1", now+minute, wire.Health{Parts: []string{wire.PartMachines}, Components: two.Components[:1]})
	comps, _ := rig.typ.Store.Components(ctx, "u1")
	if len(comps) != 2 {
		t.Fatalf("want web-01 and fw, got %d components", len(comps))
	}
	for _, e := range rig.events() {
		if e.Name == "web-02" && (e.ResolvedMs == 0 || e.Recovery != notifyNone) {
			t.Fatalf("removed component's incident: %+v", e)
		}
	}
	if len(rig.sent) != 0 {
		t.Fatal("nothing was ever delivered")
	}

	// Removing the collector removes everything health kept about it.
	if err := rig.db.CollectorRevoke(ctx, "col_a", "u1"); err != nil {
		t.Fatal(err)
	}
	rig.tick(now + minute)
	comps, _ = rig.typ.Store.Components(ctx, "u1")
	if len(comps) != 0 || len(rig.events()) != 0 {
		t.Fatalf("after removal: %d components, %d events", len(comps), len(rig.events()))
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

func TestHealthRoutes(t *testing.T) {
	rig := newHealthRig(t)
	rig.collector("col_a", "u1", "edge")
	rig.collector("col_b", "u2", "other")
	rig.collector("col_c", "u1", "quiet")
	srv := rig.server()
	now := time.Now().UnixMilli()
	rig.report("col_a", "u1", now, wire.Health{Parts: []string{wire.PartMachines, wire.PartBouncers}, Components: []wire.Component{
		{Kind: wire.KindMachine, Name: "web-01", LastSeenMs: now - time.Hour.Milliseconds(), Validated: true, IP: "10.0.0.5"},
		{Kind: wire.KindBouncer, Name: "fw", LastSeenMs: now, Type: "cs-firewall-bouncer"},
	}})
	rig.report("col_b", "u2", now, wire.Health{Components: []wire.Component{{Kind: wire.KindBouncer, Name: "theirs", LastSeenMs: now}}})
	rig.tick(now)

	code, out := call(t, srv, "u1", "GET", "/crowdsec/components", "")
	items, _ := out["items"].([]any)
	if code != 200 || len(items) != 2 {
		t.Fatalf("components: %d %v", code, out)
	}
	if strings.Contains(jsonText(out), "theirs") {
		t.Fatal("another owner's components leaked")
	}
	machine := items[0].(map[string]any)
	if machine["name"] != "web-01" || machine["state"] != StateAlert || machine["collector"] != "edge" || machine["ip"] != "10.0.0.5" {
		t.Fatalf("machine row %v", machine)
	}
	f := machine["findings"].([]any)[0].(map[string]any)
	if f["label"] != "Machine heartbeat" || f["notify"] != notifyNone {
		t.Fatalf("finding %v", f)
	}
	// col_a reports, with the held check unavailable because metrics and decisions are not read;
	// col_c is connected but its host has not switched health on.
	byName := map[string]map[string]any{}
	for _, c := range out["collectors"].([]any) {
		byName[c.(map[string]any)["name"].(string)] = c.(map[string]any)
	}
	if byName["edge"]["health"] != healthOn || !strings.Contains(jsonText(byName["edge"]["unavailable"]), `"check":"held"`) {
		t.Fatalf("edge %v", byName["edge"])
	}
	if byName["quiet"]["health"] != healthOff {
		t.Fatalf("quiet %v", byName["quiet"])
	}

	// Mute: owner only.
	id := jsonText(machine["id"])
	if code, _ := call(t, srv, "u2", "PUT", "/crowdsec/components/"+id+"/mute", ""); code != 404 {
		t.Fatalf("another owner muted it: %d", code)
	}
	if code, _ := call(t, srv, "u1", "PUT", "/crowdsec/components/"+id+"/mute", ""); code != 204 {
		t.Fatalf("mute: %d", code)
	}
	_, out = call(t, srv, "u1", "GET", "/crowdsec/components", "")
	if m := out["items"].([]any)[0].(map[string]any); m["state"] != StateMuted || len(m["findings"].([]any)) != 0 {
		t.Fatalf("muted row %v", m)
	}
	if code, _ := call(t, srv, "u1", "DELETE", "/crowdsec/components/"+id+"/mute", ""); code != 204 {
		t.Fatalf("unmute: %d", code)
	}

	// Settings: the registry, defaults, validation, and the owner's targets only.
	code, out = call(t, srv, "u1", "GET", "/col_a/crowdsec/health", "")
	if code != 200 || len(out["registry"].([]any)) != 3 || len(out["targets"].([]any)) != 1 {
		t.Fatalf("settings: %d %v", code, out)
	}
	if held := out["checks"].(map[string]any)["held"].(map[string]any); held["level"] != LevelCaution || held["values"].(map[string]any)["percent"] != float64(50) {
		t.Fatalf("held defaults %v", held)
	}
	for body, want := range map[string]string{
		`{"checks":{"nope":{"level":"alert"}}}`:                        "Unknown check",
		`{"checks":{"pull":{"level":"loud"}}}`:                         "level must be",
		`{"checks":{"pull":{"level":"alert","values":{"minutes":1}}}}`: "must be 2 to 43200",
		`{"checks":{"pull":{"level":"alert","values":{"seconds":9}}}}`: "unknown setting",
		`{"target_id":"tgt_other"}`:                                    "Unknown notification target",
	} {
		code, out := call(t, srv, "u1", "PUT", "/col_a/crowdsec/health", body)
		if code != 400 || !strings.Contains(jsonText(out), want) {
			t.Fatalf("%s: %d %v", body, code, out)
		}
	}
	if code, _ := call(t, srv, "u2", "PUT", "/col_a/crowdsec/health", `{}`); code != 404 {
		t.Fatalf("another owner's collector: %d", code)
	}
	code, out = call(t, srv, "u1", "PUT", "/col_a/crowdsec/health", `{"target_id":"tgt_1","checks":{"pull":{"level":"caution","values":{"minutes":240}},"held":{"level":"off","values":{}}}}`)
	checksOut := out["checks"].(map[string]any)
	if code != 200 || out["target_id"] != "tgt_1" || checksOut["pull"].(map[string]any)["level"] != LevelCaution ||
		checksOut["pull"].(map[string]any)["values"].(map[string]any)["minutes"] != float64(240) || checksOut["held"].(map[string]any)["level"] != LevelOff ||
		checksOut["heartbeat"].(map[string]any)["level"] != LevelAlert {
		t.Fatalf("saved settings: %d %v", code, out)
	}
}

func TestPruneHealth(t *testing.T) {
	rig := newHealthRig(t)
	ctx := context.Background()
	rig.collector("col_a", "u1", "edge")
	rig.collector("col_b", "u2", "other")
	now := time.Now().UnixMilli()
	for id, user := range map[string]string{"col_a": "u1", "col_b": "u2"} {
		rig.report(id, user, now, wire.Health{Components: []wire.Component{{Kind: wire.KindMachine, Name: "m", LastSeenMs: now - time.Hour.Milliseconds(), Validated: true}}})
	}
	rig.tick(now)
	rig.report("col_a", "u1", now+minute, wire.Health{Components: []wire.Component{{Kind: wire.KindMachine, Name: "m", LastSeenMs: now + minute, Validated: true}}})
	rig.tick(now + minute)
	// u2's account is gone; u1's resolved incident is past retention.
	if err := rig.typ.Store.pruneHealth(ctx, time.Hour.Milliseconds(), now+3*time.Hour.Milliseconds(), []string{"u1"}); err != nil {
		t.Fatal(err)
	}
	if len(rig.events()) != 0 {
		t.Fatalf("events left: %+v", rig.events())
	}
	if comps, _ := rig.typ.Store.Components(ctx, "u2"); len(comps) != 0 {
		t.Fatal("a deleted account's components must go")
	}
	if comps, _ := rig.typ.Store.Components(ctx, "u1"); len(comps) != 1 {
		t.Fatal("the remaining account keeps its components")
	}
}
