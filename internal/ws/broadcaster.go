package ws

import (
	"certainstats/internal/ws/browserpb"
	"sync"

	"golang.org/x/net/websocket"
	"google.golang.org/protobuf/proto"
)

// AgentBroadcaster manages WebSocket connections from browsers (Admin and Public)
type AgentBroadcaster struct {
	protobuf       bool
	mu             sync.RWMutex
	sessionClients map[string]map[*websocket.Conn]bool
	writers        map[*websocket.Conn]*browserWriter
	userClients    map[string]map[*websocket.Conn]bool // userID -> connections
	dashClients    map[string]map[*websocket.Conn]bool // dashID -> connections
}

func NewAgentBroadcaster(protobuf bool) *AgentBroadcaster {
	return &AgentBroadcaster{
		protobuf:       protobuf,
		writers:        make(map[*websocket.Conn]*browserWriter),
		userClients:    make(map[string]map[*websocket.Conn]bool),
		sessionClients: make(map[string]map[*websocket.Conn]bool),
		dashClients:    make(map[string]map[*websocket.Conn]bool),
	}
}

// SubscribeUser adds a browser connection for a specific user
func (b *AgentBroadcaster) SubscribeUser(userID string, conn *websocket.Conn) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if _, exists := b.userClients[userID]; !exists {
		b.userClients[userID] = make(map[*websocket.Conn]bool)
	}
	b.userClients[userID][conn] = true
	b.startWriter(conn)
}

// UnsubscribeUser removes a user connection
func (b *AgentBroadcaster) UnsubscribeUser(userID string, conn *websocket.Conn) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if clients, exists := b.userClients[userID]; exists {
		delete(clients, conn)
		b.stopWriter(conn)
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
	b.startWriter(conn)
}

// UnsubscribeDash removes a public dashboard connection
func (b *AgentBroadcaster) UnsubscribeDash(dashID string, conn *websocket.Conn) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if clients, exists := b.dashClients[dashID]; exists {
		delete(clients, conn)
		b.stopWriter(conn)
		if len(clients) == 0 {
			delete(b.dashClients, dashID)
		}
	}
}

// BroadcastToUser sends an update to an authenticted user
func (b *AgentBroadcaster) BroadcastToUser(userID string, update *browserpb.TelemetryEnvelope) {
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

	b.sendTo(targets, update, false)
}

// BroadcastToDash sends an update to a public dashboard
func (b *AgentBroadcaster) BroadcastToDash(dashID string, update *browserpb.TelemetryEnvelope) {
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

	b.sendTo(targets, update, true)
}

func (b *AgentBroadcaster) sendTo(targets []*websocket.Conn, update *browserpb.TelemetryEnvelope, public bool) {
	if public && update.GetPulse().GetNetwork() != nil {
		update = proto.Clone(update).(*browserpb.TelemetryEnvelope)
		update.Pulse.Network = nil
	}
	var payload []byte
	var err error
	if b.protobuf {
		payload, err = proto.Marshal(update)
	} else {
		payload, err = marshalLegacyJSON(update, public)
	}
	if err != nil {
		return
	}

	for _, conn := range targets {
		b.mu.RLock()
		writer := b.writers[conn]
		b.mu.RUnlock()
		if writer == nil {
			continue
		}
		select {
		case writer.queue <- payload:
		default:
			select {
			case <-writer.queue:
			default:
			}
			select {
			case writer.queue <- payload:
			default:
			}
		}
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

func (b *AgentBroadcaster) CloseDash(id string) {
	b.mu.Lock()
	targets := b.dashClients[id]
	delete(b.dashClients, id)
	b.mu.Unlock()
	for c := range targets {
		c.Close()
	}
}

func (b *AgentBroadcaster) CloseAll() {
	b.mu.Lock()
	var targets []*websocket.Conn
	for _, clients := range b.userClients {
		for c := range clients {
			targets = append(targets, c)
		}
	}
	for _, clients := range b.dashClients {
		for c := range clients {
			targets = append(targets, c)
		}
	}
	b.mu.Unlock()
	for _, c := range targets {
		c.Close()
	}
}

func (b *AgentBroadcaster) SubscribeSession(token string, conn *websocket.Conn) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.sessionClients[token] == nil {
		b.sessionClients[token] = make(map[*websocket.Conn]bool)
	}
	b.sessionClients[token][conn] = true
}
func (b *AgentBroadcaster) UnsubscribeSession(token string, conn *websocket.Conn) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.sessionClients[token], conn)
	if len(b.sessionClients[token]) == 0 {
		delete(b.sessionClients, token)
	}
}
func (b *AgentBroadcaster) CloseSession(token string) {
	b.mu.Lock()
	clients := b.sessionClients[token]
	delete(b.sessionClients, token)
	b.mu.Unlock()
	for conn := range clients {
		conn.Close()
	}
}
