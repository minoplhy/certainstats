package knot

import (
	"context"
	"fmt"
	"time"

	"certainstats/internal/base/alert"
	"certainstats/internal/collector"
	"certainstats/internal/collector/type/knot/wire"
	log "certainstats/internal/logger"
	"certainstats/internal/store"
)

// Knot DNS alert rules. A rule watches a server (alert.ScopeAll, which for zone
// conditions also means every zone of that server) or single zones (ScopeZone).
// The page's checks use the same measures with fixed limits; rules choose theirs.

// ScopeZone is one zone of one server.
const ScopeZone = "zone"

// Subject kinds a Knot condition applies to.
const (
	KindServer = "server"
	KindZone   = "zone"
)

// Knot DNS rule conditions.
const (
	TriggerUnreachable alert.TriggerType = "knot_unreachable"
	TriggerServfail    alert.TriggerType = "knot_servfail"
	TriggerDnstapQuiet alert.TriggerType = "knot_dnstap_quiet"
	TriggerExpiry      alert.TriggerType = "knot_expiry"
	TriggerDrift       alert.TriggerType = "knot_drift"
)

func init() {
	alert.RegisterSubjectSource(alert.SubjectSource{Name: wire.TypeName, Label: "Knot DNS", Scopes: []string{ScopeZone},
		Empty: "No collector reads Knot DNS yet. Add one with Knot DNS ticked on the Collectors page."})
	for _, t := range []alert.SubjectTrigger{
		{Type: TriggerUnreachable, Kind: KindServer, Unit: "minutes", Min: 1, Max: 1440, Default: 5,
			Label: "Knot unreachable or collector offline for (minutes)",
			Hint:  "Knot stopped answering on its control socket, or the collector stopped connecting."},
		{Type: TriggerServfail, Kind: KindServer, Unit: "%", Min: 0.1, Max: 100, Default: 1,
			Label: "SERVFAIL above (% of responses over 15 minutes)",
			Hint:  "From Knot's mod-stats counters; needs at least 100 responses in the 15 minutes."},
		{Type: TriggerDnstapQuiet, Kind: KindServer, Unit: "minutes", Min: 2, Max: 1440, Default: 5,
			Label: "No dnstap traffic for (minutes)",
			Hint:  "Only for collectors with KNOT_DNSTAP=true; says Knot's dnstap stream stopped reaching the collector."},
		{Type: TriggerExpiry, Kind: KindZone, Unit: "hours", Min: 1, Max: 720, Default: 24,
			Label: "Secondary zone expires within (hours)",
			Hint:  "Knot's expiry timer for a secondary zone: refreshes from its primary keep failing."},
		{Type: TriggerDrift, Kind: KindZone, Unit: "minutes", Min: 1, Max: 1440, Default: 15,
			Label: "Zone behind the primary's serial for (minutes)",
			Hint:  "Compares serials across your Knot servers; counts from when the primary's serial changed."},
	} {
		t.Source = wire.TypeName
		alert.RegisterSubjectTrigger(t)
	}
}

var _ collector.AlertSource = (*Type)(nil)

// AlertSubjects offers, per Knot server, the server itself and its zones.
func (t *Type) AlertSubjects(ctx context.Context, userID string) ([]alert.SubjectOptions, error) {
	servers, err := t.servers(ctx, userID)
	if err != nil {
		return nil, err
	}
	rows, err := t.Store.Zones(ctx, userID, "")
	if err != nil {
		return nil, err
	}
	zones := map[string][]string{}
	for _, z := range rows {
		zones[z.CollectorID] = append(zones[z.CollectorID], z.Zone)
	}
	out := []alert.SubjectOptions{}
	for _, s := range servers {
		if !s.Enabled {
			continue
		}
		o := alert.SubjectOptions{Source: wire.TypeName, CollectorID: s.CollectorID, CollectorName: s.Name,
			AllLabel: "Knot on " + s.Name + " (the server, and every zone for zone conditions)"}
		if list := zones[s.CollectorID]; len(list) > 0 {
			sec := alert.SubjectSection{Label: "Zones", Kind: KindZone}
			for _, z := range list {
				sec.Entries = append(sec.Entries, alert.SubjectEntry{Scope: ScopeZone, Name: z})
			}
			o.Sections = append(o.Sections, sec)
		}
		out = append(out, o)
	}
	return out, nil
}

