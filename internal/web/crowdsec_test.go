package web

import (
	"bytes"
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
	html := renderPage(t, "crowdsec.html", "crowdsec", nil)
	for _, want := range []string{
		`href="/panel/crowdsec" class="nav-link" aria-current="page"`,
		`data-cs-page`, `href="/panel/collectors"`, `href="/panel/collectors?add=crowdsec"`,
		`data-cs-filter-popover`, `data-cs-timeline-decisions`,
		`data-module="CertainStatsCrowdSec">{}</script>`,
		`admin/js/collectors`, `admin/js/crowdsec`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("page is missing %q", want)
		}
	}
	if strings.Contains(html, `data-collectors-page`) {
		t.Error("the CrowdSec page must not carry the collectors list")
	}
}

func TestCollectorsPageRenders(t *testing.T) {
	nodes := []nodeOption{{AgentID: "agt_1", Nickname: "web <one>"}}
	html := renderPage(t, "collectors.html", "collectors", map[string]any{"Nodes": nodes})
	for _, want := range []string{
		`href="/panel/collectors" class="nav-link" aria-current="page"`,
		`data-collectors-page`, `data-collectors-add-modal`, `id="confirm-modal"`, `id="reinstall-modal"`,
		`<option value="agt_1">web &lt;one&gt;</option>`,
		`admin/js/collectors`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("page is missing %q", want)
		}
	}
}
