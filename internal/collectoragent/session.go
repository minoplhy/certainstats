// Package collectoragent is certainstats-collector: it loads its settings,
// keeps one WebSocket open to CertainStats and runs the switched-on types
// (modules) over it. cmd/certainstats-collector only parses flags and calls Run,
// Once or Check.
package collectoragent

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	"certainstats/internal/collector/wire"
	"certainstats/internal/collectoragent/module"
	log "certainstats/internal/logger"

	"github.com/fxamacker/cbor/v2"
	"golang.org/x/net/websocket"
)

// link is one WebSocket connection to CertainStats.
type link struct {
	conn *websocket.Conn
	mu   sync.Mutex
}

// connectURL turns HUB_URL into the collector WebSocket URL.
func connectURL(hub string) (wsURL, origin string, err error) {
	u, err := url.Parse(strings.TrimRight(hub, "/"))
	if err != nil || u.Host == "" {
		return "", "", fmt.Errorf("HUB_URL must be the panel URL, such as https://stats.example.com")
	}
	origin = u.Scheme + "://" + u.Host
	switch u.Scheme {
	case "https":
		u.Scheme = "wss"
	case "http":
		u.Scheme = "ws"
	default:
		return "", "", fmt.Errorf("HUB_URL must start with http:// or https://")
	}
	u.Path = strings.TrimRight(u.Path, "/") + wire.ConnectPath
	return u.String(), origin, nil
}

func dial(ctx context.Context, cfg Config) (*link, error) {
	wsURL, origin, err := connectURL(cfg.HubURL)
	if err != nil {
		return nil, err
	}
	wc, err := websocket.NewConfig(wsURL, origin)
	if err != nil {
		return nil, err
	}
	wc.Header.Set(wire.HeaderToken, cfg.Token)
	wc.Header.Set(wire.HeaderVersion, cfg.Version)
	wc.Header.Set("User-Agent", "certainstats-collector/"+cfg.Version)
	conn, err := wc.DialContext(ctx)
	if err != nil {
		var de *websocket.DialError
		if errors.As(err, &de) && errors.Is(de.Err, websocket.ErrBadStatus) {
			return nil, fmt.Errorf("CertainStats refused the connection; check TOKEN and that the collector was not removed: %w", err)
		}
		return nil, fmt.Errorf("cannot reach %s: %w", wsURL, err)
	}
	conn.MaxPayloadBytes = wire.MaxFrameBytes
	return &link{conn: conn}, nil
}

func (l *link) send(msgType string, body any) error {
	data, err := wire.Marshal(msgType, body)
	if err != nil {
		return err
	}
	if len(data) > wire.MaxFrameBytes {
		return fmt.Errorf("%s frame of %d bytes exceeds %d", msgType, len(data), wire.MaxFrameBytes)
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if err := l.conn.SetWriteDeadline(time.Now().Add(15 * time.Second)); err != nil {
		return err
	}
	return websocket.Message.Send(l.conn, data)
}

func (l *link) receive(timeout time.Duration) (wire.Envelope, error) {
	var env wire.Envelope
	if err := l.conn.SetReadDeadline(time.Now().Add(timeout)); err != nil {
		return env, err
	}
	var data []byte
	if err := websocket.Message.Receive(l.conn, &data); err != nil {
		return env, err
	}
	return env, cbor.Unmarshal(data, &env)
}

func (l *link) close() { l.conn.Close() }

// hello waits for the server's first frame.
func (l *link) hello() (wire.Hello, error) {
	var h wire.Hello
	env, err := l.receive(15 * time.Second)
	if err != nil {
		return h, fmt.Errorf("no hello from CertainStats: %w", err)
	}
	if env.Type != wire.MsgHello {
		return h, fmt.Errorf("expected hello, got %q", env.Type)
	}
	return h, cbor.Unmarshal(env.Body, &h)
}

// Session serves one connection until it drops or ctx ends. It reports whether
// the connection got as far as running modules.
func Session(ctx context.Context, cfg Config, mods []module.Module, once bool) (bool, error) {
	l, err := dial(ctx, cfg)
	if err != nil {
		return false, err
	}
	defer l.close()
	h, err := l.hello()
	if err != nil {
		return false, err
	}
	enabled := map[string]bool{}
	for _, t := range h.Types {
		enabled[t] = true
	}
	names := make([]string, 0, len(mods))
	for _, m := range mods {
		names = append(names, m.Name())
	}
	if err := l.send(wire.MsgPing, wire.Ping{Version: cfg.Version, Types: names}); err != nil {
		return false, err
	}
	log.Printf("connected to CertainStats as %q", h.Name)

	sctx, cancel := context.WithCancel(ctx)
	defer cancel()
	inboxes := map[string]chan wire.Envelope{}
	var wg sync.WaitGroup
	var runErr error
	var errMu sync.Mutex
	running := 0
	for _, m := range mods {
		if !enabled[m.Name()] {
			log.Printf("%s is switched on here but not enabled for this collector in CertainStats", m.Name())
			continue
		}
		inbox := make(chan wire.Envelope, 16)
		inboxes[m.Prefix()] = inbox
		running++
		wg.Add(1)
		go func(m module.Module) {
			defer wg.Done()
			if err := m.Run(sctx, l.send, inbox, once); err != nil && sctx.Err() == nil {
				errMu.Lock()
				runErr = fmt.Errorf("%s: %w", m.Name(), err)
				errMu.Unlock()
			}
			if once {
				cancel()
			}
		}(m)
	}
	if running == 0 {
		return true, errors.New("none of the switched-on types are enabled for this collector in CertainStats")
	}

	keepalive := time.Duration(h.KeepaliveSeconds) * time.Second
	if keepalive <= 0 {
		keepalive = wire.Keepalive
	}
	go func() {
		t := time.NewTicker(keepalive)
		defer t.Stop()
		for {
			select {
			case <-sctx.Done():
				l.close() // unblocks the reader
				return
			case <-t.C:
				if err := l.send(wire.MsgPing, wire.Ping{Version: cfg.Version, Types: names}); err != nil {
					cancel()
				}
			}
		}
	}()

	var readErr error
	for {
		env, err := l.receive(wire.IdleTimeout)
		if err != nil {
			if sctx.Err() == nil {
				readErr = fmt.Errorf("connection lost: %w", err)
			}
			break
		}
		inbox := inboxes[wire.Prefix(env.Type)]
		if inbox == nil {
			continue
		}
		select {
		case inbox <- env:
		case <-sctx.Done():
		}
	}
	cancel()
	wg.Wait()
	if runErr != nil {
		return true, runErr
	}
	return true, readErr
}
