// Package knot is the Knot DNS type of certainstats-collector. It reads Knot's
// counters and zone status over Knot's control socket, sending only the
// read-only commands in allowed, and optionally receives Knot's dnstap stream,
// folding it into one-minute buckets on the host. Counters are sent as Knot
// reports them; the server keeps all history.
package knot

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/user"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"certainstats/internal/collector/type/knot/wire"
	cwire "certainstats/internal/collector/wire"
	"certainstats/internal/collectoragent/module"
	log "certainstats/internal/logger"

	"github.com/fxamacker/cbor/v2"
)

const (
	DefaultSocket       = "/run/knot/knot.sock"
	DefaultDnstapSocket = "/run/certainstats/dnstap.sock"
	defaultZoneStatsMax = 200
	defaultTop          = 1000
	defaultClientTop    = 1000

	// startGrace is how long after start an unreachable Knot counts as still starting.
	startGrace = 2 * time.Minute
	// statusEvery resends an unchanged status, so the server's view stays fresh.
	statusEvery = 5 * time.Minute
	// resendAfter is how long an unacknowledged dnstap part waits before it is sent again.
	resendAfter = 2 * time.Minute
	// frameBudget leaves room in a frame for the envelope.
	frameBudget = cwire.MaxFrameBytes - 8192
)

// Module is the Knot DNS collector type. One Module lives for the whole
// process, across connections, so the dnstap listener and unacknowledged
// buckets survive a reconnect.
type Module struct {
	version      string
	ctl          *ctl
	zoneStats    bool
	zoneStatsMax int
	tap          *tap // nil unless KNOT_DNSTAP=true
	// Now is replaceable in tests.
	Now     func() time.Time
	started time.Time
	reached bool

	mu      sync.Mutex
	pending []pendingPart
}

type pendingPart struct {
	d      wire.Dnstap
	sentAt time.Time
}

var _ module.Module = (*Module)(nil)

func envOn(getenv func(string) string, key string) bool {
	return strings.EqualFold(strings.TrimSpace(getenv(key)), "true")
}

