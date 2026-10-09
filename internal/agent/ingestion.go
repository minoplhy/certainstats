package agent

import (
	agentparser "certainstats/internal/agent_parser"
	"certainstats/internal/agentmeta"
	"certainstats/internal/metrics"
	"certainstats/internal/store"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/prometheus/prometheus/tsdb"
	"math"
	"time"
)

// Journal payload and lifetime updates commit together before TSDB. Recovery
// replays only the TSDB and metadata stages, never lifetime-counter increments.
func Ingest(ctx context.Context, agents store.AgentStore, tdb *tsdb.DB, cache *metrics.RealtimeCache, id *store.AgentIdentity, data *agentparser.ParsedData) error {
	unlock := lockAgent(id.AgentID)
	defer unlock()
	if data.Runtime != nil {
		if err := agentmeta.Validate(*data.Runtime); err != nil {
			return err
		}
	}
	for _, m := range data.Metrics {
		if !m.Timestamp.IsZero() && (m.Timestamp.UnixMilli() <= 0 || m.Timestamp.After(time.Now().Add(10*time.Minute))) {
			return fmt.Errorf("invalid sample timestamp")
		}
		for _, v := range []float64{m.RXBytes, m.TXBytes, m.CPUUsagePercent, m.CPUIOWaitPercent, m.CPUStealPercent, m.IntervalSeconds} {
			if math.IsNaN(v) || math.IsInf(v, 0) || v < 0 {
				return fmt.Errorf("invalid sample value")
			}
		}
	}
	journal, ok := agents.(store.IngestionJournal)
	if !ok {
		return fmt.Errorf("ingestion journal unavailable")
	}
	records, err := journal.IngestionPending(ctx)
	if err != nil {
		return err
	}
	for _, r := range records {
		if r.AgentID == id.AgentID {
			if err := replayIngestion(ctx, agents, journal, tdb, cache, r); err != nil {
				return err
			}
		}
	}
	stable := len(data.Metrics) > 0
	for _, m := range data.Metrics {
		if m.SourceID == "" {
			stable = false
		}
	}
	if stable {
		for _, sample := range data.Metrics {
			part := &agentparser.ParsedData{AgentInfo: data.AgentInfo, Runtime: data.Runtime, Metrics: []agentparser.Telemetry{sample}}
			if err := ingestRecord(ctx, agents, journal, tdb, cache, id, part); err != nil {
				return err
			}
		}
		return nil
	}
	return ingestRecord(ctx, agents, journal, tdb, cache, id, data)
}
func ingestRecord(ctx context.Context, agents store.AgentStore, journal store.IngestionJournal, tdb *tsdb.DB, cache *metrics.RealtimeCache, id *store.AgentIdentity, data *agentparser.ParsedData) error {
	raw, err := json.Marshal(data)
	if err != nil {
		return err
	}
	stable := len(data.Metrics) > 0
	for _, m := range data.Metrics {
		if m.SourceID == "" {
			stable = false
		}
	}
	if stable {
		raw = []byte(data.Metrics[0].SourceID)
	}
	hash := sha256.Sum256(append([]byte(id.AgentID+":"), raw...))
	batchID := hex.EncodeToString(hash[:])
	if !stable {
		nonce := make([]byte, 32)
		if _, err := rand.Read(nonce); err != nil {
			return err
		}
		batchID = hex.EncodeToString(nonce)
	}
	existing, err := journal.IngestionGet(ctx, batchID)
	if err == nil {
		if existing.Complete {
			return nil
		}
		return replayIngestion(ctx, agents, journal, tdb, cache, *existing)
	}
	if err != sql.ErrNoRows {
		return err
	}
	var before agentIOState
	actual, hadState := ioStates.Load(id.AgentID)
	if hadState {
		before = *actual.(*agentIOState)
	}
	durable := false
	defer func() {
		if !durable {
			if hadState {
				ioStates.Store(id.AgentID, &before)
			} else {
				ioStates.Delete(id.AgentID)
			}
		}
	}()
	normalizeIOMetrics(id.AgentID, data.Metrics)
	payload, err := json.Marshal(data)
	if err != nil {
		return err
	}
	var rx, tx uint64
	var disks []store.DiskDelta
	for _, m := range data.Metrics {
		if math.IsNaN(m.RXBytes) || math.IsInf(m.RXBytes, 0) || m.RXBytes < 0 || m.RXBytes >= float64(math.MaxInt64) || math.IsNaN(m.TXBytes) || math.IsInf(m.TXBytes, 0) || m.TXBytes < 0 || m.TXBytes >= float64(math.MaxInt64) {
			return fmt.Errorf("normalized traffic exceeds supported range")
		}
		if uint64(m.RXBytes) > math.MaxInt64-rx || uint64(m.TXBytes) > math.MaxInt64-tx {
			return fmt.Errorf("batch traffic exceeds supported range")
		}
		rx += uint64(m.RXBytes)
		tx += uint64(m.TXBytes)
		for _, d := range m.Disks {
			if d.TotalBytes > math.MaxInt64 || d.ReadBytes > math.MaxInt64 || d.WriteBytes > math.MaxInt64 {
				return fmt.Errorf("disk traffic exceeds supported range")
			}
			disks = append(disks, store.DiskDelta{Path: d.Path, TotalBytes: d.TotalBytes, ReadBytes: d.ReadBytes, WriteBytes: d.WriteBytes})
		}
	}
	record := store.IngestionRecord{ID: batchID, AgentID: id.AgentID, UserID: id.UserID, Payload: payload}
	if err := journal.IngestionBegin(ctx, record, rx, tx, disks); err != nil {
		return err
	}
	durable = true
	return replayIngestion(ctx, agents, journal, tdb, cache, record)
}
func replayIngestion(ctx context.Context, agents store.AgentStore, journal store.IngestionJournal, tdb *tsdb.DB, cache *metrics.RealtimeCache, record store.IngestionRecord) error {
	var data agentparser.ParsedData
	if err := json.Unmarshal(record.Payload, &data); err != nil {
		return err
	}
	id := &store.AgentIdentity{AgentID: record.AgentID, UserID: record.UserID}
	if len(data.Metrics) > 0 {
		if err := WriteStatsToTSDB(ctx, tdb, id, data.Metrics); err != nil {
			return err
		}
	}
	if data.Runtime != nil {
		if err := agents.AgentUpdateRuntime(ctx, id.AgentID, id.UserID, *data.Runtime); err != nil {
			return err
		}
	}
	if info := data.AgentInfo; info != nil {
		if err := agents.AgentUpsertDetails(ctx, store.Agent{AgentID: id.AgentID, UserID: id.UserID, Uptime: info.Uptime, LinuxVersion: info.LinuxVersion, CpuModel: info.CpuModel, CpuCores: info.CpuCores, RamSize: info.RamSize, SwapSize: info.SwapSize, DiskSize: info.DiskSize}); err != nil {
			return err
		}
	} else {
		if err := agents.AgentUpdateHeartbeat(ctx, id.AgentID, id.UserID); err != nil {
			return err
		}
	}
	if err := journal.IngestionComplete(ctx, record.ID); err != nil {
		return err
	}
	if cache != nil {
		cache.Update(id.UserID, id.AgentID, &data)
	}
	return nil
}
func RecoverIngestion(ctx context.Context, agents store.AgentStore, tdb *tsdb.DB, cache *metrics.RealtimeCache) error {
	journal, ok := agents.(store.IngestionJournal)
	if !ok {
		return fmt.Errorf("ingestion journal unavailable")
	}
	records, err := journal.IngestionPending(ctx)
	if err != nil {
		return err
	}
	for _, r := range records {
		unlock := lockAgent(r.AgentID)
		err := replayIngestion(ctx, agents, journal, tdb, cache, r)
		unlock()
		if err != nil {
			return err
		}
	}
	return nil
}
