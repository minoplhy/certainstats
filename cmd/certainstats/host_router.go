package main

import (
	"fmt"
	"net/http"
	"strings"
)

func cleanHost(host string) string {
	if idx := strings.Index(host, ":"); idx != -1 {
		return host[:idx]
	}
	return host
}

func HostRouter(panelHost, panelPath, publicHost, publicPath string, panelRouter, publicRouter, fallbackRouter http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host := cleanHost(r.Host)
		if forwardedHost := r.Header.Get("X-Forwarded-Host"); forwardedHost != "" {
			host = cleanHost(forwardedHost)
		}

		// Helper to check prefix
		hasPrefix := func(p, prefix string) bool {
			if prefix == "/" {
				return true
			}
			return p == prefix || strings.HasPrefix(p, prefix+"/")
		}

		checkPanel := func() bool {
			return panelHost != "" && host == panelHost && hasPrefix(r.URL.Path, panelPath)
		}

		checkPublic := func() bool {
			return publicHost != "" && host == publicHost && hasPrefix(r.URL.Path, publicPath)
		}

		if len(panelPath) >= len(publicPath) {
			if checkPanel() {
				panelRouter.ServeHTTP(w, r)
				return
			}
			if checkPublic() {
				publicRouter.ServeHTTP(w, r)
				return
			}
		} else {
			if checkPublic() {
				publicRouter.ServeHTTP(w, r)
				return
			}
			if checkPanel() {
				panelRouter.ServeHTTP(w, r)
				return
			}
		}

		fallbackRouter.ServeHTTP(w, r)
	})
}

// panelRootSegments are the top-level path segments setupPanel registers.
// Keep in sync with setupPanel in main.go.
var panelRootSegments = []string{
	"static", "api", "submit", "login", "logout", "first-time-setup",
	"agents", "agent", "dashboards", "alerts", "settings",
}

// checkPathCollision reports when the public prefix sits inside the panel
// prefix on the same host and would shadow, or be shadowed by, a panel route.
func checkPathCollision(cfg *Config) error {
	if cfg.PanelHost != cfg.PublicHost || cfg.PanelPath == cfg.PublicPath {
		return nil
	}
	rel := ""
	if cfg.PanelPath == "/" {
		rel = cfg.PublicPath
	} else if strings.HasPrefix(cfg.PublicPath, cfg.PanelPath+"/") {
		rel = strings.TrimPrefix(cfg.PublicPath, cfg.PanelPath)
	} else {
		return nil
	}
	first := strings.SplitN(strings.TrimPrefix(rel, "/"), "/", 2)[0]
	for _, seg := range panelRootSegments {
		if first == seg {
			return fmt.Errorf("public path %s collides with panel route %s; choose a different PUBLIC_PATH or PUBLIC_URL", cfg.PublicPath, strings.TrimSuffix(cfg.PanelPath, "/")+"/"+seg)
		}
	}
	return nil
}