// Tick evaluates every Knot DNS alert rule. It runs on the central timer.
func (t *Type) Tick(ctx context.Context) {
	if err := t.Evaluate(ctx, t.now()); err != nil {
		log.Printf("[Knot] alert rules: %v", err)
	}
}

// Evaluate opens and resolves Knot rule incidents. Incidents of a collector that
// is offline are held, apart from unreachable rules, which judge it; incidents
// of zones or collectors that are gone close quietly.
func (t *Type) Evaluate(ctx context.Context, now time.Time) error {
	if t.Rules == nil {
		return nil
	}
	round, err := collector.StartRound(ctx, t.Rules, wire.TypeName, t.Deliver)
	if err != nil {
		return err
	}
	for _, user := range round.Users() {
		servers, err := t.servers(ctx, user)
		if err != nil {
			return err
		}
		rows, err := t.Store.Zones(ctx, user, "")
		if err != nil {
			return err
		}
		byID := map[string]Server{}
		for _, s := range servers {
			byID[s.CollectorID] = s
		}
		views := zoneViews(rows, serverNames(servers), now.UnixMilli())
		last, err := t.Store.LastDnstap(ctx, user)
		if err != nil {
			return err
		}
		for _, s := range servers {
			if !s.Connected {
				round.Hold(s.CollectorID)
			}
		}
		for _, rule := range round.RulesOf(user) {
			for _, sub := range rule.Subjects {
				s, ok := byID[sub.CollectorID]
				if sub.Source != wire.TypeName || !ok || !s.Enabled {
					continue
				}
				s.LastDnstapMs = last[s.CollectorID]
				if err := t.judge(ctx, round, rule, sub, s, views, now); err != nil {
					return err
				}
			}
		}
	}
	return round.Finish(ctx)
}

func serverKey(collectorID string) string       { return collectorID + "/server" }
func zoneKey(collectorID, zone string) string   { return collectorID + "/zone/" + zone }
func minutesSince(nowMs, sinceMs int64) float64 { return float64(nowMs-sinceMs) / 60000 }

// judge applies one rule to one subject of one server.
func (t *Type) judge(ctx context.Context, round *collector.Round, rule store.Alert, sub alert.Subject, s Server, views []ZoneView, now time.Time) error {
	trig, ok := alert.SubjectTriggerFor(rule.Trigger.Type)
	if !ok {
		return nil
	}
	nowMs := now.UnixMilli()
	limit := rule.Trigger.Threshold
	if limit < trig.Min || limit > trig.Max {
		limit = trig.Default
	}
	snap := alert.SubjectSnapshot{Collector: s.Name, Kind: trig.Kind, Name: s.Name, Label: trig.Label, Unit: trig.Unit}
	if trig.Kind == KindServer {
		if sub.Scope != alert.ScopeAll {
			return nil
		}
		v := t.serverVerdict(ctx, rule, s, limit, nowMs)
		detail := v.Snapshot.Detail
		v.Snapshot = snap
		v.Snapshot.Detail = detail
		return round.Judge(ctx, rule, s.CollectorID, serverKey(s.CollectorID), v)
	}
	if !s.Connected {
		return nil // held: zone data is stale
	}
	for _, z := range views {
		if sub.Scope == ScopeZone && z.Zone != sub.Name {
			continue
		}
		for _, side := range z.Rows {
			if side.CollectorID != s.CollectorID {
				continue
			}
			v := zoneVerdict(rule.Trigger.Type, z, side, limit, nowMs)
			detail := v.Snapshot.Detail
			v.Snapshot = snap
			v.Snapshot.Name, v.Snapshot.Detail = z.Zone, detail
			if err := round.Judge(ctx, rule, s.CollectorID, zoneKey(s.CollectorID, z.Zone), v); err != nil {
				return err
			}
		}
	}
	return nil
}

