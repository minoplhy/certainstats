package context

import (
	base "certainstats/internal/base"
	accessrules "certainstats/internal/dashboard/accessrules"
	"sync"
	"time"
)

type contextKey string

const UserIDKey contextKey = "userID"
const PanelPathKey contextKey = "panelPath"

// Standard Cache TTLs
const (
	DefaultCacheTTL = 60 * time.Second
	ShortCacheTTL   = 10 * time.Second
	LongCacheTTL    = 365 * 24 * time.Hour
)

// Global sync.Map caches
var MetricsCache sync.Map       // Key: slug_agentID_metric_hours, Value: *CacheEntry
var DeviceCache sync.Map        // Key: device_id, Value: identity
var DashboardCache sync.Map     // Key: slug, Value: *CacheEntry
var DashboardHTMLCache sync.Map // Key: html_dash_slug or html_agent_slug_pubID, Value: *CacheEntry
var PublicAgentCache sync.Map   // Key: dashboardID_publicAgentID, Value: *PublicAgentCacheEntry
var StaticCache sync.Map        // Key: relative path (e.g. css/styles.css), Value: *CacheEntry

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
