package main

import (
	"strings"
	"testing"
)

func TestLoadConfigModes(t *testing.T) {
	cases := []struct {
		name string
		env  map[string]string
		// want is "panelScheme://panelHost panelPath | publicScheme://publicHost publicPath"; empty when an error is expected.
		want, err string
	}{
		{"nothing set", nil, ":// / | :// /dashboard", ""},
		{"path mode", map[string]string{"PANEL_PATH": "/admin", "PUBLIC_PATH": "/status/"}, ":// /admin | :// /status", ""},
		{"panel URL only", map[string]string{"PANEL_URL": "https://admin.example:8443/"}, "https://admin.example / | https://admin.example /dashboard", ""},
		{"both URLs, different hosts", map[string]string{"PANEL_URL": "https://admin.example", "PUBLIC_URL": "status.example"}, "https://admin.example / | http://status.example /", ""},
		{"both URLs, same host", map[string]string{"PANEL_URL": "https://example.com/admin", "PUBLIC_URL": "https://example.com/status"}, "https://example.com /admin | https://example.com /status", ""},
		{"host lowercased", map[string]string{"PANEL_URL": "https://Admin.Example.com"}, "https://admin.example.com / | https://admin.example.com /dashboard", ""},
		{"default paths tolerated", map[string]string{"PANEL_URL": "https://admin.example", "PANEL_PATH": "/", "PUBLIC_PATH": "/dashboard/"}, "https://admin.example / | https://admin.example /dashboard", ""},
		{"same host and path", map[string]string{"PANEL_URL": "https://example.com/x", "PUBLIC_URL": "https://example.com/x/"}, "", "same host and path"},
		{"public URL alone", map[string]string{"PUBLIC_URL": "https://status.example"}, "", "requires PANEL_URL"},
		{"URL mixed with public path", map[string]string{"PANEL_URL": "https://admin.example", "PUBLIC_PATH": "/status"}, "", "PUBLIC_PATH cannot be used"},
		{"URL mixed with panel path", map[string]string{"PANEL_URL": "https://admin.example", "PANEL_PATH": "/admin"}, "", "PANEL_PATH cannot be used"},
		{"URL without host", map[string]string{"PANEL_URL": "/admin"}, "", "must include a host"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			for _, key := range []string{"PANEL_URL", "PUBLIC_URL", "PANEL_PATH", "PUBLIC_PATH", "ALLOWED_ORIGINS"} {
				t.Setenv(key, c.env[key])
			}
			cfg, err := LoadConfig()
			if c.err != "" {
				if err == nil || !strings.Contains(err.Error(), c.err) {
					t.Fatalf("got err %v, want %q", err, c.err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			scheme := func(urlMode bool, s string) string {
				if !urlMode {
					return ""
				}
				return s
			}
			got := scheme(cfg.URLMode, cfg.PanelScheme) + "://" + cfg.PanelHost + " " + cfg.PanelPath + " | " +
				scheme(cfg.URLMode, cfg.PublicScheme) + "://" + cfg.PublicHost + " " + cfg.PublicPath
			if got != c.want {
				t.Fatalf("got %q, want %q", got, c.want)
			}
		})
	}
}
