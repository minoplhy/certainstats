package web

import (
	"certainstats/internal/store"
	"encoding/json"
	"html/template"
)

// Configuration contains only fields the active module consumes. Session
// credentials, private access policies and owner IDs never enter bootstrap data.
func bootstrap(data any) template.JS {
	config := map[string]any{}
	if m, ok := data.(map[string]any); ok {
		if rule, public := m["AccessRules"]; public {
			if d, ok := m["Dashboard"].(store.Dashboard); ok {
				config = map[string]any{"Dashboard": map[string]any{"DashboardID": d.DashboardID, "Slug": d.Slug}, "AccessRules": rule, "Agents": m["Agents"]}
			}
		} else if _, edit := m["DashboardAgents"]; edit {
			if d, ok := m["Dashboard"].(store.Dashboard); ok {
				config = map[string]any{"Dashboard": map[string]any{"DashboardID": d.DashboardID}, "DashboardAgents": m["DashboardAgents"], "IsDragged": m["IsDragged"], "IsCreate": m["IsCreate"]}
			}
		} else if agents, ok := m["Agents"]; ok {
			config["Agents"] = agents
		}
	}
	b, err := json.Marshal(config)
	if err != nil {
		return template.JS(`{}`)
	}
	// encoding/json escapes HTML-sensitive characters and closing script tags.
	return template.JS(b)
}
