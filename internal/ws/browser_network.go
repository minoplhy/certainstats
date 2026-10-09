package ws

import (
	nm "certainstats/internal/networkmonitor"
	"certainstats/internal/ws/browserpb"

	"google.golang.org/protobuf/types/known/timestamppb"
)

// BrowserNetwork copies committed owner readings into a full network pulse.
// Response times are converted to milliseconds, as in the HTTP API.
func BrowserNetwork(monitors []nm.Monitor) *browserpb.NetworkPulse {
	pulse := &browserpb.NetworkPulse{Monitors: make(map[string]*browserpb.NetworkSnapshot, len(monitors))}
	for _, m := range monitors {
		pulse.Monitors[m.ID] = &browserpb.NetworkSnapshot{
			AgentId: m.AgentID,
			Enabled: m.Enabled,
			State:   m.State,
			Sync:    browserSync(m.Sync),
			Latest:  browserLatest(m.Latest),
		}
	}
	return pulse
}

func browserSync(sync nm.Sync) *browserpb.NetworkSync {
	out := &browserpb.NetworkSync{
		DesiredGeneration:      sync.Desired,
		AcknowledgedGeneration: sync.Ack,
		Error:                  sync.Error,
	}
	if sync.LastAttempt != nil {
		out.LastAttempt = timestamppb.New(*sync.LastAttempt)
	}
	if sync.LastAck != nil {
		out.LastAck = timestamppb.New(*sync.LastAck)
	}
	return out
}

func browserLatest(latest *nm.Latest) *browserpb.NetworkLatest {
	if latest == nil {
		return nil
	}
	out := &browserpb.NetworkLatest{
		LossPct:               latest.Loss,
		Loss_1HPct:            latest.Loss1h,
		LastProbeAt:           latest.LastProbeAt,
		SampleCount:           latest.SampleCount,
		AttemptCount:          latest.Total,
		SuccessCount:          latest.Success,
		ReceivedAt:            latest.ReceivedAt,
		CertificateReceivedAt: latest.CertificateReceivedAt,
	}
	// Unset values mean unavailable, unlike a measured zero.
	if latest.Success > 0 {
		out.ResponseAvgMs = milliseconds(latest.Avg)
		out.ResponseMinMs = milliseconds(latest.Min)
		out.ResponseMaxMs = milliseconds(latest.Max)
	}
	if latest.SampleCount > 0 && latest.Loss1h < 100 {
		out.ResponseAvg_1HMs = milliseconds(latest.Avg1h)
		out.ResponseMin_1HMs = milliseconds(latest.Min1h)
		out.ResponseMax_1HMs = milliseconds(latest.Max1h)
	}
	if latest.Cert != nil {
		out.Certificate = &browserpb.NetworkCertificate{Expires: latest.Cert.Expires, Issuer: latest.Cert.Issuer}
	}
	return out
}

func milliseconds(us int64) *browserpb.FloatValue {
	return &browserpb.FloatValue{Value: nm.Milliseconds(us)}
}
