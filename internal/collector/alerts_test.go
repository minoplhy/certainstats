package collector

import (
	"context"
	"sync"
	"testing"

	"certainstats/internal/base/alert"
	"certainstats/internal/store"
)

var roundSource sync.Once

func registerRoundSource() {
	roundSource.Do(func() {
		alert.RegisterSubjectSource(alert.SubjectSource{Name: "roundsrc", Label: "Round Source"})
		alert.RegisterSubjectTrigger(alert.SubjectTrigger{Type: "roundsrc_x", Source: "roundsrc", Min: 1, Max: 10, Default: 5})
	})
}

// fakeRules records what a round does.
type fakeRules struct {
	rules    []store.Alert
	open     []store.SubjectIncident
	fired    []string
	resolved []string
	closed   []string
	asked    []alert.TriggerType
}

func (f *fakeRules) SubjectRules(_ context.Context, t []alert.TriggerType) ([]store.Alert, error) {
	f.asked = t
	return f.rules, nil
}
func (f *fakeRules) SubjectOpenIncidents(_ context.Context, source, _ string) ([]store.SubjectIncident, error) {
	return f.open, nil
}
func (f *fakeRules) SubjectFire(_ context.Context, source string, rule store.Alert, _, subjectID string, snap alert.SubjectSnapshot, _ float64) (string, error) {
	if snap.SourceLabel != "Round Source" || snap.Source != "roundsrc" {
		panic("snapshot lacks its source")
	}
	f.fired = append(f.fired, rule.AlertID+" "+subjectID)
	return "evt_fire_" + subjectID, nil
}
func (f *fakeRules) SubjectResolve(_ context.Context, id string) (string, error) {
	f.resolved = append(f.resolved, id)
	return "evt_resolve_" + id, nil
}
func (f *fakeRules) SubjectClose(_ context.Context, id string) error {
	f.closed = append(f.closed, id)
	return nil
}

func TestRoundOpensResolvesKeepsAndCloses(t *testing.T) {
	registerRoundSource()
	ctx := context.Background()
	f := &fakeRules{
		rules: []store.Alert{{AlertID: "r1", UserID: "u1"}, {AlertID: "r2", UserID: "u2"}},
		open: []store.SubjectIncident{
			{HistoryID: "h_resolve", AlertID: "r1", SubjectID: "c1/a", CollectorID: "c1"},
			{HistoryID: "h_keep", AlertID: "r1", SubjectID: "c1/b", CollectorID: "c1"},
			{HistoryID: "h_unjudged_keep", AlertID: "r1", SubjectID: "c1/c", CollectorID: "c1"},
			{HistoryID: "h_unjudged_close", AlertID: "r1", SubjectID: "c1/d", CollectorID: "c1"},
			{HistoryID: "h_held", AlertID: "r1", SubjectID: "c2/a", CollectorID: "c2"},
			{HistoryID: "h_gone_rule", AlertID: "r_deleted", SubjectID: "c2/b", CollectorID: "c2"},
			{HistoryID: "h_gone_subject", AlertID: "r1", SubjectID: "c1/z", CollectorID: "c1"},
		},
	}
	var sent []string
	round, err := StartRound(ctx, f, "roundsrc", func(id string) { sent = append(sent, id) })
	if err != nil {
		t.Fatal(err)
	}
	if len(f.asked) != 1 || f.asked[0] != "roundsrc_x" {
		t.Fatalf("rules asked for %v", f.asked)
	}
	if users := round.Users(); len(users) != 2 || len(round.RulesOf("u1")) != 1 {
		t.Fatalf("users %v", users)
	}
	r1 := f.rules[0]
	for _, j := range []struct {
		subject string
		v       Verdict
	}{
		{"c1/a", Verdict{Judged: true}},
		{"c1/b", Verdict{Judged: true, Bad: true}},
		{"c1/c", Verdict{KeepUnjudged: true}},
		{"c1/d", Verdict{}},
		{"c1/new", Verdict{Judged: true, Bad: true}},
		{"c1/fine", Verdict{Judged: true}},
	} {
		if err := round.Judge(ctx, r1, "c1", j.subject, j.v); err != nil {
			t.Fatal(err)
		}
	}
	round.Hold("c2")
	if err := round.Finish(ctx); err != nil {
		t.Fatal(err)
	}
	if len(f.fired) != 1 || f.fired[0] != "r1 c1/new" {
		t.Errorf("fired %v", f.fired)
	}
	if len(f.resolved) != 1 || f.resolved[0] != "h_resolve" {
		t.Errorf("resolved %v", f.resolved)
	}
	closed := map[string]bool{}
	for _, c := range f.closed {
		closed[c] = true
	}
	if len(closed) != 3 || !closed["h_unjudged_close"] || !closed["h_gone_rule"] || !closed["h_gone_subject"] {
		t.Errorf("closed %v", f.closed)
	}
	if len(sent) != 2 {
		t.Errorf("delivered %v", sent)
	}
	// Without rules a round does nothing at all.
	empty, _ := StartRound(ctx, nil, "roundsrc", nil)
	if err := empty.Finish(ctx); err != nil || empty.IsOpen("r1", "c1/a") {
		t.Fatal("an empty round must do nothing")
	}
}
