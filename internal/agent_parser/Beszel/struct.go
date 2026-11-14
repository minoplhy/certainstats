package beszel

import (
	nm "certainstats/internal/networkmonitor"
	"github.com/fxamacker/cbor/v2"
	"time"
)

// The types in this file mirror the CBOR wire format of the Beszel agent.
// Synced with henrygd/beszel v0.21.2 (internal/entities/system/system.go).
// CBOR keys are part of the protocol: never renumber or reuse them.

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

// WiFi describes a connected station interface. Signal is native dBm; nil
// means the OS confirmed association but could not supply RSSI.
type WiFi struct {
	SSID   string   `cbor:"0,keyasint,omitempty"`
	Signal *float64 `cbor:"1,keyasint,omitempty"`
}

type Info struct {
	Hostname      string  `cbor:"0,keyasint,omitempty"` // Deprecated: moved to Details.
	KernelVersion string  `cbor:"1,keyasint,omitempty"` // Deprecated: moved to Details.
	Cores         int     `cbor:"2,keyasint,omitzero"`  // Deprecated: moved to Details.
	Threads       int     `cbor:"3,keyasint,omitempty"`
	CpuModel      string  `cbor:"4,keyasint,omitempty"` // Deprecated: moved to Details.
	Uptime        uint64  `cbor:"5,keyasint"`
	Cpu           float64 `cbor:"6,keyasint"`
	MemPct        float64 `cbor:"7,keyasint"`
	DiskPct       float64 `cbor:"8,keyasint"`
	Bandwidth     float64 `cbor:"9,keyasint"` // Deprecated: use BandwidthBytes.
	AgentVersion  string  `cbor:"10,keyasint"`
	Podman        bool    `cbor:"11,keyasint,omitempty"` // Deprecated: moved to Details.
	GpuPct        float64 `cbor:"12,keyasint,omitempty"`
	DashboardTemp float64 `cbor:"13,keyasint,omitempty"`
	Os            Os      `cbor:"14,keyasint,omitempty"` // Deprecated: moved to Details.
	// cbor keys 15-17 were LoadAvg1/5/15 (removed upstream) - do not reuse
	BandwidthBytes uint64             `cbor:"18,keyasint"`
	LoadAvg        [3]float64         `cbor:"19,keyasint"`
	ConnectionType ConnectionType     `cbor:"20,keyasint,omitempty,omitzero"`
	ExtraFsPct     map[string]float64 `cbor:"21,keyasint,omitempty"`
	Services       []uint16           `cbor:"22,keyasint,omitempty"` // [totalServices, numFailedServices]
	Battery        [2]uint8           `cbor:"23,keyasint,omitzero"`  // [percent, charge state]
	RootDiskName   string             `cbor:"24,keyasint,omitempty"` // custom name for the root disk
	PackageUpdates []uint16           `cbor:"25,keyasint,omitempty"` // [totalUpdates, securityUpdates]
	WiFi           map[string]WiFi    `cbor:"26,keyasint,omitempty"` // keyed by OS interface, not SSID
	SystemdLogs    bool               `cbor:"27,keyasint,omitempty"` // agent can read the system journal
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
	ZfsInterval   time.Duration `cbor:"11,keyasint,omitempty"` // interval for ZFS detail refresh
	Sockets       int           `cbor:"12,keyasint,omitempty"` // physical CPU packages, 0 if unknown
}

type FsStats struct {
	Total          float64    `cbor:"0,keyasint"`           // GB
	Used           float64    `cbor:"1,keyasint"`           // GB
	DiskReadPs     float64    `cbor:"2,keyasint,omitzero"`  // Deprecated: sent by agents < 0.13.0.
	DiskWritePs    float64    `cbor:"3,keyasint,omitzero"`  // Deprecated: sent by agents < 0.13.0.
	DiskReadBytes  uint64     `cbor:"6,keyasint,omitempty"` // bytes/s
	DiskWriteBytes uint64     `cbor:"7,keyasint,omitempty"` // bytes/s
	DiskIoStats    [6]float64 `cbor:"8,keyasint,omitzero"`  // [read time %, write time %, io utilization %, r_await ms, w_await ms, weighted io %]
	TotalRead      uint64     `cbor:"9,keyasint,omitzero"`  // cumulative device read bytes
	TotalWrite     uint64     `cbor:"10,keyasint,omitzero"` // cumulative device write bytes
}

type GPUData struct {
	Name        string             `cbor:"0,keyasint"`
	MemoryUsed  float64            `cbor:"1,keyasint,omitempty,omitzero"`
	MemoryTotal float64            `cbor:"2,keyasint,omitempty,omitzero"`
	Usage       float64            `cbor:"3,keyasint,omitempty"`
	Power       float64            `cbor:"4,keyasint,omitempty"`
	Engines     map[string]float64 `cbor:"5,keyasint,omitempty"`
	PowerPkg    float64            `cbor:"6,keyasint,omitempty"`
}

