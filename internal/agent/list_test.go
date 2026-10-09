package agent

import (
	"certainstats/internal/agent_parser/registry"
	"certainstats/internal/agentmeta"
	appctx "certainstats/internal/context"
	"certainstats/internal/store"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"
)

type listAgentStore struct {
	store.AgentStore
	agent store.Agent
}

func (s listAgentStore) AgentList(_ context.Context, userID string) ([]store.Agent, error) {
	if userID != "owner" {
		panic("unexpected owner")
	}
	return []store.Agent{s.agent}, nil
}

func TestListAgentsRuntimeExtension(t *testing.T) {
	observed := time.Date(2026, 10, 9, 16, 30, 55, 0, time.UTC)
	for _, tc := range []struct {
		name, provider, source, capability string
		version, protocol                  *string
		observed                           *time.Time
	}{
		{"beszel", "beszel", "beszel_info", "supported", agentmeta.String("0.21.0"), nil, &observed},
		{"unknown version", "beszel", "", "unknown", nil, nil, nil},
		{"protocol only", "ltstats", "ltstats_header", "unsupported", nil, agentmeta.String("7"), &observed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := store.Agent{AgentID: "node", AgentType: tc.provider, Nickname: "Node", IsOnline: true,
				LastSeen: &observed, Uptime: 123, LinuxVersion: "Linux", CpuModel: "CPU", CpuCores: 4,
				RamSize: 1024, SwapSize: 512, DiskSize: 4096, TotalRxBytes: 12, TotalTxBytes: 34, Note: "note",
				AgentVersion: tc.version, ProtocolVersion: tc.protocol, AgentVersionSource: tc.source, AgentVersionObservedAt: tc.observed}
			r := httptest.NewRequest(http.MethodGet, "/api/agents", nil)
			r = r.WithContext(context.WithValue(r.Context(), appctx.UserIDKey, "owner"))
			w := httptest.NewRecorder()
			ListAgentsHandler(listAgentStore{agent: a}, nil, registry.NewRegistry())(w, r)
			if w.Code != http.StatusOK {
				t.Fatalf("status %d: %s", w.Code, w.Body.String())
			}
			var rows []map[string]any
			if err := json.Unmarshal(w.Body.Bytes(), &rows); err != nil {
				t.Fatal(err)
			}
			if len(rows) != 1 {
				t.Fatalf("unexpected rows: %v", rows)
			}
			row := rows[0]
			extensions, ok := row["extensions"].(map[string]any)
			if !ok || len(extensions) != 1 {
				t.Fatalf("unexpected extensions: %v", row["extensions"])
			}
			runtime, ok := extensions["runtime"].(map[string]any)
			if !ok {
				t.Fatalf("missing runtime: %v", extensions)
			}
			for key, want := range map[string]*string{"agent_version": tc.version, "protocol_version": tc.protocol} {
				value, exists := runtime[key]
				if !exists || (want == nil && value != nil) || (want != nil && value != *want) {
					t.Errorf("%s = %v, want %v", key, value, want)
				}
			}
			if tc.source == "" {
				if _, exists := runtime["agent_version_source"]; exists {
					t.Error("missing source should be omitted")
				}
			} else if runtime["agent_version_source"] != tc.source {
				t.Errorf("unexpected source: %v", runtime)
			}
			if tc.observed == nil {
				if _, exists := runtime["agent_version_observed_at"]; exists {
					t.Error("missing observation should be omitted")
				}
			} else if runtime["agent_version_observed_at"] != observed.Format(time.RFC3339) {
				t.Errorf("unexpected observation: %v", runtime)
			}
			caps, ok := runtime["capabilities"].(map[string]any)
			if !ok {
				t.Fatalf("missing capabilities: %v", runtime)
			}
			configure, ok := caps["network_monitor.configure"].(map[string]any)
			if !ok || configure["state"] != tc.capability {
				t.Errorf("unexpected capability: %v", configure)
			}
			delete(row, "extensions")
			wantOverview := map[string]any{
				"agent_id": "node", "agent_type": tc.provider, "nickname": "Node", "is_online": true,
				"last_seen": observed.Format(time.RFC3339), "uptime": float64(123), "linux_version": "Linux",
				"cpu_model": "CPU", "cpu_cores": float64(4), "ram_size": float64(1024), "swap_size": float64(512),
				"disk_size": float64(4096), "note": "note", "disks": []any{},
				"net": map[string]any{"total_rx_bytes": float64(12), "total_tx_bytes": float64(34)},
			}
			if !reflect.DeepEqual(row, wantOverview) {
				t.Errorf("overview changed or contains metadata: got %v, want %v", row, wantOverview)
			}
		})
	}
}
