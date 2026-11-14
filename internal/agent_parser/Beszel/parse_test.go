package beszel

import (
	agentparser "certainstats/internal/agent_parser"
	"net/http"
	"strings"
	"testing"

	"github.com/fxamacker/cbor/v2"
)

func TestAgentType(t *testing.T) {
	parser := &BeszelStats{}
	if actual := parser.AgentType(); actual != "beszel" {
		t.Errorf("expected agent type \"beszel\", got %q", actual)
	}
}

func TestParseToken(t *testing.T) {
	parser := &BeszelStats{}

	t.Run("valid token", func(t *testing.T) {
		headers := http.Header{}
		headers.Set("X-Token", "valid-beszel-token-value")
		token, err := parser.ParseToken(headers)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if token != "valid-beszel-token-value" {
			t.Errorf("expected token to be %q, got %q", "valid-beszel-token-value", token)
		}
	})

	t.Run("missing token", func(t *testing.T) {
		headers := http.Header{}
		_, err := parser.ParseToken(headers)
		if err == nil || err.Error() != "missing X-Token header" {
			t.Errorf("expected missing X-Token header error, got %v", err)
		}
	})

	t.Run("token too long", func(t *testing.T) {
		headers := http.Header{}
		headers.Set("X-Token", strings.Repeat("A", 65))
		_, err := parser.ParseToken(headers)
		if err == nil || err.Error() != "token too long" {
			t.Errorf("expected token too long error, got %v", err)
		}
	})

	t.Run("invalid input type", func(t *testing.T) {
		_, err := parser.ParseToken("not-a-header")
		if err == nil || !strings.Contains(err.Error(), "invalid input type") {
			t.Errorf("expected invalid input type error, got %v", err)
		}
	})
}

