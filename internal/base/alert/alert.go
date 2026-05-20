package alert

import "time"

type TriggerType string

const (
	TriggerTypeDown      TriggerType = "agent_down"
	TriggerTypeCPU       TriggerType = "cpu_usage"
	TriggerTypeCPUIOWait TriggerType = "cpu_iowait"
	TriggerTypeCPUSteal  TriggerType = "cpu_steal"
	TriggerTypeRAM       TriggerType = "ram_usage"
	TriggerTypeSwap      TriggerType = "swap_usage"
	TriggerTypeDisk      TriggerType = "disk_usage"
	TriggerTypeNetRx     TriggerType = "net_rx"
	TriggerTypeNetTx     TriggerType = "net_tx"
	TriggerTypeDiskRead  TriggerType = "disk_read"
	TriggerTypeDiskWrite TriggerType = "disk_write"
)

type Operator string

const (
	OpGreaterThan Operator = ">"
	OpLessThan    Operator = "<"
	OpEquals      Operator = "=="
)

type DestinationType string

const (
	DestWebhook DestinationType = "webhook"
	DestDiscord DestinationType = "discord"
)

// --- Main Structs ---
type Alert struct {
	AlertID string       `json:"alert_id"`
	UserID  string       `json:"user_id"`
	Enabled bool         `json:"enabled"`
	Trigger Trigger      `json:"trigger"`
	Action  AlertAction  `json:"action"`
	Agents  []AgentState `json:"agents"`
}

type Trigger struct {
	Type      TriggerType `json:"type"`
	Operator  Operator    `json:"operator"`
	Threshold float64     `json:"threshold"`
	Duration  string      `json:"duration"`
}

type AlertAction struct {
	Type        DestinationType `json:"type"`        // e.g., "webhook"
	Destination string          `json:"destination"` // e.g., "https://discord.com/api/webhooks/..."
	Payload     string          `json:"payload"`     // A custom JSON template to send
}

type AgentState struct {
	AgentID     string     `json:"agent_id"`
	Status      string     `json:"status"` // "ok" or "firing"
	LastFiredAt *time.Time `json:"last_fired_at,omitempty"`
}

type AlertHistory struct {
	HistoryID      string     `json:"history_id"`
	AlertID        string     `json:"alert_id"`
	AgentID        string     `json:"agent_id"`
	AgentNickname  string     `json:"agent_nickname"`
	TriggeredAt    time.Time  `json:"triggered_at"`
	ResolvedAt     *time.Time `json:"resolved_at,omitempty"`
	TriggerValue   float64    `json:"trigger_value"`
	NotifiedStatus string     `json:"notified_status"`
	Trigger        Trigger    `json:"trigger"`
}
