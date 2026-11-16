package knot

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"certainstats/internal/base/alert"
	"certainstats/internal/collector"
	"certainstats/internal/collector/type/knot/wire"
	"certainstats/internal/store"
	"certainstats/internal/store/sqlite"
)

// ruleRig is a Knot type over a real main database (collectors, rules,
// incidents), knot.db and the collector TSDB, with connections faked.
type ruleRig struct {
	t      *testing.T
	db     *sqlite.Store
	typ    *Type
	now    time.Time
	online map[string]bool
	sent   []string
}

func newRuleRig(t *testing.T) *ruleRig {
	t.Helper()
	db, err := sqlite.New(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	ctx := context.Background()
	if err := db.CreateUser(ctx, "u1", "u1", "x", false); err != nil {
		t.Fatal(err)
	}
	r := &ruleRig{t: t, db: db, now: time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC), online: map[string]bool{}}
	svc := collector.NewService(db, db)
	r.typ = &Type{Store: openStore(t), TSDB: openTSDB(t), Collectors: svc, Rules: db,
		Deliver: func(id string) { r.sent = append(r.sent, id) }, Now: func() time.Time { return r.now }}
	r.typ.live = func(id string) (time.Time, bool) { return r.now.Add(-time.Hour), r.online[id] }
	for id, name := range map[string]string{"col_p": "ns1", "col_s": "ns2"} {
		if err := db.CollectorCreate(ctx, store.Collector{CollectorID: id, UserID: "u1", Name: name, CreatedAt: 1}, "tok_"+id, "h_"+id, []string{wire.TypeName}); err != nil {
			t.Fatal(err)
		}
		if err := db.CollectorSeen(ctx, id, "test", r.now.Add(-time.Hour).UnixMilli()); err != nil {
			t.Fatal(err)
		}
		r.online[id] = true
	}
	return r
}

func (r *ruleRig) rule(id string, trigger alert.TriggerType, threshold float64, subjects ...alert.Subject) {
	r.t.Helper()
	err := r.db.AlertCreate(context.Background(), store.Alert{AlertID: id, UserID: "u1", Nickname: "rule " + id, Enabled: true,
		Trigger: alert.Trigger{Type: trigger, Operator: alert.OpGreaterThan, Threshold: threshold, Duration: "0s"},
		Action:  alert.AlertAction{Type: alert.DestPreset}, Subjects: subjects})
	if err != nil {
		r.t.Fatal(err)
	}
}

func (r *ruleRig) zones(collectorID string, at time.Time, zones ...wire.Zone) {
	r.t.Helper()
	if err := r.typ.Store.SaveZones(context.Background(), "u1", collectorID, zones, at.UnixMilli()); err != nil {
		r.t.Fatal(err)
	}
}

func (r *ruleRig) evaluate() {
	r.t.Helper()
	if err := r.typ.Evaluate(context.Background(), r.now); err != nil {
		r.t.Fatal(err)
	}
}

// open lists open Knot incidents as subject IDs.
func (r *ruleRig) open() map[string]store.SubjectIncident {
	r.t.Helper()
	list, err := r.db.SubjectOpenIncidents(context.Background(), wire.TypeName, "")
	if err != nil {
		r.t.Fatal(err)
	}
	out := map[string]store.SubjectIncident{}
	for _, i := range list {
		out[i.AlertID+" "+i.SubjectID] = i
	}
	return out
}

func all(id string) alert.Subject {
	return alert.Subject{Source: wire.TypeName, CollectorID: id, Scope: alert.ScopeAll}
}