func TestParse(t *testing.T) {
	parser := &BeszelStats{}

	t.Run("invalid cbor data", func(t *testing.T) {
		_, err := parser.Parse([]byte("not-cbor-data"))
		if err == nil || !strings.Contains(err.Error(), "beszel unmarshal failed") {
			t.Errorf("expected CBOR unmarshal error, got %v", err)
		}
	})

	t.Run("valid cbor metrics and details", func(t *testing.T) {
		combined := CombinedData{
			Info: Info{
				Uptime: 3600,
			},
			Details: &Details{
				Kernel:      "Linux 6.8",
				CpuModel:    "Intel Core i7",
				Cores:       8,
				MemoryTotal: 16384,
			},
			Stats: Stats{
				Cpu:          12.34,
				MemUsed:      4.5,
				SwapUsed:     1.5,
				DiskTotal:    200.0,
				DiskUsed:     50.0,
				CpuBreakdown: []float64{10.0, 1.0, 0.5, 0.8}, // User, System, IOWait, Steal
				NetworkInterfaces: map[string][4]uint64{
					"eth0": {100, 200, 5000, 6000}, // TX is index 2, RX is index 3
					"lo":   {10, 20, 50, 60},
				},
				DiskIO: [2]uint64{100, 200},
			},
		}

		data, err := cbor.Marshal(combined)
		if err != nil {
			t.Fatalf("failed to marshal test cbor: %v", err)
		}

		parsed, err := parser.Parse(data)
		if err != nil {
			t.Fatalf("Parse failed: %v", err)
		}

		if parsed.AgentInfo == nil {
			t.Fatal("expected AgentInfo to be populated")
		}
		if parsed.AgentInfo.Uptime != 3600 {
			t.Errorf("expected uptime 3600, got %d", parsed.AgentInfo.Uptime)
		}
		if parsed.AgentInfo.LinuxVersion != "Linux 6.8" {
			t.Errorf("expected LinuxVersion \"Linux 6.8\", got %q", parsed.AgentInfo.LinuxVersion)
		}
		if parsed.AgentInfo.CpuCores != 8 {
			t.Errorf("expected CPU cores 8, got %d", parsed.AgentInfo.CpuCores)
		}
		if parsed.AgentInfo.RamSize != 16384 {
			t.Errorf("expected RAM size 16384, got %d", parsed.AgentInfo.RamSize)
		}

		if len(parsed.Metrics) == 0 {
			t.Fatal("expected metrics to be parsed")
		}

		metrics := parsed.Metrics[0]
		if metrics.CPUUsagePercent != 12.34 {
			t.Errorf("expected CPU usage 12.34, got %f", metrics.CPUUsagePercent)
		}
		if metrics.RAMUsedBytes != uint64(4.5*1024*1024*1024) {
			t.Errorf("expected RAMUsedBytes %d, got %d", uint64(4.5*1024*1024*1024), metrics.RAMUsedBytes)
		}
		if metrics.RAMSwapUsedBytes != uint64(1.5*1024*1024*1024) {
			t.Errorf("expected RAMSwapUsedBytes %d, got %d", uint64(1.5*1024*1024*1024), metrics.RAMSwapUsedBytes)
		}
		if metrics.CPUIOWaitPercent != 0.5 {
			t.Errorf("expected CPUIOWaitPercent 0.5, got %f", metrics.CPUIOWaitPercent)
		}
		if metrics.CPUStealPercent != 0.8 {
			t.Errorf("expected CPUStealPercent 0.8, got %f", metrics.CPUStealPercent)
		}
		if metrics.TXBytes != 5000 {
			t.Errorf("expected TXBytes 5000 (from eth0), got %f", metrics.TXBytes)
		}
		if metrics.RXBytes != 6000 {
			t.Errorf("expected RXBytes 6000 (from eth0), got %f", metrics.RXBytes)
		}

		if len(metrics.Disks) == 0 {
			t.Fatal("expected disk telemetry to be parsed")
		}
		disk := metrics.Disks[0]
		if disk.Path != "/" {
			t.Errorf("expected disk path \"/\", got %q", disk.Path)
		}
		if disk.TotalBytes != uint64(200.0*1024*1024*1024) {
			t.Errorf("expected disk total %d, got %d", uint64(200.0*1024*1024*1024), disk.TotalBytes)
		}
		if disk.UsedBytes != uint64(50.0*1024*1024*1024) {
			t.Errorf("expected disk used %d, got %d", uint64(50.0*1024*1024*1024), disk.UsedBytes)
		}
		if disk.ReadBytes != 100 || disk.WriteBytes != 200 {
			t.Errorf("expected disk read/write 100/200, got %d/%d", disk.ReadBytes, disk.WriteBytes)
		}

		if metrics.NetworkIOType != agentparser.IOCumulative {
			t.Errorf("expected network io type cumulative, got %d", metrics.NetworkIOType)
		}
		if metrics.DiskIOType != agentparser.IORate {
			t.Errorf("expected disk io type rate, got %d", metrics.DiskIOType)
		}
	})

	t.Run("valid cbor details nil and fallback bandwidth network metrics", func(t *testing.T) {
		combined := CombinedData{
			Info: Info{
				Uptime: 4500,
			},
			Details: nil, // no hardware update
			Stats: Stats{
				Cpu:       25.0,
				MemUsed:   8.0,
				Bandwidth: [2]uint64{150, 300}, // sent, recv
				// No NetworkInterfaces provided
			},
		}

		data, err := cbor.Marshal(combined)
		if err != nil {
			t.Fatalf("failed to marshal test cbor: %v", err)
		}

		parsed, err := parser.Parse(data)
		if err != nil {
			t.Fatalf("Parse failed: %v", err)
		}

		if parsed.AgentInfo == nil {
			t.Fatal("expected AgentInfo to be populated")
		}
		if parsed.AgentInfo.LinuxVersion != "" {
			t.Errorf("expected empty LinuxVersion since Details was nil, got %q", parsed.AgentInfo.LinuxVersion)
		}

		metrics := parsed.Metrics[0]
		if metrics.TXBytes != 150 {
			t.Errorf("expected TXBytes 150 (fallback bandwidth), got %f", metrics.TXBytes)
		}
		if metrics.RXBytes != 300 {
			t.Errorf("expected RXBytes 300 (fallback bandwidth), got %f", metrics.RXBytes)
		}
	})

	t.Run("valid cbor multi-disk extra fs", func(t *testing.T) {
		combined := CombinedData{
			Info: Info{Uptime: 5000},
			Stats: Stats{
				Cpu:       15.0,
				DiskTotal: 100.0,
				DiskUsed:  30.0,
				DiskIO:    [2]uint64{500, 600},
				ExtraFs: map[string]*FsStats{
					"/home":     {Total: 80.0, Used: 20.0},
					"/mnt/data": {Total: 500.0, Used: 150.0},
				},
			},
		}

		data, err := cbor.Marshal(combined)
		if err != nil {
			t.Fatalf("failed to marshal cbor: %v", err)
		}

		parsed, err := parser.Parse(data)
		if err != nil {
			t.Fatalf("Parse failed: %v", err)
		}

		disks := parsed.Metrics[0].Disks
		if len(disks) != 3 {
			t.Fatalf("expected 3 disks (/, /home, /mnt/data), got %d", len(disks))
		}

		paths := make(map[string]agentparser.DiskTelemetry)
		for _, d := range disks {
			paths[d.Path] = d
		}

		if _, ok := paths["/"]; !ok {
			t.Errorf("expected root disk '/' to be present")
		}
		if homeDisk, ok := paths["/home"]; !ok || homeDisk.TotalBytes != uint64(80.0*1024*1024*1024) {
			t.Errorf("expected /home disk telemetry, got %+v", homeDisk)
		}
		if dataDisk, ok := paths["/mnt/data"]; !ok || dataDisk.UsedBytes != uint64(150.0*1024*1024*1024) {
			t.Errorf("expected /mnt/data disk telemetry, got %+v", dataDisk)
		}
	})
}

