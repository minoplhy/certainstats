package beszel

import (
	"time"
)

type Os = uint8

const (
	Linux Os = iota
	Darwin
	Windows
	Freebsd
)

type ConnectionType = uint8

const (
	ConnectionTypeNone ConnectionType = iota
	ConnectionTypeSSH
	ConnectionTypeWebSocket
)

type Info struct {
	Hostname       string             `cbor:"0,keyasint,omitempty"`
	KernelVersion  string             `cbor:"1,keyasint,omitempty"`
	Cores          int                `cbor:"2,keyasint,omitzero"`
	Threads        int                `cbor:"3,keyasint,omitempty"`
	CpuModel       string             `cbor:"4,keyasint,omitempty"`
	Uptime         uint64             `cbor:"5,keyasint"`
	Cpu            float64            `cbor:"6,keyasint"`
	MemPct         float64            `cbor:"7,keyasint"`
	DiskPct        float64            `cbor:"8,keyasint"`
	Bandwidth      float64            `cbor:"9,keyasint"`
	AgentVersion   string             `cbor:"10,keyasint"`
	Podman         bool               `cbor:"11,keyasint,omitempty"`
	GpuPct         float64            `cbor:"12,keyasint,omitempty"`
	DashboardTemp  float64            `cbor:"13,keyasint,omitempty"`
	Os             Os                 `cbor:"14,keyasint,omitempty"`
	BandwidthBytes uint64             `cbor:"18,keyasint"`
	LoadAvg        [3]float64         `cbor:"19,keyasint"`
	ConnectionType ConnectionType     `cbor:"20,keyasint,omitempty,omitzero"`
	ExtraFsPct     map[string]float64 `cbor:"21,keyasint,omitempty"`
	Services       []uint16           `cbor:"22,keyasint,omitempty"`
	Battery        [2]uint8           `cbor:"23,keyasint,omitzero"`
}

type Details struct {
	Hostname      string        `cbor:"0,keyasint"`
	Kernel        string        `cbor:"1,keyasint,omitempty"`
	Cores         int           `cbor:"2,keyasint"`
	Threads       int           `cbor:"3,keyasint"`
	CpuModel      string        `cbor:"4,keyasint"`
	Os            Os            `cbor:"5,keyasint"`
	OsName        string        `cbor:"6,keyasint"`
	Arch          string        `cbor:"7,keyasint"`
	Podman        bool          `cbor:"8,keyasint,omitempty"`
	MemoryTotal   uint64        `cbor:"9,keyasint"`
	SmartInterval time.Duration `cbor:"10,keyasint,omitempty"`
}

type FsStats struct {
	Used  float64 `cbor:"0,keyasint"`
	Total float64 `cbor:"1,keyasint"`
}

type GPUData struct {
	Usage   float64 `cbor:"0,keyasint"`
	MemUsed float64 `cbor:"1,keyasint"`
	Temp    float64 `cbor:"2,keyasint"`
}

type Stats struct {
	Cpu               float64              `cbor:"0,keyasint"`
	Mem               float64              `cbor:"2,keyasint"`
	MemUsed           float64              `cbor:"3,keyasint"`
	MemPct            float64              `cbor:"4,keyasint"`
	MemBuffCache      float64              `cbor:"5,keyasint"`
	MemZfsArc         float64              `cbor:"6,keyasint,omitempty"`
	Swap              float64              `cbor:"7,keyasint,omitempty"`
	SwapUsed          float64              `cbor:"8,keyasint,omitempty"`
	DiskTotal         float64              `cbor:"9,keyasint"`
	DiskUsed          float64              `cbor:"10,keyasint"`
	DiskPct           float64              `cbor:"11,keyasint"`
	DiskReadPs        float64              `cbor:"12,keyasint,omitzero"`
	DiskWritePs       float64              `cbor:"13,keyasint,omitzero"`
	NetworkSent       float64              `cbor:"16,keyasint,omitzero"`
	NetworkRecv       float64              `cbor:"17,keyasint,omitzero"`
	Temperatures      map[string]float64   `cbor:"20,keyasint,omitempty"`
	ExtraFs           map[string]*FsStats  `cbor:"21,keyasint,omitempty"`
	GPUData           map[string]GPUData   `cbor:"22,keyasint,omitempty"`
	Bandwidth         [2]uint64            `cbor:"26,keyasint,omitzero"`
	LoadAvg           [3]float64           `cbor:"28,keyasint"`
	Battery           [2]uint8             `cbor:"29,keyasint,omitzero"`
	NetworkInterfaces map[string][4]uint64 `cbor:"31,keyasint,omitempty"`
	DiskIO            [2]uint64            `cbor:"32,keyasint,omitzero"`
	CpuBreakdown      []float64            `cbor:"33,keyasint,omitempty"`
	CpuCoresUsage     []uint8              `cbor:"34,keyasint,omitempty"`
	DiskIoStats       [6]float64           `cbor:"35,keyasint,omitzero"`
}

type CombinedData struct {
	Stats   Stats    `cbor:"0,keyasint"`
	Info    Info     `cbor:"1,keyasint"`
	Data    []any    `cbor:"2,keyasint,omitempty"`
	Svc     []any    `cbor:"3,keyasint,omitempty"`
	Details *Details `cbor:"4,keyasint,omitempty"`
}

type BeszelStats struct{}