func envInt(getenv func(string) string, key string, def, lo, hi int) int {
	raw := strings.TrimSpace(getenv(key))
	if raw == "" {
		return def
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < lo || n > hi {
		log.Printf("knot: %s=%q is not a number from %d to %d; using %d", key, raw, lo, hi, def)
		return def
	}
	return n
}

// New reads the type's settings from the environment.
func New(getenv func(string) string, version string) *Module {
	socket := strings.TrimSpace(getenv("KNOT_SOCKET"))
	if socket == "" {
		socket = DefaultSocket
	}
	m := &Module{
		version:      version,
		ctl:          &ctl{path: socket, timeout: 5 * time.Second},
		zoneStats:    envOn(getenv, "KNOT_ZONE_STATS"),
		zoneStatsMax: envInt(getenv, "KNOT_ZONE_STATS_MAX", defaultZoneStatsMax, 1, wire.MaxZoneStats),
		Now:          time.Now,
		started:      time.Now(),
	}
	if envOn(getenv, "KNOT_DNSTAP") {
		path := strings.TrimSpace(getenv("KNOT_DNSTAP_SOCKET"))
		if path == "" {
			path = DefaultDnstapSocket
		}
		m.tap = &tap{
			path:      path,
			top:       envInt(getenv, "KNOT_DNSTAP_TOP", defaultTop, 10, wire.MaxTop),
			clientTop: envInt(getenv, "KNOT_DNSTAP_CLIENT_TOP", defaultClientTop, 10, wire.MaxClientRows),
			agg:       &aggregator{clients: envOn(getenv, "KNOT_DNSTAP_CLIENTS"), zones: zoneSet{}},
			now:       func() time.Time { return m.Now() },
		}
	}
	return m
}

func (m *Module) Name() string   { return wire.TypeName }
func (m *Module) Prefix() string { return wire.Prefix }

// Check reads Knot's version and counters and tests the dnstap socket path.
func (m *Module) Check(ctx context.Context) error {
	ver, err := m.knotVersion(ctx)
	if err != nil {
		return err
	}
	fmt.Printf("ok   knot: Knot DNS %s answers on %s\n", ver, m.ctl.path)
	units, err := m.ctl.call(ctx, request{idxCmd: "stats", idxFlags: "F"})
	if err != nil {
		return err
	}
	if !hasModStats(parseCounters(units)) {
		fmt.Println("warn knot: Knot sends no mod-stats counters; add mod-stats as a global-module (see the setup note) or traffic stays empty")
	}
	if m.zoneStats {
		units, err := m.ctl.call(ctx, request{idxCmd: "zone-stats"})
		if err != nil && !partial(units, err) {
			return fmt.Errorf("per-zone counters: %w", err)
		}
		if len(parseCounters(units)) == 0 {
			fmt.Println("warn knot: KNOT_ZONE_STATS=true but Knot sends no per-zone counters; add mod-stats to the zones' module list")
		}
	}
	if m.tap != nil {
		msg, err := m.tap.check()
		if err != nil {
			return fmt.Errorf("dnstap: %w", err)
		}
		fmt.Println("ok   knot: " + msg)
	}
	return nil
}

func (m *Module) knotVersion(ctx context.Context) (string, error) {
	units, err := m.ctl.call(ctx, request{idxCmd: "status", idxType: "version"})
	if err != nil {
		return "", err
	}
	for _, u := range units {
		if v := strings.TrimSpace(u.get(idxData)); v != "" {
			return v, nil
		}
	}
	return "unknown version", nil
}

// runner is the state of one connection.
type runner struct {
	m        *Module
	send     module.Send
	inbox    <-chan cwire.Envelope
	cfg      wire.Config
	once     bool
	last     *wire.Status
	lastSent time.Time
	version  string
}

func (m *Module) Run(ctx context.Context, send module.Send, inbox <-chan cwire.Envelope, once bool) error {
	r := &runner{m: m, send: send, inbox: inbox, once: once}
	if err := r.waitConfig(ctx); err != nil {
		return err
	}
	if m.tap != nil && !once {
		m.tap.start()
	}
	m.mu.Lock()
	for i := range m.pending {
		m.pending[i].sentAt = time.Time{} // a new connection resends everything unacknowledged
	}
	m.mu.Unlock()

	r.zonesPass(ctx)
	r.statsPass(ctx)
	if once {
		return nil
	}
	stats := time.NewTicker(time.Duration(r.cfg.StatsSeconds) * time.Second)
	zones := time.NewTicker(time.Duration(r.cfg.ZonesSeconds) * time.Second)
	flush := time.NewTicker(time.Duration(r.cfg.DnstapSeconds) * time.Second)
	defer stats.Stop()
	defer zones.Stop()
	defer flush.Stop()
	if m.tap != nil {
		r.flushPass()
	}
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-stats.C:
			r.statsPass(ctx)
		case <-zones.C:
			r.zonesPass(ctx)
		case <-flush.C:
			if m.tap != nil {
				r.flushPass()
			}
		case env := <-inbox:
			r.handle(env)
		}
	}
}

func (r *runner) waitConfig(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case env := <-r.inbox:
			if env.Type != wire.MsgConfig {
				continue
			}
			if err := cbor.Unmarshal(env.Body, &r.cfg); err != nil {
				return err
			}
			r.cfg.StatsSeconds = clamp(r.cfg.StatsSeconds, wire.StatsSeconds, 10, 3600)
			r.cfg.ZonesSeconds = clamp(r.cfg.ZonesSeconds, wire.ZonesSeconds, 30, 3600)
			r.cfg.DnstapSeconds = clamp(r.cfg.DnstapSeconds, wire.DnstapSeconds, 10, 600)
			if r.cfg.MaxZoneStats > 0 && r.cfg.MaxZoneStats < r.m.zoneStatsMax {
				r.m.zoneStatsMax = r.cfg.MaxZoneStats
			}
			return nil
		}
	}
}

