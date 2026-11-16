package sqlite

import (
	a "certainstats/internal/base/alert"
	"certainstats/internal/store"
	"context"
	"sync"
	"testing"
)

// The store only knows sources through the registry, as it does in the server.
var testSources sync.Once

func registerTestSources() {
	testSources.Do(func() {
		if _, ok := a.SubjectSourceFor("crowdsec"); !ok {
			a.RegisterSubjectSource(a.SubjectSource{Name: "crowdsec", Label: "CrowdSec", Scopes: []string{"machine", "bouncer", "group"}})
			a.RegisterSubjectTrigger(a.SubjectTrigger{Type: "crowdsec_pull", Source: "crowdsec", Kind: "bouncer", Min: 2, Max: 600, Default: 60})
		}
		if _, ok := a.SubjectSourceFor("knot"); !ok {
			a.RegisterSubjectSource(a.SubjectSource{Name: "knot", Label: "Knot DNS", Scopes: []string{"zone"}})
			a.RegisterSubjectTrigger(a.SubjectTrigger{Type: "knot_expiry", Source: "knot", Kind: "zone", Min: 1, Max: 720, Default: 24})
		}
	})
}

func collectorFixture(t *testing.T) *Store {
	t.Helper()
	registerTestSources()
	s := newTestStore(t)
	ctx := context.Background()
	for _, u := range []string{"owner", "other"} {
		if err := s.CreateUser(ctx, u, u, "hash", false); err != nil {
			t.Fatal(err)
		}
	}
	for id, user := range map[string]string{"col_a": "owner", "col_b": "other"} {
		if err := s.CollectorCreate(ctx, store.Collector{CollectorID: id, UserID: user, Name: id, CreatedAt: 1}, "tok_"+id, "hash_"+id, []string{"crowdsec", "knot"}); err != nil {
			t.Fatal(err)
		}
	}
	return s
}

func rule(id, trigger string, subjects ...a.Subject) store.Alert {
	return store.Alert{AlertID: id, UserID: "owner", Nickname: "rule " + id, Enabled: true,
		Trigger: a.Trigger{Type: a.TriggerType(trigger), Operator: a.OpGreaterThan, Threshold: 24, Duration: "0s"},
		Action:  a.AlertAction{Type: a.DestPreset}, Subjects: subjects}
}

