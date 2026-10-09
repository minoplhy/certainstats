// Package networkmonitor contains provider-neutral monitor configuration and results.
package networkmonitor

import (
	"certainstats/internal/agentmeta"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// ErrConflict reports a duplicate configuration or an edit to an archived monitor.
var ErrConflict = errors.New("monitor configuration conflict")

// Probe interval bounds, in seconds.
const (
	MinIntervalSeconds     = 60
	DefaultIntervalSeconds = MinIntervalSeconds
	MaxIntervalSeconds     = 3600
)

// Limits shared by the API and the store.
const (
	MaxMonitorsPerAgent = 100
	MaxAgentsPerCreate  = 100
	MaxResultsPerBatch  = 100
	DefaultPageSize     = 25
	MaxPageSize         = 100
)

// MinAlertSamples is the number of lifetime samples a monitor needs before loss alerts evaluate.
const MinAlertSamples = 3

const (
	maxTargetLength = 2048
	maxHostLength   = 253
	maxLabelLength  = 63
	defaultTCPPort  = 443
)

// Supported probe protocols.
const (
	ProtocolICMP = "icmp"
	ProtocolTCP  = "tcp"
	ProtocolHTTP = "http"
	ProtocolDNS  = "dns"
)

// Monitor states reported to the UI.
const (
	StateWaiting     = "waiting"
	StateActive      = "active"
	StateStale       = "stale"
	StatePaused      = "paused"
	StateArchived    = "archived"
	StateOffline     = "offline"
	StateUnsupported = "unsupported"
	StateUnknown     = "unknown"
)

// Action selects how an agent applies an Operation.
type Action uint8

const (
	// ActionReplace replaces the agent's full monitor set with Operation.Configs.
	ActionReplace Action = 0
	// ActionUpsert adds or updates Operation.Config.
	ActionUpsert Action = 1
	// ActionDelete removes Operation.Config.
	ActionDelete Action = 2
)

// ValidateInterval validates the supported probe interval range.
func ValidateInterval(seconds int) error {
	if seconds < MinIntervalSeconds || seconds > MaxIntervalSeconds {
		return Invalid(fmt.Sprintf("interval must be between %d and %d seconds", MinIntervalSeconds, MaxIntervalSeconds))
	}
	return nil
}

// Config is the probe configuration sent to an agent.
type Config struct {
	ID       string `json:"monitor_id" cbor:"0,keyasint"`
	Target   string `json:"target" cbor:"1,keyasint"`
	Protocol string `json:"protocol" cbor:"2,keyasint"`
	Port     uint16 `json:"port" cbor:"3,keyasint,omitempty"`
	Interval uint16 `json:"interval_seconds" cbor:"4,keyasint"`
	Server   string `json:"dns_server,omitempty" cbor:"5,keyasint,omitempty"`
}

// Cert is the TLS certificate summary reported by HTTPS probes.
type Cert struct {
	Expires int64  `json:"expires" cbor:"0,keyasint"`
	Issuer  string `json:"issuer,omitempty" cbor:"1,keyasint,omitempty"`
}

// Result is one reported probe window. Response times are integer microseconds.
type Result struct {
	Avg         int64   `json:"response_avg_us" cbor:"0,keyasint,omitempty"`
	Avg1h       int64   `json:"response_avg_1h_us" cbor:"1,keyasint,omitempty"`
	Min         int64   `json:"response_min_us" cbor:"2,keyasint,omitempty"`
	Min1h       int64   `json:"response_min_1h_us" cbor:"3,keyasint,omitempty"`
	Max         int64   `json:"response_max_us" cbor:"4,keyasint,omitempty"`
	Max1h       int64   `json:"response_max_1h_us" cbor:"5,keyasint,omitempty"`
	Loss        float64 `json:"loss_pct" cbor:"6,keyasint,omitempty"`
	Loss1h      float64 `json:"loss_1h_pct" cbor:"7,keyasint,omitempty"`
	LastProbeAt int64   `json:"last_probe_at" cbor:"8,keyasint"`
	SampleCount int64   `json:"sample_count" cbor:"9,keyasint,omitempty"`
	Total       int64   `json:"attempt_count" cbor:"10,keyasint"`
	Success     int64   `json:"success_count" cbor:"11,keyasint"`
	Sum         int64   `json:"response_sum_us" cbor:"12,keyasint"`
	Cert        *Cert   `json:"certificate,omitempty" cbor:"13,keyasint,omitempty"`
}

// Latest is the committed latest reading for a monitor.
type Latest struct {
	Result
	Epoch                 string `json:"connection_epoch"`
	ReceivedAt            int64  `json:"received_at"`
	CertificateReceivedAt int64  `json:"certificate_received_at,omitempty"`
}

// Monitor is a stored monitor with its latest reading and derived state.
type Monitor struct {
	Config
	UserID        string                 `json:"-"`
	AgentID       string                 `json:"agent_id"`
	AgentName     string                 `json:"agent_name"`
	Enabled       bool                   `json:"enabled"`
	CreatedAt     time.Time              `json:"created_at"`
	UpdatedAt     time.Time              `json:"updated_at"`
	ArchivedAt    *time.Time             `json:"archived_at,omitempty"`
	ReplacementID string                 `json:"replacement_monitor_id,omitempty"`
	ReplacesID    string                 `json:"replaces_monitor_id,omitempty"`
	Latest        *Latest                `json:"latest,omitempty"`
	Sync          Sync                   `json:"sync"`
	Capabilities  agentmeta.Capabilities `json:"capabilities,omitempty"`
	State         string                 `json:"state,omitempty"`
}

// Sync tracks desired and acknowledged configuration generations for an agent.
type Sync struct {
	Desired     int64      `json:"desired_generation"`
	Ack         int64      `json:"acknowledged_generation"`
	Error       string     `json:"error,omitempty"`
	LastAttempt *time.Time `json:"last_attempt,omitempty"`
	LastAck     *time.Time `json:"last_ack,omitempty"`
}

// Operation is a configuration change sent to an agent adapter.
type Operation struct {
	Action  Action
	Config  Config
	Configs []Config
	RunNow  bool
}

// Session sends a correlated request to a connected agent.
type Session interface {
	Request(ctx context.Context, action uint8, data any) ([]byte, error)
}

// Adapter applies operations using a provider's wire protocol. A non-nil
// result is an immediate probe reading.
type Adapter interface {
	Apply(ctx context.Context, session Session, op Operation) (*Result, error)
}

// Batch is a journaled set of results awaiting TSDB commit.
type Batch struct {
	ID        string
	UserID    string
	AgentID   string
	Epoch     string
	Timestamp int64
	Results   map[string]Result
}

// Point is one aggregated history bucket, in milliseconds and percent.
type Point struct {
	Timestamp int64    `json:"timestamp"`
	Avg       *float64 `json:"response_avg_ms"`
	Min       *float64 `json:"response_min_ms"`
	Max       *float64 `json:"response_max_ms"`
	Loss      *float64 `json:"loss_pct"`
}

// ID returns a new random monitor, batch or event identifier.
func ID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

// Milliseconds converts a stored microsecond value for API responses.
func Milliseconds(us int64) *float64 {
	ms := float64(us) / 1000
	return &ms
}

// Normalize trims and validates the configuration, applying protocol defaults.
func (c *Config) Normalize() error {
	c.Target = strings.TrimSpace(c.Target)
	c.Protocol = strings.ToLower(strings.TrimSpace(c.Protocol))
	c.Server = strings.TrimSpace(c.Server)

	if c.Interval == 0 {
		c.Interval = DefaultIntervalSeconds
	}
	if err := ValidateInterval(int(c.Interval)); err != nil {
		return err
	}
	if c.Target == "" || len(c.Target) > maxTargetLength || strings.ContainsAny(c.Target, "\r\n\t") {
		return Invalid("invalid target")
	}
	if c.Protocol != ProtocolTCP {
		c.Port = 0
	}
	if c.Protocol != ProtocolDNS {
		c.Server = ""
	}

	switch c.Protocol {
	case ProtocolHTTP:
		return c.normalizeURL()
	case ProtocolTCP, ProtocolICMP, ProtocolDNS:
		return c.normalizeHost()
	default:
		return Invalid("unsupported protocol")
	}
}

func (c *Config) normalizeURL() error {
	if !strings.Contains(c.Target, "://") {
		c.Target = "https://" + c.Target
	}
	u, err := url.Parse(c.Target)
	if err != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.Fragment != "" {
		return Invalid("HTTP target requires an HTTP/HTTPS URL without credentials or fragment")
	}
	u.Host = strings.ToLower(u.Host)
	c.Target = u.String()
	return nil
}

func (c *Config) normalizeHost() error {
	c.Target = strings.ToLower(c.Target)
	if strings.ContainsAny(c.Target, " /?#@") {
		return Invalid("target must be a hostname or IP address")
	}
	isIP := net.ParseIP(c.Target) != nil
	if c.Protocol == ProtocolDNS && isIP {
		return Invalid("DNS target must be a hostname")
	}
	if !isIP && !validHost(c.Target) {
		return Invalid("invalid hostname")
	}
	if c.Protocol == ProtocolTCP && c.Port == 0 {
		c.Port = defaultTCPPort
	}
	if c.Server != "" {
		return c.normalizeDNSServer()
	}
	return nil
}

func (c *Config) normalizeDNSServer() error {
	host := c.Server
	if h, port, err := net.SplitHostPort(c.Server); err == nil {
		host = h
		if n, err := strconv.Atoi(port); err != nil || n < 1 || n > 65535 {
			return Invalid("invalid DNS server port")
		}
	}
	if net.ParseIP(host) == nil && !validHost(host) {
		return Invalid("invalid DNS server")
	}
	c.Server = strings.ToLower(c.Server)
	return nil
}

func validHost(host string) bool {
	if len(host) > maxHostLength {
		return false
	}
	host = strings.TrimSuffix(host, ".")
	for _, label := range strings.Split(host, ".") {
		if len(label) == 0 || len(label) > maxLabelLength || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, ch := range label {
			if !(ch >= 'a' && ch <= 'z' || ch >= '0' && ch <= '9' || ch == '-') {
				return false
			}
		}
	}
	return true
}

// Validate rejects results with impossible counts, timestamps or values.
func (r Result) Validate(now time.Time) error {
	if r.LastProbeAt <= 0 || r.LastProbeAt > now.Add(time.Minute).UnixMilli() {
		return fmt.Errorf("invalid probe timestamp")
	}
	if r.SampleCount < 0 || r.Total <= 0 || r.Success < 0 || r.Success > r.Total || r.Sum < 0 {
		return fmt.Errorf("invalid probe counts")
	}
	for _, us := range []int64{r.Avg, r.Avg1h, r.Min, r.Min1h, r.Max, r.Max1h} {
		if us < 0 {
			return fmt.Errorf("invalid response time")
		}
	}
	for _, pct := range []float64{r.Loss, r.Loss1h} {
		if math.IsNaN(pct) || math.IsInf(pct, 0) || pct < 0 || pct > 100 {
			return fmt.Errorf("invalid loss")
		}
	}
	return nil
}

// Fresh reports whether a reading is recent enough for alert evaluation:
// three probe intervals, bounded between three minutes and one hour.
func Fresh(r Result, interval uint16, now time.Time) bool {
	age := now.Sub(time.UnixMilli(r.LastProbeAt))
	limit := max(3*time.Duration(interval)*time.Second, 3*time.Minute)
	limit = min(limit, time.Hour)
	return age >= -time.Minute && age <= limit
}

// ValidationError is a client error, returned by the API as HTTP 400.
type ValidationError struct {
	Message string
}

func (e *ValidationError) Error() string {
	return e.Message
}

// Invalid returns a ValidationError with the given message.
func Invalid(message string) error {
	return &ValidationError{Message: message}
}

// CapabilityError reports that an agent lacks a feature a configuration needs.
// The API returns it as HTTP 400 with the struct as structured details.
type CapabilityError struct {
	AgentID    string               `json:"agent_id"`
	Feature    string               `json:"feature"`
	Capability agentmeta.Capability `json:"capability"`
}

func (e *CapabilityError) Error() string {
	return e.AgentID + ": " + e.Capability.Message
}

// CheckCapabilities verifies that caps support every feature the configuration needs.
func CheckCapabilities(agentID string, c Config, caps agentmeta.Capabilities) error {
	for _, feature := range agentmeta.ConfigFeatures(c.Protocol, c.Server) {
		if !caps.Supports(feature) {
			return &CapabilityError{
				AgentID:    agentID,
				Feature:    agentmeta.Key(feature),
				Capability: caps[agentmeta.Key(feature)],
			}
		}
	}
	return nil
}
