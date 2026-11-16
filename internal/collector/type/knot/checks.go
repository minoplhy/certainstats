package knot

import (
	"context"
	"fmt"
	"sort"
	"time"

	"certainstats/internal/collector/type/knot/wire"
)

const (
	expiryWarn    = 24 * time.Hour
	driftAfter    = 15 * time.Minute
	servfailSpan  = 15 * time.Minute
	servfailShare = 0.01
	servfailMin   = 100
	dnstapQuiet   = 5 * time.Minute
)

// Check is one finding shown on the Knot page. Level is "bad" or "warn".
type Check struct {
	Level       string `json:"level"`
	Kind        string `json:"kind"`
	CollectorID string `json:"collector_id,omitempty"`
	Server      string `json:"server,omitempty"`
	Zone        string `json:"zone,omitempty"`
	Message     string `json:"message"`
}

// Server is one collector reading Knot, as the page shows it.
type Server struct {
	CollectorID    string            `json:"collector_id"`
	Name           string            `json:"name"`
	Enabled        bool              `json:"enabled"`
	Connected      bool              `json:"connected"`
	ConnectedSince int64             `json:"connected_since"`
	FirstSeen      int64             `json:"first_connected_at"`
	LastSeen       int64             `json:"last_seen_at"`
	ReportedOn     bool              `json:"reported_on"`
	Status         string            `json:"status"`
	Error          string            `json:"error"`
	Version        string            `json:"version"`
	ZoneStats      bool              `json:"zone_stats"`
	ModStats       bool              `json:"mod_stats"`
	ZonesDropped   int               `json:"zones_dropped"`
	ZonesCut       int               `json:"zones_truncated"`
	LastStatsMs    int64             `json:"last_stats_ms"`
	ErrorSinceMs   int64             `json:"error_since_ms"`
	Dnstap         *wire.DnstapState `json:"dnstap,omitempty"`
	LastDnstapMs   int64             `json:"last_dnstap_ms"`
	ZoneCount      int               `json:"zone_count"`
}

