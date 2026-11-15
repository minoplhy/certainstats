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

// A subject that is not a node, such as a CrowdSec bouncer, carries its own
// label, value text and detail, and names its collector.
func TestSubjectNotification(t *testing.T) {
	opened := time.Date(2026, 11, 15, 10, 0, 0, 0, time.UTC)
	resolved := opened.Add(95 * time.Minute)
	nctx := NotificationContext{
		AgentID: "col_1", Nickname: `fw "edge"`, Collector: "lapi-01", SubjectKind: "bouncer", TriggerType: "crowdsec_pull",
		TriggerLabel: "CrowdSec: Bouncer pull", ValueText: "75 minutes", Detail: "No pull for 75m", Status: "FIRING", Value: 75, WentOfflineAt: &opened,
	}

	got := applyTemplate(`{"text":"{{SUBJECT_KIND}} {{NICKNAME}} on {{COLLECTOR}}: {{DETAIL}} ({{TRIGGER_LABEL}}, {{VALUE}})"}`, nctx, false)
	var decoded map[string]string
	if err := json.Unmarshal([]byte(got), &decoded); err != nil {
		t.Fatalf("template output must stay valid JSON: %v\n%s", err, got)
	}
	if decoded["text"] != `bouncer fw "edge" on lapi-01: No pull for 75m (CrowdSec: Bouncer pull, 75 minutes)` {
		t.Fatalf("template: %q", decoded["text"])
	}

	var body []byte
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { body, _ = io.ReadAll(r.Body) }))
	defer ts.Close()

	if err := sendWebhook(alert.AlertAction{Type: alert.DestWebhook, Destination: ts.URL}, nctx); err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatal(err)
	}
	if payload["collector"] != "lapi-01" || payload["subject_kind"] != "bouncer" || payload["detail"] != "No pull for 75m" || payload["nickname"] != `fw "edge"` || payload["trigger_type"] != "crowdsec_pull" {
		t.Fatalf("default payload %v", payload)
	}

	if err := sendDiscordWebhook(alert.AlertAction{Type: alert.DestDiscord, Destination: ts.URL}, nctx); err != nil {
		t.Fatal(err)
	}
	firing := string(body)
	for _, want := range []string{`"name":"Bouncer"`, `"name":"Collector","value":"lapi-01"`, "CrowdSec: Bouncer pull", "No pull for 75m", "Alert Triggered"} {
		if !strings.Contains(firing, want) {
			t.Fatalf("Discord embed lacks %s:\n%s", want, firing)
		}
	}
	if strings.Contains(firing, "Agent") {
		t.Fatalf("a bouncer is not a node:\n%s", firing)
	}

	nctx.Status, nctx.ResolvedAt = "RESOLVED", &resolved
	if err := sendDiscordWebhook(alert.AlertAction{Type: alert.DestDiscord, Destination: ts.URL}, nctx); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"RECOVERED", "1h 35m 0s", "Alert Resolved"} {
		if !strings.Contains(string(body), want) {
			t.Fatalf("recovery embed lacks %s:\n%s", want, body)
		}
	}

	// Node notifications are unchanged: no subject fields in the payload.
	if err := sendWebhook(alert.AlertAction{Type: alert.DestWebhook, Destination: ts.URL}, NotificationContext{AgentID: "a1", Nickname: "n", TriggerType: "cpu_usage", Status: "FIRING", Value: 91}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), "collector") || strings.Contains(string(body), "subject_kind") {
		t.Fatalf("node payload changed: %s", body)
	}
}
