package accessrules

type AccessRule struct {
	AllowedFeatures []string `json:"allowed_fields"`
	AllowedMetrics  []string `json:"allowed_metrics"`
	MaxDays         uint     `json:"max_days"`
}

type AccessRules map[string]AccessRule

const (
	PUBLIC  = "public"
	PRIVATE = "private"
)

var FeaturesList = []string{
	"is_online",
	"uptime",
	"linux_version",
	"cpu_model",
	"cpu_cores",
	"ram_size",
	"swap_size",
	"disk_size",
}

var MetricsList = []string{
	"agent_cpu_usage",
	"agent_cpu_iowait",
	"agent_cpu_steal",
	"agent_ram_used",
	"agent_swap_used",
	"agent_disk_used",
	"agent_disk_read_bytes",
	"agent_disk_write_bytes",
	"agent_rx_bytes",
	"agent_tx_bytes",
}
