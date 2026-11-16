package knot

import (
	"context"
	"encoding/json"
	"time"

	"certainstats/internal/collector"
	ctsdb "certainstats/internal/collector/tsdb"
	"certainstats/internal/collector/type/knot/wire"
	log "certainstats/internal/logger"
	"certainstats/internal/store"

	"github.com/fxamacker/cbor/v2"
	"github.com/prometheus/prometheus/model/labels"
)

// Type is the Knot DNS collector type, registered with the collector parent.
type Type struct {
	Store      *Store
	TSDB       *ctsdb.DB
	Collectors *collector.Service
	Retention  Retention
	// Now is replaceable in tests.
	Now func() time.Time
}

var (
	_ collector.Type     = (*Type)(nil)
	_ collector.Setup    = (*Type)(nil)
	_ collector.Switcher = (*Type)(nil)
)

func (t *Type) Name() string   { return wire.TypeName }
func (t *Type) Label() string  { return "Knot DNS" }
func (t *Type) Prefix() string { return wire.Prefix }
func (t *Type) Switch() string { return wire.Switch }

func (t *Type) now() time.Time {
	if t.Now != nil {
		return t.Now()
	}
	return time.Now()
}

// Setup is Knot's part of the install steps.
func (t *Type) Setup() collector.TypeSetup {
	return collector.TypeSetup{
		Env:     [][2]string{{wire.Switch, "true"}, {"KNOT_SOCKET", "/run/knot/knot.sock"}},
		Volumes: []string{"/run/knot:/run/knot"},
		Note: "Install the collector on the machine that runs Knot DNS. It talks to Knot's control socket (KNOT_SOCKET) and sends only read commands: status, stats, zone-stats and zone-status. " +
			"Traffic counters need Knot's mod-stats module: add \"mod-stats:\\n  - id: default\" and, under the default template, \"global-module: mod-stats/default\", then knotc reload. " +
			"If Knot runs in Docker, mount the volume that holds knot.sock instead of /run/knot (the official image keeps it in /rundir) and set KNOT_SOCKET to match. " +
			"Optional: KNOT_ZONE_STATS=true reads per-zone counters (add mod-stats to the zones' module list); KNOT_DNSTAP=true receives Knot's dnstap stream on KNOT_DNSTAP_SOCKET " +
			"(default /run/certainstats/dnstap.sock; point mod-dnstap's sink at unix:<that path> with log-responses on, and mount its directory). " +
			"KNOT_DNSTAP_CLIENTS=true also counts client subnets. Knot keeps no history: traffic is counted from when the collector first reads it.",
	}
}

// state is the type's small per-collector state, kept in collector_types.state_json.
type state struct {
	Version      string            `json:"version,omitempty"`
	Socket       string            `json:"socket,omitempty"`
	ZoneStats    bool              `json:"zone_stats,omitempty"`
	ModStats     bool              `json:"mod_stats,omitempty"`
	ZonesDropped int               `json:"zones_dropped,omitempty"`
	ZonesCut     int               `json:"zones_truncated,omitempty"`
	LastStatsMs  int64             `json:"last_stats_ms,omitempty"`
	LastZonesMs  int64             `json:"last_zones_ms,omitempty"`
	Dnstap       *wire.DnstapState `json:"dnstap,omitempty"`
}

func readState(c *store.Collector) state {
	var st state
	if row, ok := c.Type(wire.TypeName); ok && row.State != "" {
		_ = json.Unmarshal([]byte(row.State), &st) // a damaged state is rebuilt by the next reports
	}
	return st
}

func (t *Type) updateState(ctx context.Context, c *collector.Conn, fn func(*state)) error {
	col, err := t.Collectors.Store.CollectorGet(ctx, c.Collector.CollectorID, c.Collector.UserID)
	if err != nil {
		return err
	}
	st := readState(col)
	fn(&st)
	raw, err := json.Marshal(st)
	if err != nil {
		return err
	}
	return t.Collectors.Store.CollectorTypeSaveState(ctx, c.Collector.CollectorID, wire.TypeName, string(raw))
}

func (t *Type) OnConnect(ctx context.Context, c *collector.Conn) error {
	return c.Send(wire.MsgConfig, wire.Config{
		StatsSeconds: wire.StatsSeconds, ZonesSeconds: wire.ZonesSeconds, DnstapSeconds: wire.DnstapSeconds,
		MaxZoneStats: wire.MaxZoneStats, MaxTop: wire.MaxTop,
	})
}

func (t *Type) OnDisconnect(*collector.Conn) {}

func (t *Type) OnMessage(ctx context.Context, c *collector.Conn, msgType string, body cbor.RawMessage) error {
	switch msgType {
	case wire.MsgStatus:
		var m wire.Status
		if err := cbor.Unmarshal(body, &m); err != nil {
			return err
		}
		return t.status(ctx, c, m)
	case wire.MsgStats:
		var m wire.Stats
		if err := cbor.Unmarshal(body, &m); err != nil {
			return err
		}
		return t.stats(ctx, c, m)
	case wire.MsgZones:
		var m wire.Zones
		if err := cbor.Unmarshal(body, &m); err != nil {
			return err
		}
		return t.zones(ctx, c, m)
	case wire.MsgDnstap:
		var m wire.Dnstap
		if err := cbor.Unmarshal(body, &m); err != nil {
			return err
		}
		return t.dnstap(ctx, c, m)
	}
	return nil
}

