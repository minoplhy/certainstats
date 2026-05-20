package notifications

import (
	"bytes"
	"certainstats/internal/base/alert"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// DispatchNotification routes the alert to the correct sender based on the Action Type
func DispatchNotification(action alert.AlertAction, nctx NotificationContext) error {
	switch action.Type {
	case alert.DestWebhook:
		return sendWebhook(action, nctx)
	case alert.DestDiscord:
		return sendDiscordWebhook(action, nctx)
	default:
		return fmt.Errorf("unknown destination type: %s", action.Type)
	}
}

func getTriggerLabel(t string) string {
	switch t {
	case "agent_down":
		return "Node Down (Offline)"
	case "cpu_usage":
		return "CPU Usage"
	case "cpu_iowait":
		return "CPU IO Wait"
	case "cpu_steal":
		return "CPU Steal"
	case "ram_usage":
		return "RAM Usage"
	case "swap_usage":
		return "Swap Usage"
	case "disk_usage":
		return "Disk Usage"
	case "net_rx":
		return "Network In"
	case "net_tx":
		return "Network Out"
	case "disk_read":
		return "Disk Read"
	case "disk_write":
		return "Disk Write"
	default:
		return strings.Title(strings.ReplaceAll(t, "_", " "))
	}
}

func formatMetricValue(t string, val float64) string {
	switch t {
	case "agent_down":
		return "Offline"
	case "cpu_usage", "cpu_iowait", "cpu_steal", "ram_usage", "swap_usage", "disk_usage":
		return fmt.Sprintf("%.2f%%", val)
	case "net_rx", "net_tx", "disk_read", "disk_write":
		return fmt.Sprintf("%.2f KB/s", val)
	default:
		return fmt.Sprintf("%.2f", val)
	}
}

func formatDuration(d time.Duration) string {
	h := d / time.Hour
	d -= h * time.Hour
	m := d / time.Minute
	d -= m * time.Minute
	s := d / time.Second
	if h > 0 {
		return fmt.Sprintf("%dh %dm %ds", h, m, s)
	}
	if m > 0 {
		return fmt.Sprintf("%dm %ds", m, s)
	}
	return fmt.Sprintf("%ds", s)
}

func formatDiscordTime(t *time.Time) string {
	if t == nil {
		return "N/A"
	}
	u := t.Unix()
	return fmt.Sprintf("<t:%d:F> (<t:%d:R>)", u, u)
}

// applyTemplate replaces all known template variables in a custom payload string.
func applyTemplate(tmpl string, nctx NotificationContext, isDiscord bool) string {
	triggerTime := "N/A"
	if nctx.WentOfflineAt != nil {
		if isDiscord {
			triggerTime = formatDiscordTime(nctx.WentOfflineAt)
		} else {
			triggerTime = nctx.WentOfflineAt.Format(time.RFC3339)
		}
	}
	resolvedTime := "N/A"
	if nctx.ResolvedAt != nil {
		if isDiscord {
			resolvedTime = formatDiscordTime(nctx.ResolvedAt)
		} else {
			resolvedTime = nctx.ResolvedAt.Format(time.RFC3339)
		}
	}
	duration := "N/A"
	if nctx.Status == "FIRING" && nctx.TriggerType == "agent_down" {
		duration = "Evaluating..."
	} else if nctx.ResolvedAt != nil && nctx.WentOfflineAt != nil {
		duration = formatDuration(nctx.ResolvedAt.Sub(*nctx.WentOfflineAt))
	}

	r := strings.NewReplacer(
		"{{AGENT_ID}}", nctx.AgentID,
		"{{NICKNAME}}", nctx.Nickname,
		"{{TRIGGER_TYPE}}", nctx.TriggerType,
		"{{TRIGGER_LABEL}}", getTriggerLabel(nctx.TriggerType),
		"{{STATUS}}", nctx.Status,
		"{{VALUE}}", formatMetricValue(nctx.TriggerType, nctx.Value),
		"{{OPERATOR}}", nctx.Operator,
		"{{THRESHOLD}}", formatMetricValue(nctx.TriggerType, nctx.Threshold),
		"{{TIME_TRIGGER}}", triggerTime,
		"{{TIME_RESOLVED}}", resolvedTime,
		"{{DOWN_DURATION}}", duration,
	)
	return r.Replace(tmpl)
}

func sendDiscordWebhook(action alert.AlertAction, nctx NotificationContext) error {
	if action.Destination == "" {
		return fmt.Errorf("discord webhook destination URL is empty")
	}

	var payloadBytes []byte

	// 1. Check if the user provided a custom JSON template
	if strings.TrimSpace(action.Payload) != "" {
		payloadBytes = []byte(applyTemplate(action.Payload, nctx, true))
	} else {
		// 2. Default Discord Embed
		var color int
		var title string
		if nctx.Status == "FIRING" {
			color = 16711680 // Red
			title = "🚨 Alert Triggered"
		} else {
			color = 65280 // Green
			title = "✅ Alert Resolved"
		}

		triggerLabel := getTriggerLabel(nctx.TriggerType)
		var fields []map[string]interface{}

		if nctx.TriggerType == "agent_down" {
			wentOfflineStr := "N/A"
			if nctx.WentOfflineAt != nil {
				wentOfflineStr = formatDiscordTime(nctx.WentOfflineAt)
			}

			if nctx.Status == "FIRING" {
				fields = []map[string]interface{}{
					{
						"name":   "Agent Nickname",
						"value":  fmt.Sprintf("%s (`%s`)", nctx.Nickname, nctx.AgentID),
						"inline": false,
					},
					{
						"name":   "Status",
						"value":  "🚨 **OFFLINE / DOWN**",
						"inline": true,
					},
					{
						"name":   "Time Went Offline",
						"value":  wentOfflineStr,
						"inline": false,
					},
				}
			} else {
				timeUpStr := "N/A"
				if nctx.ResolvedAt != nil {
					timeUpStr = formatDiscordTime(nctx.ResolvedAt)
				}
				durationStr := "Unknown"
				if nctx.ResolvedAt != nil && nctx.WentOfflineAt != nil {
					durationStr = formatDuration(nctx.ResolvedAt.Sub(*nctx.WentOfflineAt))
				}

				fields = []map[string]interface{}{
					{
						"name":   "Agent Nickname",
						"value":  fmt.Sprintf("%s (`%s`)", nctx.Nickname, nctx.AgentID),
						"inline": false,
					},
					{
						"name":   "Status",
						"value":  "✅ **ONLINE / RECOVERED**",
						"inline": true,
					},
					{
						"name":   "Time Went Offline",
						"value":  wentOfflineStr,
						"inline": false,
					},
					{
						"name":   "Time Up",
						"value":  timeUpStr,
						"inline": false,
					},
					{
						"name":   "Down Duration",
						"value":  fmt.Sprintf("⏱️ **%s**", durationStr),
						"inline": true,
					},
				}
			}
		} else {
			// Standard metric triggers
			valueStr := formatMetricValue(nctx.TriggerType, nctx.Value)
			thresholdStr := formatMetricValue(nctx.TriggerType, nctx.Threshold)

			if nctx.Status == "FIRING" {
				fields = []map[string]interface{}{
					{
						"name":   "Agent",
						"value":  fmt.Sprintf("%s (`%s`)", nctx.Nickname, nctx.AgentID),
						"inline": false,
					},
					{
						"name":   "Trigger",
						"value":  triggerLabel,
						"inline": true,
					},
					{
						"name":   "Condition",
						"value":  fmt.Sprintf("%s %s", nctx.Operator, thresholdStr),
						"inline": true,
					},
					{
						"name":   "Current Value",
						"value":  valueStr,
						"inline": true,
					},
				}
			} else {
				fields = []map[string]interface{}{
					{
						"name":   "Agent",
						"value":  fmt.Sprintf("%s (`%s`)", nctx.Nickname, nctx.AgentID),
						"inline": false,
					},
					{
						"name":   "Trigger",
						"value":  triggerLabel,
						"inline": true,
					},
					{
						"name":   "State",
						"value":  "RESOLVED / OK",
						"inline": true,
					},
				}
			}
		}

		discordPayload := map[string]interface{}{
			"embeds": []map[string]interface{}{
				{
					"title":     title,
					"color":     color,
					"fields":    fields,
					"timestamp": time.Now().UTC().Format(time.RFC3339),
					"footer": map[string]interface{}{
						"text": "CertainStats",
					},
				},
			},
		}

		var err error
		payloadBytes, err = json.Marshal(discordPayload)
		if err != nil {
			return fmt.Errorf("failed to marshal discord payload: %w", err)
		}
	}

	req, err := http.NewRequest(http.MethodPost, action.Destination, bytes.NewBuffer(payloadBytes))
	if err != nil {
		return fmt.Errorf("failed to create discord request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("discord webhook request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("discord webhook returned non-2xx status code: %d", resp.StatusCode)
	}

	return nil
}

func sendWebhook(action alert.AlertAction, nctx NotificationContext) error {
	if action.Destination == "" {
		return fmt.Errorf("webhook destination URL is empty")
	}

	var payloadBytes []byte
	var err error

	// 1. Check if the user provided a custom JSON template
	if strings.TrimSpace(action.Payload) != "" {
		payloadBytes = []byte(applyTemplate(action.Payload, nctx, false))
	} else {
		// 2. Fallback to a standard JSON format if Payload is empty
		payloadBytes, err = json.Marshal(defaultPayload{
			AgentID:     nctx.AgentID,
			Nickname:    nctx.Nickname,
			TriggerType: nctx.TriggerType,
			Status:      nctx.Status,
			Value:       nctx.Value,
			Operator:    nctx.Operator,
			Threshold:   nctx.Threshold,
			Time:        time.Now().UTC().Format(time.RFC3339),
		})
		if err != nil {
			return fmt.Errorf("failed to marshal default payload: %w", err)
		}
	}

	// 3. Create and execute the HTTP POST request
	req, err := http.NewRequest(http.MethodPost, action.Destination, bytes.NewBuffer(payloadBytes))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "CertainStats-AlertEngine/1.0")

	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("webhook request failed: %w", err)
	}
	defer resp.Body.Close()

	// 4. Validate success
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("webhook returned non-2xx status code: %d", resp.StatusCode)
	}

	return nil
}
