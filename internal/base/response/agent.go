package response

type PublicAgent struct {
	PublicID     string  `json:"public_id"`
	Name         string  `json:"display_name"`
	IsOnline     *bool   `json:"is_online,omitempty"`
	Uptime       *uint32 `json:"uptime,omitempty"`
	LinuxVersion *string `json:"linux_version,omitempty"`
	CpuModel     *string `json:"cpu_model,omitempty"`
	CpuCores     *uint16 `json:"cpu_cores,omitempty"`
	RamSize      *uint64 `json:"ram_size,omitempty"`
	SwapSize     *uint64        `json:"swap_size,omitempty"`
	DiskSize     *uint64        `json:"disk_size,omitempty"`
	Net          *NetOdometer   `json:"net,omitempty"`
	Disks        []DiskOdometer `json:"disks,omitempty"`
}

type DiskOdometer struct {
	Path       string  `json:"path"`
	ReadBytes  *uint64 `json:"read_bytes,omitempty"`
	WriteBytes *uint64 `json:"write_bytes,omitempty"`
}

type NetOdometer struct {
	TotalRxBytes *uint64 `json:"total_rx_bytes,omitempty"`
	TotalTxBytes *uint64 `json:"total_tx_bytes,omitempty"`
}

type Agent struct {
	AgentID      string         `json:"agent_id"`
	AgentType    string         `json:"agent_type"`
	Nickname     string         `json:"nickname"`
	LastSeen     *string        `json:"last_seen"`
	IsOnline     bool           `json:"is_online"`
	Uptime       uint32         `json:"uptime"`
	LinuxVersion string         `json:"linux_version"`
	CpuModel     string         `json:"cpu_model"`
	CpuCores     uint16         `json:"cpu_cores"`
	RamSize      uint64         `json:"ram_size"`
	SwapSize     uint64         `json:"swap_size"`
	DiskSize     uint64         `json:"disk_size"`
	Net          NetOdometer    `json:"net"`
	Disks        []DiskOdometer `json:"disks"`
}
