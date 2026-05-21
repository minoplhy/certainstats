package main

import (
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
