package ltstats

import (
	"bytes"
	agentparser "certainstats/internal/agent_parser"
	"errors"
	"fmt"
	"time"
)

// AgentType returns the identifier for this parser.
func (l *LTstats) AgentType() string {
	return "ltstats"
}

func (l *LTstats) ParseToken(input any) (string, error) {
	rawPayload, ok := input.([]byte)
	if !ok {
		return "", fmt.Errorf("invalid input type for LTstats token parsing")
	}
	size := 33
	offset := 0

	if len(rawPayload) < offset+size {
		return "", fmt.Errorf("data too short for token: have %d bytes, need %d", len(rawPayload)-offset, size)
	}

	return string(bytes.TrimRight(rawPayload[offset:offset+size], "\x00")), nil
}

// Parse decodes a raw binary payload from an LTstats agent into ParsedData.
//
// It extracts the auth token, optional hardware details, and the most recent
// telemetry snapshot. The caller is responsible for authenticating the token
// and resolving the full agent identity from the store.
func (l *LTstats) Parse(data []byte) (*agentparser.ParsedData, error) {
	offset := 0

	// 1. Extract Header
	if len(data) < NetHeaderSize {
		return nil, errors.New("payload too short for header")
	}
	var headerBytes [NetHeaderSize]byte
	copy(headerBytes[:], data[offset:offset+NetHeaderSize])
	header, err := NetHeaderExtract(headerBytes)
	if err != nil {
		return nil, err
	}
	offset += NetHeaderSize

	// 2. Validate Size
	expectedSize := NetHeaderSize + (int(header.StatsCount) * StatsTSize)
	if header.IncludesDetails() {
		expectedSize += DetailsSize
	}
	if len(data) != expectedSize {
		return nil, fmt.Errorf("payload size mismatch: expected %d, got %d", expectedSize, len(data))
	}

	// 3. Extract Details (optional)
	var details *Details
	if header.IncludesDetails() {
		var detailsBytes [DetailsSize]byte
		copy(detailsBytes[:], data[offset:offset+DetailsSize])
		d, err := DetailsExtract(detailsBytes)
		if err != nil {
			return nil, err
		}
		details = &d
		offset += DetailsSize
	}

	var agentInfo *agentparser.ParsedMetadata
	if details != nil {
		agentInfo = &agentparser.ParsedMetadata{
			Uptime:       details.Uptime,
			LinuxVersion: string(bytes.TrimRight(details.LinuxVersion[:], "\x00")),
			CpuModel:     string(bytes.TrimRight(details.CpuModel[:], "\x00")),
			CpuCores:     details.CpuCores,
			RamSize:      details.RamSize,
			SwapSize:     details.SwapSize,
			DiskSize:     details.DiskSize,
		}
	}

	// 4. Extract Stats
	stats := make([]agentparser.Telemetry, 0, header.StatsCount)
	for i := 0; i < int(header.StatsCount); i++ {
		var statBytes [StatsTSize]byte
		copy(statBytes[:], data[offset:offset+StatsTSize])
		s, err := StatTExtract(statBytes)
		if err != nil {
			return nil, err
		}
		ramPct := MergeDecimal(s.RamUsageBeforeDec, s.RamUsageAfterDec)
		swapPct := MergeDecimal(s.SwapUsageBeforeDec, s.SwapUsageAfterDec)
		diskPct := MergeDecimal(s.DiskUsageBeforeDec, s.DiskUsageAfterDec)

		var ramUsed, swapUsed, diskUsed uint64
		if details != nil {
			ramUsed = uint64((float64(details.RamSize) * ramPct) / 100.0)
			swapUsed = uint64((float64(details.SwapSize) * swapPct) / 100.0)
			diskUsed = uint64((float64(details.DiskSize) * diskPct) / 100.0)
		}

		stats = append(stats, agentparser.Telemetry{
			Timestamp:        time.UnixMilli(int64(s.Time) * 1000),
			CPUUsagePercent:  MergeDecimal(s.CpuUsageBeforeDec, s.CpuUsageAfterDec),
			CPUIOWaitPercent: MergeDecimal(s.CpuIoWaitBeforeDec, s.CpuIoWaitAfterDec),
			CPUStealPercent:  MergeDecimal(s.CpuStealBeforeDec, s.CpuStealAfterDec),
			RAMUsedBytes:     ramUsed,
			RAMSwapUsedBytes: swapUsed,
			RXBytes:          float64(s.RxBytes),
			TXBytes:          float64(s.TxBytes),
			Disks: []agentparser.DiskTelemetry{
				{
					Path:       "/",
					UsedBytes:  diskUsed,
					TotalBytes: details.DiskSize,
					ReadBytes:  s.ReadSectors * 512,
					WriteBytes: s.WrittenSectors * 512,
				},
			},
		})
		offset += StatsTSize
	}

	return &agentparser.ParsedData{
		AgentInfo: agentInfo,
		Metrics:   stats,
	}, nil
}
