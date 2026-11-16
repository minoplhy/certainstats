package web

import (
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	c "certainstats/internal/base/alert"
)

// A source the web layer knows nothing about, registered the way a collector type does.
var testSource sync.Once

func registerTestSource() {
	testSource.Do(func() {
		c.RegisterSubjectSource(c.SubjectSource{Name: "websrc", Label: "Web Source", Scopes: []string{"item"}, Empty: "Nothing to watch yet."})
		c.RegisterSubjectTrigger(c.SubjectTrigger{Type: "websrc_quiet", Source: "websrc", Kind: "item", Label: "Item quiet for (minutes)",
			Unit: "minutes", Min: 1, Max: 60, Default: 5, Hint: "Says the item stopped."})
		c.RegisterSubjectTrigger(c.SubjectTrigger{Type: "websrc_down", Source: "websrc", Kind: "host", Label: "Host down for (minutes)",
			Unit: "minutes", Min: 1, Max: 60, Default: 5})
	})
}

func TestAlertsPageOffersAnySource(t *testing.T) {
	registerTestSource()
	options := []c.SubjectOptions{{Source: "websrc", CollectorID: "col_a", CollectorName: "edge", AllLabel: "Everything on edge",
		Sections: []c.SubjectSection{{Label: "Items", Kind: "item", Entries: []c.SubjectEntry{
			{Scope: "item", Name: "a/b", Note: "with a slash"}, {Scope: "item", Name: "c", Child: true}}}}}}
	html := renderPage(t, "alerts_list.html", "alerts", map[string]any{"SubjectSources": subjectSources(options), "CollectorNames": map[string]string{"col_a": "edge"}})
	for _, want := range []string{
		`<optgroup label="Web Source">`, `value="websrc_quiet" data-subject-source="websrc" data-kind="item"`, `value="websrc_down" data-subject-source="websrc" data-kind="host"`,
		`data-subject-rule="create"`, `data-subject-rule="edit"`, `class="subjects-collector" data-source="websrc"`,
		`name="subjects" value="websrc/col_a/all"`, `name="subjects" value="websrc/col_a/item/a/b"`,
		`class="subjects-section" data-kind="item"`, `class="subject subject-entry"`, `Everything on edge`, `with a slash`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("alerts page lacks %q", want)
		}
	}
	if strings.Contains(strings.ToLower(html), "crowdsec_subjects") {
		t.Error("the alerts page still has the CrowdSec-only form field")
	}
	// Without options the conditions are not offered, and the source's hint is ready.
	empty := renderPage(t, "alerts_list.html", "alerts", map[string]any{"SubjectSources": subjectSources(nil)})
	if strings.Contains(empty, `value="websrc_quiet"`) || !strings.Contains(empty, `data-subject-empty="websrc" hidden>Nothing to watch yet.`) {
		t.Error("a source without collectors must not offer its conditions")
	}
}

func TestSubjectRuleReadsTheForm(t *testing.T) {
	registerTestSource()
	form := url.Values{"subjects": {"websrc/col_a/item/a/b", "websrc/col_a/all", "websrc/col_a/all"}}
	r := httptest.NewRequest("POST", "/", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.ParseForm()
	subjects, msg := subjectRule(r, "websrc_quiet", 5, nil)
	if msg != "" || len(subjects) != 2 || subjects[0].Name != "a/b" || subjects[1].Scope != c.ScopeAll {
		t.Fatalf("got %+v %q", subjects, msg)
	}
	for _, bad := range []url.Values{{"subjects": {"websrc/col_a/nope/x"}}, {"subjects": {"other/col_a/all"}}, {}} {
		r := httptest.NewRequest("POST", "/", strings.NewReader(bad.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		r.ParseForm()
		if _, msg := subjectRule(r, "websrc_quiet", 5, nil); msg == "" {
			t.Errorf("%v must be refused", bad)
		}
	}
	if _, msg := subjectRule(r, "websrc_quiet", 500, nil); msg == "" {
		t.Error("a threshold outside the condition's range must be refused")
	}
}