// A database from before subjects carried their source: rules in
// alert_crowdsec_subjects and an open CrowdSec incident under the old index.
func TestCollectorAlertMigrationKeepsCrowdSecRules(t *testing.T) {
	s := collectorFixture(t)
	ctx := context.Background()
	if err := s.AlertCreate(ctx, rule("legacy", "crowdsec_pull", a.Subject{Source: "crowdsec", CollectorID: "col_a", Scope: "group", Name: "fw"})); err != nil {
		t.Fatal(err)
	}
	for _, stmt := range []string{
		`DELETE FROM alert_subjects`,
		`DROP INDEX collector_incident_open`,
		`CREATE TABLE alert_crowdsec_subjects (alert_id TEXT NOT NULL REFERENCES alerts(alert_id) ON DELETE CASCADE, collector_id TEXT NOT NULL, scope TEXT NOT NULL, name TEXT NOT NULL DEFAULT '', PRIMARY KEY (alert_id, collector_id, scope, name))`,
		`INSERT INTO alert_crowdsec_subjects VALUES ('legacy', 'col_a', 'group', 'fw'), ('legacy', 'col_a', 'machine', 'web/01')`,
		`CREATE UNIQUE INDEX crowdsec_incident_open ON alert_history(alert_id, subject_id) WHERE subject_kind = 'crowdsec' AND resolved_at IS NULL AND closed_at IS NULL`,
	} {
		if _, err := s.db.Exec(stmt); err != nil {
			t.Fatalf("%s: %v", stmt, err)
		}
	}
	got, err := s.AlertGetInfo(ctx, "legacy", "owner")
	if err != nil {
		t.Fatal(err)
	}
	eventID, err := s.SubjectFire(ctx, "crowdsec", got, "col_a", "col_a/bouncer/fw@1.2.3.4", a.SubjectSnapshot{Collector: "col_a", Kind: "bouncer", Name: "fw@1.2.3.4"}, 90)
	if err != nil || eventID == "" {
		t.Fatalf("fire before migration: %q %v", eventID, err)
	}

	if err := s.migrateCollectorAlerts(); err != nil {
		t.Fatal(err)
	}
	got, err = s.AlertGetInfo(ctx, "legacy", "owner")
	if err != nil || len(got.Subjects) != 2 || got.Subjects[0].Key() != "crowdsec/col_a/group/fw" || got.Subjects[1].Key() != "crowdsec/col_a/machine/web/01" {
		t.Fatalf("subjects after migration: %+v %v", got.Subjects, err)
	}
	var n int
	s.db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE name IN ('alert_crowdsec_subjects', 'crowdsec_incident_open')`).Scan(&n)
	if n != 0 {
		t.Fatal("the CrowdSec-only table or index survived the migration")
	}
	open, err := s.SubjectOpenIncidents(ctx, "crowdsec", "owner")
	if err != nil || len(open) != 1 {
		t.Fatalf("open after migration: %+v %v", open, err)
	}
	// The open incident is still unique per rule and subject, and still resolves.
	if again, err := s.SubjectFire(ctx, "crowdsec", got, "col_a", "col_a/bouncer/fw@1.2.3.4", a.SubjectSnapshot{}, 90); err != nil || again != "" {
		t.Fatalf("second fire: %q %v", again, err)
	}
	if recovery, err := s.SubjectResolve(ctx, open[0].HistoryID); err != nil || recovery == "" {
		t.Fatalf("resolve: %q %v", recovery, err)
	}
	if err := s.migrateCollectorAlerts(); err != nil {
		t.Fatal("the migration must run again cleanly:", err)
	}
}

func TestSubjectIncidentsAreGenericPerSource(t *testing.T) {
	s := collectorFixture(t)
	ctx := context.Background()
	r := rule("zones", "knot_expiry", a.Subject{Source: "knot", CollectorID: "col_a", Scope: "zone", Name: "example.com."})
	if err := s.AlertCreate(ctx, r); err != nil {
		t.Fatal(err)
	}
	rules, err := s.SubjectRules(ctx, a.SourceTriggers("knot"))
	if err != nil || len(rules) != 1 || rules[0].Subjects[0].Name != "example.com." {
		t.Fatalf("rules: %+v %v", rules, err)
	}
	if cs, _ := s.SubjectRules(ctx, a.SourceTriggers("crowdsec")); len(cs) != 0 {
		t.Fatal("a Knot rule came back for CrowdSec")
	}
	snap := a.SubjectSnapshot{SourceLabel: "Knot DNS", Collector: "ns1", Kind: "zone", Name: "example.com.", Label: "Secondary zone expires within (hours)", Detail: "expires in 3.5 hours", Unit: "hours"}
	eventID, err := s.SubjectFire(ctx, "knot", rules[0], "col_a", "col_a/zone/example.com.", snap, 3.5)
	if err != nil || eventID == "" {
		t.Fatalf("fire: %q %v", eventID, err)
	}
	if cs, _ := s.SubjectOpenIncidents(ctx, "crowdsec", ""); len(cs) != 0 {
		t.Fatal("a Knot incident is listed as CrowdSec")
	}
	open, err := s.SubjectOpenIncidents(ctx, "knot", "owner")
	if err != nil || len(open) != 1 || open[0].CollectorID != "col_a" || open[0].Trigger != "knot_expiry" {
		t.Fatalf("open: %+v %v", open, err)
	}
	hp, err := s.AlertHistoryGetByID(ctx, open[0].HistoryID, "owner")
	if err != nil {
		t.Fatal(err)
	}
	h := *hp
	if h.SubjectKind != "knot" || h.Subject.Source != "knot" || h.Subject.Detail != "expires in 3.5 hours" || h.AgentNickname != "example.com. on ns1" || !h.MonitoringAvailable {
		t.Fatalf("history row: %+v", h)
	}
	if err := s.SubjectClose(ctx, open[0].HistoryID); err != nil {
		t.Fatal(err)
	}
	if left, _ := s.SubjectOpenIncidents(ctx, "knot", ""); len(left) != 0 {
		t.Fatal("closed incident still open")
	}
	if _, err := s.SubjectFire(ctx, "agent", rules[0], "col_a", "x", snap, 1); err == nil {
		t.Fatal("a server kind must not open a collector incident")
	}
}

func TestSubjectValidation(t *testing.T) {
	s := collectorFixture(t)
	ctx := context.Background()
	for name, r := range map[string]store.Alert{
		"no subjects":     rule("v1", "knot_expiry"),
		"wrong source":    rule("v2", "knot_expiry", a.Subject{Source: "crowdsec", CollectorID: "col_a", Scope: "all"}),
		"unknown scope":   rule("v3", "knot_expiry", a.Subject{Source: "knot", CollectorID: "col_a", Scope: "machine", Name: "x"}),
		"other's":         rule("v4", "knot_expiry", a.Subject{Source: "knot", CollectorID: "col_b", Scope: "all"}),
		"subjects on cpu": rule("v5", string(a.TriggerTypeCPU), a.Subject{Source: "knot", CollectorID: "col_a", Scope: "all"}),
	} {
		if err := s.AlertCreate(ctx, r); err == nil {
			t.Errorf("%s: must be refused", name)
		}
	}
	if err := s.AlertCreate(ctx, rule("ok", "knot_expiry", a.Subject{Source: "knot", CollectorID: "col_a", Scope: "all"})); err != nil {
		t.Fatalf("a valid rule: %v", err)
	}
}
