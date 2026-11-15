package crowdsec

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"certainstats/internal/base/alert"
	"certainstats/internal/crowdsec/wire"
)

const minute = int64(60000)

func allParts() map[string]bool {
	set := map[string]bool{}
	for _, p := range wire.Parts {
		set[p] = true
	}
	return set
}

var nextID int64

func component(kind, name string, sig DBSignals) *Component {
	nextID++
	raw, _ := json.Marshal(sig)
	return &Component{ID: nextID, CollectorID: "col_a", Kind: kind, Name: name, Signals: map[string]json.RawMessage{SourceDB: raw}}
}

func setSignals(c *Component, sig DBSignals) {
	c.Signals[SourceDB], _ = json.Marshal(sig)
}

func finding(c *Component, check string) *Finding {
	for i := range c.Findings {
		if c.Findings[i].Check == check {
			return &c.Findings[i]
		}
	}
	return nil
}

func TestDefaultChecksAreCautions(t *testing.T) {
	now := int64(1_800_000_000_000)
	ec := evalContext{Parts: allParts(), Now: now}

	m := component(wire.KindMachine, "web-01", DBSignals{LastSeenMs: now - 4*minute, Validated: true})
	m.evaluate(ec)
	if len(m.Findings) != 0 || m.state(true, "", false) != StateOK {
		t.Fatalf("a heartbeat 4 minutes old is healthy: %v", m.Findings)
	}
	setSignals(m, DBSignals{LastSeenMs: now - 12*minute, Validated: true})
	m.evaluate(ec)
	f := finding(m, "heartbeat")
	if f == nil || f.Detail != "No heartbeat for 12m" || f.SinceMs != now {
		t.Fatalf("stale machine: %+v", f)
	}
	ec.Now = now + minute
	m.evaluate(ec)
	if f := finding(m, "heartbeat"); f == nil || f.SinceMs != now {
		t.Fatalf("since must not move: %+v", f)
	}
	// Failing at its default numbers is a caution; red only comes from a firing rule.
	if got := m.state(true, "", false); got != StateCaution {
		t.Fatalf("state %q", got)
	}
	if got := m.state(true, "", true); got != StateAlert {
		t.Fatalf("firing state %q", got)
	}
	if got := m.state(false, "", false); got != StateUnknown {
		t.Fatalf("unjudged state %q", got)
	}

	stale := DBSignals{LastSeenMs: now - 5*time.Hour.Milliseconds(), Validated: true}
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
		if got := tc.c.state(true, tc.ec.Self, false); got != tc.want {
			t.Errorf("%s: state %q, want %q", name, got, tc.want)
		}
	}

	b := component(wire.KindBouncer, "fw", DBSignals{LastSeenMs: now - 3*time.Hour.Milliseconds()})
	b.evaluate(ec)
	if f := finding(b, "pull"); f == nil || f.Detail != "No pull for 3h" {
		t.Fatalf("stale bouncer: %+v", f)
	}
}

func TestHeldComparesWithTheLocalAPI(t *testing.T) {
	now := int64(1_800_000_000_000)
	ec := evalContext{Parts: allParts(), Now: now, Active: map[string]int64{"crowdsec": 100, "CAPI": 40000, "cscli": 3}}
	sig := DBSignals{LastSeenMs: now, MetricsMs: now - minute, Held: map[string]int64{"crowdsec": 10, "cscli": 0}}
	b := component(wire.KindBouncer, "fw", sig)
	b.evaluate(ec)
	b.evaluate(ec) // the same report again counts once
	if finding(b, "held") != nil {
		t.Fatal("one bad report is not enough")
	}
	sig.MetricsMs = now
	setSignals(b, sig)
	b.evaluate(ec)
	f := finding(b, "held")
	// CAPI is not reported by the bouncer and cscli has too few active decisions: only crowdsec is compared.
	if f == nil || !strings.Contains(f.Detail, "Holds 10 of 100 crowdsec decisions, may not be applying them") {
		t.Fatalf("second bad report: %+v", f)
	}
	sig.MetricsMs, sig.Held = now+minute, map[string]int64{"crowdsec": 98}
	setSignals(b, sig)
	b.evaluate(ec)
	if finding(b, "held") != nil {
		t.Fatal("a good report clears it")
	}
	sig.MetricsMs, sig.Held = now-3*time.Hour.Milliseconds(), map[string]int64{"crowdsec": 0}
	old := component(wire.KindBouncer, "fw", sig)
	if r := old.run(checks["held"], checks["held"].ruleValues(50), ec, "x"); r.Judged {
		t.Fatal("stale metrics must not be judged")
	}
}

