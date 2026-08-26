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
	var notifErr error
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
			notifErr = fmt.Errorf("preset target %s missing or unauthorized: %w", alert.Action.TargetID, err)
		}
	}

	// 1. Database Updates with status = "pending" first
	// This ensures the alert history log and agent trigger status are updated immediately
	// regardless of notification speed or success/failure.
	err := e.Store.AlertTrigger(ctx, alert, agentState.AgentID, info.Nickname, historyID, violationValue, "pending", targetID, targetName, "")
	if err != nil {
		log.Printf("ALERT TRIGGER DB SAVE FAILED: %v", err)
		return err
	}

	// 2. Send the Notification (Webhook, Discord, etc.)
	if notifErr == nil {
		notifErr = notifications.DispatchNotification(actionToDispatch, nctx)
	}

	// 3. Update the DB statuses based on dispatch result
	if notifErr != nil {
		log.Printf("ALERT NOTIFY FAILED: %v", notifErr)
		_ = e.Store.AlertHistoryUpdateStatus(ctx, historyID, "failed", notifErr.Error())
		_ = e.Store.AlertAgentUpdateStatus(ctx, alert.AlertID, agentState.AgentID, "failed", notifErr.Error())
	} else {
		_ = e.Store.AlertHistoryUpdateStatus(ctx, historyID, "success", "")
		_ = e.Store.AlertAgentUpdateStatus(ctx, alert.AlertID, agentState.AgentID, "firing", "")
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
	var notifErr error
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
			notifErr = fmt.Errorf("preset target %s missing or unauthorized: %w", alert.Action.TargetID, err)
		}
	}

	// 1. Send "Resolved" Notification
	if notifErr == nil {
		notifErr = notifications.DispatchNotification(actionToDispatch, nctx)
	}
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

func (e *Routine) RetryFailedAlerts(ctx context.Context) {
	failedHistory, err := e.Store.AlertHistoryGetFailed(ctx)
	if err != nil {
		log.Printf("ALERT RETRY WORKER ERROR: %v", err)
		return
	}

	if len(failedHistory) == 0 {
		return
	}

	log.Printf("ALERT RETRY WORKER: Found %d failed alert notifications to retry", len(failedHistory))

	for _, history := range failedHistory {
		if history.ResolvedAt != nil {
			// Incident has already resolved, skip sending stale FIRING notification
			_ = e.Store.AlertHistoryUpdateStatus(ctx, history.HistoryID, "skipped", "Alert already resolved before retry")
			continue
		}

		// 1. Fetch corresponding alert details
		alertVal, err := e.Store.AlertGetInfo(ctx, history.AlertID, history.UserID)
		if err != nil {
			log.Printf("ALERT RETRY WORKER: Failed to fetch alert info for alert %s: %v", history.AlertID, err)
			continue
		}

		// 2. Construct Notification Context
		nctx := notifications.NotificationContext{
			AgentID:       history.AgentID,
			Nickname:      history.AgentNickname,
			TriggerType:   string(alertVal.Trigger.Type),
			Status:        "FIRING",
			Value:         history.TriggerValue,
			Operator:      string(alertVal.Trigger.Operator),
			Threshold:     alertVal.Trigger.Threshold,
			WentOfflineAt: &history.TriggeredAt,
		}

		// 3. Resolve destination / targets
		actionToDispatch := alertVal.Action
		var notifErr error
		if alertVal.Action.Type == basealert.DestPreset && alertVal.Action.TargetID != "" {
			target, err := e.Store.TargetGetByID(ctx, alertVal.Action.TargetID, history.UserID)
			if err == nil {
				actionToDispatch.Type = target.Type
				actionToDispatch.Destination = target.Destination
				if actionToDispatch.Payload == "" {
					actionToDispatch.Payload = target.Payload
				}
			} else {
				notifErr = fmt.Errorf("preset target %s missing or unauthorized: %w", alertVal.Action.TargetID, err)
			}
		}

		// 4. Dispatch notification
		if notifErr == nil {
			notifErr = notifications.DispatchNotification(actionToDispatch, nctx)
		}

		// 5. Update DB statuses
		if notifErr != nil {
			log.Printf("ALERT RETRY WORKER: Notification retry failed for history %s: %v", history.HistoryID, notifErr)
			_ = e.Store.AlertHistoryUpdateStatus(ctx, history.HistoryID, "failed", notifErr.Error())
			_ = e.Store.AlertAgentUpdateStatus(ctx, alertVal.AlertID, history.AgentID, "failed", notifErr.Error())
		} else {
			log.Printf("ALERT RETRY WORKER: Notification retry succeeded for history %s", history.HistoryID)
			_ = e.Store.AlertHistoryUpdateStatus(ctx, history.HistoryID, "success", "")
			_ = e.Store.AlertAgentUpdateStatus(ctx, alertVal.AlertID, history.AgentID, "firing", "")
		}
	}
}
