package alert

import (
	"slices"
	"sort"
	"strings"
	"sync"
)

// Subject triggers are rule conditions about something a collector type watches
// rather than a server or a network monitor: a CrowdSec bouncer, a Knot DNS zone.
// Each type registers itself as a source and its conditions at start-up, so the
// alert layer can validate rules and the Alerts page can offer them without
// knowing any type.

// SubjectTrigger describes one registered condition. The rule's Threshold is
// its one adjustable number, in Unit, between Min and Max.
type SubjectTrigger struct {
	Type    TriggerType `json:"type"`
	Source  string      `json:"source"` // the collector type that evaluates it, e.g. "crowdsec"
	Group   string      `json:"group"`  // heading in the "When" list, the source's label
	Label   string      `json:"label"`  // e.g. "Bouncer pull: quiet for (minutes)"
	Kind    string      `json:"kind"`   // the subject kind it applies to, e.g. "bouncer"
	Unit    string      `json:"unit"`   // e.g. "minutes", "%"
	Min     float64     `json:"min"`
	Max     float64     `json:"max"`
	Default float64     `json:"default"`
	Hint    string      `json:"hint"` // what a firing rule does and does not show
}

// SubjectSource is a collector type that rules can watch: its name (the type
// name, used as the incident's subject kind), its label, and the scopes its
// subjects may have besides ScopeAll.
type SubjectSource struct {
	Name   string
	Label  string
	Scopes []string
	// Empty tells the Alerts page what to do when no collector offers subjects yet.
	Empty string
}

// ScopeAll watches everything a source reports on one collector, new subjects included.
const ScopeAll = "all"

var (
	subjectMu       sync.RWMutex
	subjectTriggers = map[TriggerType]SubjectTrigger{}
	subjectSources  = map[string]SubjectSource{}
)

// RegisterSubjectSource adds a source. It panics on a duplicate, at start-up.
func RegisterSubjectSource(s SubjectSource) {
	subjectMu.Lock()
	defer subjectMu.Unlock()
	if _, dup := subjectSources[s.Name]; dup || s.Name == "" || strings.Contains(s.Name, "/") {
		panic("alert: subject source " + s.Name + " registered twice or badly named")
	}
	subjectSources[s.Name] = s
}

// SubjectSourceFor returns a registered source.
func SubjectSourceFor(name string) (SubjectSource, bool) {
	subjectMu.RLock()
	defer subjectMu.RUnlock()
	s, ok := subjectSources[name]
	return s, ok
}

// SubjectSources lists the registered sources by name.
func SubjectSources() []SubjectSource {
	subjectMu.RLock()
	defer subjectMu.RUnlock()
	list := make([]SubjectSource, 0, len(subjectSources))
	for _, s := range subjectSources {
		list = append(list, s)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Name < list[j].Name })
	return list
}

// IsSubjectKind reports whether an incident's subject kind is a collector source.
func IsSubjectKind(kind string) bool {
	_, ok := SubjectSourceFor(kind)
	return ok
}

// RegisterSubjectTrigger adds a condition of a registered source. It panics on
// a duplicate or an unknown source, at start-up. Group defaults to the source's label.
func RegisterSubjectTrigger(t SubjectTrigger) {
	subjectMu.Lock()
	defer subjectMu.Unlock()
	src, ok := subjectSources[t.Source]
	if !ok {
		panic("alert: subject trigger " + string(t.Type) + " has unknown source " + t.Source)
	}
	if _, dup := subjectTriggers[t.Type]; dup {
		panic("alert: subject trigger " + string(t.Type) + " registered twice")
	}
	if t.Group == "" {
		t.Group = src.Label
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

// SourceOf returns the source evaluating t, when t is a subject condition.
func SourceOf(t TriggerType) (string, bool) {
	st, ok := SubjectTriggerFor(t)
	return st.Source, ok
}

// SubjectTriggers lists every registered condition, by source then type.
func SubjectTriggers() []SubjectTrigger {
	subjectMu.RLock()
	defer subjectMu.RUnlock()
	list := make([]SubjectTrigger, 0, len(subjectTriggers))
	for _, t := range subjectTriggers {
		list = append(list, t)
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].Source != list[j].Source {
			return list[i].Source < list[j].Source
		}
		return list[i].Type < list[j].Type
	})
	return list
}