func TestParseByteMemoryFields(t *testing.T) {
	parser := &BeszelStats{}
	const gb = 1024 * 1024 * 1024

	t.Run("byte fields win over deprecated GB fields", func(t *testing.T) {
		data, err := cbor.Marshal(CombinedData{
			Details: &Details{},
			Stats: Stats{
				MemUsed:   4.5,
				Swap:      2.0,
				SwapUsed:  1.5,
				MemBytes:  [4]uint64{16*gb + 123, 4*gb + 7, 0, 0},
				SwapBytes: [2]uint64{2*gb + 5, 1*gb + 3},
			},
		})
		if err != nil {
			t.Fatalf("failed to marshal cbor: %v", err)
		}
		parsed, err := parser.Parse(data)
		if err != nil {
			t.Fatalf("Parse failed: %v", err)
		}
		m := parsed.Metrics[0]
		if m.RAMUsedBytes != 4*gb+7 {
			t.Errorf("expected RAMUsedBytes %d, got %d", uint64(4*gb+7), m.RAMUsedBytes)
		}
		if m.RAMSwapUsedBytes != 1*gb+3 {
			t.Errorf("expected RAMSwapUsedBytes %d, got %d", uint64(1*gb+3), m.RAMSwapUsedBytes)
		}
		if parsed.AgentInfo.SwapSize != 2*gb+5 {
			t.Errorf("expected SwapSize %d, got %d", uint64(2*gb+5), parsed.AgentInfo.SwapSize)
		}
		// Details.MemoryTotal is 0 here, so RAM size falls back to MemBytes.
		if parsed.AgentInfo.RamSize != 16*gb+123 {
			t.Errorf("expected RamSize %d, got %d", uint64(16*gb+123), parsed.AgentInfo.RamSize)
		}
	})

	t.Run("byte fields alone are not reported missing", func(t *testing.T) {
		// A future agent that drops the deprecated GB keys entirely.
		data, err := cbor.Marshal(map[int]any{
			0: map[int]any{
				0:  1.0,
				41: [4]uint64{8 * gb, 3 * gb, 0, 0},
				42: [2]uint64{2 * gb, 1 * gb},
			},
			1: map[int]any{5: 10},
		})
		if err != nil {
			t.Fatalf("failed to marshal cbor: %v", err)
		}
		parsed, err := parser.Parse(data)
		if err != nil {
			t.Fatalf("Parse failed: %v", err)
		}
		m := parsed.Metrics[0]
		if m.Missing["agent_ram_used"] || m.Missing["agent_swap_used"] {
			t.Errorf("expected ram/swap present, got missing=%v", m.Missing)
		}
		if m.RAMUsedBytes != 3*gb || m.RAMSwapUsedBytes != 1*gb {
			t.Errorf("expected ram/swap 3GiB/1GiB, got %d/%d", m.RAMUsedBytes, m.RAMSwapUsedBytes)
		}
	})

	t.Run("neither form present is reported missing", func(t *testing.T) {
		data, err := cbor.Marshal(map[int]any{0: map[int]any{0: 1.0}, 1: map[int]any{5: 10}})
		if err != nil {
			t.Fatalf("failed to marshal cbor: %v", err)
		}
		parsed, err := parser.Parse(data)
		if err != nil {
			t.Fatalf("Parse failed: %v", err)
		}
		m := parsed.Metrics[0]
		if !m.Missing["agent_ram_used"] || !m.Missing["agent_swap_used"] {
			t.Errorf("expected ram/swap missing, got missing=%v", m.Missing)
		}
	})
}

