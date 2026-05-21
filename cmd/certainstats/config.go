package main

import (
	"fmt"
	"net/url"
	"os"
	"strings"
)

type Config struct {
	PanelHost         string
	PanelPath         string
	PublicHost        string
	PublicPath        string
	InjectedPublicURL string
}

// LoadConfig resolves unified PANEL_URL/PUBLIC_URL configuration setups
// with full backward compatibility to host/path parameters, and automatically
// updates allowed origins for secure WebSocket handshakes.
func LoadConfig() *Config {
	panelPath := ""
	panelHost := ""
	panelURL := os.Getenv("PANEL_URL")
	if panelURL != "" {
		var uStr = panelURL
		if !strings.Contains(uStr, "://") {
			uStr = "http://" + uStr
		}
		if u, err := url.Parse(uStr); err == nil {
			panelHost = u.Hostname()
			panelPath = u.Path
			if panelPath == "" {
				panelPath = "/"
			}
		}
	}
	if panelPath == "" {
		panelPath = getRoutePrefix("PANEL_PATH", "/")
	} else {
		if !strings.HasPrefix(panelPath, "/") {
			panelPath = "/" + panelPath
		}
		panelPath = strings.TrimSuffix(panelPath, "/")
		if panelPath == "" {
			panelPath = "/"
		}
	}


	publicPath := ""
	publicHost := ""
	publicURL := os.Getenv("PUBLIC_URL")
	if publicURL != "" {
		var uStr = publicURL
		if !strings.Contains(uStr, "://") {
			uStr = "http://" + uStr
		}
		if u, err := url.Parse(uStr); err == nil {
			publicHost = u.Hostname()
			publicPath = u.Path
			if publicPath == "" {
				publicPath = "/"
			}
		}
	}
	if publicPath == "" {
		publicPath = getRoutePrefix("PUBLIC_PATH", "/dashboard")
		if publicPath == "/" {
			publicPath = "/dashboard"
		}
	} else {
		if !strings.HasPrefix(publicPath, "/") {
			publicPath = "/" + publicPath
		}
		publicPath = strings.TrimSuffix(publicPath, "/")
		if publicPath == "" {
			publicPath = "/"
		}
	}

	if publicHost == "" && publicPath == "/" {
		publicPath = "/dashboard"
	}

	updateAllowedOrigins(panelURL, panelHost, publicURL, publicHost)

	injectedPublicURL := os.Getenv("PUBLIC_URL")
	if publicHost != "" {
		if publicURL != "" && strings.Contains(publicURL, "://") {
			injectedPublicURL = publicURL
		} else {
			injectedPublicURL = "//" + publicHost + publicPath
		}
	}

	return &Config{
		PanelHost:         panelHost,
		PanelPath:         panelPath,
		PublicHost:        publicHost,
		PublicPath:        publicPath,
		InjectedPublicURL: injectedPublicURL,
	}
}

func getRoutePrefix(envKey, defaultPath string) string {
	path := os.Getenv(envKey)
	if path == "" {
		path = defaultPath
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	path = strings.TrimSuffix(path, "/")
	if path == "" {
		path = "/"
	}
	return path
}

func getOriginFromURLOrHost(val string) []string {
	if val == "" {
		return nil
	}
	if strings.Contains(val, "://") {
		u, err := url.Parse(val)
		if err == nil {
			scheme := u.Scheme
			if scheme == "" {
				scheme = "http"
			}
			return []string{fmt.Sprintf("%s://%s", scheme, u.Host)}
		}
		return nil
	}
	return []string{
		"http://" + val,
		"https://" + val,
	}
}

func updateAllowedOrigins(panelURL, panelHost, publicURL, publicHost string) {
	allowed := os.Getenv("ALLOWED_ORIGINS")
	origins := []string{}
	if allowed != "" {
		for _, o := range strings.Split(allowed, ",") {
			origins = append(origins, strings.TrimSpace(o))
		}
	}

	addOrigins := func(val string) {
		for _, origin := range getOriginFromURLOrHost(val) {
			if origin == "" {
				continue
			}
			exists := false
			for _, o := range origins {
				if o == origin {
					exists = true
					break
				}
			}
			if !exists {
				origins = append(origins, origin)
			}
		}
	}

	addOrigins(panelURL)
	addOrigins(panelHost)
	addOrigins(publicURL)
	addOrigins(publicHost)

	if len(origins) > 0 {
		os.Setenv("ALLOWED_ORIGINS", strings.Join(origins, ","))
	}
}
