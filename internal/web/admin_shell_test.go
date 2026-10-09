package web

import (
	"bytes"
	"certainstats/internal/store"
	"os"
	"strings"
	"testing"
)

func TestSharedAdminShell(t *testing.T) {
	renderer, err := NewRenderer(false)
	if err != nil {
		t.Fatal(err)
	}
	for _, nav := range []string{"agents", "network"} {
		var buf bytes.Buffer
		pd := PageData{Title: "Hub", PanelPath: "/panel", StaticPath: "/static", Authenticated: true, ActiveNav: nav, Data: map[string]any{"PanelPath": "/panel", "StaticPath": "/static", "ActiveNav": nav, "Agents": []store.Agent{{AgentID: "node", Nickname: "Node", AgentType: "beszel", IsOnline: true, CpuCores: 4, RamSize: 16000000000}}}}
		if err := renderer.Render(&buf, "agents_list.html", pd); err != nil {
			t.Fatal(err)
		}
		html := buf.String()
		if strings.Count(html, `data-network-monitors`) != 2 || strings.Count(html, `id="page-config"`) != 1 {
			t.Fatal("shell must have two scoped network components and one bootstrap")
		}
		for _, id := range []string{"agents-spa-view", "network-spa-view"} {
			hidden := (nav == "network" && id == "agents-spa-view") || (nav == "agents" && id == "network-spa-view")
			tag := `<div id="` + id + `"`
			if hidden {
				tag += ` hidden`
			}
			tag += `>`
			if !strings.Contains(html, tag) {
				t.Fatalf("wrong initial %s visibility for %s", id, nav)
			}
		}
		if nav == "network" && os.Getenv("ADMIN_SHELL_FIXTURE") != "" {
			if err := os.WriteFile(os.Getenv("ADMIN_SHELL_FIXTURE"), buf.Bytes(), 0644); err != nil {
				t.Fatal(err)
			}
		}
	}
}
