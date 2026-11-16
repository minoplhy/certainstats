// Package wire is the collector WebSocket protocol shared by the server and
// certainstats-collector. Every frame is a CBOR Envelope. Unprefixed message
// types belong to the collector itself; each collector type owns a prefix
// (CrowdSec uses "cs_") and the server routes frames on it.
package wire

import (
	"strings"
	"time"

	"github.com/fxamacker/cbor/v2"
)

const (
	// ConnectPath is the WebSocket path below the panel's mount path.
	ConnectPath = "/api/collectors/connect"
	// HeaderToken carries the collector token on the upgrade request.
	HeaderToken = "X-Token"
	// HeaderVersion carries the collector's software version.
	HeaderVersion = "X-CertainStats-Collector"
	// MaxFrameBytes caps one frame in either direction.
	MaxFrameBytes = 1 << 20
	// Keepalive is how often the collector sends a ping.
	Keepalive = 30 * time.Second
	// IdleTimeout is how long the server waits for any frame before dropping the connection.
	IdleTimeout = 90 * time.Second

	MsgHello = "hello"
	MsgPing  = "ping"
	// MsgPong answers every ping, so the server-to-collector direction carries traffic
	// too: the collector's read deadline and proxies' idle timers (nginx's
	// proxy_read_timeout counts only that direction) would otherwise expire on a quiet host.
	MsgPong = "pong"
)

// Envelope wraps every frame.
type Envelope struct {
	Type string          `cbor:"type"`
	Body cbor.RawMessage `cbor:"body,omitempty"`
}

// Hello is the server's first frame after the upgrade.
type Hello struct {
	CollectorID      string   `cbor:"collector_id"`
	Name             string   `cbor:"name"`
	Types            []string `cbor:"types"`
	KeepaliveSeconds int      `cbor:"keepalive_seconds"`
}

// Ping is the collector's keepalive. Types lists the types switched on locally
// (each by its switch, such as COLLECT_CROWDSEC=true), which may differ from the
// types enabled in the panel.
type Ping struct {
	Version string   `cbor:"version"`
	Types   []string `cbor:"types"`
}

// Encode builds an envelope around body.
func Encode(msgType string, body any) (Envelope, error) {
	env := Envelope{Type: msgType}
	if body != nil {
		raw, err := cbor.Marshal(body)
		if err != nil {
			return env, err
		}
		env.Body = raw
	}
	return env, nil
}

// Marshal encodes a whole frame.
func Marshal(msgType string, body any) ([]byte, error) {
	env, err := Encode(msgType, body)
	if err != nil {
		return nil, err
	}
	return cbor.Marshal(env)
}

// Prefix returns the type prefix of a message type, including the underscore,
// or "" for the collector's own messages.
func Prefix(msgType string) string {
	if i := strings.IndexByte(msgType, '_'); i > 0 {
		return msgType[:i+1]
	}
	return ""
}
