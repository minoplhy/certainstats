package notifications

import (
	"certainstats/internal/base/alert"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestGetTriggerLabel(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"agent_down", "Node Down (Offline)"},
		{"cpu_usage", "CPU Usage"},
		{"cpu_iowait", "CPU IO Wait"},
		{"cpu_steal", "CPU Steal"},
		{"ram_usage", "RAM Usage"},
		{"swap_usage", "Swap Usage"},
		{"disk_usage", "Disk Usage"},
		{"net_rx", "Network In"},
		{"net_tx", "Network Out"},
		{"disk_read", "Disk Read"},
		{"disk_write", "Disk Write"},
		{"custom_metric_test", "Custom Metric Test"},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			actual := getTriggerLabel(tc.input)
			if actual != tc.expected {
				t.Errorf("expected %q, got %q", tc.expected, actual)
			}
		})
	}
}

func TestFormatMetricValue(t *testing.T) {
	tests := []struct {
		t        string
		val      float64
		expected string
	}{
		{"agent_down", 0.0, "Offline"},
		{"cpu_usage", 82.35, "82.35%"},
		{"ram_usage", 45.1, "45.10%"},
		{"net_rx", 1024.56, "1024.56 KB/s"},
		{"disk_write", 50.0, "50.00 KB/s"},
		{"other_metric", 9.99, "9.99"},
	}

	for _, tc := range tests {
		t.Run(tc.t, func(t *testing.T) {
			actual := formatMetricValue(tc.t, tc.val)
			if actual != tc.expected {
				t.Errorf("expected %q, got %q", tc.expected, actual)
			}
		})
	}
}

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		d        time.Duration
		expected string
	}{
		{2*time.Hour + 3*time.Minute + 4*time.Second, "2h 3m 4s"},
		{5*time.Minute + 6*time.Second, "5m 6s"},
		{7 * time.Second, "7s"},
	}

	for _, tc := range tests {
		t.Run(tc.expected, func(t *testing.T) {
			actual := formatDuration(tc.d)
			if actual != tc.expected {
				t.Errorf("expected %q, got %q", tc.expected, actual)
			}
		})
	}
}

func TestFormatDiscordTime(t *testing.T) {
	if actual := formatDiscordTime(nil); actual != "N/A" {
		t.Errorf("expected N/A for nil, got %q", actual)
	}

	trigTime := time.Unix(1718021000, 0)
	actual := formatDiscordTime(&trigTime)
	expected := "<t:1718021000:F> (<t:1718021000:R>)"
	if actual != expected {
		t.Errorf("expected %q, got %q", expected, actual)
	}
}

func TestApplyTemplate(t *testing.T) {
	t1 := time.Unix(1718021000, 0)
	t2 := time.Unix(1718021600, 0)
	nctx := NotificationContext{
		AgentID:       "agent-1",
		Nickname:      "MyServer",
		TriggerType:   "cpu_usage",
		Status:        "FIRING",
		Value:         90.0,
		Operator:      ">",
		Threshold:     80.0,
		WentOfflineAt: &t1,
		ResolvedAt:    &t2,
	}

	tmpl := "Agent {{NICKNAME}} ({{AGENT_ID}}) is {{STATUS}}. {{TRIGGER_LABEL}} is {{VALUE}} {{OPERATOR}} {{THRESHOLD}}. Down: {{DOWN_DURATION}}."
	
	// Test without Discord time
	resNormal := applyTemplate(tmpl, nctx, false)
	if !strings.Contains(resNormal, "Agent MyServer (agent-1) is FIRING.") {
		t.Errorf("incorrect templating: %q", resNormal)
	}
	if !strings.Contains(resNormal, "CPU Usage is 90.00% > 80.00%") {
		t.Errorf("incorrect values: %q", resNormal)
	}
	if !strings.Contains(resNormal, "Down: 10m 0s") {
		t.Errorf("incorrect duration: %q", resNormal)
	}

	// Test special FIRING agent_down duration
	downCtx := nctx
	downCtx.TriggerType = "agent_down"
	resDown := applyTemplate("Duration: {{DOWN_DURATION}}", downCtx, false)
	if !strings.Contains(resDown, "Duration: Evaluating...") {
		t.Errorf("expected Evaluating..., got %q", resDown)
	}
}

