package collector

import (
	"certainstats/internal/collector/wire"
	"certainstats/internal/store"
	"certainstats/internal/ws"
	"sync"
	"time"
)

// Conn is one live collector connection.
type Conn struct {
	Collector store.Collector
	Since     time.Time
	hub       *ws.Hub
	mu        sync.Mutex
	version   string
	enabled   map[string]bool
	done      chan struct{}
	closeOnce sync.Once
}

// Send writes one frame. Safe for concurrent use.
func (c *Conn) Send(msgType string, body any) error {
	env, err := wire.Encode(msgType, body)
	if err != nil {
		return err
	}
	return c.hub.Send(env)
}

// Enabled reports whether typ is enabled for this collector in the panel.
func (c *Conn) Enabled(typ string) bool { return c.enabled[typ] }

// Version is the collector version from its last ping or handshake.
func (c *Conn) Version() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.version
}

func (c *Conn) setVersion(v string) {
	if v == "" {
		return
	}
	c.mu.Lock()
	c.version = v
	c.mu.Unlock()
}

// Close ends the connection.
func (c *Conn) Close() {
	c.closeOnce.Do(func() {
		close(c.done)
		c.hub.Close()
	})
}

// registry holds live connections, one per collector.
type registry struct {
	mu    sync.Mutex
	conns map[string]*Conn
	limit int
}

func newRegistry(limit int) *registry {
	return &registry{conns: make(map[string]*Conn), limit: limit}
}

// add registers c, closing an older connection of the same collector.
// It refuses a new collector once the limit is reached.
func (r *registry) add(c *Conn) bool {
	r.mu.Lock()
	old := r.conns[c.Collector.CollectorID]
	if old == nil && len(r.conns) >= r.limit {
		r.mu.Unlock()
		return false
	}
	r.conns[c.Collector.CollectorID] = c
	r.mu.Unlock()
	if old != nil {
		old.Close()
	}
	return true
}

// remove unregisters c if it is still the collector's current connection.
func (r *registry) remove(c *Conn) {
	r.mu.Lock()
	if r.conns[c.Collector.CollectorID] == c {
		delete(r.conns, c.Collector.CollectorID)
	}
	r.mu.Unlock()
}

func (r *registry) get(id string) *Conn {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.conns[id]
}

func (r *registry) close(id string) {
	if c := r.get(id); c != nil {
		c.Close()
	}
}

func (r *registry) closeAll() {
	r.mu.Lock()
	list := make([]*Conn, 0, len(r.conns))
	for _, c := range r.conns {
		list = append(list, c)
	}
	r.mu.Unlock()
	for _, c := range list {
		c.Close()
	}
}
