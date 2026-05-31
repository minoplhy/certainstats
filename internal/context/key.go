package context

import (
	base "certainstats/internal/base"
	"certainstats/internal/compress"
	accessrules "certainstats/internal/dashboard/accessrules"
	"sync"
	"time"
)

type contextKey string

const UserIDKey contextKey = "userID"
const PanelPathKey contextKey = "panelPath"


//var PromTSDB *tsdb.DB

var MetricsCache sync.Map // Key: slug_agentID_metric, Value: *CacheEntry
var DeviceCache sync.Map
var DashboardCache sync.Map // Key: slug, Value: *CacheEntry
var PublicAgentCache sync.Map // Key: dashboardID_publicAgentID, Value: *PublicAgentCacheEntry

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

//var SqliteDB *sql.DB

func NewCacheEntry(payload []byte, ttl time.Duration) *CacheEntry {
	return &CacheEntry{
		Payload:     payload,
		GzipPayload: compress.CompressGzip(payload),
		ZstdPayload: compress.CompressZstd(payload),
		ExpiresAt:   time.Now().Add(ttl),
	}
}
