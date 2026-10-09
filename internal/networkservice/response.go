package networkservice

import nm "certainstats/internal/networkmonitor"

// Wire results remain integer microseconds in storage. Private HTTP responses
// expose milliseconds and distinguish failed probes from a measured zero.
type latestResponse struct {
	Avg       *float64 `json:"response_avg_ms"`
	Min       *float64 `json:"response_min_ms"`
	Max       *float64 `json:"response_max_ms"`
	Avg1h     *float64 `json:"response_avg_1h_ms"`
	Min1h     *float64 `json:"response_min_1h_ms"`
	Max1h     *float64 `json:"response_max_1h_ms"`
	Loss      float64  `json:"loss_pct"`
	Loss1h    float64  `json:"loss_1h_pct"`
	LastProbe int64    `json:"last_probe_at"`
	Samples   int64    `json:"sample_count"`
	Attempts  int64    `json:"attempt_count"`
	Success   int64    `json:"success_count"`
	Received  int64    `json:"received_at"`
	Cert      *nm.Cert `json:"certificate,omitempty"`
	CertAt    int64    `json:"certificate_received_at,omitempty"`
}

type monitorResponse struct {
	nm.Monitor
	Latest *latestResponse `json:"latest,omitempty"`
}

func monitorJSON(m nm.Monitor) monitorResponse {
	out := monitorResponse{Monitor: m}
	latest := m.Latest
	if latest == nil {
		return out
	}

	out.Latest = &latestResponse{
		Loss:      latest.Loss,
		Loss1h:    latest.Loss1h,
		LastProbe: latest.LastProbeAt,
		Samples:   latest.SampleCount,
		Attempts:  latest.Total,
		Success:   latest.Success,
		Received:  latest.ReceivedAt,
		Cert:      latest.Cert,
		CertAt:    latest.CertificateReceivedAt,
	}
	if latest.Success > 0 {
		out.Latest.Avg = nm.Milliseconds(latest.Avg)
		out.Latest.Min = nm.Milliseconds(latest.Min)
		out.Latest.Max = nm.Milliseconds(latest.Max)
	}
	if latest.SampleCount > 0 && latest.Loss1h < 100 {
		out.Latest.Avg1h = nm.Milliseconds(latest.Avg1h)
		out.Latest.Min1h = nm.Milliseconds(latest.Min1h)
		out.Latest.Max1h = nm.Milliseconds(latest.Max1h)
	}
	return out
}

func monitorsJSON(items []nm.Monitor) []monitorResponse {
	out := make([]monitorResponse, len(items))
	for i, m := range items {
		out[i] = monitorJSON(m)
	}
	return out
}