func clamp(v, def, lo, hi int) int {
	if v <= 0 {
		return def
	}
	return min(max(v, lo), hi)
}

func (r *runner) handle(env cwire.Envelope) {
	if env.Type != wire.MsgAck {
		return
	}
	var ack wire.Ack
	if cbor.Unmarshal(env.Body, &ack) != nil {
		return
	}
	r.m.mu.Lock()
	defer r.m.mu.Unlock()
	for i, p := range r.m.pending {
		if p.d.MinuteMs != ack.MinuteMs || p.d.Part != ack.Part {
			continue
		}
		if ack.Error != "" {
			// Resent on the next flush after resendAfter.
			log.Printf("knot: CertainStats could not store dnstap minute %s: %s", time.UnixMilli(ack.MinuteMs).UTC().Format(time.RFC3339), ack.Error)
			return
		}
		r.m.pending = append(r.m.pending[:i], r.m.pending[i+1:]...)
		return
	}
}

// starting reports whether an unreachable Knot may still be starting up.
func (r *runner) starting() bool {
	return !r.once && !r.m.reached && r.m.Now().Sub(r.m.started) < startGrace
}

func (r *runner) statsPass(ctx context.Context) {
	at := r.m.Now()
	units, err := r.m.ctl.call(ctx, request{idxCmd: "stats", idxFlags: "F"})
	if err != nil {
		r.fail(err)
		return
	}
	r.m.reached = true
	counters := parseCounters(units)
	st := wire.Stats{AtMs: at.UnixMilli()}
	modStats := hasModStats(counters)
	if r.m.zoneStats {
		zunits, err := r.m.ctl.call(ctx, request{idxCmd: "zone-stats"})
		if err != nil {
			log.Printf("knot: per-zone counters: %v", err)
		}
		if err == nil || partial(zunits, err) {
			zc, dropped := capZones(parseCounters(zunits), r.m.zoneStatsMax)
			counters = append(counters, zc...)
			st.ZonesDropped = dropped
		}
	}
	if len(counters) > wire.MaxCounters {
		log.Printf("knot: %d counters; sending the first %d", len(counters), wire.MaxCounters)
		counters = counters[:wire.MaxCounters]
	}
	st.Counters = counters
	if r.version == "" {
		r.version, _ = r.m.knotVersion(ctx)
	}
	r.report(nil, modStats)
	if err := r.send(wire.MsgStats, st); err != nil {
		log.Printf("knot: %v", err)
	}
}

func (r *runner) zonesPass(ctx context.Context) {
	at := r.m.Now()
	// "u" asks for event times as Unix times; versions before 3.3.3 ignore it.
	units, err := r.m.ctl.call(ctx, request{idxCmd: "zone-status", idxFlags: "u"})
	if err != nil && !partial(units, err) {
		r.fail(err)
		return
	}
	if err != nil {
		log.Printf("knot: zone status: %v", err)
	}
	r.m.reached = true
	zones := parseZones(units, at)
	if r.m.tap != nil {
		zs := make(zoneSet, len(zones))
		for _, z := range zones {
			zs[z.Name] = true
		}
		r.m.tap.agg.setZones(zs)
	}
	msg := wire.Zones{AtMs: at.UnixMilli(), Zones: zones}
	if len(zones) > wire.MaxZones {
		msg.Zones, msg.Truncated = zones[:wire.MaxZones], len(zones)-wire.MaxZones
	}
	if err := r.send(wire.MsgZones, msg); err != nil {
		log.Printf("knot: %v", err)
	}
}