// SourceTriggers lists the condition types of one source.
func SourceTriggers(source string) []TriggerType {
	var out []TriggerType
	for _, t := range SubjectTriggers() {
		if t.Source == source {
			out = append(out, t.Type)
		}
	}
	return out
}

// Subject is what a rule watches on one collector: everything of a source
// (ScopeAll), or one named subject in a source scope.
type Subject struct {
	Source      string `json:"source"`
	CollectorID string `json:"collector_id"`
	Scope       string `json:"scope"`
	Name        string `json:"name,omitempty"`
}

// Key is the subject's form value: source/collector/scope[/name]. Sources,
// collector IDs and scopes never contain "/", so the name may.
func (s Subject) Key() string {
	if s.Scope == ScopeAll {
		return s.Source + "/" + s.CollectorID + "/" + ScopeAll
	}
	return s.Source + "/" + s.CollectorID + "/" + s.Scope + "/" + s.Name
}

// ParseSubject reads a form value written by Key, checking the source and its scope.
func ParseSubject(v string) (Subject, bool) {
	parts := strings.SplitN(strings.TrimSpace(v), "/", 4)
	if len(parts) < 3 || parts[0] == "" || parts[1] == "" {
		return Subject{}, false
	}
	s := Subject{Source: parts[0], CollectorID: parts[1], Scope: parts[2]}
	return s, s.valid(len(parts) == 4, partOr(parts, 3))
}

func partOr(parts []string, i int) string {
	if i < len(parts) {
		return parts[i]
	}
	return ""
}

func (s *Subject) valid(hasName bool, name string) bool {
	src, ok := SubjectSourceFor(s.Source)
	if !ok {
		return false
	}
	if s.Scope == ScopeAll {
		return !hasName
	}
	if !slices.Contains(src.Scopes, s.Scope) || name == "" {
		return false
	}
	s.Name = name
	return true
}

// Valid reports whether s names a registered source and one of its scopes.
func (s Subject) Valid() bool {
	_, ok := ParseSubject(s.Key())
	return ok && s.CollectorID != "" && !strings.Contains(s.CollectorID, "/")
}

// SubjectSnapshot is kept with a collector incident: what fired, in words.
type SubjectSnapshot struct {
	Source      string `json:"source,omitempty"`
	SourceLabel string `json:"source_label,omitempty"`
	Collector   string `json:"collector"`
	Kind        string `json:"kind"`
	Name        string `json:"name"`
	Label       string `json:"label"`
	Detail      string `json:"detail"`
	Unit        string `json:"unit"`
}

// SubjectOptions is what one collector offers to rules of one source, for the
// Alerts page: ScopeAll, then sections of named subjects.
type SubjectOptions struct {
	Source        string           `json:"source"`
	CollectorID   string           `json:"collector_id"`
	CollectorName string           `json:"collector_name"`
	AllLabel      string           `json:"all_label"` // what ScopeAll covers, e.g. "Every machine and bouncer"
	Sections      []SubjectSection `json:"sections"`
}

// SubjectSection is one group of named subjects of one kind.
type SubjectSection struct {
	Label   string         `json:"label"`
	Kind    string         `json:"kind"`
	Entries []SubjectEntry `json:"entries"`
}

// SubjectEntry is one named subject. Note is shown beside its name; Child
// indents it under the entry before (a bouncer address under its shared key).
type SubjectEntry struct {
	Scope string `json:"scope"`
	Name  string `json:"name"`
	Note  string `json:"note,omitempty"`
	Child bool   `json:"child,omitempty"`
}

// Key is the form value of an entry of o.
func (o SubjectOptions) Key(e SubjectEntry) string {
	return Subject{Source: o.Source, CollectorID: o.CollectorID, Scope: e.Scope, Name: e.Name}.Key()
}

// AllKey is the form value of ScopeAll for o.
func (o SubjectOptions) AllKey() string {
	return Subject{Source: o.Source, CollectorID: o.CollectorID, Scope: ScopeAll}.Key()
}