// ZfsPool holds per-pool ZFS metrics for a single collection interval.
type ZfsPool struct {
	Total       float64 `cbor:"0,keyasint"`           // total capacity in GiB
	Used        float64 `cbor:"1,keyasint"`           // allocated in GiB
	ReadBytes   uint64  `cbor:"2,keyasint,omitzero"`  // read throughput in bytes/s
	WriteBytes  uint64  `cbor:"3,keyasint,omitzero"`  // write throughput in bytes/s
	Health      string  `cbor:"4,keyasint,omitempty"` // ONLINE, DEGRADED, FAULTED, ...
	Raw         bool    `cbor:"5,keyasint,omitempty"`
	HideUsage   bool    `cbor:"6,keyasint,omitempty"`
	HideIO      bool    `cbor:"7,keyasint,omitempty"`
	DisplayName string  `cbor:"8,keyasint,omitempty"`
}

type Stats struct {
	Cpu          float64             `cbor:"0,keyasint"`
	Mem          float64             `cbor:"2,keyasint"` // Deprecated: GB, use MemBytes.
	MemUsed      float64             `cbor:"3,keyasint"` // Deprecated: GB, use MemBytes.
	MemPct       float64             `cbor:"4,keyasint"`
	MemBuffCache float64             `cbor:"5,keyasint"`           // Deprecated: GB, use MemBytes.
	MemZfsArc    float64             `cbor:"6,keyasint,omitempty"` // Deprecated: GB, use MemBytes.
	Swap         float64             `cbor:"7,keyasint,omitempty"` // Deprecated: GB, use SwapBytes.
	SwapUsed     float64             `cbor:"8,keyasint,omitempty"` // Deprecated: GB, use SwapBytes.
	DiskTotal    float64             `cbor:"9,keyasint"`
	DiskUsed     float64             `cbor:"10,keyasint"`
	DiskPct      float64             `cbor:"11,keyasint"`
	DiskReadPs   float64             `cbor:"12,keyasint,omitzero"` // Deprecated: sent by agents < 0.13.0.
	DiskWritePs  float64             `cbor:"13,keyasint,omitzero"` // Deprecated: sent by agents < 0.13.0.
	NetworkSent  float64             `cbor:"16,keyasint,omitzero"`
	NetworkRecv  float64             `cbor:"17,keyasint,omitzero"`
	Temperatures map[string]float64  `cbor:"20,keyasint,omitempty"`
	ExtraFs      map[string]*FsStats `cbor:"21,keyasint,omitempty"`
	// GPUData values decode as GPUData. Kept raw so a GPU shape change can
	// never fail the whole payload.
	GPUData map[string]cbor.RawMessage `cbor:"22,keyasint,omitempty"`
	// cbor keys 23-25 were LoadAvg1/5/15 (removed upstream) - do not reuse
	Bandwidth         [2]uint64            `cbor:"26,keyasint,omitzero"` // [sent bytes, recv bytes]
	LoadAvg           [3]float64           `cbor:"28,keyasint"`
	Battery           [2]uint8             `cbor:"29,keyasint,omitzero"`  // [percent, charge state]
	NetworkInterfaces map[string][4]uint64 `cbor:"31,keyasint,omitempty"` // [upload bytes, download bytes, total upload, total download]
	DiskIO            [2]uint64            `cbor:"32,keyasint,omitzero"`  // [read bytes, write bytes]
	CpuBreakdown      []float64            `cbor:"33,keyasint,omitempty"` // [user, system, iowait, steal, idle, irq, softirq, nice]
	CpuCoresUsage     []uint8              `cbor:"34,keyasint,omitempty"` // per-core busy usage
	DiskIoStats       [6]float64           `cbor:"35,keyasint,omitzero"`  // [read time %, write time %, io utilization %, r_await ms, w_await ms, weighted io %]
	Fans              map[string]uint16    `cbor:"36,keyasint,omitempty"`
	Batteries         map[string]uint8     `cbor:"37,keyasint,omitempty"`
	DiskIOTotal       [2]uint64            `cbor:"38,keyasint,omitzero"`  // [total read bytes, total write bytes] cumulative
	ZfsPools          map[string]*ZfsPool  `cbor:"39,keyasint,omitempty"` // keyed by pool name
	WiFi              map[string]int8      `cbor:"40,keyasint,omitempty"` // RSSI dBm keyed by interface
	MemBytes          [4]uint64            `cbor:"41,keyasint,omitzero"`  // [total, used, buff/cache, zfs arc] bytes
	SwapBytes         [2]uint64            `cbor:"42,keyasint,omitzero"`  // [total, used] bytes
}

type CombinedData struct {
	Monitors map[string]nm.Result `cbor:"6,keyasint"`
	Stats    Stats                `cbor:"0,keyasint"`
	Info     Info                 `cbor:"1,keyasint"`
	Data     []any                `cbor:"2,keyasint,omitempty"`
	Svc      []any                `cbor:"3,keyasint,omitempty"`
	Details  *Details             `cbor:"4,keyasint,omitempty"`
	// SystemdServicesUpdated distinguishes a fresh empty systemd snapshot
	// from a response that omitted systemd data.
	SystemdServicesUpdated bool `cbor:"5,keyasint,omitempty"`
}

type BeszelStats struct{}
