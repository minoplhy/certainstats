// Package wire holds the CrowdSec collector type's messages. Every message type
// starts with Prefix. Alert mirrors the alert object of CrowdSec's Local API
// (GET /v1/alerts), minus events and meta, which carry raw log lines and are
// dropped on the host.
package wire

const (
	Prefix     = "cs_"
	TypeName   = "crowdsec"
	MsgConfig  = "cs_config"
	MsgStatus  = "cs_status"
	MsgAlerts  = "cs_alerts"
	MsgAck     = "cs_ack"
	MsgResync  = "cs_resync"
	MaxBatch   = 200
	PollSecond = 60
	// OverlapSeconds is how far before the cursor each poll starts. Alerts are found by
	// their start time, and a long bucket can be created well after it started.
	OverlapSeconds = 3600
)

// Config is sent by the server right after hello.
type Config struct {
	CursorMs       int64 `cbor:"cursor_ms"`
	PollSeconds    int   `cbor:"poll_seconds"`
	MaxBatch       int   `cbor:"max_batch"`
	OverlapSeconds int   `cbor:"overlap_seconds"`
}

// Status reports whether the collector can read the Local API.
type Status struct {
	Reachable bool   `cbor:"reachable"`
	Error     string `cbor:"error"`
	Machine   string `cbor:"machine"`
	LAPIURL   string `cbor:"lapi_url"`
}

// Alerts is one batch. The collector waits for its Ack before sending the next.
type Alerts struct {
	BatchID string  `cbor:"batch_id"`
	Alerts  []Alert `cbor:"alerts"`
}

// Ack answers a batch. A non-empty Error means nothing was stored and the
// collector should resend after RetryAfterSeconds.
type Ack struct {
	BatchID           string `cbor:"batch_id"`
	Stored            int    `cbor:"stored"`
	Duplicates        int    `cbor:"duplicates"`
	CursorMs          int64  `cbor:"cursor_ms"`
	Error             string `cbor:"error,omitempty"`
	RetryAfterSeconds int    `cbor:"retry_after_seconds,omitempty"`
}

// Resync asks the collector to read again from SinceMs.
type Resync struct {
	SinceMs int64 `cbor:"since_ms"`
}

type Alert struct {
	ID          int64      `json:"id" cbor:"id"`
	UUID        string     `json:"uuid" cbor:"uuid"`
	MachineID   string     `json:"machine_id" cbor:"machine_id"`
	CreatedAt   string     `json:"created_at" cbor:"created_at"`
	Scenario    string     `json:"scenario" cbor:"scenario"`
	Message     string     `json:"message" cbor:"message"`
	EventsCount int32      `json:"events_count" cbor:"events_count"`
	StartAt     string     `json:"start_at" cbor:"start_at"`
	StopAt      string     `json:"stop_at" cbor:"stop_at"`
	Simulated   bool       `json:"simulated" cbor:"simulated"`
	Kind        string     `json:"kind" cbor:"kind"`
	Source      *Source    `json:"source" cbor:"source"`
	Decisions   []Decision `json:"decisions" cbor:"decisions"`
}

type Source struct {
	Scope    string `json:"scope" cbor:"scope"`
	Value    string `json:"value" cbor:"value"`
	IP       string `json:"ip" cbor:"ip"`
	Range    string `json:"range" cbor:"range"`
	ASNumber string `json:"as_number" cbor:"as_number"`
	ASName   string `json:"as_name" cbor:"as_name"`
	CN       string `json:"cn" cbor:"cn"`
}

type Decision struct {
	UUID      string `json:"uuid" cbor:"uuid"`
	Origin    string `json:"origin" cbor:"origin"`
	Type      string `json:"type" cbor:"type"`
	Scope     string `json:"scope" cbor:"scope"`
	Value     string `json:"value" cbor:"value"`
	Duration  string `json:"duration" cbor:"duration"`
	Until     string `json:"until" cbor:"until"`
	Scenario  string `json:"scenario" cbor:"scenario"`
	Simulated bool   `json:"simulated" cbor:"simulated"`
}
