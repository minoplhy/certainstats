package agentparser

import (
	"time"
)

// IOSemantics describes how throughput byte values should be interpreted.
// submit.go normalizes all values to IODelta before writing to TSDB.
type IOSemantics uint8

const (
	IODelta      IOSemantics = iota // bytes transferred in this interval (LTstats, HetrixTools)
	IORate                          // bytes per second (Beszel disk I/O)
	IOCumulative                    // cumulative total bytes since boot (Beszel network)
)

type DiskTelemetry struct {
	Path       string `json:"path"`
	UsedBytes  uint64 `json:"used_bytes"`
	TotalBytes uint64 `json:"total_bytes"`
	ReadBytes  uint64 `json:"read_bytes"`
	WriteBytes uint64 `json:"write_bytes"`
}

// Telemetry holds the fast-changing metrics for the TSDB
type Telemetry struct {
	Timestamp           time.Time
	CPUUsagePercent     float64
	CPUIOWaitPercent    float64
	CPUStealPercent     float64
	RAMUsedBytes     uint64
	RAMSwapUsedBytes uint64
	TXBytes             float64
	RXBytes             float64
	Disks               []DiskTelemetry

	// IO semantics — parsers set these to declare what RX/TX/Disk byte values represent.
	// Default zero value (IODelta) means "bytes in this interval", which requires no conversion.
	NetworkIOType IOSemantics
	DiskIOType    IOSemantics
}

type ParsedMetadata struct {
	Uptime       uint32
	LinuxVersion string
	CpuModel     string
	CpuCores     uint16
	RamSize      uint64
	SwapSize     uint64
	DiskSize     uint64
}

// ParsedData is the final output of the parsers
type ParsedData struct {
	AgentInfo *ParsedMetadata
	Metrics   []Telemetry
}

type AgentParser interface {
	Parse(rawPayload []byte) (*ParsedData, error)
	ParseToken(input any) (string, error)
	AgentType() string
}
