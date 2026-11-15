package crowdsec

import (
	"encoding/json"
	"fmt"
	"net"
	"sort"
	"strings"
	"time"

	"certainstats/internal/base/alert"
	"certainstats/internal/crowdsec/wire"
)

// Machine and bouncer health is a list of named checks. A check reads the
// signals a collector reported about one component and says whether it is
// unhealthy. On the CrowdSec page a check failing at its default numbers is a
// caution (amber). Notifications come only from alert rules on the Alerts page:
// each check is also a rule condition, "crowdsec_<name>", whose threshold sets
// the check's RuleParam. A new source of signals (say, a collector on a
// bouncer's own host reading its log) adds a signal set under its own key and a
// check here; the rule condition, the page and the Alerts form follow.

// Signal sources. Each is a key in a component's signals.
const SourceDB = "db" // CrowdSec's database, read by the collector beside the Local API

// Param is one adjustable number of a check.
type Param struct {
	Key     string `json:"key"`
	Label   string `json:"label"`
	Unit    string `json:"unit"`
	Min     int    `json:"min"`
	Max     int    `json:"max"`
	Default int    `json:"default"`
}

// DBSignals is what CrowdSec's database says about a component.
type DBSignals struct {
	LastSeenMs int64            `json:"last_seen_ms,omitempty"`
	Validated  bool             `json:"validated,omitempty"`
	Revoked    bool             `json:"revoked,omitempty"`
	MetricsMs  int64            `json:"metrics_ms,omitempty"`
	Held       map[string]int64 `json:"held,omitempty"`
	Dropped    int64            `json:"dropped,omitempty"`
	Processed  int64            `json:"processed,omitempty"`
}

func (s DBSignals) heldTotal() int64 {
	var n int64
	for _, v := range s.Held {
		n += v
	}
	return n
}

// peer is another entry of the same shared bouncer key.
type peer struct {
	Name string
	DB   DBSignals
}

// evalInput is everything a check may look at.
type evalInput struct {
	Component *Component
	DB        DBSignals
	Values    map[string]int
	// Active is the Local API's active decisions by origin.
	Active map[string]int64
	// Peers are the other entries sharing this bouncer's key (name@ip).
	Peers []peer
	Now   int64
}

// verdict is a check's answer for one component.
type verdict struct {
	// Judged is false when the check has nothing to go on.
	Judged bool
	Bad    bool
	Detail string
	Value  float64
	// Sample identifies the observation. When set, a check with a "reports"
	// parameter needs that many different bad samples in a row.
	Sample int64
}

// Check is one registered health check.
type Check struct {
	Name  string `json:"name"`
	Label string `json:"label"`
	// Kind is the component kind it applies to.
	Kind string `json:"kind"`
	// Proves says what a finding does and does not show.
	Proves string  `json:"proves"`
	Params []Param `json:"params"`
	// RuleParam is the parameter an alert rule's threshold sets.
	RuleParam string `json:"rule_param"`
	// RuleLabel is the condition's wording in the Alerts form.
	RuleLabel string   `json:"rule_label"`
	Needs     []string `json:"needs"` // parts of the report it reads
	// Unit says how Value reads in a notification.
	Unit string                  `json:"unit"`
	Eval func(evalInput) verdict `json:"-"`
}

var checks = map[string]*Check{}

// TriggerType is the alert rule condition of a check.
func (c *Check) TriggerType() alert.TriggerType { return alert.TriggerType("crowdsec_" + c.Name) }

// checkForTrigger returns the check behind a rule condition.
func checkForTrigger(t alert.TriggerType) *Check {
	name, ok := strings.CutPrefix(string(t), "crowdsec_")
	if !ok {
		return nil
	}
	return checks[name]
}

// RegisterCheck adds a check and its rule condition. It panics on a duplicate, at start-up.
func RegisterCheck(c *Check) {
	if _, dup := checks[c.Name]; dup {
		panic("crowdsec: check " + c.Name + " registered twice")
	}
	checks[c.Name] = c
	if p, ok := c.param(c.RuleParam); ok {
		alert.RegisterSubjectTrigger(alert.SubjectTrigger{Type: c.TriggerType(), Group: "CrowdSec", Label: c.RuleLabel, Kind: c.Kind,
			Unit: p.Unit, Min: float64(p.Min), Max: float64(p.Max), Default: float64(p.Default), Hint: c.Proves})
	}
}

// Checks returns the registry in a stable order: machines first, then by name.
func Checks() []*Check {
	list := make([]*Check, 0, len(checks))
	for _, c := range checks {
		list = append(list, c)
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].Kind != list[j].Kind {
			return list[i].Kind == wire.KindMachine
		}
		return list[i].Name < list[j].Name
	})
	return list
}