// servers lists the owner's collectors that have Knot as a type.
func (t *Type) servers(ctx context.Context, userID string) ([]Server, error) {
	list, err := t.Collectors.Store.CollectorList(ctx, userID)
	if err != nil {
		return nil, err
	}
	out := []Server{}
	for i := range list {
		c := &list[i]
		row, ok := c.Type(wire.TypeName)
		if !ok {
			continue
		}
		st := readState(c)
		s := Server{CollectorID: c.CollectorID, Name: c.Name, Enabled: row.Enabled, FirstSeen: c.FirstConnectedAt, LastSeen: c.LastSeenAt,
			ReportedOn: row.ReportedOn, Status: row.Status, Error: row.Error, Version: st.Version, ZoneStats: st.ZoneStats,
			ModStats: st.ModStats, ZonesDropped: st.ZonesDropped, ZonesCut: st.ZonesCut, LastStatsMs: st.LastStatsMs, ErrorSinceMs: st.ErrorSinceMs, Dnstap: st.Dnstap}
		if since, ok := t.connected(c.CollectorID); ok {
			s.Connected, s.ConnectedSince = true, since.UnixMilli()
		}
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// connected reports whether a collector is connected, and since when.
func (t *Type) connected(collectorID string) (time.Time, bool) {
	if t.live != nil {
		return t.live(collectorID)
	}
	if conn := t.Collectors.Conn(collectorID); conn != nil {
		return conn.Since, true
	}
	return time.Time{}, false
}

// serialLess compares SOA serials with RFC 1982 arithmetic.
func serialLess(a, b uint32) bool {
	return a != b && ((a < b && b-a < 1<<31) || (a > b && a-b > 1<<31))
}

// ZoneView is one zone across every server that serves it.
type ZoneView struct {
	Zone          string     `json:"zone"`
	Rows          []ZoneSide `json:"servers"`
	PrimarySerial *uint32    `json:"primary_serial"`
	// PrimaryChangedAt is when the reference serial last changed.
	PrimaryChangedAt int64 `json:"primary_changed_at"`
	Drift            bool  `json:"drift"`
	ExpireAt         int64 `json:"expire_at"`
}

// ZoneSide is one server's view of a zone. Lagging is a serial below the
// reference; Behind is lagging for longer than driftAfter.
type ZoneSide struct {
	ZoneRow
	Server  string `json:"server"`
	Lagging bool   `json:"lagging"`
	Behind  bool   `json:"behind"`
}

// zoneViews groups zone rows by zone and marks secondaries behind the primary
// for longer than driftAfter.
func zoneViews(rows []ZoneRow, names map[string]string, now int64) []ZoneView {
	by := map[string]*ZoneView{}
	var order []string
	for _, r := range rows {
		name, ok := names[r.CollectorID]
		if !ok {
			continue // a removed collector, or one without Knot
		}
		v := by[r.Zone]
		if v == nil {
			v = &ZoneView{Zone: r.Zone}
			by[r.Zone] = v
			order = append(order, r.Zone)
		}
		v.Rows = append(v.Rows, ZoneSide{ZoneRow: r, Server: name})
	}
	sort.Strings(order)
	out := make([]ZoneView, 0, len(order))
	for _, z := range order {
		v := by[z]
		// The reference is the newest serial among primaries, or among all when none is primary.
		var ref *ZoneSide
		for i := range v.Rows {
			r := &v.Rows[i]
			if !r.HasSerial {
				continue
			}
			if r.Role == "primary" && (ref == nil || ref.Role != "primary" || serialLess(ref.Serial, r.Serial)) {
				ref = r
			} else if r.Role != "primary" && (ref == nil || ref.Role != "primary" && serialLess(ref.Serial, r.Serial)) {
				ref = r
			}
		}
		if ref != nil {
			s := ref.Serial
			v.PrimarySerial, v.PrimaryChangedAt = &s, ref.SerialChangedAt
			for i := range v.Rows {
				r := &v.Rows[i]
				if r.HasSerial && serialLess(r.Serial, ref.Serial) {
					r.Lagging = true
					if now-ref.SerialChangedAt > driftAfter.Milliseconds() {
						r.Behind, v.Drift = true, true
					}
				}
			}
		}
		for _, r := range v.Rows {
			if r.Role == "secondary" && r.ExpireAt > 0 && (v.ExpireAt == 0 || r.ExpireAt < v.ExpireAt) {
				v.ExpireAt = r.ExpireAt
			}
		}
		out = append(out, *v)
	}
	return out
}

// checks gathers the page's findings.
func (t *Type) checks(ctx context.Context, userID string, servers []Server, zones []ZoneView, now time.Time) []Check {
	out := []Check{}
	nowMs := now.UnixMilli()
	for _, s := range servers {
		if !s.Enabled {
			continue
		}
		switch {
		case !s.Connected && s.FirstSeen == 0:
			out = append(out, Check{Level: "warn", Kind: "waiting", CollectorID: s.CollectorID, Server: s.Name, Message: "Waiting for the collector's first connection"})
			continue
		case !s.Connected:
			out = append(out, Check{Level: "bad", Kind: "offline", CollectorID: s.CollectorID, Server: s.Name, Message: "Collector offline since " + time.UnixMilli(s.LastSeen).UTC().Format("2006-01-02 15:04 UTC")})
			continue
		case !s.ReportedOn:
			out = append(out, Check{Level: "warn", Kind: "switched_off", CollectorID: s.CollectorID, Server: s.Name, Message: "Knot DNS is not switched on at the collector (" + wire.Switch + "=true)"})
			continue
		case s.Status == "error":
			out = append(out, Check{Level: "bad", Kind: "unreachable", CollectorID: s.CollectorID, Server: s.Name, Message: "Knot unreachable: " + s.Error})
			continue
		}
		if s.LastStatsMs > 0 && !s.ModStats {
			out = append(out, Check{Level: "warn", Kind: "no_mod_stats", CollectorID: s.CollectorID, Server: s.Name, Message: "Knot sends no mod-stats counters, so traffic stays empty; enable mod-stats as a global module"})
		}
		if s.ZonesDropped > 0 {
			out = append(out, Check{Level: "warn", Kind: "zone_cap", CollectorID: s.CollectorID, Server: s.Name, Message: fmt.Sprintf("Per-zone counters cover the busiest zones only; %d zones are left out (KNOT_ZONE_STATS_MAX)", s.ZonesDropped)})
		}
		if d := s.Dnstap; d != nil {
			switch {
			case !d.Listening:
				msg := "dnstap is switched on but the collector is not listening on " + d.Socket
				if d.Error != "" {
					msg += ": " + d.Error
				}
				out = append(out, Check{Level: "bad", Kind: "dnstap_down", CollectorID: s.CollectorID, Server: s.Name, Message: msg})
			case nowMs-s.ConnectedSince > dnstapQuiet.Milliseconds() && nowMs-s.LastDnstapMs > dnstapQuiet.Milliseconds():
				out = append(out, Check{Level: "warn", Kind: "dnstap_quiet", CollectorID: s.CollectorID, Server: s.Name, Message: "No dnstap traffic for 5 minutes; check mod-dnstap's sink points at " + d.Socket + " and Knot was reloaded"})
			}
		}
		if total, share, err := t.servfail(ctx, userID, s.CollectorID, nowMs); err == nil && total >= servfailMin && share > 100*servfailShare {
			out = append(out, Check{Level: "warn", Kind: "servfail", CollectorID: s.CollectorID, Server: s.Name,
				Message: fmt.Sprintf("SERVFAIL is %.1f%% of responses over the last 15 minutes", share)})
		}
	}
	for _, z := range zones {
		for _, r := range z.Rows {
			if r.Role == "secondary" && r.ExpireAt > 0 {
				left := time.Duration(r.ExpireAt-nowMs) * time.Millisecond
				switch {
				case left <= 0:
					out = append(out, Check{Level: "bad", Kind: "expired", CollectorID: r.CollectorID, Server: r.Server, Zone: z.Zone, Message: z.Zone + " has expired on " + r.Server})
				case left < expiryWarn:
					out = append(out, Check{Level: "warn", Kind: "expiring", CollectorID: r.CollectorID, Server: r.Server, Zone: z.Zone, Message: fmt.Sprintf("%s expires on %s in %s", z.Zone, r.Server, left.Round(time.Minute))})
				}
			}
			if r.Behind && z.PrimarySerial != nil {
				out = append(out, Check{Level: "warn", Kind: "drift", CollectorID: r.CollectorID, Server: r.Server, Zone: z.Zone,
					Message: fmt.Sprintf("%s on %s is at serial %d; the primary is at %d", z.Zone, r.Server, r.Serial, *z.PrimarySerial)})
			}
			if r.Error != "" {
				out = append(out, Check{Level: "warn", Kind: "zone_error", CollectorID: r.CollectorID, Server: r.Server, Zone: z.Zone, Message: z.Zone + " on " + r.Server + ": " + r.Error})
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Level == "bad" && out[j].Level != "bad" })
	return out
}

// servfail returns the responses of one server over the last servfailSpan and
// the share of them that were SERVFAIL, in percent.
func (t *Type) servfail(ctx context.Context, userID, collectorID string, nowMs int64) (total, share float64, err error) {
	inc, err := readIncreases(ctx, t.TSDB, counterQuery{UserID: userID, CollectorID: collectorID, Items: []string{"response-code"},
		Start: nowMs - servfailSpan.Milliseconds(), End: nowMs, Step: servfailSpan.Milliseconds()})
	if err != nil {
		return 0, 0, err
	}
	total = inc.sumItem("response-code")
	if total > 0 {
		share = 100 * inc.total(seriesKey{"response-code", "SERVFAIL"}) / total
	}
	return total, share, nil
}

// serverNames maps the collector IDs of servers to their names.
func serverNames(servers []Server) map[string]string {
	m := make(map[string]string, len(servers))
	for _, s := range servers {
		m[s.CollectorID] = s.Name
	}
	return m
}
