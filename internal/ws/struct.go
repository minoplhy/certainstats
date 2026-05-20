package ws

import (
	"errors"
	log "certainstats/internal/logger"
	"sync"
	"time"

	"golang.org/x/net/websocket"

	"github.com/fxamacker/cbor/v2"
)

type Hub struct {
	conn *websocket.Conn
	mu   sync.Mutex
}

// Send sends a CBOR message to the hub
func (h *Hub) Send(v any) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.conn == nil {
		return errors.New("not connected")
	}
	data, err := cbor.Marshal(v)
	if err != nil {
		return err
	}
	log.Debugf("[WS] Sending message to agent (size: %d bytes): %+v", len(data), v)
	return websocket.Message.Send(h.conn, data)
}

// SetConn updates the active websocket connection
func (h *Hub) SetConn(conn *websocket.Conn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.conn != nil {
		h.conn.Close()
	}
	h.conn = conn
}

// Close closes the active connection
func (h *Hub) Close() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.conn != nil {
		h.conn.Close()
		h.conn = nil
	}
}

// SetReadDeadline sets the read deadline on the underlying connection
func (h *Hub) SetReadDeadline(t time.Time) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.conn == nil {
		return errors.New("not connected")
	}
	return h.conn.SetReadDeadline(t)
}

// SetWriteDeadline sets the write deadline on the underlying connection
func (h *Hub) SetWriteDeadline(t time.Time) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.conn == nil {
		return errors.New("not connected")
	}
	return h.conn.SetWriteDeadline(t)
}

// SetDeadline sets both read and write deadlines on the underlying connection
func (h *Hub) SetDeadline(t time.Time) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.conn == nil {
		return errors.New("not connected")
	}
	return h.conn.SetDeadline(t)
}

// NewHub creates a new Hub instance
func NewHub() *Hub {
	return &Hub{}
}

// IsConnected returns true if the hub is currently connected
func (h *Hub) IsConnected() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.conn != nil
}

// Receive receives a CBOR message from the hub
func (h *Hub) Receive(v any) error {
	// We don't lock here while waiting for a message to allow concurrent Sends
	h.mu.Lock()
	conn := h.conn
	h.mu.Unlock()
	if conn == nil {
		return errors.New("not connected")
	}
	var data []byte
	if err := websocket.Message.Receive(conn, &data); err != nil {
		return err
	}
	if len(data) > 0 {
		log.Debugf("[WS] Received message from agent (size: %d bytes)", len(data))
	}
	return cbor.Unmarshal(data, v)
}

// Connect establishes a new connection to the hub
func (h *Hub) Connect(url, token string) error {
	config, err := websocket.NewConfig(url, url)
	if err != nil {
		return err
	}
	if token != "" {
		config.Header.Set("X-Beszel-Token", token)
	}
	conn, err := websocket.DialConfig(config)
	if err != nil {
		return err
	}
	h.SetConn(conn)
	return nil
}