func (c *Check) param(key string) (Param, bool) {
	for _, p := range c.Params {
		if p.Key == key {
			return p, true
		}
	}
	return Param{}, false
}

// defaults returns the check's numbers as shown on the CrowdSec page.
func (c *Check) defaults() map[string]int {
	out := map[string]int{}
	for _, p := range c.Params {
		out[p.Key] = p.Default
	}
	return out
}

// ruleValues returns the check's numbers for a rule: its threshold sets RuleParam.
func (c *Check) ruleValues(threshold float64) map[string]int {
	out := c.defaults()
	if p, ok := c.param(c.RuleParam); ok {
		out[p.Key] = min(max(int(threshold), p.Min), p.Max)
	}
	return out
}

func silentFor(ms int64) string {
	d := time.Duration(ms) * time.Millisecond
	switch {
	case d >= 48*time.Hour:
		return fmt.Sprintf("%dd", int(d/(24*time.Hour)))
	case d >= 2*time.Hour:
		return fmt.Sprintf("%dh", int(d/time.Hour))
	}
	return fmt.Sprintf("%dm", max(int(d/time.Minute), 1))
}

// quiet builds the verdict of a "not seen for too long" check.
func quiet(in evalInput, what string) verdict {
	if in.DB.LastSeenMs <= 0 {
		return verdict{} // never seen: shown as such, not judged
	}
	age := in.Now - in.DB.LastSeenMs
	v := verdict{Judged: true, Value: float64(age) / 60000}
	if age > int64(in.Values["minutes"])*60000 {
		v.Bad, v.Detail = true, "No "+what+" for "+silentFor(age)
	}
	return v
}

// metricsFresh is how old a bouncer's last usage metrics may be and still be compared.
const metricsFresh = 2 * time.Hour

func (s DBSignals) metricsUsable(now int64) bool {
	return !s.Revoked && s.MetricsMs > 0 && s.Held != nil && now-s.MetricsMs <= metricsFresh.Milliseconds()
}

func init() {
	minutes := func(def int) []Param {
		return []Param{{Key: "minutes", Label: "Quiet for", Unit: "minutes", Min: 2, Max: 43200, Default: def}}
	}
	RegisterCheck(&Check{
		Name: "heartbeat", Label: "Machine heartbeat", Kind: wire.KindMachine, Unit: "minutes",
		RuleLabel: "Machine sends no heartbeat for (minutes)", RuleParam: "minutes",
		Proves: "The CrowdSec agent on that machine is running and reaches the Local API.",
		Params: minutes(5), Needs: []string{wire.PartMachines},
		Eval: func(in evalInput) verdict {
			if !in.DB.Validated {
				return verdict{}
			}
			return quiet(in, "heartbeat")
		},
	})
	RegisterCheck(&Check{
		Name: "pull", Label: "Bouncer pull", Kind: wire.KindBouncer, Unit: "minutes",
		RuleLabel: "Bouncer pulls no decisions for (minutes)", RuleParam: "minutes",
		Proves: "The bouncer is running and fetching decisions. A bouncer in live mode only asks when it has traffic, so allow a longer quiet time for it.",
		Params: minutes(60), Needs: []string{wire.PartBouncers},
		Eval: func(in evalInput) verdict {
			if in.DB.Revoked {
				return verdict{}
			}
			return quiet(in, "pull")
		},
	})
	RegisterCheck(&Check{
		Name: "held", Label: "Bouncer holds decisions", Kind: wire.KindBouncer, Unit: "%",
		RuleLabel: "Bouncer holds under (%) of the Local API's active decisions", RuleParam: "percent",
		Proves: "Compares how many decisions the bouncer says its firewall holds with how many the Local API has active. A gap can mean decisions are not being applied, but sizes alone cannot prove it; the bouncer's own log has the reason. Needs CrowdSec 1.6.3 or newer and a bouncer that sends usage metrics.",
		Params: []Param{
			{Key: "percent", Label: "Holds under", Unit: "%", Min: 1, Max: 100, Default: 50},
			{Key: "min_active", Label: "Only when active decisions are at least", Min: 1, Max: 1000000, Default: 20},
			{Key: "reports", Label: "For this many reports in a row", Min: 1, Max: 10, Default: 2},
		},
		Needs: []string{wire.PartBouncers, wire.PartMetrics, wire.PartDecisions},
		Eval:  evalHeld,
	})
	RegisterCheck(&Check{
		Name: "siblings", Label: "Bouncer behind its siblings", Kind: wire.KindBouncer, Unit: "%",
		RuleLabel: "Bouncer holds under (%) of what its siblings on the same key hold", RuleParam: "percent",
		Proves: "CrowdSec makes one entry per address when a bouncer key is shared (name@ip). Those entries apply the same decisions, so one holding almost nothing while its siblings hold thousands points at a broken ipset or nftables set on that host; its log has the reason. Needs bouncers that send usage metrics.",
		Params: []Param{
			{Key: "percent", Label: "Holds under", Unit: "%", Min: 1, Max: 100, Default: 10},
			{Key: "min_held", Label: "Only when that sibling holds at least", Min: 1, Max: 10000000, Default: 1000},
			{Key: "reports", Label: "For this many reports in a row", Min: 1, Max: 10, Default: 2},
		},
		Needs: []string{wire.PartBouncers, wire.PartMetrics},
		Eval:  evalSiblings,
	})
}

