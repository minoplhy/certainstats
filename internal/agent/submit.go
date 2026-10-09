package agent

import (
	agentparser "certainstats/internal/agent_parser"
	"certainstats/internal/agent_parser/registry"
	"certainstats/internal/metrics"
	apiresponse "certainstats/internal/response"

	"certainstats/internal/store"
	"io"

	"context"
	"log"
	"math"
	"net/http"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/tsdb"
)

func SubmitHandler(agents store.AgentStore, tdb *tsdb.DB, parserRegistry *registry.Registry, cache *metrics.RealtimeCache) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// 1. Read Payload (Max 16KB)
		data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 16384))
		if err != nil {
			apiresponse.Error(w, http.StatusBadRequest, "payload too large or read error")
			return
		}

		agentType, token, err := parserRegistry.Detect(data)
		if err != nil {
			log.Printf("detection failed: %v", err)
			apiresponse.Error(w, http.StatusBadRequest, "invalid payload or token")
			return
		}

		identity, err := agents.AgentGetByToken(r.Context(), token)
		if err != nil {
			apiresponse.Error(w, http.StatusUnauthorized, "unauthorized")
			return
		}

		parsedData, err := parserRegistry.ParsePayload(agentType, data)
		if err != nil {
			log.Printf("invalid payload from %s: %s", agentType, err)
			apiresponse.Error(w, http.StatusBadRequest, "invalid payload")
			return
		}

		if identity.AgentType != "" && identity.AgentType != agentType {
			apiresponse.Error(w, 400, "Agent type does not match provisioned integration")
			return
		}

		// 4. Disk size is the total across all partitions (matches the summed usage in the live snapshot)
		if parsedData.AgentInfo != nil && len(parsedData.Metrics) > 0 {
			var totalDiskSize uint64
			for _, d := range parsedData.Metrics[0].Disks {
				totalDiskSize += d.TotalBytes
			}
			if totalDiskSize > 0 {
				parsedData.AgentInfo.DiskSize = totalDiskSize
			}
		}

		if err := Ingest(r.Context(), agents, tdb, cache, identity, parsedData); err != nil {
			log.Printf("ingestion failed for %s: %v", identity.AgentID, err)
			apiresponse.Error(w, http.StatusInternalServerError, "Ingestion failed; retry")
			return
		}

		w.WriteHeader(http.StatusOK)
		w.Write([]byte("1"))
	}
}

func WriteStatsToTSDB(ctx context.Context, tdb *tsdb.DB, identity *store.AgentIdentity, metrics []agentparser.Telemetry) error {
	app := tdb.Appender(ctx)

	lbl := func(name string) labels.Labels {
		return labels.FromStrings(
			"__name__", name,
			"user_id", identity.UserID,
			"agent_id", identity.AgentID,
		)
	}

	defer app.Rollback()
	var missing map[string]bool
	appendValue := func(l labels.Labels, t int64, v float64) error {
		if missing[l.Get("__name__")] {
			v = math.NaN()
		}
		_, err := app.Append(0, l, t, v)
		return err
	}
	for _, s := range metrics {
		missing = s.Missing
		tMs := s.Timestamp.UnixMilli()
		if err := appendValue(lbl("agent_sample_interval_seconds"), tMs, s.IntervalSeconds); err != nil {
			return err
		}

		if err := appendValue(lbl("agent_cpu_usage"), tMs, s.CPUUsagePercent); err != nil {
			return err
		}
		if err := appendValue(lbl("agent_cpu_iowait"), tMs, s.CPUIOWaitPercent); err != nil {
			return err
		}
		if err := appendValue(lbl("agent_cpu_steal"), tMs, s.CPUStealPercent); err != nil {
			return err
		}
		if err := appendValue(lbl("agent_ram_used"), tMs, float64(s.RAMUsedBytes)); err != nil {
			return err
		}
		if err := appendValue(lbl("agent_swap_used"), tMs, float64(s.RAMSwapUsedBytes)); err != nil {
			return err
		}

		// Multi-disk Support
		for _, disk := range s.Disks {
			diskLabels := labels.FromStrings(
				"__name__", "agent_disk_used",
				"user_id", identity.UserID,
				"agent_id", identity.AgentID,
				"path", disk.Path,
			)
			if err := appendValue(diskLabels, tMs, float64(disk.UsedBytes)); err != nil {
				return err
			}

			diskPctLabels := labels.FromStrings(
				"__name__", "agent_disk_usage",
				"user_id", identity.UserID,
				"agent_id", identity.AgentID,
				"path", disk.Path,
			)
			usagePct := math.NaN()
			if disk.TotalBytes > 0 {
				usagePct = (float64(disk.UsedBytes) / float64(disk.TotalBytes)) * 100.0
			}
			if err := appendValue(diskPctLabels, tMs, usagePct); err != nil {
				return err
			}

			// Disk Activity (Read/Write)
			readLabels := labels.FromStrings(
				"__name__", "agent_disk_read_bytes",
				"user_id", identity.UserID,
				"agent_id", identity.AgentID,
				"path", disk.Path,
			)
			if err := appendValue(readLabels, tMs, float64(disk.ReadBytes)); err != nil {
				return err
			}

			writeLabels := labels.FromStrings(
				"__name__", "agent_disk_write_bytes",
				"user_id", identity.UserID,
				"agent_id", identity.AgentID,
				"path", disk.Path,
			)
			if err := appendValue(writeLabels, tMs, float64(disk.WriteBytes)); err != nil {
				return err
			}
		}

		if !s.NetworkMissing {
			if err := appendValue(lbl("agent_rx_bytes"), tMs, s.RXBytes); err != nil {
				return err
			}
			if err := appendValue(lbl("agent_tx_bytes"), tMs, s.TXBytes); err != nil {
				return err
			}
		}
	}

	return app.Commit()
}
