package ws

import (
	"certainstats/internal/ws/browserpb"
	"encoding/json"
	"math"
)

// marshalLegacyJSON preserves the browser JSON contract after permission
// filtering. It deliberately does not use Protobuf's JSON representation.
func marshalLegacyJSON(envelope *browserpb.TelemetryEnvelope, public bool) ([]byte, error) {
	data := make(map[string]any)
	for id, s := range envelope.GetPulse().GetAgents() {
		data[id] = legacySnapshot(s, public)
	}
	var network *map[string]any
	if !public && envelope.GetPulse().GetNetwork() != nil {
		n := legacyNetwork(envelope.GetPulse().GetNetwork())
		network = &n
	}
	return json.Marshal(struct {
		Type    string          `json:"type"`
		Data    map[string]any  `json:"data"`
		Network *map[string]any `json:"network,omitempty"`
	}{"agent_update", data, network})
}

func legacySnapshot(s *browserpb.Snapshot, public bool) map[string]any {
	out := make(map[string]any)
	put := func(admin, pub string, value any) {
		if public {
			out[pub] = value
		} else {
			out[admin] = value
		}
	}
	f := func(admin, pub string, v *browserpb.FloatValue) {
		if v != nil {
			var value any
			if v.Value != nil {
				value = finite(*v.Value)
			}
			put(admin, pub, value)
		}
	}
	u := func(admin, pub string, v *browserpb.UintValue) {
		if v != nil {
			var value any
			if v.Value != nil {
				value = *v.Value
			}
			put(admin, pub, value)
		}
	}
	if s.Timestamp != nil {
		put("timestamp", "Timestamp", s.Timestamp.AsTime())
	}
	if s.Available != nil {
		out["available"] = *s.Available
	}
	if s.IsOnline != nil {
		out["is_online"] = *s.IsOnline
	}
	if !public && s.AgentId != nil {
		out["agent_id"] = *s.AgentId
	}
	if public && s.Uptime != nil {
		out["Uptime"] = *s.Uptime
	}
	f("cpu_usage_percent", "CPUUsagePercent", s.CpuUsagePercent)
	f("cpu_iowait_percent", "CPUIOWaitPercent", s.CpuIowaitPercent)
	f("cpu_steal_percent", "CPUStealPercent", s.CpuStealPercent)
	u("ram_used_bytes", "RAMUsedBytes", s.RamUsedBytes)
	u("ram_swap_used_bytes", "RAMSwapUsedBytes", s.RamSwapUsedBytes)
	u("disk_used_bytes", "DiskUsedBytes", s.DiskUsedBytes)
	u("disk_total_bytes", "DiskTotalBytes", s.DiskTotalBytes)
	f("rx_bytes", "RXBytes", s.RxBytes)
	f("tx_bytes", "TXBytes", s.TxBytes)
	f("rx_bps", "RXBps", s.RxBps)
	f("tx_bps", "TXBps", s.TxBps)
	f("disk_read_bps", "DiskReadBps", s.DiskReadBps)
	f("disk_write_bps", "DiskWriteBps", s.DiskWriteBps)
	if s.Disks != nil {
		disks := make([]map[string]any, 0, len(s.Disks.Items))
		for _, d := range s.Disks.Items {
			item := map[string]any{"path": d.Path}
			for name, v := range map[string]*browserpb.UintValue{"used_bytes": d.UsedBytes, "total_bytes": d.TotalBytes, "read_bytes": d.ReadBytes, "write_bytes": d.WriteBytes} {
				if v != nil {
					if v.Value == nil {
						item[name] = nil
					} else {
						item[name] = *v.Value
					}
				}
			}
			disks = append(disks, item)
		}
		put("disks", "Disks", disks)
	}
	if !public {
		if s.LoadAvg != nil {
			values := make([]any, 0, len(s.LoadAvg.Values))
			for _, v := range s.LoadAvg.Values {
				values = append(values, finite(v))
			}
			out["load_avg"] = values
		}
		if s.Temperatures != nil {
			values := map[string]any{}
			for k, v := range s.Temperatures.Values {
				values[k] = finite(v)
			}
			out["temperatures"] = values
		}
		if s.Missing != nil {
			out["missing"] = s.Missing.Values
		}
		if m := s.Metadata; m != nil {
			out["metadata"] = map[string]any{"Uptime": m.Uptime, "LinuxVersion": m.LinuxVersion, "CpuModel": m.CpuModel, "CpuCores": m.CpuCores, "RamSize": m.RamSize, "SwapSize": m.SwapSize, "DiskSize": m.DiskSize}
		}
	}
	return out
}
func finite(v float64) any {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return nil
	}
	return v
}

func legacyNetwork(p *browserpb.NetworkPulse) map[string]any {
	out := make(map[string]any, len(p.Monitors))
	for id, m := range p.Monitors {
		sync := map[string]any{"desired_generation": m.Sync.DesiredGeneration, "acknowledged_generation": m.Sync.AcknowledgedGeneration, "error": m.Sync.Error}
		if m.Sync.LastAttempt != nil {
			sync["last_attempt"] = m.Sync.LastAttempt.AsTime()
		}
		if m.Sync.LastAck != nil {
			sync["last_ack"] = m.Sync.LastAck.AsTime()
		}
		item := map[string]any{"agent_id": m.AgentId, "enabled": m.Enabled, "state": m.State, "sync": sync, "latest": nil}
		if l := m.Latest; l != nil {
			latest := map[string]any{"loss_pct": l.LossPct, "loss_1h_pct": l.Loss_1HPct, "last_probe_at": l.LastProbeAt, "sample_count": l.SampleCount, "attempt_count": l.AttemptCount, "success_count": l.SuccessCount, "received_at": l.ReceivedAt, "certificate_received_at": l.CertificateReceivedAt}
			for key, v := range map[string]*browserpb.FloatValue{"response_avg_ms": l.ResponseAvgMs, "response_min_ms": l.ResponseMinMs, "response_max_ms": l.ResponseMaxMs, "response_avg_1h_ms": l.ResponseAvg_1HMs, "response_min_1h_ms": l.ResponseMin_1HMs, "response_max_1h_ms": l.ResponseMax_1HMs} {
				latest[key] = nil
				if v != nil && v.Value != nil {
					latest[key] = finite(*v.Value)
				}
			}
			if l.Certificate != nil {
				latest["certificate"] = map[string]any{"expires": l.Certificate.Expires, "issuer": l.Certificate.Issuer}
			}
			item["latest"] = latest
		}
		out[id] = item
	}
	return out
}
