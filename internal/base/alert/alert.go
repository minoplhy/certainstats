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
	DestPreset  DestinationType = "preset"
)

// --- Main Structs ---
type Alert struct {
	AlertID  string       `json:"alert_id"`
	UserID   string       `json:"-"`
	Nickname string       `json:"nickname"`
	Enabled  bool         `json:"enabled"`
	Trigger  Trigger      `json:"trigger"`
	Action   AlertAction  `json:"action"`
	Agents   []AgentState `json:"agents"`
}

type AlertPayload struct {
	Nickname string      `json:"nickname"`
	Enabled  bool        `json:"enabled"`
	Trigger  Trigger     `json:"trigger"`
	Action   AlertAction `json:"action"`
	Agents   []string    `json:"agents"` // List of AgentIDs
}

type Trigger struct {
	Type      TriggerType `json:"type"`
	Operator  Operator    `json:"operator"`
	Threshold float64     `json:"threshold"`
	Duration  string      `json:"duration"`
}

type AlertAction struct {
	Type        DestinationType `json:"type"`                  // e.g., "webhook", "discord", "preset"
	TargetID    string          `json:"target_id,omitempty"`   // Referenced AlertTarget ID
	Destination string          `json:"destination,omitempty"` // e.g., "https://discord.com/api/webhooks/..."
	Payload     string          `json:"payload,omitempty"`     // A custom JSON template to send
}

type AlertTarget struct {
	TargetID    string          `json:"target_id"`
	UserID      string          `json:"-"`
	Name        string          `json:"name"`
	Type        DestinationType `json:"type"`
	Destination string          `json:"destination"`
	Payload     string          `json:"payload"`
	CreatedAt   time.Time       `json:"created_at"`
}

type AgentState struct {
	AgentID     string     `json:"agent_id"`
	Status      string     `json:"status"` // "ok" or "firing"
	LastFiredAt *time.Time `json:"last_fired_at,omitempty"`
}

type AlertHistory struct {
	HistoryID      string     `json:"history_id"`
	AlertID        string     `json:"alert_id"`
	UserID         string     `json:"-"` // Denormalized for ultra-fast query
	AgentID        string     `json:"agent_id"`
	AgentNickname  string     `json:"agent_nickname"`
	AlertNickname  string     `json:"alert_nickname"`
	TriggeredAt    time.Time  `json:"triggered_at"`
	ResolvedAt     *time.Time `json:"resolved_at,omitempty"`
	TriggerValue   float64    `json:"trigger_value"`
	NotifiedStatus string     `json:"notified_status"`
	Trigger        Trigger    `json:"trigger"`
	TargetID       string     `json:"target_id,omitempty"`
	TargetName     string     `json:"target_name,omitempty"`
}

