package metrics

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	ctxpkg "certainstats/internal/context"
	"certainstats/internal/store"
)

// mockAgentStore implements store.AgentStore for handler tests.
type mockAgentStore struct {
	store.AgentStore
	agents map[string]*store.Agent // key: agentID + "_" + userID
}

func newMockAgentStore() *mockAgentStore {
	return &mockAgentStore{agents: make(map[string]*store.Agent)}
}

func (m *mockAgentStore) addAgent(agentID, userID, name string) {
	m.agents[agentID+"_"+userID] = &store.Agent{
		AgentID:  agentID,
		UserID:   userID,
		Nickname: name,
	}
}

func (m *mockAgentStore) AgentGetByID(ctx context.Context, agentID, userID string) (*store.Agent, error) {
	if ag, ok := m.agents[agentID+"_"+userID]; ok {
		return ag, nil
	}
	return nil, fmt.Errorf("not found")
}
func (m *mockAgentStore) AgentIncrementTraffic(ctx context.Context, agentID, userID string, rx, tx uint64, disks []store.DiskDelta) error {
	return nil
}
func (m *mockAgentStore) AgentList(ctx context.Context, userID string) ([]store.Agent, error) {
	return nil, nil
}
func (m *mockAgentStore) AgentListManagement(ctx context.Context, userID string) ([]store.AgentManagement, error) {
	return nil, nil
}
func (m *mockAgentStore) AgentResetToken(ctx context.Context, agentID, userID, token string) error {
	return nil
}
func (m *mockAgentStore) AgentResetSSHKey(ctx context.Context, agentID, userID, key string) error {
	return nil
}
func (m *mockAgentStore) AgentGetSSHKey(ctx context.Context, agentID, userID string) (string, error) {
	return "", nil
}

func TestMetricsQueryHandler_PlannedSecrecy(t *testing.T) {
	mockDB := newMockAgentStore()
	mockDB.addAgent("ag_owner_1", "usr_alice", "Alice Server")
	mockDB.addAgent("ag_owner_2", "usr_bob", "Bob Server")

	cache := NewRealtimeCache()
	// Populate some live memory cache samples for Alice's agent covering the requested 1 hour window
	now := time.Now().UnixMilli()
	cache.appendPoint("usr_alice\tag_owner_1\tagent_cpu_usage", now-2*3600*1000, 20.0)
	cache.appendPoint("usr_alice\tag_owner_1\tagent_cpu_usage", now-10000, 25.5)
	cache.appendPoint("usr_alice\tag_owner_1\tagent_cpu_usage", now, 30.0)

	handler := MetricsQueryHandler(mockDB, nil, cache)

	t.Run("authorized user with valid agent and metric returns 200", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/metrics?agent_id=ag_owner_1&metric=agent_cpu_usage&hours=1", nil)
		req = req.WithContext(context.WithValue(req.Context(), ctxpkg.UserIDKey, "usr_alice"))
		w := httptest.NewRecorder()

		handler(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d: %s", w.Code, w.Body.String())
		}
		var res map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
			t.Fatalf("failed to decode json: %v", err)
		}
		if res["metric"] != "agent_cpu_usage" {
			t.Errorf("unexpected metric name: %v", res["metric"])
		}
	})

	t.Run("unauthorized user querying another user's agent returns 404 (planned secrecy)", func(t *testing.T) {
		// Bob tries to query Alice's agent
		req := httptest.NewRequest("GET", "/api/metrics?agent_id=ag_owner_1&metric=agent_cpu_usage&hours=1", nil)
		req = req.WithContext(context.WithValue(req.Context(), ctxpkg.UserIDKey, "usr_bob"))
		w := httptest.NewRecorder()

		handler(w, req)

		if w.Code != http.StatusNotFound {
			t.Fatalf("expected 404 Not Found for unauthorized agent query, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("querying nonexistent agent returns 404 (planned secrecy)", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/metrics?agent_id=ag_nonexistent&metric=agent_cpu_usage&hours=1", nil)
		req = req.WithContext(context.WithValue(req.Context(), ctxpkg.UserIDKey, "usr_alice"))
		w := httptest.NewRecorder()

		handler(w, req)

		if w.Code != http.StatusNotFound {
			t.Fatalf("expected 404 Not Found for nonexistent agent, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("authorized query with empty series data returns 404", func(t *testing.T) {
		// Bob's agent has no recorded points in cache/tsdb
		req := httptest.NewRequest("GET", "/api/metrics?agent_id=ag_owner_2&metric=agent_cpu_usage&hours=1", nil)
		req = req.WithContext(context.WithValue(req.Context(), ctxpkg.UserIDKey, "usr_bob"))
		w := httptest.NewRecorder()

		handler(w, req)

		if w.Code != http.StatusNotFound {
			t.Fatalf("expected 404 Not Found for empty metric series, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("missing required parameters returns 400", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/metrics?agent_id=&metric=", nil)
		req = req.WithContext(context.WithValue(req.Context(), ctxpkg.UserIDKey, "usr_alice"))
		w := httptest.NewRecorder()

		handler(w, req)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 Bad Request for missing params, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("unknown metric returns 400", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/metrics?agent_id=ag_owner_1&metric=malicious_metric", nil)
		req = req.WithContext(context.WithValue(req.Context(), ctxpkg.UserIDKey, "usr_alice"))
		w := httptest.NewRecorder()

		handler(w, req)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 Bad Request for unknown metric, got %d: %s", w.Code, w.Body.String())
		}
	})
}