func TestSendWebhook(t *testing.T) {
	t.Run("default payload success", func(t *testing.T) {
		var receivedBody []byte
		var receivedHeaders http.Header

		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			receivedHeaders = r.Header
			var err error
			receivedBody, err = io.ReadAll(r.Body)
			if err != nil {
				t.Fatalf("failed to read body: %v", err)
			}
			w.WriteHeader(http.StatusOK)
		}))
		defer ts.Close()

		nctx := NotificationContext{
			AgentID:     "agent-99",
			Nickname:    "Server99",
			TriggerType: "cpu_usage",
			Status:      "FIRING",
			Value:       95.5,
			Operator:    ">",
			Threshold:   90.0,
		}

		action := alert.AlertAction{
			Destination: ts.URL,
		}

		err := sendWebhook(action, nctx)
		if err != nil {
			t.Fatalf("sendWebhook failed: %v", err)
		}

		if receivedHeaders.Get("Content-Type") != "application/json" {
			t.Errorf("expected json content type, got %q", receivedHeaders.Get("Content-Type"))
		}
		if receivedHeaders.Get("User-Agent") != "CertainStats-AlertEngine/1.0" {
			t.Errorf("expected custom User-Agent, got %q", receivedHeaders.Get("User-Agent"))
		}

		var payload defaultPayload
		if err := json.Unmarshal(receivedBody, &payload); err != nil {
			t.Fatalf("failed to unmarshal request body: %v. Body: %s", err, string(receivedBody))
		}

		if payload.AgentID != "agent-99" || payload.Nickname != "Server99" || payload.Value != 95.5 {
			t.Errorf("unexpected payload values: %+v", payload)
		}
	})

	t.Run("custom payload success", func(t *testing.T) {
		var receivedBody []byte

		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var err error
			receivedBody, err = io.ReadAll(r.Body)
			if err != nil {
				t.Fatalf("failed to read body: %v", err)
			}
			w.WriteHeader(http.StatusOK)
		}))
		defer ts.Close()

		nctx := NotificationContext{
			AgentID:     "agent-99",
			Nickname:    "Server99",
			TriggerType: "cpu_usage",
			Status:      "FIRING",
		}

		action := alert.AlertAction{
			Destination: ts.URL,
			Payload:     `{"message": "Agent {{NICKNAME}} is down"}`,
		}

		err := sendWebhook(action, nctx)
		if err != nil {
			t.Fatalf("sendWebhook failed: %v", err)
		}

		expected := `{"message": "Agent Server99 is down"}`
		if string(receivedBody) != expected {
			t.Errorf("expected %q, got %q", expected, string(receivedBody))
		}
	})

	t.Run("empty URL error", func(t *testing.T) {
		action := alert.AlertAction{Destination: ""}
		err := sendWebhook(action, NotificationContext{})
		if err == nil || !strings.Contains(err.Error(), "webhook destination URL is empty") {
			t.Errorf("expected empty URL error, got %v", err)
		}
	})

	t.Run("non-2xx response status error", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer ts.Close()

		action := alert.AlertAction{Destination: ts.URL}
		err := sendWebhook(action, NotificationContext{})
		if err == nil || !strings.Contains(err.Error(), "webhook returned non-2xx status code: 500") {
			t.Errorf("expected status code error, got %v", err)
		}
	})
}

