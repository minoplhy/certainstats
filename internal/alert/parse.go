package alert

import (
	"certainstats/internal/base/alert"
	"certainstats/internal/notifications"
	"fmt"
	"math"
	"strings"
)

// ParseTrigger validates and normalizes a Trigger.
func ParseTrigger(t *alert.Trigger) error {
	if st, ok := alert.SubjectTriggerFor(t.Type); ok {
		// The check does its own timing; only the threshold is the rule's.
		if math.IsNaN(t.Threshold) || math.IsInf(t.Threshold, 0) || t.Threshold < st.Min || t.Threshold > st.Max {
			return fmt.Errorf("%s: threshold must be between %g and %g", st.Label, st.Min, st.Max)
		}
		t.Operator = alert.OpGreaterThan
		t.Duration = "0s"
		return nil
	}
	if t.Type == alert.TriggerTypeNetworkLoss {
		if math.IsNaN(t.Threshold) || math.IsInf(t.Threshold, 0) || t.Threshold < 0 || t.Threshold > 100 {
			return fmt.Errorf("loss threshold must be between 0 and 100")
		}
		t.Operator = alert.OpGreaterThan
		t.Duration = "1h"
		return nil
	}
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
		if err := notifications.ValidateWebhook(a.Destination); err != nil {
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

// CheckSubjects says which subjects a rule must have for its trigger: network
// rules need monitors, CrowdSec rules need machines or bouncers, and every other
// rule watches servers. It returns the problem in words, or "".
func CheckSubjects(t alert.TriggerType, agents, monitors int, crowdsec []alert.CrowdSecSubject) string {
	switch {
	case alert.IsCrowdSecTrigger(t):
		if agents > 0 || monitors > 0 || len(crowdsec) == 0 {
			return "Select machines or bouncers for CrowdSec rules"
		}
		for _, s := range crowdsec {
			if _, ok := alert.ParseCrowdSecSubject(s.Key()); !ok {
				return "Invalid machine or bouncer selection"
			}
		}
	case t == alert.TriggerTypeNetworkLoss:
		if agents > 0 || monitors == 0 || len(crowdsec) > 0 {
			return "Select monitors for network rules, agents for resource rules"
		}
	default:
		if monitors > 0 || len(crowdsec) > 0 {
			return "Select monitors for network rules, agents for resource rules"
		}
	}
	return ""
}
