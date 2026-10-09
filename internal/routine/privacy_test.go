package routine

import (
	parser "certainstats/internal/agent_parser"
	"certainstats/internal/metrics"
	"testing"
)

func TestNestedDiskPrivacy(t *testing.T) {
	r := &Routine{}
	snap := &metrics.AgentSnapshot{DiskUsedBytes: 10, DiskTotalBytes: 100, Disks: []parser.DiskTelemetry{{Path: "/private", UsedBytes: 10, TotalBytes: 100, ReadBytes: 99, WriteBytes: 999}}}
	out := r.filterSnapshot(snap, map[string]struct{}{"agent_disk_used": {}})
	disks := out["Disks"].([]map[string]any)
	if _, ok := disks[0]["read_bytes"]; ok {
		t.Fatal("read bytes leaked")
	}
	if _, ok := disks[0]["write_bytes"]; ok {
		t.Fatal("write bytes leaked")
	}
	if _, ok := disks[0]["total_bytes"]; ok {
		t.Fatal("capacity leaked")
	}
}
