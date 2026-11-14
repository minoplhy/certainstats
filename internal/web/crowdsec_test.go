package web

import (
	"bytes"
	"strings"
	"testing"
)

func TestCrowdSecPageRenders(t *testing.T) {
	renderer, err := NewRenderer(false)
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	nodes := []nodeOption{{AgentID: "agt_1", Nickname: "web <one>"}}
	pd := PageData{Title: "CrowdSec", PanelPath: "/panel", StaticPath: "/static", Authenticated: true, ActiveNav: "crowdsec",
		Data: map[string]any{"PanelPath": "/panel", "StaticPath": "/static", "Nodes": nodes, "CSRFToken": "tok"}}
	if err := renderer.Render(&buf, "crowdsec.html", pd); err != nil {
		t.Fatal(err)
	}
	html := buf.String()
	for _, want := range []string{
		`href="/panel/crowdsec" class="nav-link" aria-current="page"`,
		`data-cs-page`, `data-collectors-modal`, `id="confirm-modal"`,
		`<option value="agt_1">web &lt;one&gt;</option>`,
		`data-module="CertainStatsCrowdSec">{}</script>`,
		`admin/js/collectors`, `admin/js/crowdsec`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("page is missing %q", want)
		}
	}
}
