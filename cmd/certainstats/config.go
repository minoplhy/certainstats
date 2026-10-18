package main

import (
	log "certainstats/internal/logger"
	"certainstats/internal/ws"
	"fmt"
	"net/url"
	"os"
	"strings"
)

type Config struct {
	// URLMode is true when PANEL_URL is set: each side is served only on its host.
	URLMode      bool
	PanelHost    string
	PanelPath    string
	PanelScheme  string
	PublicHost   string
	PublicPath   string
	PublicScheme string
	// PanelAddr and PublicAddr are host[:port] as written in the URLs, for links in logs.
	PanelAddr  string
	PublicAddr string
	Host       string
	Port       string
}

const (
	defaultPanelPath  = "/"
	defaultPublicPath = "/dashboard"
)

// LoadConfig resolves one of two modes and rejects mixing them:
//   - path mode (no *_URL): PANEL_PATH and PUBLIC_PATH, served on any host;
//   - URL mode (PANEL_URL, optional PUBLIC_URL): each side only on its own host.
//
// It also updates allowed origins for secure WebSocket handshakes.
func LoadConfig() (*Config, error) {
	cfg := &Config{PanelScheme: "http", PublicScheme: "http"}
	panelURL := os.Getenv("PANEL_URL")
	publicURL := os.Getenv("PUBLIC_URL")

	switch {
	case panelURL == "" && publicURL != "":
		return nil, fmt.Errorf("PUBLIC_URL requires PANEL_URL; set both, or use PANEL_PATH/PUBLIC_PATH without URLs")
	case panelURL == "":
		cfg.PanelPath = getRoutePrefix("PANEL_PATH", defaultPanelPath)
		cfg.PublicPath = getRoutePrefix("PUBLIC_PATH", defaultPublicPath)
		if cfg.PublicPath == "/" {
			cfg.PublicPath = defaultPublicPath
		}
	default:
		cfg.URLMode = true
		// Old compose files always pass the default paths; only other values are mixing.
		for key, def := range map[string]string{"PANEL_PATH": defaultPanelPath, "PUBLIC_PATH": defaultPublicPath} {
			if os.Getenv(key) == "" {
				continue
			}
			if getRoutePrefix(key, def) != def {
				return nil, fmt.Errorf("%s cannot be used with PANEL_URL/PUBLIC_URL; put the path in the URL instead", key)
			}
			log.Printf("config: ignoring %s=%s because PANEL_URL is set; remove it", key, os.Getenv(key))
		}
		var err error
		if cfg.PanelScheme, cfg.PanelAddr, cfg.PanelHost, cfg.PanelPath, err = parseSideURL("PANEL_URL", panelURL); err != nil {
			return nil, err
		}
		if publicURL == "" {
			cfg.PublicScheme, cfg.PublicAddr, cfg.PublicHost, cfg.PublicPath = cfg.PanelScheme, cfg.PanelAddr, cfg.PanelHost, defaultPublicPath
		} else if cfg.PublicScheme, cfg.PublicAddr, cfg.PublicHost, cfg.PublicPath, err = parseSideURL("PUBLIC_URL", publicURL); err != nil {
			return nil, err
		}
		if cfg.PanelHost == cfg.PublicHost && cfg.PanelPath == cfg.PublicPath {
			return nil, fmt.Errorf("PANEL_URL and PUBLIC_URL point at the same host and path; give the public side its own host or path")
		}
	}

	updateAllowedOrigins(panelURL, cfg.PanelHost, publicURL, cfg.PublicHost)
	ws.ConfigureOrigins(os.Getenv("ALLOWED_ORIGINS"))

	cfg.Host = os.Getenv("HOST")
	cfg.Port = strings.TrimPrefix(os.Getenv("PORT"), ":")
	if cfg.Port == "" {
		cfg.Port = "8080"
	}
	return cfg, nil
}

// parseSideURL splits a PANEL_URL/PUBLIC_URL value; a scheme is optional, a host is required.
func parseSideURL(key, raw string) (scheme, addr, host, path string, err error) {
	if !strings.Contains(raw, "://") {
		raw = "http://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" {
		return "", "", "", "", fmt.Errorf("%s must include a host, e.g. https://admin.example.com", key)
	}
	path = strings.TrimSuffix(u.Path, "/")
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return u.Scheme, strings.ToLower(u.Host), strings.ToLower(u.Hostname()), path, nil
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
