package alert

import (
	"sort"
	"strings"
	"sync"
)

// Subject triggers are rule conditions about something other than a server or a
// network monitor, such as a CrowdSec machine or bouncer. The package that
// evaluates them registers each one at start-up, so the alert layer can
// validate rules and the Alerts page can offer them without knowing the details.

// SubjectTrigger describes one registered condition. The rule's Threshold is
// its one adjustable number, in Unit, between Min and Max.
type SubjectTrigger struct {
	Type    TriggerType `json:"type"`
	Group   string      `json:"group"` // heading in the "When" list, e.g. "CrowdSec"
	Label   string      `json:"label"` // e.g. "Bouncer pull: quiet for (minutes)"
	Kind    string      `json:"kind"`  // the subject kind it applies to, e.g. "bouncer"
	Unit    string      `json:"unit"`  // e.g. "minutes", "%"
	Min     float64     `json:"min"`
	Max     float64     `json:"max"`
	Default float64     `json:"default"`
	Hint    string      `json:"hint"` // what a firing rule does and does not show
}

var (
	subjectMu       sync.RWMutex
	subjectTriggers = map[TriggerType]SubjectTrigger{}
)

// RegisterSubjectTrigger adds a condition. It panics on a duplicate, at start-up.
func RegisterSubjectTrigger(t SubjectTrigger) {
	subjectMu.Lock()
	defer subjectMu.Unlock()
	if _, dup := subjectTriggers[t.Type]; dup {
		panic("alert: subject trigger " + string(t.Type) + " registered twice")
	}
	subjectTriggers[t.Type] = t
}

// SubjectTriggerFor returns the registered condition for t.
func SubjectTriggerFor(t TriggerType) (SubjectTrigger, bool) {
	subjectMu.RLock()
	defer subjectMu.RUnlock()
	st, ok := subjectTriggers[t]
	return st, ok
}

// SubjectTriggers lists every registered condition in a stable order.
func SubjectTriggers() []SubjectTrigger {
	subjectMu.RLock()
	defer subjectMu.RUnlock()
	list := make([]SubjectTrigger, 0, len(subjectTriggers))
	for _, t := range subjectTriggers {
		list = append(list, t)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Type < list[j].Type })
	return list
}

// IsCrowdSecTrigger reports whether t is a CrowdSec machine or bouncer condition.
func IsCrowdSecTrigger(t TriggerType) bool { return strings.HasPrefix(string(t), "crowdsec_") }

// Scopes a CrowdSec rule subject can have.
const (
	ScopeAll     = "all"     // every machine and bouncer of a collector, new ones included
	ScopeMachine = "machine" // one machine
	ScopeBouncer = "bouncer" // one bouncer entry
	ScopeGroup   = "group"   // every entry of one shared bouncer key (name@ip), new IPs included
)

// CrowdSecSubject is what a CrowdSec rule watches on one collector.
type CrowdSecSubject struct {
	CollectorID string `json:"collector_id"`
	Scope       string `json:"scope"`
	Name        string `json:"name,omitempty"`
}

// Key is the subject's form value: collector/scope/name. Collector IDs and
// scopes never contain "/", so the name may.
func (s CrowdSecSubject) Key() string {
	if s.Scope == ScopeAll {
		return s.CollectorID + "/" + ScopeAll
	}
	return s.CollectorID + "/" + s.Scope + "/" + s.Name
}

// ParseCrowdSecSubject reads a form value written by Key.
func ParseCrowdSecSubject(v string) (CrowdSecSubject, bool) {
	parts := strings.SplitN(strings.TrimSpace(v), "/", 3)
	if len(parts) < 2 || parts[0] == "" {
		return CrowdSecSubject{}, false
	}
	s := CrowdSecSubject{CollectorID: parts[0], Scope: parts[1]}
	switch s.Scope {
	case ScopeAll:
		return s, len(parts) == 2
	case ScopeMachine, ScopeBouncer, ScopeGroup:
		if len(parts) != 3 || parts[2] == "" {
			return CrowdSecSubject{}, false
		}
		s.Name = parts[2]
		return s, true
	}
	return CrowdSecSubject{}, false
}

// CrowdSecSnapshot is kept with a CrowdSec incident: what fired, in words.
type CrowdSecSnapshot struct {
	Collector string `json:"collector"`
	Kind      string `json:"kind"`
	Name      string `json:"name"`
	Label     string `json:"label"`
	Detail    string `json:"detail"`
	Unit      string `json:"unit"`
}
