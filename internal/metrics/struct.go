package metrics

// allowedMetrics is the server-side allowlist of metric names that may be queried.
// Any name not present here is rejected with 400 before any cache or TSDB access.
var allowedMetrics = map[string]bool{
	"agent_cpu_usage":         true,
	"agent_cpu_iowait":        true,
	"agent_cpu_steal":         true,
	"agent_ram_used":          true,
	"agent_swap_used":         true,
	"agent_disk_used":         true,
	"agent_disk_usage":        true, // percentage, derived from disk_used/disk_total
	"agent_disk_read_bytes":   true,
	"agent_disk_write_bytes":  true,
	"agent_rx_bytes":          true,
	"agent_tx_bytes":          true,
}