func evalHeld(in evalInput) verdict {
	db := in.DB
	if !db.metricsUsable(in.Now) {
		return verdict{}
	}
	v := verdict{Judged: true, Sample: db.MetricsMs, Value: 100}
	// Only origins the bouncer reports are compared, worst first.
	origins := make([]string, 0, len(db.Held))
	for origin := range db.Held {
		origins = append(origins, origin)
	}
	sort.Strings(origins)
	for _, origin := range origins {
		held, active := db.Held[origin], in.Active[origin]
		if active < int64(in.Values["min_active"]) {
			continue
		}
		share := float64(held) * 100 / float64(active)
		if share >= v.Value {
			continue
		}
		v.Value = share
		if share < float64(in.Values["percent"]) {
			v.Bad = true
			v.Detail = fmt.Sprintf("Holds %d of %d %s decisions, may not be applying them; check the bouncer's log", held, active, origin)
		}
	}
	return v
}

func evalSiblings(in evalInput) verdict {
	db := in.DB
	if !db.metricsUsable(in.Now) || len(in.Peers) == 0 {
		return verdict{}
	}
	var top peer
	for _, p := range in.Peers {
		if p.DB.metricsUsable(in.Now) && p.DB.heldTotal() > top.DB.heldTotal() {
			top = p
		}
	}
	if top.Name == "" || top.DB.heldTotal() < int64(in.Values["min_held"]) {
		return verdict{}
	}
	held, best := db.heldTotal(), top.DB.heldTotal()
	v := verdict{Judged: true, Sample: db.MetricsMs, Value: float64(held) * 100 / float64(best)}
	if v.Value < float64(in.Values["percent"]) {
		v.Bad = true
		v.Detail = fmt.Sprintf("Holds %d of the %d decisions its sibling %s holds; its ipset or nftables set may be failing, check its log", held, best, top.Name)
	}
	return v
}

// BaseName is a bouncer's name without the "@<ip>" suffixes CrowdSec appends
// when one key is used from several addresses, as its own baseBouncerName does.
func BaseName(name string) string {
	for {
		i := strings.LastIndexByte(name, '@')
		if i <= 0 || net.ParseIP(name[i+1:]) == nil {
			return name
		}
		name = name[:i]
	}
}

// Finding is one check currently failing for a component at its default numbers.
type Finding struct {
	Check   string  `json:"check"`
	Detail  string  `json:"detail"`
	SinceMs int64   `json:"since_ms"`
	Value   float64 `json:"value"`
}

// progress counts bad samples in a row, for checks that need more than one.
type progress struct {
	Streak int   `json:"streak"`
	Sample int64 `json:"sample"`
}

func (c *Component) db() DBSignals {
	var s DBSignals
	if raw, ok := c.Signals[SourceDB]; ok {
		_ = json.Unmarshal(raw, &s) // damaged signals read as none: nothing is judged until the next report
	}
	return s
}

// evalContext is what an evaluation knows beyond the component.
type evalContext struct {
	Active map[string]int64
	Parts  map[string]bool
	// Self is the collector's own machine login, which never sends a heartbeat.
	Self string
	// Groups holds the bouncers of the collector by base name.
	Groups map[string][]*Component
	Now    int64
}

func (c *Component) skipped(ec evalContext) bool {
	return c.Muted || (c.Kind == wire.KindMachine && ec.Self != "" && c.Name == ec.Self)
}

// readable reports whether the parts a check needs were read.
func (ec evalContext) readable(check *Check) bool {
	for _, part := range check.Needs {
		if !ec.Parts[part] {
			return false
		}
	}
	return true
}

