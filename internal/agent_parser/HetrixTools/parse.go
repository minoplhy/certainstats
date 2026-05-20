package hetrixtools

import (
	"bytes"
	agentparser "certainstats/internal/agent_parser"
	"compress/gzip"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"strconv"
	"strings"
	"time"
)

func (h *HTStats) AgentType() string {
	return "hetrixtools"
}

func (h *HTStats) ParseToken(input any) (string, error) {
	data, ok := input.([]byte)
	if !ok {
		return "", fmt.Errorf("invalid input type for HetrixTools token parsing")
	}
	jsonBytes, err := h.decode(data)
	if err != nil {
		return "", err
	}

	var JSONdata JSONdata
	if err := json.Unmarshal(jsonBytes, &JSONdata); err != nil {
		return "", fmt.Errorf("unmarshal failed: %w", err)
	}

	return JSONdata.Token, nil
}

func (h *HTStats) decode(data []byte) ([]byte, error) {
	decodedURL, err := url.QueryUnescape(string(data))
	if err != nil {
		return nil, fmt.Errorf("url decode failed: %w", err)
	}

	// HetrixTools sends data as j=<base64_gzip>
	decodedURL = strings.TrimPrefix(decodedURL, "j=")

	decodedURL = strings.TrimSpace(decodedURL)
	gzipData, err := base64.StdEncoding.DecodeString(decodedURL)
	if err != nil {
		return nil, fmt.Errorf("base64 decode failed: %w", err)
	}

	gzReader, err := gzip.NewReader(bytes.NewReader(gzipData))
	if err != nil {
		return nil, fmt.Errorf("gzip reader failed: %w", err)
	}
	defer gzReader.Close()

	return io.ReadAll(gzReader)
}

func (h *HTStats) Parse(data []byte) (*agentparser.ParsedData, error) {
	jsonBytes, err := h.decode(data)
	if err != nil {
		return nil, err
	}

	var JSONdata JSONdata
	if err := json.Unmarshal(jsonBytes, &JSONdata); err != nil {
		return nil, fmt.Errorf("unmarshal failed: %w", err)
	}

	var totalDiskSize uint64
	var totalDiskUsed uint64
	decodedDisks, err := base64.StdEncoding.DecodeString(JSONdata.Disks)
	if err != nil {
		return nil, fmt.Errorf("disks base64 decode failed: %w", err)

	}

	// Format: mountpoint,type,total,used,available;mountpoint,type,total,used,available
	diskParts := strings.Split(string(decodedDisks), ";")

	for _, part := range diskParts {
		fields := strings.Split(part, ",")
		if len(fields) >= 5 {
			size, _ := strconv.ParseUint(fields[2], 10, 64)
			used, _ := strconv.ParseUint(fields[3], 10, 64)
			totalDiskSize += size
			totalDiskUsed += used
		}
	}

	var totalRX float64
	var totalTX float64
	decodedNICS, err := base64.StdEncoding.DecodeString(JSONdata.NICS)
	if err != nil {
		return nil, fmt.Errorf("nics base64 decode failed: %w", err)
	}

	// Format: iface,rx,tx;iface,rx,tx
	nicParts := strings.Split(string(decodedNICS), ";")
	for _, part := range nicParts {
		fields := strings.Split(part, ",")
		if len(fields) >= 3 {
			rx, _ := strconv.ParseFloat(fields[1], 64)
			tx, _ := strconv.ParseFloat(fields[2], 64)
			totalRX += rx
			totalTX += tx
		}
	}

	uptime, _ := strconv.ParseUint(JSONdata.Uptime, 10, 32)
	cpuCores, _ := strconv.ParseUint(JSONdata.CPUCores, 10, 16)
	ramSize, _ := strconv.ParseUint(JSONdata.RAMSize, 10, 64)
	swapSize, _ := strconv.ParseUint(JSONdata.RAMSwapSize, 10, 64)

	kernel, err := base64.StdEncoding.DecodeString(JSONdata.Kernel)
	if err != nil {
		return nil, fmt.Errorf("kernel base64 decode failed: %w", err)
	}

	cpuModel, err := base64.StdEncoding.DecodeString(JSONdata.CPUModel)
	if err != nil {
		return nil, fmt.Errorf("cpu model base64 decode failed: %w", err)
	}

	agentInfo := &agentparser.ParsedMetadata{
		Uptime:       uint32(uptime),
		LinuxVersion: string(kernel),
		CpuModel:     string(cpuModel),
		CpuCores:     uint16(cpuCores),
		RamSize:      ramSize * 1024,
		SwapSize:     swapSize * 1024,
		DiskSize:     totalDiskSize,
	}

	cpuUsage, _ := strconv.ParseFloat(JSONdata.CPU, 64)
	cpuWa, _ := strconv.ParseFloat(JSONdata.CPUwa, 64)
	cpuSt, _ := strconv.ParseFloat(JSONdata.CPUst, 64)
	ramUsage, _ := strconv.ParseFloat(JSONdata.RAM, 64)
	ramSwapUsage, _ := strconv.ParseFloat(JSONdata.RAMSwap, 64)

	disks := []agentparser.DiskTelemetry{}
	//	for _, part := range diskParts {
	if len(diskParts) > 0 {
		part := diskParts[0] // Only take the first disk reported
		fields := strings.Split(part, ",")
		if len(fields) >= 5 {
			size, _ := strconv.ParseUint(fields[2], 10, 64)
			used, _ := strconv.ParseUint(fields[3], 10, 64)
			path := fields[0]
			if path == "" {
				path = "/"
			}

			var readBps, writeBps uint64
			decodedIOPS, _ := base64.StdEncoding.DecodeString(JSONdata.IOPS)
			iopsParts := strings.Split(string(decodedIOPS), ";")
			if len(iopsParts) > 0 {
				fields := strings.Split(iopsParts[0], ",")
				if len(fields) >= 3 {
					rb, _ := strconv.ParseUint(fields[1], 10, 64)
					wb, _ := strconv.ParseUint(fields[2], 10, 64)
					readBps = rb
					writeBps = wb
				}
			}

			disks = append(disks, agentparser.DiskTelemetry{
				Path:       path,
				UsedBytes:  used,
				TotalBytes: size,
				ReadBytes:  readBps,
				WriteBytes: writeBps,
			})
		}
	}

	ramUsed := uint64((float64(ramSize*1024) * ramUsage) / 100.0)
	swapUsed := uint64((float64(swapSize*1024) * ramSwapUsage) / 100.0)

	stats := []agentparser.Telemetry{
		{
			Timestamp:        time.Now(),
			CPUUsagePercent:  cpuUsage,
			CPUIOWaitPercent: cpuWa,
			CPUStealPercent:  cpuSt,
			RAMUsedBytes:     ramUsed,
			RAMSwapUsedBytes: swapUsed,
			RXBytes:          totalRX,
			TXBytes:          totalTX,
			Disks:            disks,
			NetworkIOType:    agentparser.IORate, // HetrixTools divides by TIMEDIFF → B/s
			DiskIOType:       agentparser.IORate, // HetrixTools divides by tTIMEDIFF → B/s
		},
	}

	return &agentparser.ParsedData{
		AgentInfo: agentInfo,
		Metrics:   stats,
	}, nil
}
