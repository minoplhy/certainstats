package context

import (
	base "certainstats/internal/base"
	accessrules "certainstats/internal/dashboard/accessrules"
	"net/http"
	"strings"
	"sync"
	"time"
)

type contextKey string

const UserIDKey contextKey = "userID"
const PanelPathKey contextKey = "panelPath"

// PanelOriginKey holds PANEL_URL's scheme and host ("https://stats.example.com")
// when it is set; absent in path mode.
const PanelOriginKey contextKey = "panelOrigin"

// PanelOrigin is the scheme and host agents and collectors should dial. PANEL_URL
// decides when it is set: behind a reverse proxy the request alone can look like
// plain HTTP, because forwarded headers from untrusted peers are dropped. Without
// it, the request's host is used, with https when secure.
func PanelOrigin(r *http.Request, secure bool) (scheme, host string) {
	if origin, _ := r.Context().Value(PanelOriginKey).(string); origin != "" {
		if s, h, ok := strings.Cut(origin, "://"); ok && s != "" && h != "" {
			return s, h
		}
	}
	if secure {
		return "https", r.Host
	}
	return "http", r.Host
}

// Standard Cache TTLs
const (
	DefaultCacheTTL = 60 * time.Second
	ShortCacheTTL   = 10 * time.Second
	LongCacheTTL    = 365 * 24 * time.Hour
)

// Global sync.Map caches
var MetricsCache ResponseCache       // Key: slug_agentID_metric_hours, Value: *CacheEntry
var DeviceCache sync.Map             // Key: device_id, Value: identity
var DashboardCache ResponseCache     // Key: slug, Value: *CacheEntry
var DashboardHTMLCache ResponseCache // Key: html_dash_slug or html_agent_slug_pubID, Value: *CacheEntry
var PublicAgentCache sync.Map        // Key: dashboardID_publicAgentID, Value: *PublicAgentCacheEntry
var StaticCache sync.Map             // Key: relative path (e.g. css/styles.css), Value: *CacheEntry

type CacheEntry struct {
	Payload     []byte
	GzipPayload []byte
	ZstdPayload []byte
	ExpiresAt   time.Time
}

type PublicAgentCacheEntry struct {
	Agent       base.FindAgentByPublicID
	ParsedRules accessrules.AccessRules
	ExpiresAt   time.Time
}
