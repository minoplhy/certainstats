package crowdsec

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"certainstats/internal/base/alert"
	"certainstats/internal/collector"
	"certainstats/internal/crowdsec/wire"
	log "certainstats/internal/logger"
	"certainstats/internal/notifications"
)

// reportFresh is how old a collector's last health report may be and still be
// judged. Past it nothing is concluded, so a collector that died or had health
// switched off never sets off a round of notifications.
const reportFresh = 5 * time.Minute

// Targets reads the owner's notification targets, the ones alert rules use.
type Targets interface {
	TargetGetByID(ctx context.Context, targetID string, userID string) (alert.AlertTarget, error)
	TargetList(ctx context.Context, userID string) ([]alert.AlertTarget, error)
}

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

// Tick evaluates every check and opens or resolves incidents. It runs on the
// central timer. Notifications are sent in the background.
func (t *Type) Tick(ctx context.Context) {
	if err := t.Evaluate(ctx, time.Now().UnixMilli()); err != nil {
		log.Printf("[CrowdSec] health: %v", err)
	}
	if t.delivering.CompareAndSwap(false, true) {
		go func() {
			defer t.delivering.Store(false)
			t.Deliver(context.WithoutCancel(ctx))
		}()
	}
}

// Evaluate runs the checks for every collector that can be judged at now.
func (t *Type) Evaluate(ctx context.Context, now int64) error {
	list, err := t.Store.reportedHealth(ctx)
	if err != nil {
		return err
	}
	for _, h := range list {
		col, err := t.Collectors.Store.CollectorGet(ctx, h.CollectorID, h.UserID)
		if errors.Is(err, sql.ErrNoRows) {
			// The collector was removed: nothing about it is kept.
			if err := t.Store.dropCollectorHealth(ctx, h.CollectorID); err != nil {
				return err
			}
			continue
		}
		if err != nil {
			return err
		}
		if !t.judged(h, now) {
			continue
		}
		if err := t.evaluateCollector(ctx, h, readState(col).Machine, now); err != nil {
			return err
		}
	}
	return nil
}

func (t *Type) evaluateCollector(ctx context.Context, h *CollectorHealth, self string, now int64) error {
	comps, err := t.Store.collectorComponents(ctx, h.CollectorID)
	if err != nil {
		return err
	}
	open, err := t.Store.events(ctx, eventsOpenByCollector, h.CollectorID)
	if err != nil {
		return err
	}
	type key struct {
		component int64
		check     string
	}
	byKey := map[key]*HealthEvent{}
	for _, e := range open {
		byKey[key{e.ComponentID, e.Check}] = e
	}
	ec := evalContext{Settings: h.Checks, Active: h.Active, Parts: h.partSet(), Self: self, Now: now}
	for _, c := range comps {
		before := jsonText(c.Findings) + jsonText(c.Progress)
		for _, o := range c.evaluate(ec) {
			e := byKey[key{c.ID, o.Check.Name}]
			alerting := o.Finding != nil && o.Finding.Level == LevelAlert
			switch {
			case alerting && e == nil:
				if err := t.Store.openEvent(ctx, c, *o.Finding, h.TargetID, now); err != nil {
					return err
				}
			case !alerting && e != nil:
				if err := t.Store.resolveEvent(ctx, e, o.Recovered, now); err != nil {
					return err
				}
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

// Deliver sends every notification that is waiting, one attempt each.
func (t *Type) Deliver(ctx context.Context) {
	pending, err := t.Store.events(ctx, eventsPending)
	if err != nil {
		log.Printf("[CrowdSec] health notifications: %v", err)
		return
	}
	names := map[string]map[string]string{}
	for _, e := range pending {
		phase := "firing"
		if e.Firing != notifyPending {
			phase = "recovery"
		}
		if names[e.UserID] == nil {
			list, err := t.Collectors.Store.CollectorNames(ctx, e.UserID)
			if err != nil {
				log.Printf("[CrowdSec] health notifications: collector names: %v", err)
				list = map[string]string{}
			}
			names[e.UserID] = list
		}
		name := names[e.UserID][e.CollectorID]
		if name == "" {
			name = e.CollectorID // still says which collector, if its name is unavailable
		}
		sendErr := t.send(ctx, e, phase, name)
		if sendErr != nil {
			log.Printf("[CrowdSec] health notification for %s %s: %v", e.Kind, e.Name, sendErr)
		}
		if err := t.Store.recordDelivery(ctx, e, phase, sendErr); err != nil {
			log.Printf("[CrowdSec] health notifications: %v", err)
		}
	}
}

func (t *Type) send(ctx context.Context, e *HealthEvent, phase, collectorName string) error {
	if t.Targets == nil {
		return errors.New("notification targets are not available")
	}
	target, err := t.Targets.TargetGetByID(ctx, e.TargetID, e.UserID)
	if err != nil {
		return errors.New("the notification target no longer exists")
	}
	nctx := notifications.NotificationContext{
		AgentID: e.CollectorID, Nickname: e.Name, Collector: collectorName, TriggerType: "crowdsec_" + e.Check,
		Status: "FIRING", Value: e.Value, Detail: e.Detail, SubjectKind: e.Kind,
	}
	opened := time.UnixMilli(e.OpenedMs)
	nctx.WentOfflineAt = &opened
	if check := checks[e.Check]; check != nil {
		nctx.TriggerLabel = "CrowdSec: " + check.Label
		nctx.ValueText = fmt.Sprintf("%.0f %s", e.Value, check.Unit)
	}
	if phase == "recovery" {
		resolved := time.UnixMilli(e.ResolvedMs)
		nctx.Status, nctx.ResolvedAt, nctx.Value, nctx.ValueText = "RESOLVED", &resolved, 0, "Recovered"
	}
	action := alert.AlertAction{Type: target.Type, Destination: target.Destination, Payload: target.Payload}
	notify := t.Notify
	if notify == nil {
		notify = notifications.DispatchNotification
	}
	if err := notify(action, nctx); err != nil {
		// The destination is a secret: keep it out of the log and the page, as alert delivery does.
		return errors.New(strings.ReplaceAll(err.Error(), target.Destination, "[notification destination]"))
	}
	return nil
}
