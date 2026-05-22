package agent

import "time"

type Agent struct {
	AgentID      string
	UserID       string
	AgentType    string
	Nickname     string
	LastSeen     *time.Time
	IsOnline     bool
	Uptime       uint32
	LinuxVersion string
	CpuModel     string
	CpuCores     uint16
	RamSize      uint64
	SwapSize     uint64
	DiskSize     uint64

	TotalRxBytes        uint64
	TotalTxBytes        uint64
	TotalDiskReadBytes  uint64
	TotalDiskWriteBytes uint64
	Disks               []DiskOdometer
	Note                string
}

type DiskOdometer struct {
	Path       string
	ReadBytes  uint64
	WriteBytes uint64
}

type RenameRequest struct {
	AgentID  string  `json:"agent_id"`
	Nickname *string `json:"nickname,omitempty"`
	Note     *string `json:"note,omitempty"`
}