func TestHeldTreatsEmptyDecisionMetricsAsZero(t *testing.T) {
	now := int64(1_800_000_000_000)
	ec := evalContext{Parts: allParts(), Now: now, Active: map[string]int64{"crowdsec": 100, "small": 3}}
	empty := DBSignals{LastSeenMs: now, MetricsMs: now - minute, Held: map[string]int64{}, HoldsDecisions: true}
	b := component(wire.KindBouncer, "fw", empty)
	b.evaluate(ec)
	if finding(b, "held") != nil {
		t.Fatal("one empty report is not enough")
	}

	ec.Now = now + minute
	empty.LastSeenMs, empty.MetricsMs = ec.Now, ec.Now
	setSignals(b, empty)
	b.evaluate(ec)
	f := finding(b, "held")
	if f == nil || !strings.Contains(f.Detail, "Holds 0 of 100 crowdsec decisions") {
		t.Fatalf("second empty report should count as zero held: %+v", f)
	}

	// One report is enough below, so a pass is not just a streak still building.
	once := checks["held"].defaults()
	once["reports"] = 1
	if r := component(wire.KindBouncer, "fw-once", empty).run(checks["held"], once, ec, "x"); !r.Bad {
		t.Fatalf("one-report values must flag the empty report: %+v", r)
	}
	// A bouncer that has never sent decision counts (one in live mode) is not
	// judged on what it does not report.
	live := DBSignals{LastSeenMs: ec.Now, MetricsMs: ec.Now, Held: map[string]int64{}}
	if r := component(wire.KindBouncer, "nginx", live).run(checks["held"], once, ec, "x"); r.Bad {
		t.Fatalf("a bouncer that never counts decisions must not be flagged: %+v", r)
	}

	// Empty or below-threshold Local API decision sets have nothing to compare.
	for name, active := range map[string]map[string]int64{"no active": {}, "below min_active": {"small": 3}} {
		c := component(wire.KindBouncer, "fw-"+name, empty)
		if r := c.run(checks["held"], once, evalContext{Parts: allParts(), Now: ec.Now, Active: active}, "x"); r.Bad {
			t.Fatalf("%s must not produce a finding: %+v", name, r)
		}
	}
}

// A firewall bouncer whose ipset fails sends metrics with no decision counts at
// all; next to a sibling holding thousands it is flagged, even if it never sent
// counts before, since its siblings show the same software counts them.
func TestSiblingsFlagAnEntryWithNoDecisionCounts(t *testing.T) {
	now := int64(1_800_000_000_000)
	broken := component(wire.KindBouncer, "fw@10.0.0.5", DBSignals{LastSeenMs: now, MetricsMs: now - minute, Held: map[string]int64{}})
	good := component(wire.KindBouncer, "fw@10.0.0.6", DBSignals{LastSeenMs: now, MetricsMs: now - minute, Held: map[string]int64{"crowdsec": 1204, "CAPI": 30000}})
	comps := []*Component{broken, good}
	ec := evalContext{Parts: map[string]bool{wire.PartBouncers: true, wire.PartMetrics: true}, Now: now, Groups: groupBouncers(comps)}
	for i := 0; i < 2; i++ {
		setSignals(broken, DBSignals{LastSeenMs: ec.Now, MetricsMs: ec.Now, Held: map[string]int64{}})
		broken.evaluate(ec)
		ec.Now += minute
	}
	f := finding(broken, "siblings")
	if f == nil || !strings.HasPrefix(f.Detail, "Holds 0 of the 31204 decisions its sibling fw@10.0.0.6 holds") {
		t.Fatalf("an entry with no decision counts: %+v", f)
	}
	// Without usage metrics at all there is nothing to compare.
	quiet := component(wire.KindBouncer, "fw@10.0.0.7", DBSignals{LastSeenMs: now})
	ec.Groups = groupBouncers([]*Component{quiet, good})
	if r := quiet.run(checks["siblings"], checks["siblings"].defaults(), ec, "x"); r.Judged {
		t.Fatal("a bouncer sending no usage metrics is not compared")
	}
}

