package beszel

import (
	agentparser "certainstats/internal/agent_parser"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/fxamacker/cbor/v2"
)

func (b *BeszelStats) AgentType() string {
	return "beszel"
}

// ParseToken extracts the token from HTTP headers for Beszel WebSocket connections.
func (b *BeszelStats) ParseToken(input any) (string, error) {
	headers, ok := input.(http.Header)
	if !ok {
		return "", errors.New("invalid input type for Beszel token parsing: expected http.Header")
	}

	token := headers.Get("X-Token")
	//	agentVersion := headers.Get("X-Beszel")

	if token == "" {
		return "", errors.New("missing X-Token header")
	}
	if len(token) > 64 {
		return "", errors.New("token too long")
	}

	// Note: X-Beszel (version) is also available in headers if needed
	return token, nil
}

func (b *BeszelStats) Parse(data []byte) (*agentparser.ParsedData, error) {
	var combined CombinedData
	if err := cbor.Unmarshal(data, &combined); err != nil {
		return nil, fmt.Errorf("beszel unmarshal failed: %w", err)
	}

	// 1. Map Metadata
	// We distinguish between hardware specs (static) and status (uptime).
	agentInfo := &agentparser.ParsedMetadata{
		Uptime: uint32(combined.Info.Uptime),
	}

	// 2. Hardware Specs Update
	// We ONLY update hardware specs (RAM, CPU, OS) when the 'Details' packet is present.
	if combined.Details != nil {
		agentInfo.LinuxVersion = combined.Details.Kernel
		agentInfo.CpuModel = combined.Details.CpuModel
		agentInfo.CpuCores = uint16(combined.Details.Cores)

		// RAM: Use explicitly-defined MemoryTotal (MB)
		if combined.Details.MemoryTotal > 0 {
			//agentInfo.RamSize = combined.Details.MemoryTotal * 1024 * 1024
			agentInfo.RamSize = combined.Details.MemoryTotal
		}

		// Disk/Swap: Use Stats fields with standard GB assumption,
		// but only update them during this high-fidelity Details event.
		if combined.Stats.DiskTotal > 0 {
			agentInfo.DiskSize = uint64(combined.Stats.DiskTotal * 1024 * 1024 * 1024)
		}
		if combined.Stats.Swap > 0 {
			agentInfo.SwapSize = uint64(combined.Stats.Swap * 1024 * 1024 * 1024)
		}
	}

	// 2. Map Metrics
	disks := []agentparser.DiskTelemetry{
		{
			Path:       "/",
			UsedBytes:  uint64(combined.Stats.DiskUsed * 1024 * 1024 * 1024),
			TotalBytes: uint64(combined.Stats.DiskTotal * 1024 * 1024 * 1024),
			ReadBytes:  combined.Stats.DiskIO[0],
			WriteBytes: combined.Stats.DiskIO[1],
		},
	}

	/*
		// Add Extra Disks if present
		for path, fs := range combined.Stats.ExtraFs {
			disks = append(disks, agentparser.DiskTelemetry{
				Path:       path,
				UsedBytes:  uint64(fs.Used * 1024 * 1024 * 1024),
				TotalBytes: uint64(fs.Total * 1024 * 1024 * 1024),
			})
		}
	*/

	// We extract the cumulative interface counters directly from the agent.
	// We specifically pick the single interface with the most cumulative traffic
	// Reference: sumAndTrackPerNicDeltas in https://github.com/henrygd/beszel/blob/main/agent/network.go
	var maxTX uint64
	var totalTX, totalRX float64
	for _, ni := range combined.Stats.NetworkInterfaces {
		if ni[2] > maxTX {
			maxTX = ni[2]
			totalTX = float64(ni[2])
			totalRX = float64(ni[3])
		}
	}

	// Fallback to Bandwidth if per-interface stats are missing (will result in dashboard '0 B/s' bug due to being a rate)
	if totalTX == 0 && totalRX == 0 {
		totalTX = float64(combined.Stats.Bandwidth[0])
		totalRX = float64(combined.Stats.Bandwidth[1])
	}

	telemetry := agentparser.Telemetry{
		Timestamp:        time.Now(),
		CPUUsagePercent:  combined.Stats.Cpu,
		RAMUsedBytes:     uint64(combined.Stats.MemUsed * 1024 * 1024 * 1024),
		RAMSwapUsedBytes: uint64(combined.Stats.SwapUsed * 1024 * 1024 * 1024),
		TXBytes:          totalTX,
		RXBytes:          totalRX,
		Disks:            disks,
		NetworkIOType:    agentparser.IOCumulative, // Beszel sends cumulative byte counters for network
		DiskIOType:       agentparser.IORate,       // Beszel sends B/s for disk I/O
	}

	// https://github.com/henrygd/beszel/blob/main/agent/system.go#L144
	if len(combined.Stats.CpuBreakdown) >= 4 {
		telemetry.CPUIOWaitPercent = combined.Stats.CpuBreakdown[2]
		telemetry.CPUStealPercent = combined.Stats.CpuBreakdown[3]
	}

	return &agentparser.ParsedData{
		AgentInfo: agentInfo,
		Metrics:   []agentparser.Telemetry{telemetry},
	}, nil
}