func (r *runner) fail(err error) {
	if r.starting() {
		log.Printf("knot: waiting for Knot (%v)", err)
		return
	}
	r.report(err, false)
}

// report sends kn_status when it changes, and every statusEvery regardless.
func (r *runner) report(err error, modStats bool) {
	st := wire.Status{Reachable: err == nil, Socket: r.m.ctl.path, ZoneStats: r.m.zoneStats, ModStats: modStats, Version: r.version}
	if err != nil {
		st.Error = err.Error()
		if r.last != nil {
			st.ModStats = r.last.ModStats
		}
	}
	if r.m.tap != nil {
		st.Dnstap = r.m.tap.state()
	}
	now := r.m.Now()
	if r.last != nil && sameStatus(*r.last, st) && now.Sub(r.lastSent) < statusEvery {
		return
	}
	if r.send(wire.MsgStatus, st) == nil {
		r.last, r.lastSent = &st, now
	}
	if err != nil {
		log.Printf("knot: %v", err)
	}
}

// sameStatus compares statuses, ignoring the dnstap last-frame time, which moves constantly.
func sameStatus(a, b wire.Status) bool {
	da, db := a.Dnstap, b.Dnstap
	a.Dnstap, b.Dnstap = nil, nil
	if a != b {
		return false
	}
	if (da == nil) != (db == nil) {
		return false
	}
	if da == nil {
		return true
	}
	x, y := *da, *db
	x.LastFrameMs, y.LastFrameMs = 0, 0
	x.Dropped, y.Dropped = 0, 0
	return x == y
}

// flushPass queues ended minutes and sends every part that is new or overdue.
func (r *runner) flushPass() {
	now := r.m.Now()
	t := r.m.tap
	r.m.mu.Lock()
	for _, mn := range t.agg.take(now) {
		for _, p := range split(mn.bucket(t.top, t.clientTop), frameBudget) {
			r.m.pending = append(r.m.pending, pendingPart{d: p})
		}
	}
	r.m.pending = trimPending(r.m.pending)
	var due []wire.Dnstap
	for i := range r.m.pending {
		p := &r.m.pending[i]
		if p.sentAt.IsZero() || now.Sub(p.sentAt) >= resendAfter {
			p.sentAt = now
			due = append(due, p.d)
		}
	}
	r.m.mu.Unlock()
	for _, d := range due {
		if err := r.send(wire.MsgDnstap, d); err != nil {
			log.Printf("knot: dnstap: %v", err)
			return
		}
	}
}

// trimPending keeps the newest MaxPending minutes.
func trimPending(p []pendingPart) []pendingPart {
	minutes := map[int64]bool{}
	for _, x := range p {
		minutes[x.d.MinuteMs] = true
	}
	if len(minutes) <= wire.MaxPending {
		return p
	}
	oldest := make([]int64, 0, len(minutes))
	for mn := range minutes {
		oldest = append(oldest, mn)
	}
	slices.Sort(oldest)
	cut := oldest[len(oldest)-wire.MaxPending]
	out := p[:0]
	dropped := 0
	for _, x := range p {
		if x.d.MinuteMs < cut {
			dropped++
			continue
		}
		out = append(out, x)
	}
	log.Printf("knot: dropped %d unacknowledged dnstap parts older than %d minutes", dropped, wire.MaxPending)
	return out
}

// tap is the dnstap listener. It starts once and runs for the life of the process.
type tap struct {
	path string
	top  int
	// clientTop caps the subnet-by-name rows sent per minute.
	clientTop int
	agg       *aggregator
	now       func() time.Time

	once      sync.Once
	mu        sync.Mutex
	listening bool
	conns     int
	err       string
	lastFrame int64
	dropped   uint64
}

func (t *tap) state() *wire.DnstapState {
	t.mu.Lock()
	defer t.mu.Unlock()
	return &wire.DnstapState{Socket: t.path, Listening: t.listening, Connected: t.conns > 0, Error: t.err,
		LastFrameMs: t.lastFrame, Dropped: t.dropped, Clients: t.agg.clients}
}

