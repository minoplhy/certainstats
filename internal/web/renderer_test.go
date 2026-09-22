package web

import (
	"bytes"
	alertbase "certainstats/internal/base/alert"
	"certainstats/internal/dashboard/accessrules"
	"certainstats/internal/store"
	"strings"
	"testing"
	"time"
)

func boolPtr(b bool) *bool    { return &b }
func u16Ptr(u uint16) *uint16 { return &u }
func u64Ptr(u uint64) *uint64 { return &u }
func strPtr(s string) *string { return &s }

func TestRenderer_ParseAndRender(t *testing.T) {
	renderer, err := NewRenderer()
	if err != nil {
		t.Fatalf("NewRenderer failed: %v", err)
	}

	testCases := []struct {
		page string
		data map[string]any
	}{
		{"setup.html", map[string]any{"Token": "test_token"}},
		{"login.html", map[string]any{}},
		{"agents_list.html", map[string]any{
			"Agents": []store.Agent{
				{AgentID: "node-1", Nickname: "Server 01", IsOnline: true, CpuCores: 8, RamSize: 16000000000, DiskSize: 500000000000, AgentType: "beszel"},
				{AgentID: "node-2", Nickname: "Server 02", IsOnline: false, CpuCores: 4, RamSize: 8000000000, DiskSize: 250000000000, AgentType: "ltstats"},
			},
		}},
		{"agent_management.html", map[string]any{
			"Agents": []store.AgentManagement{
				{AgentID: "node-1", Nickname: "Server 01", AgentType: "beszel", Token: "tok_123", BeszelPublicKey: "ssh-ed25519 AAA..."},
			},
		}},
		{"dashboards_list.html", map[string]any{
			"Dashboards": []store.Dashboard{
				{DashboardID: "dash-1", Title: "Status Page", Slug: "status-prod", AccessRules: accessrules.AccessRules{}},
			},
		}},
		{"dashboard_edit.html", map[string]any{
			"IsCreate": false,
			"Dashboard": store.Dashboard{
				DashboardID: "dash-1",
				Title:       "Status Page",
				Slug:        "status-prod",
				AccessRules: accessrules.AccessRules{
					"public": accessrules.AccessRule{
						AllowedFeatures: []string{"is_online", "uptime"},
						AllowedMetrics:  []string{"agent_cpu_usage", "agent_ram_used"},
						MaxDays:         7,
					},
				},
			},
			"AvailableAgents": []store.Agent{
				{AgentID: "node-1", Nickname: "Server 01"},
			},
			"DashboardAgents": []store.PublicAgentIdentity{
				{AgentID: "node-1", PublicAgentNickname: "Edge 1", SortKey: "00000000"},
			},
		}},
		{"public_dashboard.html", map[string]any{
			"Dashboard":   store.Dashboard{Title: "Status", Slug: "status-prod"},
			"AccessRules": accessrules.AccessRule{MaxDays: 7, AllowedMetrics: []string{"agent_cpu_usage", "agent_ram_used"}},
			"Agents": []store.PublicAgent{
				{PublicID: "pub-1", Name: "Web Node", IsOnline: boolPtr(true), CpuCores: u16Ptr(4), RamSize: u64Ptr(8000000000)},
			},
		}},
		{"alerts_list.html", map[string]any{
			"Alerts": []store.Alert{
				{AlertID: "al-1", Nickname: "High CPU", Trigger: alertbase.Trigger{Type: alertbase.TriggerTypeCPU, Operator: alertbase.OpGreaterThan, Threshold: 90, Duration: "5m"}, Enabled: true},
			},
			"Targets": []any{
				map[string]any{"TargetID": "tgt-1", "Name": "Discord Ops", "Type": "discord", "Destination": "https://discord.com/api/webhooks/..."},
			},
		}},
		{"settings.html", map[string]any{
			"Sessions": []store.Session{
				{Token: "tok_abcdef123456", UserAgent: "Mozilla/5.0 (Windows NT 10.0; Win64; x64) Chrome/120.0.0.0", IPAddress: "192.168.1.100", LastConnectedAt: time.Now()},
			},
		}},
	}

	for _, tc := range testCases {
		t.Run(tc.page, func(t *testing.T) {
			var buf bytes.Buffer
			pd := PageData{
				Title:         "Test Title",
				PanelPath:     "/",
				PublicPath:    "/dashboard",
				StaticPath:    "/static",
				ActiveNav:     "agents",
				Authenticated: true,
				Year:          2026,
				Data:          tc.data,
			}

			err := renderer.Render(&buf, tc.page, pd)
			if err != nil {
				t.Fatalf("Failed to render %s: %v", tc.page, err)
			}

			output := buf.String()
			if !strings.Contains(output, "CertainStats") {
				t.Errorf("Render output for %s missing 'CertainStats'", tc.page)
			}

			if tc.page == "public_dashboard.html" {
				if !strings.Contains(output, "Initial Render Time:") {
					t.Errorf("Render output for %s missing 'Initial Render Time:'", tc.page)
				}
			} else {
				if !strings.Contains(output, "Render Time:") {
					t.Errorf("Render output for %s missing 'Render Time:'", tc.page)
				}
			}

			if strings.Contains(output, "src=\"/js/") || strings.Contains(output, "href=\"/css/") {
				t.Errorf("Render output for %s contains asset URL missing StaticPath prefix", tc.page)
			}
		})
	}
}

