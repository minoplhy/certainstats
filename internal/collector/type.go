// Package collector is the parent of every collector type. It owns collectors
// (installed certainstats-collector programs), their tokens, the WebSocket they
// connect over and their API. A type such as CrowdSec registers itself with a
// message prefix and its own routes; the parent never knows what a type stores.
package collector

import (
	"context"
	"net/http"
	"strings"

	"certainstats/internal/store"

	"github.com/fxamacker/cbor/v2"
	"github.com/go-chi/chi/v5"
)

// Type is one kind of data a collector can gather.
type Type interface {
	// Name is the type's ID in URLs, settings and the database, e.g. "crowdsec".
	Name() string
	// Label is the name shown to people, e.g. "CrowdSec".
	Label() string
	// Prefix starts every message type the type sends or receives, e.g. "cs_".
	Prefix() string
	// OnConnect runs after hello when the type is enabled for the collector.
	OnConnect(ctx context.Context, c *Conn) error
	// OnMessage handles one frame with the type's prefix. An error closes the connection.
	OnMessage(ctx context.Context, c *Conn, msgType string, body cbor.RawMessage) error
	// OnDisconnect runs when the connection ends.
	OnDisconnect(c *Conn)
	// CollectorRoutes is mounted at /collectors/{id}/<name>; FromRequest gives the collector.
	CollectorRoutes(r chi.Router)
	// DataRoutes is mounted at /collectors/<name>, for data across the owner's collectors.
	DataRoutes(r chi.Router)
}

// SwitchOf returns the environment variable that switches t on at the collector
// host: COLLECT_<NAME>, for every type.
func SwitchOf(t Type) string {
	return "COLLECT_" + strings.ToUpper(t.Name())
}

type collectorKey struct{}

// FromRequest returns the collector resolved for a /collectors/{id}/... route.
func FromRequest(r *http.Request) *store.Collector {
	c, _ := r.Context().Value(collectorKey{}).(*store.Collector)
	return c
}
