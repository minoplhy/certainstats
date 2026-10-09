package beszel

import (
	"certainstats/internal/agentmeta"
	nm "certainstats/internal/networkmonitor"
	"context"
	"fmt"
	"strings"

	"github.com/fxamacker/cbor/v2"
	"golang.org/x/mod/semver"
)

// syncNetworkMonitorsAction is the Beszel WebSocket action for monitor
// configuration (ws.SyncNetworkMonitors).
const syncNetworkMonitorsAction uint8 = 7

// Minimum Beszel versions for network monitoring features.
const (
	networkBaseVersion     = "v0.20.0"
	networkExtendedVersion = "v0.21.0"
)

// minimumVersion returns the first Beszel release that supports feature.
func minimumVersion(feature string) string {
	switch feature {
	case agentmeta.FeatureCustomDNSServer, agentmeta.FeatureTLSCertificate:
		return networkExtendedVersion
	default:
		return networkBaseVersion
	}
}

// ResolveCapabilities applies Beszel's semantic-version rules, narrowed by
// any capabilities the agent explicitly disables.
func (b *BeszelStats) ResolveCapabilities(r agentmeta.Runtime) agentmeta.Capabilities {
	if r.AgentVersion == nil || !semver.IsValid("v"+strings.TrimPrefix(*r.AgentVersion, "v")) {
		caps := agentmeta.Capabilities{}
		for _, feature := range agentmeta.NetworkFeatures {
			caps[agentmeta.Key(feature)] = agentmeta.Capability{
				State:      agentmeta.StateUnknown,
				ReasonCode: "version_unknown",
				Message:    "Agent software version is unknown",
			}
		}
		return caps
	}

	version := "v" + strings.TrimPrefix(*r.AgentVersion, "v")
	caps := agentmeta.Capabilities{}
	for _, feature := range agentmeta.NetworkFeatures {
		minimum := minimumVersion(feature)
		if semver.Compare(version, minimum) >= 0 {
			caps[agentmeta.Key(feature)] = agentmeta.Capability{State: agentmeta.StateSupported}
			continue
		}
		caps[agentmeta.Key(feature)] = agentmeta.Capability{
			State:      agentmeta.StateUnsupported,
			ReasonCode: "upgrade_required",
			Message:    fmt.Sprintf("Beszel %s or newer is required", strings.TrimPrefix(minimum, "v")),
		}
	}
	return agentmeta.Narrow(caps, r.ReportedCapabilities)
}

// Apply sends a configuration operation and returns the immediate probe
// result, if the agent ran one.
func (b *BeszelStats) Apply(ctx context.Context, session nm.Session, op nm.Operation) (*nm.Result, error) {
	request := struct {
		Action  uint8       `cbor:"0,keyasint"`
		Config  nm.Config   `cbor:"1,keyasint,omitempty"`
		Configs []nm.Config `cbor:"2,keyasint,omitempty"`
		RunNow  bool        `cbor:"3,keyasint,omitempty"`
	}{
		Action:  uint8(op.Action),
		Config:  op.Config,
		Configs: op.Configs,
		RunNow:  op.RunNow,
	}
	raw, err := session.Request(ctx, syncNetworkMonitorsAction, request)
	if err != nil {
		return nil, err
	}

	var response struct {
		Result nm.Result `cbor:"0,keyasint,omitempty"`
	}
	if err = cbor.Unmarshal(raw, &response); err != nil {
		return nil, err
	}
	if response.Result.LastProbeAt == 0 {
		return nil, nil
	}
	return &response.Result, nil
}
