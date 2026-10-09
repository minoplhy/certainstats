package ws

import (
	"certainstats/internal/metrics"
	"certainstats/internal/ws/browserpb"
	"math"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// BrowserSnapshot copies an immutable cached snapshot into the wire model.
// A nil allowlist means the authenticated owner; a non-nil (even empty)
// allowlist exposes only permitted public fields and never private IDs.
func BrowserSnapshot(s *metrics.AgentSnapshot, allowed map[string]struct{}) *browserpb.Snapshot {
	out := &browserpb.Snapshot{}
	if s == nil {
		return out
	}
	full := allowed == nil
	can := func(name string) bool { _, ok := allowed[name]; return full || ok }
	out.Timestamp = timestamppb.New(s.Timestamp)
	if full {
		out.AgentId = proto.String(s.AgentID)
		out.LoadAvg = &browserpb.LoadAverage{Values: append([]float64(nil), s.LoadAvg[:]...)}
		if len(s.Temperatures) > 0 {
			out.Temperatures = &browserpb.Temperatures{Values: make(map[string]float64, len(s.Temperatures))}
			for k, v := range s.Temperatures {
				out.Temperatures.Values[k] = v
			}
		}
		if len(s.Missing) > 0 {
			out.Missing = &browserpb.MissingMetrics{Values: make(map[string]bool, len(s.Missing))}
			for k, v := range s.Missing {
				out.Missing.Values[k] = v
			}
		}
		if m := s.Metadata; m != nil {
			out.Metadata = &browserpb.Metadata{Uptime: m.Uptime, LinuxVersion: m.LinuxVersion, CpuModel: m.CpuModel, CpuCores: uint32(m.CpuCores), RamSize: m.RamSize, SwapSize: m.SwapSize, DiskSize: m.DiskSize}
		}
	}
	if can("uptime") && s.Metadata != nil {
		out.Uptime = proto.Uint32(s.Metadata.Uptime)
	}
	if can("agent_cpu_usage") {
		out.CpuUsagePercent = floatValue(s.CPUUsagePercent, s.Missing["agent_cpu_usage"])
	}
	if can("agent_cpu_iowait") {
		out.CpuIowaitPercent = floatValue(s.CPUIOWaitPercent, s.Missing["agent_cpu_iowait"])
	}
	if can("agent_cpu_steal") {
		out.CpuStealPercent = floatValue(s.CPUStealPercent, s.Missing["agent_cpu_steal"])
	}
	if can("agent_ram_used") {
		out.RamUsedBytes = uintValue(s.RAMUsedBytes, s.Missing["agent_ram_used"])
	}
	if can("agent_swap_used") {
		out.RamSwapUsedBytes = uintValue(s.RAMSwapUsedBytes, s.Missing["agent_swap_used"])
	}
	if can("agent_disk_used") {
		out.DiskUsedBytes = uintValue(s.DiskUsedBytes, s.Missing["agent_disk_used"])
	}
	if can("disk_size") {
		out.DiskTotalBytes = uintValue(s.DiskTotalBytes, false)
	}
	if can("agent_rx_bytes") {
		out.RxBytes = floatValue(s.RXBytes, s.Missing["agent_rx_bytes"])
		out.RxBps = floatValue(s.RXBps, s.Missing["agent_rx_bytes"])
	}
	if can("agent_tx_bytes") {
		out.TxBytes = floatValue(s.TXBytes, s.Missing["agent_tx_bytes"])
		out.TxBps = floatValue(s.TXBps, s.Missing["agent_tx_bytes"])
	}
	if can("agent_disk_read_bytes") {
		out.DiskReadBps = floatValue(s.DiskReadBps, s.Missing["agent_disk_read_bytes"])
	}
	if can("agent_disk_write_bytes") {
		out.DiskWriteBps = floatValue(s.DiskWriteBps, s.Missing["agent_disk_write_bytes"])
	}
	if can("agent_disk_used") || can("agent_disk_read_bytes") || can("agent_disk_write_bytes") {
		out.Disks = &browserpb.DiskList{}
		for _, disk := range s.Disks {
			d := &browserpb.Disk{Path: disk.Path}
			if can("disk_size") {
				d.TotalBytes = uintValue(disk.TotalBytes, false)
			}
			if can("agent_disk_used") {
				d.UsedBytes = uintValue(disk.UsedBytes, s.Missing["agent_disk_used"])
			}
			if can("agent_disk_read_bytes") {
				d.ReadBytes = uintValue(disk.ReadBytes, s.Missing["agent_disk_read_bytes"])
			}
			if can("agent_disk_write_bytes") {
				d.WriteBytes = uintValue(disk.WriteBytes, s.Missing["agent_disk_write_bytes"])
			}
			out.Disks.Items = append(out.Disks.Items, d)
		}
	}
	return out
}

func floatValue(value float64, missing bool) *browserpb.FloatValue {
	out := &browserpb.FloatValue{}
	if !missing && !math.IsNaN(value) && !math.IsInf(value, 0) {
		out.Value = proto.Float64(value)
	}
	return out
}
func uintValue(value uint64, missing bool) *browserpb.UintValue {
	out := &browserpb.UintValue{}
	if !missing {
		out.Value = proto.Uint64(value)
	}
	return out
}
