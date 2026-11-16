package web

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
)

func renderPage(t *testing.T, page, nav string, data map[string]any) string {
	t.Helper()
	renderer, err := NewRenderer(false)
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	base := map[string]any{"PanelPath": "/panel", "StaticPath": "/static", "CSRFToken": "tok"}
	for k, v := range data {
		base[k] = v
	}
	pd := PageData{Title: nav, PanelPath: "/panel", StaticPath: "/static", Authenticated: true, ActiveNav: nav, Data: base}
	if err := renderer.Render(&buf, page, pd); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}

func TestCrowdSecPageRenders(t *testing.T) {
	html := renderPage(t, "collector_types/crowdsec.html", "collectors", nil)
	for _, want := range []string{
		`href="/panel/collectors" class="nav-link" aria-current="page"`,
		`data-cs-page`, `href="/panel/collectors">&larr; Collectors</a>`, `href="/panel/collectors?add=crowdsec"`,
		`data-filter-popover`, `data-cs-timeline-decisions`,
		`data-module="CertainStatsCrowdSec">{}</script>`,
		`admin/js/collectors`, `admin/js/crowdsec`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("page is missing %q", want)
		}
	}
	assertFilterMenu(t, html)
	if strings.Contains(html, `data-collectors-page`) {
		t.Error("the CrowdSec page must not carry the collectors list")
	}
}

func TestCollectorsPageRenders(t *testing.T) {
	nodes := []nodeOption{{AgentID: "agt_1", Nickname: "web <one>"}}
	html := renderPage(t, "collectors.html", "collectors", map[string]any{"Nodes": nodes})
	for _, want := range []string{
		`href="/panel/collectors" class="nav-link" aria-current="page"`,
		`data-collectors-page`, `data-collectors-type-list`, `data-collectors-add-modal`, `id="confirm-modal"`, `id="reinstall-modal"`,
		`<option value="agt_1">web &lt;one&gt;</option>`,
		`admin/js/collectors`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("page is missing %q", want)
		}
	}
	if strings.Contains(html, "&larr; CrowdSec") {
		t.Error("the Collectors page must not lead back to one type")
	}
}

// Collector types are reached from the Collectors page, not the main nav.
func TestNavHasNoCollectorTypes(t *testing.T) {
	html := renderPage(t, "collectors.html", "collectors", nil)
	for _, gone := range []string{`href="/panel/crowdsec"`, `href="/panel/knot"`, `>CrowdSec</a>`, `>Knot DNS</a>`} {
		if strings.Contains(html, gone) {
			t.Errorf("nav still has %q", gone)
		}
	}
}

func TestKnotPageRenders(t *testing.T) {
	html := renderPage(t, "collector_types/knot.html", "collectors", nil)
	for _, want := range []string{
		`href="/panel/collectors" class="nav-link" aria-current="page"`, `href="/panel/collectors">&larr; Collectors</a>`,
		`data-kn-page`, `href="/panel/collectors?add=knot"`, `data-kn-zone-list`,
		`id="kn-chart-protocol"`, `id="kn-chart-bytes"`, `id="kn-chart-dnstap"`,
		`data-module="CertainStatsKnot">{}</script>`, `admin/js/knot`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("knot page lacks %q", want)
		}
	}
	// Every data-kn-* hook the script looks up must exist exactly once.
	for _, role := range []string{"main", "empty", "refresh", "headline", "server-count", "stat-requests", "stat-qps", "stat-nxdomain",
		"stat-servfail", "stat-transfers", "stat-note", "server", "time-picker",
		"source-note", "checks", "check-list", "zone-view", "zone-title", "zone-note", "zone-rows", "zone-close", "dnstap",
		"dnstap-note", "dnstap-total", "dnstap-message", "breakdowns", "zone-count", "zone-search", "zone-list", "zones-message"} {
		if n := len(regexp.MustCompile(`data-kn-`+role+`[\s>=]`).FindAllString(html, -1)); n != 1 {
			t.Errorf("data-kn-%s appears %d times", role, n)
		}
	}
	assertFilterMenu(t, html)
	if strings.Contains(html, "<script>") {
		t.Error("the Knot page must not carry inline scripts")
	}
}

// The page handler is the same for every collector type: it renders
// collector_types/<name>.html under the Collectors nav entry with the type's label as title.
func TestCollectorTypePageIsGeneric(t *testing.T) {
	renderer, err := NewRenderer(false)
	if err != nil {
		t.Fatal(err)
	}
	h := &WebHandler{Renderer: renderer, PanelPath: "/panel", StaticPath: "/static"}
	for _, c := range []struct{ name, label, hook string }{{"crowdsec", "CrowdSec", "data-cs-page"}, {"knot", "Knot DNS", "data-kn-page"}} {
		rec := httptest.NewRecorder()
		h.CollectorTypePage(c.name, c.label)(rec, httptest.NewRequest(http.MethodGet, "/panel/collectors/"+c.name, nil))
		body := rec.Body.String()
		if rec.Code != http.StatusOK || !strings.Contains(body, c.hook) || !strings.Contains(body, c.label) {
			t.Errorf("%s: status %d, hook or title missing", c.name, rec.Code)
		}
	}
}

// The old top-level address of a type's page redirects under /collectors, keeping the query.
func TestCollectorTypeRedirect(t *testing.T) {
	h := &WebHandler{PanelPath: "/panel"}
	rec := httptest.NewRecorder()
	h.CollectorTypeRedirect("crowdsec")(rec, httptest.NewRequest(http.MethodGet, "/panel/crowdsec?hours=168&ip_view=1.2.3.4", nil))
	if rec.Code != http.StatusMovedPermanently || rec.Header().Get("Location") != "/panel/collectors/crowdsec?hours=168&ip_view=1.2.3.4" {
		t.Fatalf("got %d %q", rec.Code, rec.Header().Get("Location"))
	}
	rec = httptest.NewRecorder()
	(&WebHandler{}).CollectorTypeRedirect("knot")(rec, httptest.NewRequest(http.MethodGet, "/knot", nil))
	if rec.Header().Get("Location") != "/collectors/knot" {
		t.Fatalf("root panel: %q", rec.Header().Get("Location"))
	}
}

// assertFilterMenu checks a type page carries the shared filter menu and chips once each.
func assertFilterMenu(t *testing.T, html string) {
	t.Helper()
	for _, hook := range []string{"menu", "toggle", "popover", "field", "match", "value", "cancel", "chips"} {
		if n := len(regexp.MustCompile(`data-filter-`+hook+`[\s>=]`).FindAllString(html, -1)); n != 1 {
			t.Errorf("data-filter-%s appears %d times", hook, n)
		}
	}
}