func TestDashboardEdit_RenderJSValid(t *testing.T) {
	renderer, err := NewRenderer()
	if err != nil {
		t.Fatalf("NewRenderer failed: %v", err)
	}

	t.Run("renders valid isDragged boolean when agents have SortKey", func(t *testing.T) {
		var buf bytes.Buffer
		pd := PageData{
			Title:         "Edit Dashboard",
			PanelPath:     "/",
			StaticPath:    "/static",
			ActiveNav:     "dashboards",
			Authenticated: true,
			Year:          2026,
			Data: map[string]any{
				"IsCreate": false,
				"Dashboard": store.Dashboard{
					DashboardID: "dash-multi-agent",
					Title:       "Multi Agent Dash",
					Slug:        "multi-agent",
					AccessRules: accessrules.AccessRules{},
				},
				"AvailableAgents": []store.Agent{
					{AgentID: "node-1", Nickname: "Server 01"},
					{AgentID: "node-2", Nickname: "Server 02"},
				},
				"DashboardAgents": []store.PublicAgentIdentity{
					{AgentID: "node-1", PublicAgentNickname: "Edge 1", SortKey: "00000000"},
					{AgentID: "node-2", PublicAgentNickname: "Edge 2", SortKey: "00000001"},
				},
				"IsDragged": true,
			},
		}

		err := renderer.Render(&buf, "dashboard_edit.html", pd)
		if err != nil {
			t.Fatalf("Failed to render dashboard_edit.html: %v", err)
		}

		output := buf.String()
		if strings.Contains(output, "true false") || strings.Contains(output, "truetrue") {
			t.Errorf("Rendered output contains invalid JS tokens: %s", output)
		}
		if !strings.Contains(output, "isDragged: true,") {
			t.Errorf("Expected output to contain 'isDragged: true,', got: %s", output)
		}
		if !strings.Contains(output, `value="node-1,node-2"`) {
			t.Errorf("Expected pre-populated agents_order to contain 'node-1,node-2', got: %s", output)
		}
	})

	t.Run("renders valid isDragged false when agents do not have SortKey", func(t *testing.T) {
		var buf bytes.Buffer
		pd := PageData{
			Title:         "Edit Dashboard",
			PanelPath:     "/",
			StaticPath:    "/static",
			ActiveNav:     "dashboards",
			Authenticated: true,
			Year:          2026,
			Data: map[string]any{
				"IsCreate": false,
				"Dashboard": store.Dashboard{
					DashboardID: "dash-nosort",
					Title:       "No Sort Dash",
					Slug:        "no-sort",
					AccessRules: accessrules.AccessRules{},
				},
				"AvailableAgents": []store.Agent{
					{AgentID: "node-1", Nickname: "Server 01"},
				},
				"DashboardAgents": []store.PublicAgentIdentity{
					{AgentID: "node-1", PublicAgentNickname: "Edge 1", SortKey: ""},
				},
				"IsDragged": false,
			},
		}

		err := renderer.Render(&buf, "dashboard_edit.html", pd)
		if err != nil {
			t.Fatalf("Failed to render dashboard_edit.html: %v", err)
		}

		output := buf.String()
		if !strings.Contains(output, "isDragged: false,") {
			t.Errorf("Expected output to contain 'isDragged: false,', got: %s", output)
		}
	})
}