func TestParseCurrentAgentFields(t *testing.T) {
	// Every field added up to Beszel v0.21.2 must decode without failing the payload.
	signal := -55.0
	gpu, err := cbor.Marshal(GPUData{Name: "RTX", MemoryUsed: 512, MemoryTotal: 8192, Usage: 42, Power: 90, Engines: map[string]float64{"render": 10}, PowerPkg: 12})
	if err != nil {
		t.Fatalf("failed to marshal gpu: %v", err)
	}
	combined := CombinedData{
		SystemdServicesUpdated: true,
		Info: Info{
			Uptime:         1,
			RootDiskName:   "root",
			PackageUpdates: []uint16{12, 3},
			WiFi:           map[string]WiFi{"wlan0": {SSID: "home", Signal: &signal}},
			SystemdLogs:    true,
		},
		Details: &Details{ZfsInterval: 1, Sockets: 2, MemoryTotal: 1},
		Stats: Stats{
			GPUData:     map[string]cbor.RawMessage{"0": gpu},
			Fans:        map[string]uint16{"fan1": 1200},
			Batteries:   map[string]uint8{"BAT0": 80},
			DiskIOTotal: [2]uint64{1, 2},
			ZfsPools:    map[string]*ZfsPool{"tank": {Total: 100, Used: 40, ReadBytes: 1, WriteBytes: 2, Health: "ONLINE", Raw: true, HideUsage: true, HideIO: true, DisplayName: "Tank"}},
			WiFi:        map[string]int8{"wlan0": -55},
			ExtraFs:     map[string]*FsStats{"/data": {Total: 10, Used: 5, DiskReadBytes: 1, DiskWriteBytes: 2, DiskIoStats: [6]float64{1}, TotalRead: 3, TotalWrite: 4}},
		},
	}
	data, err := cbor.Marshal(combined)
	if err != nil {
		t.Fatalf("failed to marshal cbor: %v", err)
	}
	if _, err = (&BeszelStats{}).Parse(data); err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	var decoded CombinedData
	if err = cbor.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("round-trip decode failed: %v", err)
	}
	if got := decoded.Stats.ZfsPools["tank"]; got == nil || *got != *combined.Stats.ZfsPools["tank"] {
		t.Errorf("zfs pool round-trip mismatch: %+v", got)
	}
	if got := decoded.Info.WiFi["wlan0"]; got.SSID != "home" || got.Signal == nil || *got.Signal != signal {
		t.Errorf("wifi round-trip mismatch: %+v", got)
	}
	if decoded.Details.Sockets != 2 || !decoded.Info.SystemdLogs || !decoded.SystemdServicesUpdated {
		t.Errorf("scalar round-trip mismatch: %+v", decoded)
	}

	// GPUData must use upstream's key layout: 0=name, 3=usage, 4=power.
	rawGPU := map[int]any{}
	if err = cbor.Unmarshal(decoded.Stats.GPUData["0"], &rawGPU); err != nil {
		t.Fatalf("gpu decode failed: %v", err)
	}
	if rawGPU[0] != "RTX" || rawGPU[3] != 42.0 || rawGPU[4] != 90.0 {
		t.Errorf("unexpected gpu key layout: %v", rawGPU)
	}
}
