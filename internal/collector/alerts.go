package collector

import (
	"context"
	"sort"

	"certainstats/internal/base/alert"
	"certainstats/internal/store"
)

// Rules is the part of the main database that holds collector alert rules and
// their incidents, which are ordinary alert rules and incident history. Every
// type uses it with its own source (its type name).
type Rules interface {
	SubjectRules(ctx context.Context, triggers []alert.TriggerType) ([]store.Alert, error)
	SubjectOpenIncidents(ctx context.Context, source, userID string) ([]store.SubjectIncident, error)
	SubjectFire(ctx context.Context, source string, rule store.Alert, collectorID, subjectID string, snap alert.SubjectSnapshot, value float64) (string, error)
	SubjectResolve(ctx context.Context, historyID string) (string, error)
	SubjectClose(ctx context.Context, historyID string) error
}

// AlertSource is implemented by types whose subjects alert rules can watch. It
// lists, per collector of the owner, what the Alerts page may offer.
type AlertSource interface {
	AlertSubjects(ctx context.Context, userID string) ([]alert.SubjectOptions, error)
}

// AlertSubjects gathers the alert subjects of every registered type, by type then collector name.
func (s *Service) AlertSubjects(ctx context.Context, userID string) ([]alert.SubjectOptions, error) {
	var out []alert.SubjectOptions
	for _, t := range s.Types() {
		src, ok := t.(AlertSource)
		if !ok {
			continue
		}
		list, err := src.AlertSubjects(ctx, userID)
		if err != nil {
			return nil, err
		}
		sort.SliceStable(list, func(i, j int) bool { return list[i].CollectorName < list[j].CollectorName })
		out = append(out, list...)
	}
	return out, nil
}

// Verdict is a type's finding for one rule on one subject.
type Verdict struct {
	// Judged is false when there is nothing to go on (never seen, data missing).
	Judged bool
	Bad    bool
	// KeepUnjudged keeps an open incident when the verdict is not judged; false
	// lets it close (the subject was muted, for example).
	KeepUnjudged bool
	Value        float64
	Snapshot     alert.SubjectSnapshot
}

type incidentKey struct{ alertID, subjectID string }

// Round is one evaluation pass of a source's rules. It loads the enabled rules
// and open incidents, takes a verdict per rule and subject, opens and resolves
// incidents, and finally closes quietly every open incident nothing kept: its
// rule is gone, disabled or no longer covers the subject, or the subject is gone.
type Round struct {
	source  string
	label   string
	rules   Rules
	deliver func(eventID string)
	list    []store.Alert
	byID    map[string]bool
	open    map[incidentKey]store.SubjectIncident
	kept    map[incidentKey]bool
}

// StartRound loads a round. With rules nil it evaluates nothing and closes nothing.
func StartRound(ctx context.Context, rules Rules, source string, deliver func(eventID string)) (*Round, error) {
	r := &Round{source: source, label: source, rules: rules, deliver: deliver,
		byID: map[string]bool{}, open: map[incidentKey]store.SubjectIncident{}, kept: map[incidentKey]bool{}}
	if src, ok := alert.SubjectSourceFor(source); ok {
		r.label = src.Label
	}
	if rules == nil {
		return r, nil
	}
	var err error
	if r.list, err = rules.SubjectRules(ctx, alert.SourceTriggers(source)); err != nil {
		return nil, err
	}
	for _, rule := range r.list {
		r.byID[rule.AlertID] = true
	}
	incidents, err := rules.SubjectOpenIncidents(ctx, source, "")
	if err != nil {
		return nil, err
	}
	for _, i := range incidents {
		r.open[incidentKey{i.AlertID, i.SubjectID}] = i
	}
	return r, nil
}

// RulesOf returns the round's rules of one owner.
func (r *Round) RulesOf(userID string) []store.Alert {
	var out []store.Alert
	for _, rule := range r.list {
		if rule.UserID == userID {
			out = append(out, rule)
		}
	}
	return out
}

// Users lists the owners that have rules in this round.
func (r *Round) Users() []string {
	seen := map[string]bool{}
	var out []string
	for _, rule := range r.list {
		if !seen[rule.UserID] {
			seen[rule.UserID] = true
			out = append(out, rule.UserID)
		}
	}
	return out
}

// Hold keeps every open incident of a collector that cannot be judged now, as
// long as its rule still exists.
func (r *Round) Hold(collectorID string) {
	for k, i := range r.open {
		if i.CollectorID == collectorID && r.byID[i.AlertID] {
			r.kept[k] = true
		}
	}
}

// IsOpen reports whether a rule has an open incident on a subject.
func (r *Round) IsOpen(alertID, subjectID string) bool {
	_, ok := r.open[incidentKey{alertID, subjectID}]
	return ok
}

// Judge applies one verdict of a rule on one subject.
func (r *Round) Judge(ctx context.Context, rule store.Alert, collectorID, subjectID string, v Verdict) error {
	k := incidentKey{rule.AlertID, subjectID}
	incident, isOpen := r.open[k]
	switch {
	case !v.Judged:
		if isOpen && v.KeepUnjudged {
			r.kept[k] = true
		}
	case v.Bad && isOpen:
		r.kept[k] = true
	case v.Bad:
		snap := v.Snapshot
		snap.Source, snap.SourceLabel = r.source, r.label
		eventID, err := r.rules.SubjectFire(ctx, r.source, rule, collectorID, subjectID, snap, v.Value)
		if err != nil {
			return err
		}
		r.kept[k] = true
		r.send(eventID)
	case isOpen:
		eventID, err := r.rules.SubjectResolve(ctx, incident.HistoryID)
		if err != nil {
			return err
		}
		r.kept[k] = true // resolved, not to be closed as well
		r.send(eventID)
	}
	return nil
}

// Finish closes, without a notification, every open incident nothing kept.
func (r *Round) Finish(ctx context.Context) error {
	if r.rules == nil {
		return nil
	}
	for k, i := range r.open {
		if r.kept[k] {
			continue
		}
		if err := r.rules.SubjectClose(ctx, i.HistoryID); err != nil {
			return err
		}
	}
	return nil
}

func (r *Round) send(eventID string) {
	if eventID != "" && r.deliver != nil {
		r.deliver(eventID)
	}
}
