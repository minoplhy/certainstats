package crowdsec

import (
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"certainstats/internal/crowdsec/wire"
)

// Machine and bouncer health is a list of named checks. A check reads the
// signals a collector reported about one component and says whether it is
// unhealthy. Level and thresholds are set per collector, so nothing about
// severity is fixed in code. A new source of signals (say, a collector on a
// bouncer's own host reading its log) adds a signal set under its own key and a
// check here; the settings, storage, routes and form follow from the registry.

// Levels a check can be set to.
const (
	LevelOff     = "off"
	LevelCaution = "caution" // shown, never an incident, never notified
	LevelAlert   = "alert"   // an incident, notified
)

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

// CheckSetting is one check's configuration for one collector.
type CheckSetting struct {
	Level  string         `json:"level"`
	Values map[string]int `json:"values"`
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

// evalInput is everything a check may look at.
type evalInput struct {
	Component *Component
	DB        DBSignals
	Values    map[string]int
	// Active is the Local API's active decisions by origin.
	Active map[string]int64
	Now    int64
}

// verdict is a check's answer for one component.
type verdict struct {
	// Judged is false when the check has nothing to go on; any finding is then dropped quietly.
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
	// Proves says, for the settings form, what a finding does and does not show.
	Proves       string   `json:"proves"`
	DefaultLevel string   `json:"default_level"`
	Params       []Param  `json:"params"`
	Needs        []string `json:"needs"` // parts of the report it reads
	// Unit says how Value reads in a notification: "minutes" or "percent".
	Unit string                  `json:"unit"`
	Eval func(evalInput) verdict `json:"-"`
}

var checks = map[string]*Check{}

// RegisterCheck adds a check. It panics on a duplicate name, at start-up.
func RegisterCheck(c *Check) {
	if _, dup := checks[c.Name]; dup {
		panic("crowdsec: check " + c.Name + " registered twice")
	}
	checks[c.Name] = c
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

// setting returns the check's configuration with defaults for whatever is not set,
// so settings saved before a check or parameter existed keep working.
func (c *Check) setting(saved map[string]CheckSetting) CheckSetting {
	out := CheckSetting{Level: c.DefaultLevel, Values: map[string]int{}}
	got, ok := saved[c.Name]
	if ok && (got.Level == LevelOff || got.Level == LevelCaution || got.Level == LevelAlert) {
		out.Level = got.Level
	}
	for _, p := range c.Params {
		out.Values[p.Key] = p.Default
		if v, set := got.Values[p.Key]; ok && set && v >= p.Min && v <= p.Max {
			out.Values[p.Key] = v
		}
	}
	return out
}

// validate checks a setting sent by the user.
func (c *Check) validate(s CheckSetting) error {
	if s.Level != LevelOff && s.Level != LevelCaution && s.Level != LevelAlert {
		return fmt.Errorf("%s: level must be off, caution or alert", c.Label)
	}
	known := map[string]Param{}
	for _, p := range c.Params {
		known[p.Key] = p
	}
	for key, v := range s.Values {
		p, ok := known[key]
		if !ok {
			return fmt.Errorf("%s: unknown setting %q", c.Label, key)
		}
		if v < p.Min || v > p.Max {
			return fmt.Errorf("%s: %s must be %d to %d", c.Label, p.Label, p.Min, p.Max)
		}
	}
	return nil
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

func init() {
	minutes := func(def int) []Param {
		return []Param{{Key: "minutes", Label: "Quiet for", Unit: "minutes", Min: 2, Max: 43200, Default: def}}
	}
	RegisterCheck(&Check{
		Name: "heartbeat", Label: "Machine heartbeat", Kind: wire.KindMachine, DefaultLevel: LevelAlert, Unit: "minutes",
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
		Name: "pull", Label: "Bouncer pull", Kind: wire.KindBouncer, DefaultLevel: LevelAlert, Unit: "minutes",
		Proves: "The bouncer is running and fetching decisions. A bouncer in live mode only asks when it has traffic, so mute it or allow a longer quiet time.",
		Params: minutes(60), Needs: []string{wire.PartBouncers},
		Eval: func(in evalInput) verdict {
			if in.DB.Revoked {
				return verdict{}
			}
			return quiet(in, "pull")
		},
	})
	RegisterCheck(&Check{
		Name: "held", Label: "Bouncer holds decisions", Kind: wire.KindBouncer, DefaultLevel: LevelCaution, Unit: "percent",
		Proves: "Compares how many decisions the bouncer says its firewall holds with how many the Local API has active. A gap can mean decisions are not being applied, but sizes alone cannot prove it; the bouncer's own log has the reason. Needs CrowdSec 1.6.3 or newer and a bouncer that sends usage metrics.",
		Params: []Param{
			{Key: "percent", Label: "Holds under", Unit: "% of active", Min: 1, Max: 100, Default: 50},
			{Key: "min_active", Label: "Only when active decisions are at least", Unit: "", Min: 1, Max: 1000000, Default: 20},
			{Key: "reports", Label: "For this many reports in a row", Unit: "", Min: 1, Max: 10, Default: 2},
		},
		Needs: []string{wire.PartBouncers, wire.PartMetrics, wire.PartDecisions},
		Eval:  evalHeld,
	})
}

func evalHeld(in evalInput) verdict {
	db := in.DB
	if db.Revoked || db.MetricsMs <= 0 || db.Held == nil || in.Now-db.MetricsMs > metricsFresh.Milliseconds() {
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

// Finding is one check currently unhealthy for a component.
type Finding struct {
	Check   string  `json:"check"`
	Level   string  `json:"level"`
	Detail  string  `json:"detail"`
	SinceMs int64   `json:"since_ms"`
	Value   float64 `json:"value"`
}

// progress counts bad samples in a row, for checks that need more than one.
type progress struct {
	Streak int   `json:"streak"`
	Sample int64 `json:"sample"`
}

// outcome is what one check concluded for one component on one evaluation.
type outcome struct {
	Check   *Check
	Finding *Finding // nil when healthy or not judged
	// Recovered is true when a finding ended because the check passed, as
	// opposed to the check being switched off, muted or unable to judge.
	Recovered bool
}

func (c *Component) db() DBSignals {
	var s DBSignals
	if raw, ok := c.Signals[SourceDB]; ok {
		_ = json.Unmarshal(raw, &s)
	}
	return s
}

// evalContext is what an evaluation knows beyond the component.
type evalContext struct {
	Settings map[string]CheckSetting
	Active   map[string]int64
	Parts    map[string]bool
	// Self is the collector's own machine login, which never sends a heartbeat.
	Self string
	Now  int64
}

func (c *Component) skipped(ec evalContext) bool {
	return c.Muted || (c.Kind == wire.KindMachine && ec.Self != "" && c.Name == ec.Self)
}

// evaluate runs every check that applies to c and updates its findings and
// progress in place. It returns one outcome per applicable check.
func (c *Component) evaluate(ec evalContext) []outcome {
	old := map[string]Finding{}
	for _, f := range c.Findings {
		old[f.Check] = f
	}
	if c.Progress == nil {
		c.Progress = map[string]progress{}
	}
	c.Findings = nil
	var out []outcome
	in := evalInput{Component: c, DB: c.db(), Active: ec.Active, Now: ec.Now}
	for _, check := range Checks() {
		if check.Kind != c.Kind {
			continue
		}
		o := outcome{Check: check}
		set := check.setting(ec.Settings)
		readable := true
		for _, part := range check.Needs {
			readable = readable && ec.Parts[part]
		}
		if set.Level == LevelOff || !readable || c.skipped(ec) {
			delete(c.Progress, check.Name)
			out = append(out, o)
			continue
		}
		in.Values = set.Values
		v := check.Eval(in)
		p := c.Progress[check.Name]
		switch {
		case !v.Judged:
			p = progress{}
		case !v.Bad:
			o.Recovered = true
			p = progress{Sample: v.Sample}
		default:
			need := max(set.Values["reports"], 1)
			if v.Sample == 0 {
				p.Streak = need
			} else if v.Sample != p.Sample {
				p.Streak, p.Sample = p.Streak+1, v.Sample
			}
			if p.Streak >= need {
				f := Finding{Check: check.Name, Level: set.Level, Detail: v.Detail, SinceMs: ec.Now, Value: v.Value}
				if was, ok := old[check.Name]; ok {
					f.SinceMs = was.SinceMs
				}
				o.Finding = &f
				c.Findings = append(c.Findings, f)
			}
		}
		if p == (progress{}) {
			delete(c.Progress, check.Name)
		} else {
			c.Progress[check.Name] = p
		}
		out = append(out, o)
	}
	return out
}

// Component states shown on the page.
const (
	StateOK      = "ok"
	StateCaution = "caution"
	StateAlert   = "alert"
	StateNever   = "never"   // registered with CrowdSec, never seen
	StateUnknown = "unknown" // cannot be judged right now
	StateMuted   = "muted"
	StateSelf    = "self" // the collector's own machine login
)

// state is the one word shown for a component. judged is false while its
// collector's reports are missing, old or failing.
func (c *Component) state(judged bool, self string) string {
	switch {
	case c.Kind == wire.KindMachine && self != "" && c.Name == self:
		return StateSelf
	case c.Muted:
		return StateMuted
	case !judged:
		return StateUnknown
	}
	worst := StateOK
	for _, f := range c.Findings {
		if f.Level == LevelAlert {
			return StateAlert
		}
		worst = StateCaution
	}
	if worst == StateOK && c.db().LastSeenMs <= 0 {
		return StateNever
	}
	return worst
}
