package collector

import (
	"certainstats/internal/collector/wire"
	log "certainstats/internal/logger"
	apiresponse "certainstats/internal/response"
	"certainstats/internal/security"
	"certainstats/internal/store"
	"certainstats/internal/ws"
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/fxamacker/cbor/v2"
	"golang.org/x/net/websocket"
)

// Connect is the collector WebSocket. The token is checked before the upgrade.
func (s *Service) Connect(w http.ResponseWriter, r *http.Request) {
	limits := s.Limits
	if limits == nil {
		limits = security.CollectorLimits
	}
	if !limits.Allow(security.ClientIP(r), false) {
		security.Reject(w, http.StatusTooManyRequests, "Too many connection attempts")
		return
	}
	token := strings.TrimSpace(r.Header.Get(wire.HeaderToken))
	if token == "" {
		apiresponse.Error(w, http.StatusUnauthorized, "Unauthorized")
		return
	}
	col, err := s.Store.CollectorByTokenHash(r.Context(), HashToken(token))
	if err != nil {
		apiresponse.Error(w, http.StatusUnauthorized, "Unauthorized")
		return
	}
	if !strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
		apiresponse.Error(w, http.StatusBadRequest, "WebSocket upgrade required")
		return
	}
	version := r.Header.Get(wire.HeaderVersion)
	server := websocket.Server{
		// Collectors authenticate by token, not by browser origin.
		Handshake: func(*websocket.Config, *http.Request) error { return nil },
		Handler: func(conn *websocket.Conn) {
			hub := ws.NewHub()
			hub.SetConn(conn)
			s.serve(hub, *col, version)
		},
	}
	server.ServeHTTP(w, r)
}

func (s *Service) serve(hub *ws.Hub, col store.Collector, version string) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c := &Conn{Collector: col, Since: time.Now(), hub: hub, version: version, enabled: map[string]bool{}, done: make(chan struct{})}
	defer c.Close()
	var types []string
	for _, t := range col.Types {
		if t.Enabled && s.types[t.Type] != nil {
			c.enabled[t.Type] = true
			types = append(types, t.Type)
		}
	}
	if !s.conns.add(c) {
		log.Printf("[Collector] refusing %s: %d connections open", col.CollectorID, MaxConnections)
		return
	}
	defer s.conns.remove(c)
	if err := s.Store.CollectorSeen(ctx, col.CollectorID, version, time.Now().UnixMilli()); err != nil {
		log.Printf("[Collector] %s: %v", col.CollectorID, err)
	}
	log.Debugf("[Collector] %s connected (version %s, types %v)", col.CollectorID, version, types)

	if types == nil {
		types = []string{}
	}
	if err := c.Send(wire.MsgHello, wire.Hello{CollectorID: col.CollectorID, Name: col.Name, Types: types, KeepaliveSeconds: int(wire.Keepalive / time.Second)}); err != nil {
		return
	}
	var connected []Type
	defer func() {
		for _, t := range connected {
			t.OnDisconnect(c)
		}
		if err := s.Store.CollectorSeen(context.Background(), col.CollectorID, "", time.Now().UnixMilli()); err != nil {
			log.Printf("[Collector] %s: %v", col.CollectorID, err)
		}
	}()
	for _, name := range types {
		t := s.types[name]
		if err := t.OnConnect(ctx, c); err != nil {
			log.Printf("[Collector] %s: %s connect: %v", col.CollectorID, name, err)
			return
		}
		connected = append(connected, t)
	}

	for {
		if err := hub.SetReadDeadline(time.Now().Add(wire.IdleTimeout)); err != nil {
			return
		}
		var env wire.Envelope
		if err := hub.Receive(&env); err != nil {
			log.Debugf("[Collector] %s disconnected: %v", col.CollectorID, err)
			return
		}
		if err := s.dispatch(ctx, c, env); err != nil {
			log.Printf("[Collector] %s: closing: %v", col.CollectorID, err)
			return
		}
	}
}

func (s *Service) dispatch(ctx context.Context, c *Conn, env wire.Envelope) error {
	prefix := wire.Prefix(env.Type)
	if prefix == "" {
		if env.Type == wire.MsgPing {
			return s.ping(ctx, c, env.Body)
		}
		return nil // unknown collector message: ignored, so newer collectors still work
	}
	t := s.typeByPrefix(prefix)
	if t == nil || !c.Enabled(t.Name()) {
		return nil
	}
	return t.OnMessage(ctx, c, env.Type, env.Body)
}

func (s *Service) ping(ctx context.Context, c *Conn, body cbor.RawMessage) error {
	var p wire.Ping
	if err := cbor.Unmarshal(body, &p); err != nil {
		return err
	}
	c.setVersion(p.Version)
	on := map[string]bool{}
	for _, t := range p.Types {
		on[t] = true
	}
	for name := range c.enabled {
		if err := s.Store.CollectorTypeReport(ctx, c.Collector.CollectorID, name, on[name]); err != nil {
			log.Printf("[Collector] %s: %v", c.Collector.CollectorID, err)
		}
	}
	if err := s.Store.CollectorSeen(ctx, c.Collector.CollectorID, p.Version, time.Now().UnixMilli()); err != nil {
		log.Printf("[Collector] %s: %v", c.Collector.CollectorID, err)
	}
	return c.Send(wire.MsgPong, nil)
}
