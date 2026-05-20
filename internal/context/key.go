package context

import (
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

type CacheEntry struct {
	Payload   []byte
	ExpiresAt time.Time
}

//var SqliteDB *sql.DB
