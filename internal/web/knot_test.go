package web

import (
	"regexp"
	"strings"
	"testing"
)

func TestKnotPageRenders(t *testing.T) {
	html := renderPage(t, "knot.html", "knot", nil)
	for _, want := range []string{
		`href="/panel/knot" class="nav-link" aria-current="page"`,
		`data-kn-page`, `href="/panel/collectors?add=knot"`, `data-kn-add-filter`, `data-kn-zone-list`,
		`id="kn-chart-protocol"`, `id="kn-chart-bytes"`, `id="kn-chart-dnstap"`,
		`data-module="CertainStatsKnot">{}</script>`, `admin/js/knot`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("knot page lacks %q", want)
		}
	}
	// Every data-kn-* hook the script looks up must exist exactly once.
	for _, role := range []string{"main", "empty", "refresh", "headline", "server-count", "stat-requests", "stat-qps", "stat-nxdomain",
		"stat-servfail", "stat-transfers", "stat-note", "server", "range", "add-filter", "filter-field", "filter-value", "chips",
		"source-note", "checks", "check-list", "zone-view", "zone-title", "zone-note", "zone-rows", "zone-close", "dnstap",
		"dnstap-note", "dnstap-total", "dnstap-message", "breakdowns", "zone-count", "zone-search", "zone-list", "zones-message"} {
		if n := len(regexp.MustCompile(`data-kn-`+role+`[\s>=]`).FindAllString(html, -1)); n != 1 {
			t.Errorf("data-kn-%s appears %d times", role, n)
		}
	}
	if strings.Contains(html, "<script>") {
		t.Error("the Knot page must not carry inline scripts")
	}
}
