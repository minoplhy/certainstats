package alertdelivery

import (
	"sync"
	"testing"

	a "certainstats/internal/base/alert"
)

var deliverySource sync.Once

func TestCollectorSubjectLabels(t *testing.T) {
	deliverySource.Do(func() {
		a.RegisterSubjectSource(a.SubjectSource{Name: "crowdsec", Label: "CrowdSec"})
	})
	for kind, want := range map[string]bool{"crowdsec": true, "knot": true, "agent": false, "network_monitor": false, "": false} {
		if isCollectorSubject(kind) != want {
			t.Errorf("%q: got %v", kind, !want)
		}
	}
	// A new snapshot names its source; an incident from before that falls back to
	// the registered label, and an unknown one to its kind.
	if got := sourceLabel("knot", a.SubjectSnapshot{SourceLabel: "Knot DNS"}); got != "Knot DNS" {
		t.Errorf("snapshot label: %q", got)
	}
	if got := sourceLabel("crowdsec", a.SubjectSnapshot{}); got != "CrowdSec" {
		t.Errorf("old CrowdSec snapshot: %q", got)
	}
	if got := sourceLabel("gone", a.SubjectSnapshot{}); got != "gone" {
		t.Errorf("unknown source: %q", got)
	}
	if formatValue(75) != "75" || formatValue(3.25) != "3.2" && formatValue(3.25) != "3.3" {
		t.Errorf("values: %q %q", formatValue(75), formatValue(3.25))
	}
}