func TestSiblingsOnASharedKey(t *testing.T) {
	now := int64(1_800_000_000_000)
	broken := component(wire.KindBouncer, "fw@10.0.0.5", DBSignals{LastSeenMs: now, MetricsMs: now - minute, Held: map[string]int64{"crowdsec": 0, "CAPI": 0}})
	good := component(wire.KindBouncer, "fw@10.0.0.6", DBSignals{LastSeenMs: now, MetricsMs: now - minute, Held: map[string]int64{"crowdsec": 1204, "CAPI": 30000}})
	other := component(wire.KindBouncer, "fw@10.0.0.7", DBSignals{LastSeenMs: now, MetricsMs: now - minute, Held: map[string]int64{"crowdsec": 1200, "CAPI": 29800}})
	alone := component(wire.KindBouncer, "nginx", DBSignals{LastSeenMs: now, MetricsMs: now - minute, Held: map[string]int64{"crowdsec": 0}})
	comps := []*Component{broken, good, other, alone}
	ec := evalContext{Parts: map[string]bool{wire.PartBouncers: true, wire.PartMetrics: true}, Now: now, Groups: groupBouncers(comps)}
	for _, c := range comps {
		c.evaluate(ec)
	}
	if finding(broken, "siblings") != nil {
		t.Fatal("one report is not enough")
	}
	setSignals(broken, DBSignals{LastSeenMs: now, MetricsMs: now, Held: map[string]int64{"crowdsec": 0, "CAPI": 0}})
	for _, c := range comps {
		c.evaluate(ec)
	}
	f := finding(broken, "siblings")
	if f == nil || f.Detail != "Holds 0 of the 31204 decisions its sibling fw@10.0.0.6 holds; its ipset or nftables set may be failing, check its log" {
		t.Fatalf("broken entry: %+v", f)
	}
	if broken.state(true, "", false) != StateCaution {
		t.Fatalf("siblings is a caution by default, got %q", broken.state(true, "", false))
	}
	for _, c := range []*Component{good, other, alone} {
		if finding(c, "siblings") != nil {
			t.Errorf("%s must not be flagged", c.Name)
		}
	}
	// A sibling whose metrics are old is not a reference.
	stale := []*Component{broken, component(wire.KindBouncer, "fw@10.0.0.8", DBSignals{LastSeenMs: now, MetricsMs: now - 3*time.Hour.Milliseconds(), Held: map[string]int64{"crowdsec": 50000}})}
	ec.Groups = groupBouncers(stale)
	if r := broken.run(checks["siblings"], checks["siblings"].ruleValues(10), ec, "y"); r.Judged {
		t.Fatal("stale sibling metrics must not be compared")
	}

	for in, want := range map[string]string{"fw@1.2.3.4": "fw", "fw@1.2.3.4@5.6.7.8": "fw", "fw@::1": "fw", "fw@notanip": "fw@notanip", "@1.2.3.4": "@1.2.3.4", "plain": "plain"} {
		if got := BaseName(in); got != want {
			t.Errorf("BaseName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestChecksAreRuleConditions(t *testing.T) {
	for _, c := range Checks() {
		st, ok := alert.SubjectTriggerFor(c.TriggerType())
		if !ok || st.Kind != c.Kind || st.Group != "CrowdSec" || st.Label == "" {
			t.Errorf("%s: rule condition %+v", c.Name, st)
		}
		if checkForTrigger(c.TriggerType()) != c {
			t.Errorf("%s: trigger does not lead back to the check", c.Name)
		}
	}
	if got := checks["pull"].ruleValues(240)["minutes"]; got != 240 {
		t.Fatalf("rule threshold sets the rule parameter: %d", got)
	}
	if got := checks["pull"].ruleValues(1)["minutes"]; got != 2 {
		t.Fatalf("rule threshold is clamped to the parameter's range: %d", got)
	}
	// A check registered later (as a bouncer-log source would) runs with no other change.
	checks["apply_errors"] = &Check{Name: "apply_errors", Label: "Bouncer apply errors", Kind: wire.KindBouncer,
		Params: []Param{{Key: "errors", Min: 1, Max: 100, Default: 1}},
		Eval: func(in evalInput) verdict {
			var log struct {
				Errors int `json:"errors"`
			}
			raw, ok := in.Component.Signals["log"]
			if !ok || json.Unmarshal(raw, &log) != nil {
				return verdict{}
			}
			return verdict{Judged: true, Bad: log.Errors >= in.Values["errors"], Detail: "ipset errors in the bouncer log"}
		}}
	defer delete(checks, "apply_errors")
	b := component(wire.KindBouncer, "fw", DBSignals{LastSeenMs: int64(1_800_000_000_000)})
	b.Signals["log"] = json.RawMessage(`{"errors":3}`)
	b.evaluate(evalContext{Parts: allParts(), Now: 1_800_000_000_000})
	if finding(b, "apply_errors") == nil {
		t.Fatalf("a registered check must run: %v", b.Findings)
	}
}

func TestSubjectMatching(t *testing.T) {
	m := component(wire.KindMachine, "web-01", DBSignals{})
	b := component(wire.KindBouncer, "fw@10.0.0.5", DBSignals{})
	for _, tc := range []struct {
		s    alert.CrowdSecSubject
		c    *Component
		want bool
	}{
		{alert.CrowdSecSubject{CollectorID: "col_a", Scope: alert.ScopeAll}, m, true},
		{alert.CrowdSecSubject{CollectorID: "col_b", Scope: alert.ScopeAll}, m, false},
		{alert.CrowdSecSubject{CollectorID: "col_a", Scope: alert.ScopeMachine, Name: "web-01"}, m, true},
		{alert.CrowdSecSubject{CollectorID: "col_a", Scope: alert.ScopeMachine, Name: "web-01"}, b, false},
		{alert.CrowdSecSubject{CollectorID: "col_a", Scope: alert.ScopeGroup, Name: "fw"}, b, true},
		{alert.CrowdSecSubject{CollectorID: "col_a", Scope: alert.ScopeBouncer, Name: "fw"}, b, false},
		{alert.CrowdSecSubject{CollectorID: "col_a", Scope: alert.ScopeBouncer, Name: "fw@10.0.0.5"}, b, true},
	} {
		if got := tc.c.matches(tc.s); got != tc.want {
			t.Errorf("%+v on %s: %v", tc.s, tc.c.Name, got)
		}
	}
}
