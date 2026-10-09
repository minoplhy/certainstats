// Package agentmeta describes provider-neutral runtime metadata and capabilities.
package agentmeta

import (
	"fmt"
	"strings"
	"unicode"
)

const maxVersionLength = 128

// Capability states.
const (
	StateSupported   = "supported"
	StateUnsupported = "unsupported"
	StateUnknown     = "unknown"
)

// Network monitor features, without the Key prefix.
const (
	FeatureConfigure       = "configure"
	FeatureICMP            = "icmp"
	FeatureTCP             = "tcp"
	FeatureHTTP            = "http"
	FeatureDNS             = "dns"
	FeatureCustomDNSServer = "custom_dns_server"
	FeatureTLSCertificate  = "tls_certificate"
	FeatureHourlyLoss      = "hourly_loss"
)

// NetworkFeatures lists every network monitor feature a provider resolves.
var NetworkFeatures = []string{
	FeatureConfigure,
	FeatureICMP,
	FeatureTCP,
	FeatureHTTP,
	FeatureDNS,
	FeatureCustomDNSServer,
	FeatureTLSCertificate,
	FeatureHourlyLoss,
}

// Declaration is an agent's explicit feature switch list, keyed like Capabilities.
type Declaration map[string]bool

// Runtime is the software metadata an agent reports about itself.
type Runtime struct {
	AgentVersion         *string      `json:"agent_version,omitempty"`
	ProtocolVersion      *string      `json:"protocol_version,omitempty"`
	VersionSource        string       `json:"version_source,omitempty"`
	ReportedCapabilities *Declaration `json:"reported_capabilities,omitempty"`
}

// Capability is the resolved support state of one feature.
type Capability struct {
	State      string `json:"state"`
	ReasonCode string `json:"reason_code,omitempty"`
	Message    string `json:"message,omitempty"`
}

// Capabilities maps Key(feature) to its resolved state.
type Capabilities map[string]Capability

// Key returns the capability map key for a network monitor feature.
func Key(feature string) string {
	return "network_monitor." + feature
}

// Supports reports whether feature is resolved as supported.
func (c Capabilities) Supports(feature string) bool {
	return c[Key(feature)].State == StateSupported
}

// ConfigFeatures lists the features a monitor configuration needs.
func ConfigFeatures(protocol, dnsServer string) []string {
	features := []string{FeatureConfigure, protocol}
	if dnsServer != "" {
		features = append(features, FeatureCustomDNSServer)
	}
	return features
}

// SupportsConfig reports whether caps support every feature in ConfigFeatures.
func (c Capabilities) SupportsConfig(protocol, dnsServer string) bool {
	for _, feature := range ConfigFeatures(protocol, dnsServer) {
		if !c.Supports(feature) {
			return false
		}
	}
	return true
}

// Unsupported returns every network feature as unsupported with the same reason.
func Unsupported(reason, message string) Capabilities {
	c := Capabilities{}
	for _, feature := range NetworkFeatures {
		c[Key(feature)] = Capability{State: StateUnsupported, ReasonCode: reason, Message: message}
	}
	return c
}

// Validate rejects oversized versions or versions containing control characters.
func Validate(r Runtime) error {
	for _, version := range []*string{r.AgentVersion, r.ProtocolVersion} {
		if version == nil {
			continue
		}
		if len(*version) > maxVersionLength {
			return fmt.Errorf("version exceeds %d characters", maxVersionLength)
		}
		for _, ch := range *version {
			if unicode.IsControl(ch) {
				return fmt.Errorf("version contains control characters")
			}
		}
	}
	return nil
}

// String returns a pointer to the trimmed value, or nil when it is empty.
func String(s string) *string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	return &s
}

// Narrow never permits a declaration to expand the hub adapter's capabilities.
func Narrow(c Capabilities, d *Declaration) Capabilities {
	if d == nil {
		return c
	}
	for key, capability := range c {
		if enabled, ok := (*d)[key]; ok && !enabled {
			capability.State = StateUnsupported
			capability.ReasonCode = "agent_disabled"
			capability.Message = "Disabled by the agent"
			c[key] = capability
		}
	}
	return c
}
