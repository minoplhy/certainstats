package ws

import (
	log "certainstats/internal/logger"
	"sync"
)

// Manager tracks all active agent connections.
type Manager struct {
	mu     sync.RWMutex
	agents map[string]*Hub
}

// NewManager creates a new connection manager.
func NewManager() *Manager {
	return &Manager{
		agents: make(map[string]*Hub),
	}
}

// Register adds a new agent connection to the manager.
func (m *Manager) Register(token string, hub *Hub) {
	m.mu.Lock()
	defer m.mu.Unlock()
	
	// If there's an existing connection for this token, close it
	if oldHub, exists := m.agents[token]; exists {
		log.Debugf("[WS] Closing redundant connection for agent %s", token)
		oldHub.Close()
	}
	
	m.agents[token] = hub
	log.Debugf("[WS] Agent %s registered. Total active: %d", token, len(m.agents))
}

// Unregister removes an agent connection.
func (m *Manager) Unregister(token string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	
	delete(m.agents, token)
	log.Debugf("[WS] Agent %s unregistered. Total active: %d", token, len(m.agents))
}

// GetHub retrieves a hub for a specific agent token.
func (m *Manager) GetHub(token string) (*Hub, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	
	hub, exists := m.agents[token]
	return hub, exists
}

// Broadcast sends a request to all connected agents.
func (m *Manager) Broadcast(req any) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	
	for token, hub := range m.agents {
		if err := hub.Send(req); err != nil {
			log.Printf("[WS] Failed to send broadcast to %s: %v", token, err)
		}
	}
}

// Range iterates over all connected agents and calls the provided function.
func (m *Manager) Range(fn func(token string, hub *Hub)) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	
	for token, hub := range m.agents {
		fn(token, hub)
	}
}