func (t *Type) status(ctx context.Context, c *collector.Conn, m wire.Status) error {
	status, message := "ok", ""
	if !m.Reachable {
		status, message = "error", m.Error
		if message == "" {
			message = "Knot DNS unreachable"
		}
	}
	if err := t.Collectors.Store.CollectorTypeStatus(ctx, c.Collector.CollectorID, wire.TypeName, status, message); err != nil {
		return err
	}
	return t.updateState(ctx, c, func(st *state) {
		if m.Version != "" {
			st.Version = m.Version
		}
		st.Socket, st.ZoneStats, st.ModStats, st.Dnstap = m.Socket, m.ZoneStats, m.ModStats, m.Dnstap
	})
}

// stats stores one counter reading. A failed write is logged, not fatal: the
// next reading carries the same cumulative values, so nothing is lost.
func (t *Type) stats(ctx context.Context, c *collector.Conn, m wire.Stats) error {
	if len(m.Counters) > wire.MaxCounters {
		m.Counters = m.Counters[:wire.MaxCounters]
	}
	now := t.now().UnixMilli()
	if t.TSDB != nil {
		if err := writeCounters(ctx, t.TSDB, c.Collector.UserID, c.Collector.CollectorID, m.Counters, now); err != nil {
			log.Printf("[Knot] %s: store counters: %v", c.Collector.CollectorID, err)
		}
	}
	return t.updateState(ctx, c, func(st *state) {
		st.LastStatsMs, st.ZonesDropped = now, m.ZonesDropped
	})
}

func (t *Type) zones(ctx context.Context, c *collector.Conn, m wire.Zones) error {
	if len(m.Zones) > wire.MaxZones {
		m.Zones = m.Zones[:wire.MaxZones]
	}
	now := t.now().UnixMilli()
	if err := t.Store.SaveZones(ctx, c.Collector.UserID, c.Collector.CollectorID, m.Zones, now); err != nil {
		log.Printf("[Knot] %s: store zones: %v", c.Collector.CollectorID, err)
		return nil
	}
	return t.updateState(ctx, c, func(st *state) { st.LastZonesMs, st.ZonesCut = now, m.Truncated })
}

// dnstap stores one bucket part and acknowledges it. A store failure is
// answered, so the collector keeps the part and sends it again.
func (t *Type) dnstap(ctx context.Context, c *collector.Conn, m wire.Dnstap) error {
	if len(m.Names) > wire.MaxTop*4 || len(m.Clients) > wire.MaxClients {
		return c.Send(wire.MsgAck, wire.Ack{MinuteMs: m.MinuteMs, Part: m.Part, Error: "bucket too large"})
	}
	now := t.now().UnixMilli()
	// A minute from the future or older than minute retention is acknowledged and dropped.
	if m.MinuteMs > now+5*minuteMs || (t.Retention.MinuteMs > 0 && m.MinuteMs < now-t.Retention.MinuteMs) {
		return c.Send(wire.MsgAck, wire.Ack{MinuteMs: m.MinuteMs, Part: m.Part})
	}
	if err := t.Store.SaveDnstap(ctx, c.Collector.UserID, c.Collector.CollectorID, m, now); err != nil {
		log.Printf("[Knot] %s: store dnstap: %v", c.Collector.CollectorID, err)
		return c.Send(wire.MsgAck, wire.Ack{MinuteMs: m.MinuteMs, Part: m.Part, Error: "storage failed; resend later"})
	}
	return c.Send(wire.MsgAck, wire.Ack{MinuteMs: m.MinuteMs, Part: m.Part})
}

// Maintain rolls settled dnstap hours, applies retention and drops the
// counters of deleted accounts; it runs on the hourly cleanup.
func (t *Type) Maintain(ctx context.Context) {
	users, err := t.Collectors.Store.UserIDs(ctx)
	if err != nil {
		log.Printf("[Knot] maintenance: %v", err)
		return
	}
	now := t.now().UnixMilli()
	if n, err := t.Store.Rollup(ctx, now); err != nil {
		log.Printf("[Knot] rollup: %v", err)
	} else if n > 0 {
		log.Printf("[Knot] rolled %d dnstap hours", n)
	}
	res, err := t.Store.Prune(ctx, t.Retention, now, users)
	if err != nil {
		log.Printf("[Knot] maintenance: %v", err)
		return
	}
	log.Printf("[Maintenance] %s", res)
	if t.TSDB == nil {
		return
	}
	known := map[string]bool{}
	for _, u := range users {
		known[u] = true
	}
	owners, err := t.TSDB.LabelValues(ctx, "user_id", labels.MustNewMatcher(labels.MatchEqual, "__name__", metricName))
	if err != nil {
		log.Printf("[Knot] counter owners: %v", err)
		return
	}
	for _, o := range owners {
		if known[o] {
			continue
		}
		if err := t.TSDB.Delete(ctx, labels.MustNewMatcher(labels.MatchEqual, "__name__", metricName), labels.MustNewMatcher(labels.MatchEqual, "user_id", o)); err != nil {
			log.Printf("[Knot] drop counters of a deleted account: %v", err)
		}
	}
}
