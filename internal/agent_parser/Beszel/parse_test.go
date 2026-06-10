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
}
