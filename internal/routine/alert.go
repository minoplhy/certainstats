package routine

import (
	agentdata "certainstats/internal/agent_data"
	basealert "certainstats/internal/base/alert"
	"certainstats/internal/notifications"
	"certainstats/internal/store"
	"context"
	"fmt"
	"log"
	"time"
)

func (e *Routine) TriggerAlert(ctx context.Context, alert store.Alert, agentState basealert.AgentState, info store.AgentInfo, violationValue float64) error {
	historyID := fmt.Sprintf("alh_%d_%s", time.Now().UnixMicro(), agentdata.GenerateRandomString(8))

	now := time.Now()
	nctx := notifications.NotificationContext{
		AgentID:       agentState.AgentID,
		Nickname:      info.Nickname,
		TriggerType:   string(alert.Trigger.Type),
		Status:        "FIRING",
		Value:         violationValue,
		Operator:      string(alert.Trigger.Operator),
		Threshold:     alert.Trigger.Threshold,
		WentOfflineAt: &now,
	}

	actionToDispatch := alert.Action
	var targetID, targetName string
	if alert.Action.Type == basealert.DestPreset && alert.Action.TargetID != "" {
		target, err := e.Store.TargetGetByID(ctx, alert.Action.TargetID, alert.UserID)
		if err == nil {
			actionToDispatch.Type = target.Type
			actionToDispatch.Destination = target.Destination
			// Use action custom payload override if specified, otherwise target payload template
			if actionToDispatch.Payload == "" {
				actionToDispatch.Payload = target.Payload
			}
			targetID = target.TargetID
			targetName = target.Name
		} else {
			log.Printf("ALERT RESOLVE TARGET FAILED: target %s missing or unauthorized, falling back: %v", alert.Action.TargetID, err)
		}
	}

	// 1. Send the Notification (Webhook, Discord, etc.)
	notifErr := notifications.DispatchNotification(actionToDispatch, nctx)
	notifStatus := "success"
	if notifErr != nil {
		log.Printf("ALERT NOTIFY FAILED: %v", notifErr)
		notifStatus = "failed"
	}

	// 2. Database Updates with snapshot and denormalized columns
	err := e.Store.AlertTrigger(ctx, alert, agentState.AgentID, info.Nickname, historyID, violationValue, notifStatus, targetID, targetName)
	if err != nil {
		return err
	}

	log.Printf("ALERT TRIGGERED: Alert %s for Agent %s (%s). Value: %.2f", alert.AlertID, agentState.AgentID, info.Nickname, violationValue)
	return nil
}

func (e *Routine) ResolveAlert(ctx context.Context, alert store.Alert, agentState basealert.AgentState, info store.AgentInfo) error {
	now := time.Now()
	nctx := notifications.NotificationContext{
		AgentID:       agentState.AgentID,
		Nickname:      info.Nickname,
		TriggerType:   string(alert.Trigger.Type),
		Status:        "RESOLVED",
		Value:         0,
		Operator:      string(alert.Trigger.Operator),
		Threshold:     alert.Trigger.Threshold,
		WentOfflineAt: agentState.LastFiredAt,
		ResolvedAt:    &now,
	}

	actionToDispatch := alert.Action
	if alert.Action.Type == basealert.DestPreset && alert.Action.TargetID != "" {
		target, err := e.Store.TargetGetByID(ctx, alert.Action.TargetID, alert.UserID)
		if err == nil {
			actionToDispatch.Type = target.Type
			actionToDispatch.Destination = target.Destination
			// Use action custom payload override if specified, otherwise target payload template
			if actionToDispatch.Payload == "" {
				actionToDispatch.Payload = target.Payload
			}
		} else {
			log.Printf("ALERT RESOLVE TARGET FAILED: target %s missing or unauthorized, falling back: %v", alert.Action.TargetID, err)
		}
	}

	// 1. Send "Resolved" Notification
	notifErr := notifications.DispatchNotification(actionToDispatch, nctx)
	if notifErr != nil {
		log.Printf("ALERT RESOLVE NOTIFY FAILED: %v", notifErr)
	}

	err := e.Store.AlertResolve(ctx, alert, agentState.AgentID)
	if err != nil {
		return err
	}

	log.Printf("ALERT RESOLVED: Alert %s for Agent %s (%s).", alert.AlertID, agentState.AgentID, info.Nickname)
	return nil
}
