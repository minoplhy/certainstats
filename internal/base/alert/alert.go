package alert

import (
	nm "certainstats/internal/networkmonitor"
	"time"
)

type TriggerType string

const (
	TriggerTypeNetworkLoss TriggerType = "network_loss"
	TriggerTypeDown        TriggerType = "agent_down"
	TriggerTypeCPU         TriggerType = "cpu_usage"
	TriggerTypeCPUIOWait   TriggerType = "cpu_iowait"
	TriggerTypeCPUSteal    TriggerType = "cpu_steal"
	TriggerTypeRAM         TriggerType = "ram_usage"
	TriggerTypeSwap        TriggerType = "swap_usage"
	TriggerTypeDisk        TriggerType = "disk_usage"
	TriggerTypeNetRx       TriggerType = "net_rx"
	TriggerTypeNetTx       TriggerType = "net_tx"
	TriggerTypeDiskRead    TriggerType = "disk_read"
	TriggerTypeDiskWrite   TriggerType = "disk_write"
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
	MonitorIDs []string     `json:"monitor_ids"`
	AlertID    string       `json:"alert_id"`
	UserID     string       `json:"-"`
	Nickname   string       `json:"nickname"`
	Enabled    bool         `json:"enabled"`
	Trigger    Trigger      `json:"trigger"`
	Action     AlertAction  `json:"action"`
	Agents     []AgentState `json:"agents"`
	// Subjects lists what a collector rule watches.
	Subjects []Subject `json:"subjects,omitempty"`
}

type AlertPayload struct {
	MonitorIDs []string  `json:"monitor_ids"`
	Subjects   []Subject `json:"subjects,omitempty"`
	// CrowdSec is the older form of Subjects for CrowdSec rules; AllSubjects merges it.
	CrowdSec []LegacySubject `json:"crowdsec,omitempty"`
	Nickname string          `json:"nickname"`
	Enabled  bool            `json:"enabled"`
	Trigger  Trigger         `json:"trigger"`
	Action   AlertAction     `json:"action"`
	Agents   []string        `json:"agents"` // List of AgentIDs
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
	AgentID      string     `json:"agent_id"`
	Status       string     `json:"status"` // "ok" or "firing"
	LastFiredAt  *time.Time `json:"last_fired_at,omitempty"`
	ErrorMessage string     `json:"error_message,omitempty"`
}

type HistoryNode struct {
	AgentID  string `json:"agent_id"`
	Nickname string `json:"nickname"`
}

// HistoryEvent is a lifecycle transition or one durable delivery attempt.
// Completed attempts are immutable; a retry creates a new event.
type HistoryEvent struct {
	EventID        string     `json:"event_id"`
	HistoryID      string     `json:"history_id"`
	Kind           string     `json:"kind"`
	Phase          string     `json:"phase,omitempty"`
	Status         string     `json:"status,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	StartedAt      *time.Time `json:"started_at,omitempty"`
	CompletedAt    *time.Time `json:"completed_at,omitempty"`
	ErrorMessage   string     `json:"error_message,omitempty"`
	RetryOf        string     `json:"retry_of,omitempty"`
	RetryAvailable bool       `json:"retry_available"`
}

type AlertHistory struct {
	SubjectKind         string    `json:"subject_kind"`
	MonitorID           string    `json:"monitor_id,omitempty"`
	Monitor             nm.Config `json:"monitor,omitempty"`
	MonitoringAvailable bool      `json:"-"`
	// SubjectID and Subject are set for collector incidents: the subject
	// (collector/kind/name) and what fired, in words.
	SubjectID        string          `json:"subject_id,omitempty"`
	Subject          SubjectSnapshot `json:"subject,omitempty"`
	ClosedAt         *time.Time      `json:"closed_at,omitempty"`
	CloseReason      string          `json:"close_reason,omitempty"`
	Legacy           bool            `json:"legacy"`
	RetryAvailable   bool            `json:"retry_available"`
	FiringDelivery   string          `json:"firing_delivery,omitempty"`
	RecoveryDelivery string          `json:"recovery_delivery,omitempty"`
	HistoryID        string          `json:"history_id"`
	AlertID          string          `json:"alert_id"`
	UserID           string          `json:"-"` // Denormalized for ultra-fast query
	AgentID          string          `json:"agent_id"`
	AgentNickname    string          `json:"agent_nickname"`
	AlertNickname    string          `json:"alert_nickname"`
	TriggeredAt      time.Time       `json:"triggered_at"`
	ResolvedAt       *time.Time      `json:"resolved_at,omitempty"`
	TriggerValue     float64         `json:"trigger_value"`
	NotifiedStatus   string          `json:"notified_status"`
	Trigger          Trigger         `json:"trigger"`
	TargetID         string          `json:"target_id,omitempty"`
	TargetName       string          `json:"target_name,omitempty"`
	ErrorMessage     string          `json:"error_message,omitempty"`
}

// LegacySubject is a CrowdSec rule subject as the API took it before subjects
// carried their source.
type LegacySubject struct {
	CollectorID string `json:"collector_id"`
	Scope       string `json:"scope"`
	Name        string `json:"name,omitempty"`
}

// LegacySource is the source of LegacySubject entries.
const LegacySource = "crowdsec"

// AllSubjects returns Subjects followed by the legacy CrowdSec entries.
func (p AlertPayload) AllSubjects() []Subject {
	out := append([]Subject{}, p.Subjects...)
	for _, l := range p.CrowdSec {
		out = append(out, Subject{Source: LegacySource, CollectorID: l.CollectorID, Scope: l.Scope, Name: l.Name})
	}
	return out
}
