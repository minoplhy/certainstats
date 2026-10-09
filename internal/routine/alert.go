package routine

import (
	agentdata "certainstats/internal/agent_data"
	"certainstats/internal/alertdelivery"
	a "certainstats/internal/base/alert"
	"certainstats/internal/store"
	"context"
	"fmt"
	"log"
	"time"
)

func (e *Routine) TriggerAlert(ctx context.Context, rule store.Alert, state a.AgentState, info store.AgentInfo, value float64) error {
	id := fmt.Sprintf("alh_%d_%s", time.Now().UnixMicro(), agentdata.GenerateRandomString(8))
	targetID, targetName := rule.Action.TargetID, ""
	if targetID != "" {
		if target, err := e.Store.TargetGetByID(ctx, targetID, rule.UserID); err == nil {
			targetName = target.Name
		}
	}
	if err := e.Store.AlertTrigger(ctx, rule, state.AgentID, info.Nickname, id, value, "pending", targetID, targetName, ""); err != nil {
		return err
	}
	attempt, err := e.Store.AlertAttemptQueue(ctx, rule.UserID, id, "firing", "")
	if err != nil {
		return err
	}
	return alertdelivery.Dispatch(ctx, e.Store, attempt.EventID)
}
func (e *Routine) ResolveAlert(ctx context.Context, rule store.Alert, state a.AgentState, info store.AgentInfo) error {
	h, err := e.Store.AlertHistoryActive(ctx, rule.UserID, rule.AlertID, state.AgentID)
	if err != nil {
		return err
	}
	if err = e.Store.AlertResolve(ctx, rule, state.AgentID); err != nil {
		return err
	}
	attempt, err := e.Store.AlertAttemptQueue(ctx, rule.UserID, h.HistoryID, "recovery", "")
	if err != nil {
		return err
	}
	return alertdelivery.Dispatch(ctx, e.Store, attempt.EventID)
}
func (e *Routine) RetryFailedAlerts(ctx context.Context) {
	attempts, err := e.Store.AlertAttemptsFailed(ctx)
	if err != nil {
		log.Printf("Alert retries: %v", err)
		return
	}
	for _, attempt := range attempts {
		// The attempt lookup is owned by its incident; the worker is not user-facing.
		// Failed legacy records are handled below without fabricating old attempts.
		e.retryAttempt(ctx, attempt)
	}
	// Older databases have only a stored firing delivery result, not attempts.
	legacy, err := e.Store.AlertHistoryGetFailed(ctx)
	if err != nil {
		log.Printf("Legacy alert retries: %v", err)
		return
	}
	for _, h := range legacy {
		if h.ResolvedAt != nil || h.ClosedAt != nil {
			_ = e.Store.AlertHistoryUpdateStatus(ctx, h.HistoryID, "skipped", "Incident already ended before retry")
			continue
		}
		if !h.Legacy {
			continue
		}
		latest, err := alertdelivery.LatestAttempt(ctx, e.Store, h.UserID, h.HistoryID, "firing")
		if err != nil || latest != nil {
			continue
		}
		attempt, err := e.Store.AlertAttemptQueue(ctx, h.UserID, h.HistoryID, "firing", "")
		if err == nil {
			if err = alertdelivery.Dispatch(ctx, e.Store, attempt.EventID); err != nil {
				log.Printf("Legacy alert dispatch: %v", err)
			}
		}
	}
}
func (e *Routine) retryAttempt(ctx context.Context, attempt a.HistoryEvent) {
	// Find the owner through the failed-work query's durable attempt association.
	h, err := e.Store.AlertAttemptIncident(ctx, attempt.EventID)
	if err != nil {
		log.Printf("Alert retry lookup: %v", err)
		return
	}
	if h.ClosedAt != nil || (attempt.Phase == "firing" && h.ResolvedAt != nil) {
		if attempt.Phase == "firing" {
			_ = e.Store.AlertHistoryUpdateStatus(ctx, h.HistoryID, "skipped", "Incident already ended before retry")
		}
		return
	}
	queued, err := e.Store.AlertAttemptQueue(ctx, h.UserID, h.HistoryID, attempt.Phase, attempt.EventID)
	if err == nil {
		if err = alertdelivery.Dispatch(ctx, e.Store, queued.EventID); err != nil {
			log.Printf("Alert retry dispatch: %v", err)
		}
	}
}
