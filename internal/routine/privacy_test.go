package routine

import (
	parser "certainstats/internal/agent_parser"
	"certainstats/internal/metrics"
	"certainstats/internal/ws"
	"testing"
)

func TestNestedDiskPrivacy(t *testing.T) {
	snap := &metrics.AgentSnapshot{DiskUsedBytes: 10, DiskTotalBytes: 100, Disks: []parser.DiskTelemetry{{Path: "/private", UsedBytes: 10, TotalBytes: 100, ReadBytes: 99, WriteBytes: 999}}}
	out := ws.BrowserSnapshot(snap, map[string]struct{}{"agent_disk_used": {}})
	disk := out.Disks.Items[0]
	if disk.ReadBytes != nil || disk.WriteBytes != nil || disk.TotalBytes != nil {
		t.Fatal("restricted nested disk values leaked")
	}
	if out.AgentId != nil || out.Metadata != nil || out.DiskTotalBytes != nil {
		t.Fatal("private identity or capacity leaked")
	}
}