func TestZoneRulesFireRecoverHoldAndClose(t *testing.T) {
	r := newRuleRig(t)
	sec := func(serial uint32, expireIn time.Duration) wire.Zone {
		return wire.Zone{Name: "example.com.", Role: "secondary", Serial: serial, HasSerial: true, ExpireAtMs: r.now.Add(expireIn).UnixMilli()}
	}
	r.zones("col_p", r.now.Add(-20*time.Minute), wire.Zone{Name: "example.com.", Role: "primary", Serial: 10, HasSerial: true})
	r.zones("col_s", r.now.Add(-time.Hour), sec(9, 3*time.Hour))
	r.rule("exp", TriggerExpiry, 24, all("col_s"))
	r.rule("drift", TriggerDrift, 15, alert.Subject{Source: wire.TypeName, CollectorID: "col_s", Scope: ScopeZone, Name: "example.com."})
	r.rule("drift_late", TriggerDrift, 30, all("col_s"))
	r.evaluate()
	open := r.open()
	if _, ok := open["exp col_s/zone/example.com."]; !ok {
		t.Fatalf("expiry did not fire: %v", open)
	}
	if _, ok := open["drift col_s/zone/example.com."]; !ok {
		t.Fatalf("drift did not fire after 20 minutes: %v", open)
	}
	if _, ok := open["drift_late col_s/zone/example.com."]; ok || len(open) != 2 || len(r.sent) != 2 {
		t.Fatalf("drift with a 30-minute limit fired early, or extra incidents: %v, sent %d", open, len(r.sent))
	}
	h, err := r.db.AlertHistoryGetByID(context.Background(), open["exp col_s/zone/example.com."].HistoryID, "u1")
	if err != nil || h.Subject.SourceLabel != "Knot DNS" || h.Subject.Kind != KindZone || h.Subject.Name != "example.com." || h.Subject.Collector != "ns2" {
		t.Fatalf("snapshot: %+v %v", h, err)
	}

	// The secondary goes offline: its zone incidents are held, not closed.
	r.online["col_s"] = false
	r.evaluate()
	if len(r.open()) != 2 {
		t.Fatalf("an offline collector's incidents must be held: %v", r.open())
	}

	// Back online and refreshed: both recover.
	r.online["col_s"] = true
	r.zones("col_s", r.now, sec(10, 48*time.Hour))
	r.evaluate()
	if len(r.open()) != 0 || len(r.sent) != 4 {
		t.Fatalf("recovery: %v, sent %d", r.open(), len(r.sent))
	}

	// A zone that disappears takes its incident with it, quietly.
	r.zones("col_s", r.now, sec(10, time.Hour))
	r.evaluate()
	if len(r.open()) != 1 {
		t.Fatal("expiry did not fire again")
	}
	r.zones("col_s", r.now)
	r.evaluate()
	if len(r.open()) != 0 || len(r.sent) != 5 {
		t.Fatalf("a removed zone must close quietly: %v, sent %d", r.open(), len(r.sent))
	}
}

func TestServerRules(t *testing.T) {
	r := newRuleRig(t)
	ctx := context.Background()
	rc := func(ok, fail uint64) []wire.Counter {
		return []wire.Counter{
			{Section: "mod-stats", Item: "response-code", ID: "NOERROR", Value: ok},
			{Section: "mod-stats", Item: "response-code", ID: "SERVFAIL", Value: fail},
		}
	}
	writeCounters(ctx, r.typ.TSDB, "u1", "col_p", rc(0, 0), r.now.Add(-10*time.Minute).UnixMilli())
	writeCounters(ctx, r.typ.TSDB, "u1", "col_p", rc(950, 50), r.now.Add(-5*time.Minute).UnixMilli())
	r.rule("sf", TriggerServfail, 1, all("col_p"))
	r.rule("sf_high", TriggerServfail, 10, all("col_p"))
	r.rule("down", TriggerUnreachable, 5, all("col_s"))
	r.rule("quiet", TriggerDnstapQuiet, 5, all("col_p"))
	r.evaluate()
	open := r.open()
	if _, ok := open["sf col_p/server"]; !ok || len(open) != 1 {
		t.Fatalf("SERVFAIL 5%% must fire at 1%% and only there: %v", open)
	}

	// The collector of ns2 stops connecting; after the limit it is unreachable.
	r.online["col_s"] = false
	if err := r.db.CollectorSeen(ctx, "col_s", "", r.now.Add(-3*time.Minute).UnixMilli()); err != nil {
		t.Fatal(err)
	}
	r.evaluate()
	if _, ok := r.open()["down col_s/server"]; ok {
		t.Fatal("unreachable fired before its limit")
	}
	r.now = r.now.Add(5 * time.Minute)
	r.evaluate()
	if _, ok := r.open()["down col_s/server"]; !ok {
		t.Fatalf("unreachable did not fire: %v", r.open())
	}
	r.online["col_s"] = true
	r.evaluate()
	if _, ok := r.open()["down col_s/server"]; ok {
		t.Fatal("unreachable did not recover once connected")
	}
}

func TestAlertSubjectsOfferServersAndZones(t *testing.T) {
	r := newRuleRig(t)
	r.zones("col_p", r.now, wire.Zone{Name: "a.example."}, wire.Zone{Name: "b.example."})
	list, err := r.typ.AlertSubjects(context.Background(), "u1")
	if err != nil || len(list) != 2 {
		t.Fatalf("options: %+v %v", list, err)
	}
	var ns1 alert.SubjectOptions
	for _, o := range list {
		if o.CollectorID == "col_p" {
			ns1 = o
		}
	}
	if ns1.Source != wire.TypeName || len(ns1.Sections) != 1 || ns1.Sections[0].Kind != KindZone || len(ns1.Sections[0].Entries) != 2 ||
		ns1.Key(ns1.Sections[0].Entries[0]) != "knot/col_p/zone/a.example." {
		t.Fatalf("ns1: %+v", ns1)
	}
	if _, ok := alert.ParseSubject(ns1.AllKey()); !ok {
		t.Fatal("the all key must parse")
	}
}
