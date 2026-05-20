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

	// 1. Send the Notification (Webhook, Discord, etc.)
	notifErr := notifications.DispatchNotification(alert.Action, nctx)
	notifStatus := "success"
	if notifErr != nil {
		log.Printf("ALERT NOTIFY FAILED: %v", notifErr)
		notifStatus = "failed"
	}

	// 2. Database Updates
	err := e.Store.AlertTrigger(ctx, alert, agentState.AgentID, historyID, violationValue, notifStatus)
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

	// 1. Send "Resolved" Notification
	notifErr := notifications.DispatchNotification(alert.Action, nctx)
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