// serverVerdict judges a server condition.
func (t *Type) serverVerdict(ctx context.Context, rule store.Alert, s Server, limit float64, nowMs int64) collector.Verdict {
	switch rule.Trigger.Type {
	case TriggerUnreachable:
		var since int64
		switch {
		case !s.Connected && s.FirstSeen == 0:
			return collector.Verdict{} // never connected: nothing to judge yet
		case !s.Connected:
			since = s.LastSeen
		case s.Status == "error" && s.ErrorSinceMs > 0:
			since = s.ErrorSinceMs
		default:
			return collector.Verdict{Judged: true}
		}
		down := minutesSince(nowMs, since)
		detail := "collector offline"
		if s.Connected {
			detail = "Knot unreachable: " + s.Error
		}
		return collector.Verdict{Judged: true, Bad: down >= limit, Value: down,
			Snapshot: alert.SubjectSnapshot{Detail: fmt.Sprintf("%s for %.0f minutes", detail, down)}}
	case TriggerServfail:
		if !s.Connected {
			return collector.Verdict{KeepUnjudged: true}
		}
		total, share, err := t.servfail(ctx, rule.UserID, s.CollectorID, nowMs)
		if err != nil {
			return collector.Verdict{KeepUnjudged: true}
		}
		if total < servfailMin {
			return collector.Verdict{Judged: true, Value: share}
		}
		return collector.Verdict{Judged: true, Bad: share > limit, Value: share,
			Snapshot: alert.SubjectSnapshot{Detail: fmt.Sprintf("SERVFAIL %.1f%% of %.0f responses in 15 minutes", share, total)}}
	case TriggerDnstapQuiet:
		if s.Dnstap == nil {
			return collector.Verdict{} // dnstap switched off: an open incident closes
		}
		if !s.Connected {
			return collector.Verdict{KeepUnjudged: true}
		}
		since := max(s.LastDnstapMs, s.ConnectedSince)
		quiet := minutesSince(nowMs, since)
		return collector.Verdict{Judged: true, Bad: quiet >= limit, Value: quiet,
			Snapshot: alert.SubjectSnapshot{Detail: fmt.Sprintf("no dnstap traffic for %.0f minutes", quiet)}}
	}
	return collector.Verdict{}
}

// zoneVerdict judges a zone condition on one server's copy of a zone.
func zoneVerdict(t alert.TriggerType, z ZoneView, side ZoneSide, limit float64, nowMs int64) collector.Verdict {
	switch t {
	case TriggerExpiry:
		if side.Role != "secondary" || side.ExpireAt == 0 {
			return collector.Verdict{Judged: true}
		}
		left := float64(side.ExpireAt-nowMs) / 3600000
		detail := fmt.Sprintf("expires in %.1f hours", left)
		if left <= 0 {
			detail = "expired"
		}
		return collector.Verdict{Judged: true, Bad: left < limit, Value: left, Snapshot: alert.SubjectSnapshot{Detail: detail}}
	case TriggerDrift:
		if !side.Lagging || z.PrimarySerial == nil {
			return collector.Verdict{Judged: true}
		}
		behind := minutesSince(nowMs, z.PrimaryChangedAt)
		return collector.Verdict{Judged: true, Bad: behind >= limit, Value: behind,
			Snapshot: alert.SubjectSnapshot{Detail: fmt.Sprintf("serial %d, primary at %d for %.0f minutes", side.Serial, *z.PrimarySerial, behind)}}
	}
	return collector.Verdict{}
}
