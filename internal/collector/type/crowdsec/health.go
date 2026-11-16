package crowdsec

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"certainstats/internal/base/alert"
	"certainstats/internal/collector"
	"certainstats/internal/collector/type/crowdsec/wire"
	log "certainstats/internal/logger"
	"certainstats/internal/store"
)

// reportFresh is how old a collector's last health report may be and still be
// judged. Past it nothing is concluded, so a collector that died or had health
// switched off never sets off a round of notifications.
const reportFresh = 5 * time.Minute

// health stores a cs_health report. Only a host that set CROWDSEC_HEALTH=true sends one.
func (t *Type) health(ctx context.Context, c *collector.Conn, m wire.Health) error {
	if len(m.Components) > wire.MaxComponents {
		return fmt.Errorf("health report of %d components exceeds %d", len(m.Components), wire.MaxComponents)
	}
	if len(m.Error) > 500 {
		m.Error = m.Error[:500]
	}
	return t.Store.SaveReport(ctx, c.Collector.UserID, c.Collector.CollectorID, m, time.Now().UnixMilli())
}

// judged reports whether a collector's components can be judged now: it is
// connected with CrowdSec enabled, and its last report is recent and error-free.
func (t *Type) judged(h *CollectorHealth, now int64) bool {
	_, connected := t.connected(h.CollectorID)
	return connected && h.Error == "" && h.ReportedMs > 0 && now-h.ReportedMs <= reportFresh.Milliseconds()
}

// connected reports whether a collector is connected with CrowdSec enabled, and since when.
func (t *Type) connected(collectorID string) (time.Time, bool) {
	if t.live != nil {
		return t.live(collectorID)
	}
	conn := t.Collectors.Conn(collectorID)
	if conn == nil || !conn.Enabled(wire.TypeName) {
		return time.Time{}, false
	}
	return conn.Since, true
}

// Tick evaluates every check and every CrowdSec alert rule. It runs on the central timer.
func (t *Type) Tick(ctx context.Context) {
	if err := t.Evaluate(ctx, time.Now().UnixMilli()); err != nil {
		log.Printf("[CrowdSec] health: %v", err)
	}
}

// Evaluate refreshes every judged collector's findings, then opens and resolves
// rule incidents through a collector.Round. An open incident is closed quietly
// when nothing keeps it: its rule is gone, disabled or no longer covers the
// component, the component was muted or removed, or its collector was removed.
// Incidents of a collector that cannot be judged right now are left as they are.
func (t *Type) Evaluate(ctx context.Context, now int64) error {
	list, err := t.Store.reportedHealth(ctx)
	if err != nil {
		return err
	}
	var rules collector.Rules
	if t.Rules != nil {
		rules = t.Rules
	}
	round, err := collector.StartRound(ctx, rules, wire.TypeName, t.Deliver)
	if err != nil {
		return err
	}
	for _, h := range list {
		col, err := t.Collectors.Store.CollectorGet(ctx, h.CollectorID, h.UserID)
		if errors.Is(err, sql.ErrNoRows) {
			// The collector was removed: nothing about it is kept, and its incidents close below.
			if err := t.Store.dropCollectorHealth(ctx, h.CollectorID); err != nil {
				return err
			}
			continue
		}
		if err != nil {
			return err
		}
		if !t.judged(h, now) {
			round.Hold(h.CollectorID)
			continue
		}
		if err := t.evaluateCollector(ctx, h, col, round, now); err != nil {
			return err
		}
	}
	return round.Finish(ctx)
}

func (t *Type) evaluateCollector(ctx context.Context, h *CollectorHealth, col *store.Collector, round *collector.Round, now int64) error {
	comps, err := t.Store.collectorComponents(ctx, h.CollectorID)
	if err != nil {
		return err
	}
	rules := round.RulesOf(h.UserID)
	ec := evalContext{Active: h.Active, Parts: h.partSet(), Self: readState(col).Machine, Groups: groupBouncers(comps), Now: now}
	for _, c := range comps {
		before := jsonText(c.Findings) + jsonText(c.Progress)
		c.evaluate(ec)
		for _, rule := range rules {
			if err := t.evaluateRule(ctx, rule, c, col.Name, ec, round); err != nil {
				return err
			}
		}
		if jsonText(c.Findings)+jsonText(c.Progress) != before {
			if err := t.Store.saveEvaluation(ctx, c); err != nil {
				return err
			}
		}
	}
	return nil
}

func (t *Type) evaluateRule(ctx context.Context, rule store.Alert, c *Component, collectorName string, ec evalContext, round *collector.Round) error {
	check := checkForTrigger(rule.Trigger.Type)
	if check == nil || check.Kind != c.Kind {
		return nil
	}
	covered := false
	for _, s := range rule.Subjects {
		covered = covered || c.matches(s)
	}
	if !covered {
		return nil
	}
	r := c.run(check, check.ruleValues(rule.Trigger.Threshold), ec, "rule:"+rule.AlertID)
	// Nothing to go on (never seen, metrics missing): an open incident stays,
	// unless the component was muted, which ends it.
	return round.Judge(ctx, rule, c.CollectorID, c.key(), collector.Verdict{
		Judged: r.Judged, Bad: r.Bad, KeepUnjudged: !c.skipped(ec), Value: r.Value,
		Snapshot: alert.SubjectSnapshot{Collector: collectorName, Kind: c.Kind, Name: c.Name, Label: check.Label, Detail: r.Detail, Unit: check.Unit},
	})
}
