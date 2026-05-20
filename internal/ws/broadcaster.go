package ws

import (
	"encoding/json"
	"sync"

	"golang.org/x/net/websocket"
)

// UIUpdate represents the packet sent to the frontend
type UIUpdate struct {
	Type string      `json:"type"`
	Data interface{} `json:"data"`
}

// AgentBroadcaster manages WebSocket connections from browsers (Admin and Public)
type AgentBroadcaster struct {
	mu          sync.RWMutex
	userClients map[string]map[*websocket.Conn]bool // userID -> connections
	dashClients map[string]map[*websocket.Conn]bool // dashID -> connections
}

func NewAgentBroadcaster() *AgentBroadcaster {
	return &AgentBroadcaster{
		userClients: make(map[string]map[*websocket.Conn]bool),
		dashClients: make(map[string]map[*websocket.Conn]bool),
	}
}

// SubscribeUser adds a browser connection for a specific user (Admin)
func (b *AgentBroadcaster) SubscribeUser(userID string, conn *websocket.Conn) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if _, exists := b.userClients[userID]; !exists {
		b.userClients[userID] = make(map[*websocket.Conn]bool)
	}
	b.userClients[userID][conn] = true
}

// UnsubscribeUser removes an admin connection
func (b *AgentBroadcaster) UnsubscribeUser(userID string, conn *websocket.Conn) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if clients, exists := b.userClients[userID]; exists {
		delete(clients, conn)
		if len(clients) == 0 {
			delete(b.userClients, userID)
		}
	}
}

// SubscribeDash adds a browser connection for a specific public dashboard
func (b *AgentBroadcaster) SubscribeDash(dashID string, conn *websocket.Conn) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if _, exists := b.dashClients[dashID]; !exists {
		b.dashClients[dashID] = make(map[*websocket.Conn]bool)
	}
	b.dashClients[dashID][conn] = true
}

// UnsubscribeDash removes a public dashboard connection
func (b *AgentBroadcaster) UnsubscribeDash(dashID string, conn *websocket.Conn) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if clients, exists := b.dashClients[dashID]; exists {
		delete(clients, conn)
		if len(clients) == 0 {
			delete(b.dashClients, dashID)
		}
	}
}

// BroadcastToUser sends an update to an admin user
func (b *AgentBroadcaster) BroadcastToUser(userID string, update UIUpdate) {
	b.mu.RLock()
	clients, exists := b.userClients[userID]
	if !exists {
		b.mu.RUnlock()
		return
	}

	var targets []*websocket.Conn
	for conn := range clients {
		targets = append(targets, conn)
	}
	b.mu.RUnlock()

	b.sendTo(targets, update)
}

// BroadcastToDash sends an update to a public dashboard
func (b *AgentBroadcaster) BroadcastToDash(dashID string, update UIUpdate) {
	b.mu.RLock()
	clients, exists := b.dashClients[dashID]
	if !exists {
		b.mu.RUnlock()
		return
	}

	var targets []*websocket.Conn
	for conn := range clients {
		targets = append(targets, conn)
	}
	b.mu.RUnlock()

	b.sendTo(targets, update)
}

func (b *AgentBroadcaster) sendTo(targets []*websocket.Conn, update UIUpdate) {
	payload, err := json.Marshal(update)
	if err != nil {
		return
	}

	for _, conn := range targets {
		_ = websocket.Message.Send(conn, string(payload))
	}
}

// GetActiveUserIDs returns all users currently viewing their dashboard
func (b *AgentBroadcaster) GetActiveUserIDs() []string {
	b.mu.RLock()
	defer b.mu.RUnlock()

	ids := make([]string, 0, len(b.userClients))
	for id := range b.userClients {
		ids = append(ids, id)
	}
	return ids
}

// GetActiveDashIDs returns all dashboards currently being viewed publicly
func (b *AgentBroadcaster) GetActiveDashIDs() []string {
	b.mu.RLock()
	defer b.mu.RUnlock()

	ids := make([]string, 0, len(b.dashClients))
	for id := range b.dashClients {
		ids = append(ids, id)
	}
	return ids
}