func (ec evalContext) peers(c *Component) []peer {
	if c.Kind != wire.KindBouncer {
		return nil
	}
	var out []peer
	for _, other := range ec.Groups[BaseName(c.Name)] {
		if other.ID != c.ID {
			out = append(out, peer{Name: other.Name, DB: other.db()})
		}
	}
	return out
}

// groupBouncers indexes a collector's bouncers by base name.
func groupBouncers(comps []*Component) map[string][]*Component {
	out := map[string][]*Component{}
	for _, c := range comps {
		if c.Kind == wire.KindBouncer {
			out[BaseName(c.Name)] = append(out[BaseName(c.Name)], c)
		}
	}
	return out
}

// result is one check's conclusion for one component.
type result struct {
	Judged bool
	Bad    bool // and confirmed over the reports it needs
	Detail string
	Value  float64
}

// run evaluates one check with values, keeping its streak under key in the
// component's progress. Muted components, the collector's own login and checks
// whose data is not read are not judged.
func (c *Component) run(check *Check, values map[string]int, ec evalContext, key string) result {
	if c.Progress == nil {
		c.Progress = map[string]progress{}
	}
	if check.Kind != c.Kind || c.skipped(ec) || !ec.readable(check) {
		delete(c.Progress, key)
		return result{}
	}
	v := check.Eval(evalInput{Component: c, DB: c.db(), Values: values, Active: ec.Active, Peers: ec.peers(c), Now: ec.Now})
	p := c.Progress[key]
	out := result{Judged: v.Judged, Value: v.Value, Detail: v.Detail}
	switch {
	case !v.Judged:
		p = progress{}
	case !v.Bad:
		p = progress{Sample: v.Sample}
	default:
		need := max(values["reports"], 1)
		if v.Sample == 0 {
			p.Streak = need
		} else if v.Sample != p.Sample {
			p.Streak, p.Sample = p.Streak+1, v.Sample
		}
		out.Bad = p.Streak >= need
	}
	if p == (progress{}) {
		delete(c.Progress, key)
	} else {
		c.Progress[key] = p
	}
	return out
}

// evaluate runs every check at its default numbers and refreshes the
// component's findings, which the CrowdSec page shows as cautions.
func (c *Component) evaluate(ec evalContext) {
	old := map[string]Finding{}
	for _, f := range c.Findings {
		old[f.Check] = f
	}
	c.Findings = nil
	for _, check := range Checks() {
		r := c.run(check, check.defaults(), ec, check.Name)
		if !r.Bad {
			continue
		}
		f := Finding{Check: check.Name, Detail: r.Detail, SinceMs: ec.Now, Value: r.Value}
		if was, ok := old[check.Name]; ok {
			f.SinceMs = was.SinceMs
		}
		c.Findings = append(c.Findings, f)
	}
}

// Component states shown on the page.
const (
	StateOK      = "ok"
	StateCaution = "caution" // a check fails at its default numbers
	StateAlert   = "alert"   // an alert rule is firing on it
	StateNever   = "never"   // registered with CrowdSec, never seen
	StateUnknown = "unknown" // cannot be judged right now
	StateMuted   = "muted"
	StateSelf    = "self" // the collector's own machine login
)

// state is the one word shown for a component. judged is false while its
// collector's reports are missing, old or failing; firing is true while an
// alert rule has an open incident on it.
func (c *Component) state(judged bool, self string, firing bool) string {
	switch {
	case c.Kind == wire.KindMachine && self != "" && c.Name == self:
		return StateSelf
	case c.Muted:
		return StateMuted
	case firing:
		return StateAlert
	case !judged:
		return StateUnknown
	case len(c.Findings) > 0:
		return StateCaution
	case c.db().LastSeenMs <= 0:
		return StateNever
	}
	return StateOK
}

// matches reports whether a rule subject covers this component.
func (c *Component) matches(s alert.CrowdSecSubject) bool {
	if s.CollectorID != c.CollectorID {
		return false
	}
	switch s.Scope {
	case alert.ScopeAll:
		return true
	case alert.ScopeMachine:
		return c.Kind == wire.KindMachine && c.Name == s.Name
	case alert.ScopeBouncer:
		return c.Kind == wire.KindBouncer && c.Name == s.Name
	case alert.ScopeGroup:
		return c.Kind == wire.KindBouncer && BaseName(c.Name) == s.Name
	}
	return false
}

// key identifies a component in a rule incident: collector/kind/name.
func (c *Component) key() string { return c.CollectorID + "/" + c.Kind + "/" + c.Name }
