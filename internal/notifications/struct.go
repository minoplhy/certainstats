package notifications

import (
	"net/http"
	"time"
)

// Use a single, global HTTP client with a strict timeout.
// NEVER use the default http.Client in production, as it has no timeout
// and will cause massive goroutine leaks if a destination server hangs.
var httpClient = &http.Client{
	Timeout: 10 * time.Second,
}

// NotificationContext carries all the context needed for rich alert messages.
type NotificationContext struct {
	AgentID       string
	Nickname      string
	TriggerType   string // e.g. "agent_down", "cpu_usage", "ram_usage"
	Status        string // "FIRING" or "RESOLVED"
	Value         float64
	Operator      string
	Threshold     float64
	WentOfflineAt *time.Time
	ResolvedAt    *time.Time
}

// defaultPayload is used if the user doesn't provide a custom JSON template
type defaultPayload struct {
	AgentID     string  `json:"agent_id"`
	Nickname    string  `json:"nickname"`
	TriggerType string  `json:"trigger_type"`
	Status      string  `json:"status"`
	Value       float64 `json:"value"`
	Operator    string  `json:"operator"`
	Threshold   float64 `json:"threshold"`
	Time        string  `json:"time"`
}