func TestSendDiscordWebhook(t *testing.T) {
	t.Run("custom template success", func(t *testing.T) {
		var receivedBody []byte

		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var err error
			receivedBody, err = io.ReadAll(r.Body)
			if err != nil {
				t.Fatalf("failed to read body: %v", err)
			}
			w.WriteHeader(http.StatusNoContent)
		}))
		defer ts.Close()

		nctx := NotificationContext{
			AgentID:     "agent-1",
			Nickname:    "Node1",
			TriggerType: "cpu_usage",
			Status:      "FIRING",
		}

		action := alert.AlertAction{
			Destination: ts.URL,
			Payload:     `{"content": "Discord alert: {{NICKNAME}} status is {{STATUS}}"}`,
		}

		err := sendDiscordWebhook(action, nctx)
		if err != nil {
			t.Fatalf("sendDiscordWebhook failed: %v", err)
		}

		expected := `{"content": "Discord alert: Node1 status is FIRING"}`
		if string(receivedBody) != expected {
			t.Errorf("expected %q, got %q", expected, string(receivedBody))
		}
	})

	t.Run("default embed firing agent_down", func(t *testing.T) {
		var receivedBody []byte

		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var err error
			receivedBody, err = io.ReadAll(r.Body)
			if err != nil {
				t.Fatalf("failed to read body: %v", err)
			}
			w.WriteHeader(http.StatusNoContent)
		}))
		defer ts.Close()

		wentOffline := time.Unix(1718021000, 0)
		nctx := NotificationContext{
			AgentID:       "agent-1",
			Nickname:      "Node1",
			TriggerType:   "agent_down",
			Status:        "FIRING",
			WentOfflineAt: &wentOffline,
		}

		action := alert.AlertAction{
			Destination: ts.URL,
		}

		err := sendDiscordWebhook(action, nctx)
		if err != nil {
			t.Fatalf("sendDiscordWebhook failed: %v", err)
		}

		var payload map[string]interface{}
		if err := json.Unmarshal(receivedBody, &payload); err != nil {
			t.Fatalf("failed to parse json: %v", err)
		}

		embeds, ok := payload["embeds"].([]interface{})
		if !ok || len(embeds) == 0 {
			t.Fatalf("invalid embeds in payload: %+v", payload)
		}

		embed := embeds[0].(map[string]interface{})
		if embed["title"] != "🚨 Alert Triggered" {
			t.Errorf("expected firing title, got %q", embed["title"])
		}

		fields := embed["fields"].([]interface{})
		// Fields: Agent Nickname, Status, Time Went Offline
		if len(fields) != 3 {
			t.Errorf("expected 3 fields, got %d", len(fields))
		}
	})

	t.Run("default embed resolved agent_down", func(t *testing.T) {
		var receivedBody []byte

		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var err error
			receivedBody, err = io.ReadAll(r.Body)
			if err != nil {
				t.Fatalf("failed to read body: %v", err)
			}
			w.WriteHeader(http.StatusNoContent)
		}))
		defer ts.Close()

		wentOffline := time.Unix(1718021000, 0)
		resolvedAt := time.Unix(1718021600, 0)
		nctx := NotificationContext{
			AgentID:       "agent-1",
			Nickname:      "Node1",
			TriggerType:   "agent_down",
			Status:        "RESOLVED",
			WentOfflineAt: &wentOffline,
			ResolvedAt:    &resolvedAt,
		}

		action := alert.AlertAction{
			Destination: ts.URL,
		}

		err := sendDiscordWebhook(action, nctx)
		if err != nil {
			t.Fatalf("sendDiscordWebhook failed: %v", err)
		}

		var payload map[string]interface{}
		if err := json.Unmarshal(receivedBody, &payload); err != nil {
			t.Fatalf("failed to parse json: %v", err)
		}

		embeds := payload["embeds"].([]interface{})
		embed := embeds[0].(map[string]interface{})
		if embed["title"] != "✅ Alert Resolved" {
			t.Errorf("expected resolved title, got %q", embed["title"])
		}

		fields := embed["fields"].([]interface{})
		// Fields: Agent Nickname, Status, Time Went Offline, Time Up, Down Duration
		if len(fields) != 5 {
			t.Errorf("expected 5 fields, got %d", len(fields))
		}
	})

	t.Run("default embed firing metric", func(t *testing.T) {
		var receivedBody []byte

		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var err error
			receivedBody, err = io.ReadAll(r.Body)
			if err != nil {
				t.Fatalf("failed to read body: %v", err)
			}
			w.WriteHeader(http.StatusNoContent)
		}))
		defer ts.Close()

		nctx := NotificationContext{
			AgentID:     "agent-1",
			Nickname:    "Node1",
			TriggerType: "cpu_usage",
			Status:      "FIRING",
			Value:       99.1,
			Operator:    ">",
			Threshold:   90.0,
		}

		action := alert.AlertAction{
			Destination: ts.URL,
		}

		err := sendDiscordWebhook(action, nctx)
		if err != nil {
			t.Fatalf("sendDiscordWebhook failed: %v", err)
		}

		var payload map[string]interface{}
		if err := json.Unmarshal(receivedBody, &payload); err != nil {
			t.Fatalf("failed to parse json: %v", err)
		}

		embeds := payload["embeds"].([]interface{})
		embed := embeds[0].(map[string]interface{})
		fields := embed["fields"].([]interface{})
		// Fields: Agent, Trigger, Condition, Current Value
		if len(fields) != 4 {
			t.Errorf("expected 4 fields, got %d", len(fields))
		}
	})

	t.Run("default embed resolved metric", func(t *testing.T) {
		var receivedBody []byte

		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var err error
			receivedBody, err = io.ReadAll(r.Body)
			if err != nil {
				t.Fatalf("failed to read body: %v", err)
			}
			w.WriteHeader(http.StatusNoContent)
		}))
		defer ts.Close()

		nctx := NotificationContext{
			AgentID:     "agent-1",
			Nickname:    "Node1",
			TriggerType: "cpu_usage",
			Status:      "RESOLVED",
		}

		action := alert.AlertAction{
			Destination: ts.URL,
		}

		err := sendDiscordWebhook(action, nctx)
		if err != nil {
			t.Fatalf("sendDiscordWebhook failed: %v", err)
		}

		var payload map[string]interface{}
		if err := json.Unmarshal(receivedBody, &payload); err != nil {
			t.Fatalf("failed to parse json: %v", err)
		}

		embeds := payload["embeds"].([]interface{})
		embed := embeds[0].(map[string]interface{})
		fields := embed["fields"].([]interface{})
		// Fields: Agent, Trigger, State
		if len(fields) != 3 {
			t.Errorf("expected 3 fields, got %d", len(fields))
		}
	})

	t.Run("empty URL error", func(t *testing.T) {
		action := alert.AlertAction{Destination: ""}
		err := sendDiscordWebhook(action, NotificationContext{})
		if err == nil || !strings.Contains(err.Error(), "discord webhook destination URL is empty") {
			t.Errorf("expected empty URL error, got %v", err)
		}
	})

	t.Run("non-2xx response status error", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusBadRequest)
		}))
		defer ts.Close()

		action := alert.AlertAction{Destination: ts.URL}
		err := sendDiscordWebhook(action, NotificationContext{})
		if err == nil || !strings.Contains(err.Error(), "discord webhook returned non-2xx status code: 400") {
			t.Errorf("expected status code error, got %v", err)
		}
	})
}

func TestDispatchNotification(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	t.Run("webhook dispatch", func(t *testing.T) {
		action := alert.AlertAction{
			Type:        alert.DestWebhook,
			Destination: ts.URL,
		}
		err := DispatchNotification(action, NotificationContext{})
		if err != nil {
			t.Errorf("failed to dispatch webhook: %v", err)
		}
	})

	t.Run("discord dispatch", func(t *testing.T) {
		action := alert.AlertAction{
			Type:        alert.DestDiscord,
			Destination: ts.URL,
		}
		err := DispatchNotification(action, NotificationContext{})
		if err != nil {
			t.Errorf("failed to dispatch discord webhook: %v", err)
		}
	})

	t.Run("unknown dispatch type", func(t *testing.T) {
		action := alert.AlertAction{
			Type:        alert.DestinationType("unknown"),
			Destination: ts.URL,
		}
		err := DispatchNotification(action, NotificationContext{})
		if err == nil || !strings.Contains(err.Error(), "unknown destination type") {
			t.Errorf("expected unknown destination type error, got %v", err)
		}
	})
}
