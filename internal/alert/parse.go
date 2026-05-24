package alert

import (
	"certainstats/internal/base/alert"
	"fmt"
	"strings"
)

// ParseTrigger validates and normalizes a Trigger.
func ParseTrigger(t *alert.Trigger) error {
	switch t.Type {
	case alert.TriggerTypeDown, alert.TriggerTypeCPU, alert.TriggerTypeCPUIOWait, alert.TriggerTypeCPUSteal,
		alert.TriggerTypeRAM, alert.TriggerTypeSwap, alert.TriggerTypeDisk, alert.TriggerTypeNetRx,
		alert.TriggerTypeNetTx, alert.TriggerTypeDiskRead, alert.TriggerTypeDiskWrite:
		// Valid
	default:
		return fmt.Errorf("alert: invalid trigger metric type: %q", t.Type)
	}

	if t.Type == alert.TriggerTypeDown {
		t.Operator = ""
		t.Threshold = 0
	} else {
		switch t.Operator {
		case alert.OpGreaterThan, alert.OpLessThan, alert.OpEquals:
			// Valid
		default:
			return fmt.Errorf("alert: invalid comparison operator: %q", t.Operator)
		}
	}
	return nil
}

// ParseAction validates and normalizes an AlertAction.
func ParseAction(a *alert.AlertAction) error {
	switch a.Type {
	case alert.DestWebhook, alert.DestDiscord:
		a.Destination = strings.TrimSpace(a.Destination)
		if a.Destination == "" {
			return fmt.Errorf("alert: destination URL is required for custom %s action", a.Type)
		}
		if !strings.HasPrefix(a.Destination, "http://") && !strings.HasPrefix(a.Destination, "https://") {
			return fmt.Errorf("alert: destination must be a valid HTTP or HTTPS URL")
		}
		a.TargetID = ""
	case alert.DestPreset:
		a.TargetID = strings.TrimSpace(a.TargetID)
		if a.TargetID == "" {
			return fmt.Errorf("alert: preset target ID is required")
		}
		a.Destination = ""
		a.Payload = ""
	default:
		return fmt.Errorf("alert: invalid action channel type: %q", a.Type)
	}
	return nil
}
