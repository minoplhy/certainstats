package agent

import (
	"certainstats/internal/agentmeta"
	"time"
)

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

	// Runtime metadata reported by the agent; nil when never reported.
	AgentVersion           *string
	ProtocolVersion        *string
	AgentVersionSource     string
	AgentVersionObservedAt *time.Time
	ReportedCapabilities   *agentmeta.Declaration
	CapabilitiesReportedAt *time.Time
}

type DiskOdometer struct {
	Path       string
	TotalBytes uint64
	ReadBytes  uint64
	WriteBytes uint64
}

type RenameRequest struct {
	AgentID  string  `json:"agent_id"`
	Nickname *string `json:"nickname,omitempty"`
	Note     *string `json:"note,omitempty"`
}
