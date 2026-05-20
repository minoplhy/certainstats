package ws

import (
	log "certainstats/internal/logger"
	"net/http"

	"github.com/fxamacker/cbor/v2"
	"golang.org/x/net/websocket"
)

type WebSocketAction = uint8

const (
	// Request system data from agent
	GetData WebSocketAction = iota
	// Hub verification challenge
	CheckFingerprint
)

// HubRequest defines the structure for requests sent from hub to agent.
type HubRequest[T any] struct {
	Action WebSocketAction `cbor:"0,keyasint"`
	Data   T               `cbor:"1,keyasint,omitempty,omitzero"`
	Id     *uint32         `cbor:"2,keyasint,omitempty"`
}

// AgentResponse defines the structure for responses sent from agent to hub.
type AgentResponse struct {
	Id          *uint32         `cbor:"0,keyasint,omitempty"`
	SystemData  cbor.RawMessage `cbor:"1,keyasint,omitempty"` // Data for GetData
	Fingerprint cbor.RawMessage `cbor:"2,keyasint,omitempty"` // Data for Auth
	Error       string          `cbor:"3,keyasint,omitempty,omitzero"`
	// Data is the generic response payload for new endpoints (0.18+)
	Data cbor.RawMessage `cbor:"7,keyasint,omitempty,omitzero"`
}

// GetPayload returns the raw message bytes from any available data field.
func (ar *AgentResponse) GetPayload() cbor.RawMessage {
	if len(ar.Data) > 0 {
		return ar.Data
	}
	if len(ar.SystemData) > 0 {
		return ar.SystemData
	}
	return ar.Fingerprint
}

type DataRequestOptions struct {
	CacheTimeMs    uint16 `cbor:"0,keyasint"`
	IncludeDetails bool   `cbor:"1,keyasint"`
}

type FingerprintRequest struct {
	Signature   []byte `cbor:"0,keyasint"`
	NeedSysInfo bool   `cbor:"1,keyasint"`
}

type AgentConnectRequest struct{}

// Upgrade upgrades the HTTP connection to a WebSocket connection.
// The token and version should be validated by the caller before calling this.
func (acr *AgentConnectRequest) Upgrade(w http.ResponseWriter, r *http.Request, token, version string, handler func(*websocket.Conn, string, string)) {
	log.Debugf("[WS] Raw connection details - Method: %s, URL: %s, RemoteAddr: %s", r.Method, r.URL.String(), r.RemoteAddr)

	// If it's not a websocket upgrade request, it might be an availability check.
	if r.Header.Get("Upgrade") != "websocket" {
		log.Debugf("[WS] Non-websocket request detected (availability check). Returning 200 OK.")
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("Beszel Agent Connection Endpoint"))
		return
	}

	server := websocket.Server{
		Handshake: func(config *websocket.Config, req *http.Request) error {
			log.Debugf("[WS] Handshake successful for %s", r.RemoteAddr)
			return nil // Disable origin check to avoid 403 Forbidden with agents
		},
		Handler: func(conn *websocket.Conn) {
			log.Debugf("[WS] Connection established with agent %s", token)
			handler(conn, token, version)
		},
	}
	server.ServeHTTP(w, r)
}