func (t *tap) setErr(err error) {
	t.mu.Lock()
	t.err = errText(err)
	t.mu.Unlock()
	if err != nil {
		log.Printf("knot: dnstap: %v", err)
	}
}

// check tests the socket path without disturbing a collector already listening on it.
func (t *tap) check() (string, error) {
	info, err := os.Lstat(t.path)
	switch {
	case err == nil && info.Mode()&os.ModeSocket == 0:
		return "", fmt.Errorf("%s exists and is not a socket", t.path)
	case err == nil:
		if c, err := net.DialTimeout("unix", t.path, time.Second); err == nil {
			c.Close()
			return "dnstap socket " + t.path + " is in use by a running collector", nil
		}
		return "dnstap socket " + t.path + " can be created (a stale one is replaced)", nil
	case errors.Is(err, os.ErrNotExist):
		dir := filepath.Dir(t.path)
		if info, err := os.Stat(dir); err == nil && !info.IsDir() {
			return "", fmt.Errorf("%s is not a directory", dir)
		}
		return "dnstap socket " + t.path + " can be created", nil
	default:
		return "", err
	}
}

func (t *tap) start() {
	t.once.Do(func() { go t.listen() })
}

// listen creates the socket and accepts Knot's connections, retrying on failure.
func (t *tap) listen() {
	for {
		ln, err := t.open()
		if err != nil {
			t.setErr(err)
			time.Sleep(30 * time.Second)
			continue
		}
		t.mu.Lock()
		t.listening, t.err = true, ""
		t.mu.Unlock()
		log.Printf("knot: listening for dnstap on %s", t.path)
		for {
			c, err := ln.Accept()
			if err != nil {
				t.setErr(err)
				break
			}
			go t.serve(c)
		}
		ln.Close()
		t.mu.Lock()
		t.listening = false
		t.mu.Unlock()
		time.Sleep(5 * time.Second)
	}
}

// open replaces a stale socket and lets Knot connect: owned by the knot user
// when there is one (mode 0660), else open to all local users (0666).
func (t *tap) open() (net.Listener, error) {
	if err := os.MkdirAll(filepath.Dir(t.path), 0o755); err != nil {
		return nil, err
	}
	if info, err := os.Lstat(t.path); err == nil {
		if info.Mode()&os.ModeSocket == 0 {
			return nil, fmt.Errorf("%s exists and is not a socket", t.path)
		}
		if err := os.Remove(t.path); err != nil {
			return nil, err
		}
	}
	ln, err := net.Listen("unix", t.path)
	if err != nil {
		return nil, err
	}
	mode := os.FileMode(0o666)
	if u, err := user.Lookup("knot"); err == nil {
		uid, _ := strconv.Atoi(u.Uid)
		gid, _ := strconv.Atoi(u.Gid)
		if os.Chown(t.path, uid, gid) == nil {
			mode = 0o660
		}
	}
	if err := os.Chmod(t.path, mode); err != nil {
		ln.Close()
		return nil, err
	}
	return ln, nil
}

func (t *tap) serve(c net.Conn) {
	defer c.Close()
	t.mu.Lock()
	t.conns++
	t.mu.Unlock()
	defer func() {
		t.mu.Lock()
		t.conns--
		t.mu.Unlock()
	}()
	err := serveStream(c, func(frame []byte) {
		now := t.now()
		ev, err := decodeDnstap(frame)
		t.mu.Lock()
		t.lastFrame = now.UnixMilli()
		if err != nil && !errors.Is(err, errSkip) {
			t.dropped++
		}
		t.mu.Unlock()
		if err == nil {
			t.agg.add(ev, now)
		}
	})
	if err != nil && !errors.Is(err, errStopped) {
		log.Printf("knot: dnstap connection: %v", err)
	}
}
