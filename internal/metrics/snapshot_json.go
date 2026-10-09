package metrics

import "encoding/json"

func (s AgentSnapshot) MarshalJSON() ([]byte, error) {
	type plain AgentSnapshot
	raw, err := json.Marshal(plain(s))
	if err != nil {
		return nil, err
	}
	var fields map[string]any
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil, err
	}
	names := map[string]string{"agent_cpu_usage": "cpu_usage_percent", "agent_cpu_iowait": "cpu_iowait_percent", "agent_cpu_steal": "cpu_steal_percent", "agent_ram_used": "ram_used_bytes", "agent_swap_used": "ram_swap_used_bytes", "agent_disk_used": "disk_used_bytes"}
	for metric, field := range names {
		if s.Missing[metric] {
			fields[field] = nil
		}
	}
	for metric, names := range map[string][]string{"agent_rx_bytes": {"rx_bytes", "rx_bps"}, "agent_tx_bytes": {"tx_bytes", "tx_bps"}, "agent_disk_read_bytes": {"disk_read_bps"}, "agent_disk_write_bytes": {"disk_write_bps"}} {
		if s.Missing[metric] {
			for _, field := range names {
				fields[field] = nil
			}
		}
	}
	return json.Marshal(fields)
}
