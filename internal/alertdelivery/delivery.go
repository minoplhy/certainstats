// Package alertdelivery shares durable delivery handling between the routine and
// HTTP retries. The database, rather than either caller, owns the attempt claim.
package alertdelivery

import (
	a "certainstats/internal/base/alert"
	"certainstats/internal/notifications"
	"certainstats/internal/store"
	"context"
	"fmt"
	"strings"
)

func Dispatch(ctx context.Context, db store.AlertsStore, eventID string) error {
	h, e, err := db.AlertAttemptStart(ctx, eventID)
	if err != nil {
		return err
	}
	status, message := "success", ""
	action, err := destination(ctx, db, h)
	if err == nil {
		// Resolve/delete may have happened while reading the destination.
		fresh, readErr := db.AlertHistoryGetByID(ctx, h.HistoryID, h.UserID)
		if readErr != nil {
			err = readErr
		} else if !fresh.MonitoringAvailable || (e.Phase == "firing" && fresh.ResolvedAt != nil) {
			return db.AlertAttemptComplete(ctx, eventID, "skipped", "Incident is no longer eligible")
		}
	}
	if err == nil {
		phase := "FIRING"
		value := h.TriggerValue
		if e.Phase == "recovery" {
			phase = "RESOLVED"
			value = 0
		}
		err = notifications.DispatchNotification(action, notifications.NotificationContext{
			MonitorID: h.MonitorID, MonitorTarget: h.Monitor.Target, MonitorProtocol: h.Monitor.Protocol, DNSServer: h.Monitor.Server,
			AgentID: h.AgentID, Nickname: h.AgentNickname, TriggerType: string(h.Trigger.Type), Status: phase, Value: value,
			Operator: string(h.Trigger.Operator), Threshold: h.Trigger.Threshold, WentOfflineAt: &h.TriggeredAt, ResolvedAt: h.ResolvedAt,
		})
	}
	if err != nil {
		status = "failed"
		message = err.Error()
		if action.Destination != "" {
			message = strings.ReplaceAll(message, action.Destination, "[notification destination]")
		}
	}
	// A delivery outcome must be saved even if the originating HTTP request ended.
	if saveErr := db.AlertAttemptComplete(context.WithoutCancel(ctx), eventID, status, message); saveErr != nil {
		return saveErr
	}
	return nil
}
func destination(ctx context.Context, db store.AlertsStore, h *a.AlertHistory) (a.AlertAction, error) {
	rule, err := db.AlertGetInfo(ctx, h.AlertID, h.UserID)
	if err != nil {
		return a.AlertAction{}, fmt.Errorf("original rule unavailable: %w", err)
	}
	action := rule.Action
	if h.TargetID != "" {
		target, err := db.TargetGetByID(ctx, h.TargetID, h.UserID)
		if err != nil {
			return action, fmt.Errorf("original notification target unavailable: %w", err)
		}
		action.Type = target.Type
		action.Destination = target.Destination
		// Overrides on a rule now using another destination do not belong to this target.
		if rule.Action.TargetID != h.TargetID || action.Payload == "" {
			action.Payload = target.Payload
		}
	} else if action.Type == a.DestPreset {
		return action, fmt.Errorf("original direct notification destination unavailable")
	}
	return action, nil
}

// LatestAttempt locates the latest phase event without assuming there are fewer
// attempts than one API page. Lifecycle events never qualify.
func LatestAttempt(ctx context.Context, db store.AlertsStore, user, id, phase string) (*a.HistoryEvent, error) {
	var latest *a.HistoryEvent
	for page := 1; ; page++ {
		events, total, err := db.AlertHistoryEvents(ctx, user, id, page, 100)
		if err != nil {
			return nil, err
		}
		for i := range events {
			if events[i].Kind == "notification" && events[i].Phase == phase {
				e := events[i]
				latest = &e
			}
		}
		if page*100 >= total {
			return latest, nil
		}
	}
}
