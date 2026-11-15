// Package wire holds the CrowdSec collector type's messages. Every message type
// starts with Prefix. Alert mirrors the alert object of CrowdSec's Local API
// (GET /v1/alerts), minus events and meta, which carry raw log lines and are
// dropped on the host.
package wire

import (
	"strings"
	"time"
)

// goTimeLayout is time.Time.String(), which CrowdSec's Local API uses for an
// alert's start_at and stop_at (created_at is RFC 3339).
const goTimeLayout = "2006-01-02 15:04:05.999999999 -0700 MST"

// ParseTime reads a Local API timestamp in either RFC 3339 or Go's String()
// form. Zero, unparsable or pre-2000 values are not ok.
func ParseTime(v string) (time.Time, bool) {
	v = strings.TrimSpace(v)
	if v == "" {
		return time.Time{}, false
	}
	// String() appends the monotonic clock reading (" m=+1.23") when present.
	if i := strings.Index(v, " m="); i > 0 {
		v = v[:i]
	}
	t, err := time.Parse(time.RFC3339Nano, v)
	if err != nil {
		if t, err = time.Parse(goTimeLayout, v); err != nil {
			return time.Time{}, false
		}
	}
	if t.Year() < 2000 {
		return time.Time{}, false
	}
	return t, true
}

const (
	Prefix     = "cs_"
	TypeName   = "crowdsec"
	MsgConfig  = "cs_config"
	MsgStatus  = "cs_status"
	MsgAlerts  = "cs_alerts"
	MsgAck     = "cs_ack"
	MsgResync  = "cs_resync"
	MsgHealth  = "cs_health"
	MaxBatch   = 200
	PollSecond = 60
	// OverlapSeconds is how far before the cursor each poll starts. Alerts are found by
	// their start time, and a long bucket can be created well after it started.
	OverlapSeconds = 3600
	// HealthSecond is how often a collector with health switched on reads CrowdSec's database.
	HealthSecond = 60
	// MaxComponents caps the machines and bouncers in one health report.
	MaxComponents = 1000

	KindMachine = "machine"
	KindBouncer = "bouncer"

	// The parts of CrowdSec's database a host may let the collector read.
	PartMachines  = "machines"
	PartBouncers  = "bouncers"
	PartMetrics   = "metrics"
	PartDecisions = "decisions"
)

// Parts lists every part, in the order they are read.
var Parts = []string{PartMachines, PartBouncers, PartMetrics, PartDecisions}

// Config is sent by the server right after hello.
type Config struct {
	CursorMs       int64 `cbor:"cursor_ms"`
	PollSeconds    int   `cbor:"poll_seconds"`
	MaxBatch       int   `cbor:"max_batch"`
	OverlapSeconds int   `cbor:"overlap_seconds"`
	// HealthSeconds is the health read interval; zero (an older server) means HealthSecond.
	HealthSeconds int `cbor:"health_seconds,omitempty"`
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

// Health is what a collector read from CrowdSec's database about its machines and
// bouncers. It is sent only by a host that set CROWDSEC_HEALTH=true. Components
// is the whole registry of the parts read, so one that is missing was removed.
type Health struct {
	// Error is set when the database could not be read; nothing else is then meaningful.
	Error string `cbor:"error,omitempty"`
	// Parts names what the host allows to be read and what answered.
	Parts []string `cbor:"parts"`
	// Active counts the Local API's active ban decisions on IPs and ranges, by origin.
	Active     map[string]int64 `cbor:"active,omitempty"`
	Components []Component      `cbor:"components"`
}

// Component is one machine (log processor) or bouncer registered with the Local API.
type Component struct {
	Kind    string `cbor:"kind"`
	Name    string `cbor:"name"`
	Version string `cbor:"version,omitempty"`
	IP      string `cbor:"ip,omitempty"`
	Type    string `cbor:"type,omitempty"`
	OS      string `cbor:"os,omitempty"`
	// LastSeenMs is the last heartbeat (machine) or last pull (bouncer); zero is never.
	LastSeenMs int64 `cbor:"last_seen_ms,omitempty"`
	Validated  bool  `cbor:"validated,omitempty"`
	Revoked    bool  `cbor:"revoked,omitempty"`
	// MetricsMs is when a bouncer last pushed usage metrics; zero means it never did.
	MetricsMs int64 `cbor:"metrics_ms,omitempty"`
	// Held counts the decisions the bouncer says it holds, by origin. It is nil
	// without usage metrics and empty when they carry no decision counts, so it
	// is sent even when empty.
	Held      map[string]int64 `cbor:"held"`
	Dropped   int64            `cbor:"dropped,omitempty"`
	Processed int64            `cbor:"processed,omitempty"`
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
