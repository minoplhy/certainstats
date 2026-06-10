package accessrules

import (
	"strings"
	"testing"
)

func TestParse(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		expectErr bool
		errMsg    string
	}{
		{
			name:      "Valid rule",
			input:     `{"allowed_fields": ["is_online", "uptime"], "allowed_metrics": ["agent_cpu_usage"], "max_days": 30}`,
			expectErr: false,
		},
		{
			name:      "Zero max_days",
			input:     `{"allowed_fields": ["is_online"], "max_days": 0}`,
			expectErr: true,
			errMsg:    "max_days must be greater than 0",
		},
		{
			name:      "Invalid max_days quick range value",
			input:     `{"allowed_fields": ["is_online"], "max_days": 5}`,
			expectErr: true,
			errMsg:    "max_days must be one of the selective quick ranges",
		},
		{
			name:      "Unknown feature",
			input:     `{"allowed_fields": ["is_online", "unknown_field_name"], "max_days": 30}`,
			expectErr: true,
			errMsg:    `unknown feature "unknown_field_name"`,
		},
		{
			name:      "Unknown metric",
			input:     `{"allowed_metrics": ["unknown_metric_name"], "max_days": 30}`,
			expectErr: true,
			errMsg:    `unknown metric "unknown_metric_name"`,
		},
		{
			name:      "Invalid JSON syntax",
			input:     `{invalid}`,
			expectErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rule, err := Parse(tc.input)
			if tc.expectErr {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tc.errMsg)
				}
				if tc.errMsg != "" && !strings.Contains(err.Error(), tc.errMsg) {
					t.Errorf("expected error %q to contain %q", err.Error(), tc.errMsg)
				}
			} else {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if rule.MaxDays != 30 {
					t.Errorf("expected MaxDays to be 30, got %d", rule.MaxDays)
				}
				if len(rule.AllowedFeatures) != 2 || rule.AllowedFeatures[0] != "is_online" {
					t.Errorf("unexpected AllowedFeatures: %v", rule.AllowedFeatures)
				}
			}
		})
	}
}

func TestParseRules(t *testing.T) {
	t.Run("Valid rules map", func(t *testing.T) {
		input := `{
			"admin": {"allowed_fields": ["is_online"], "allowed_metrics": ["agent_cpu_usage"], "max_days": 365},
			"viewer": {"allowed_fields": ["is_online"], "max_days": 7}
		}`
		rules, err := ParseRules(input)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(rules) != 2 {
			t.Errorf("expected 2 rules, got %d", len(rules))
		}

		adminRule, ok := rules["admin"]
		if !ok {
			t.Fatal("expected admin rule to exist")
		}
		if adminRule.MaxDays != 365 {
			t.Errorf("expected admin max_days 365, got %d", adminRule.MaxDays)
		}

		viewerRule, ok := rules["viewer"]
		if !ok {
			t.Fatal("expected viewer rule to exist")
		}
		if viewerRule.MaxDays != 7 {
			t.Errorf("expected viewer max_days 7, got %d", viewerRule.MaxDays)
		}
	})

	t.Run("Invalid role JSON payload", func(t *testing.T) {
		input := `{
			"admin": {"allowed_fields": ["is_online"], "max_days": 5}
		}`
		_, err := ParseRules(input)
		if err == nil {
			t.Fatal("expected error parsing invalid max_days in role, got nil")
		}
		if !strings.Contains(err.Error(), "role \"admin\"") {
			t.Errorf("expected error message to specify role \"admin\", got %q", err.Error())
		}
	})

	t.Run("Invalid overall JSON syntax", func(t *testing.T) {
		_, err := ParseRules(`{invalid}`)
		if err == nil {
			t.Fatal("expected error parsing invalid json, got nil")
		}
	})
}

func TestAccessRuleMethods(t *testing.T) {
	t.Run("IsEmpty", func(t *testing.T) {
		emptyRule := AccessRule{
			AllowedFeatures: []string{},
			AllowedMetrics:  []string{},
			MaxDays:         30,
		}
		if !emptyRule.IsEmpty() {
			t.Error("expected IsEmpty to return true for empty lists")
		}

		nonEmptyFeatures := AccessRule{
			AllowedFeatures: []string{"is_online"},
			MaxDays:         30,
		}
		if nonEmptyFeatures.IsEmpty() {
			t.Error("expected IsEmpty to return false when features are populated")
		}

		nonEmptyMetrics := AccessRule{
			AllowedMetrics: []string{"agent_cpu_usage"},
			MaxDays:        30,
		}
		if nonEmptyMetrics.IsEmpty() {
			t.Error("expected IsEmpty to return false when metrics are populated")
		}
	})

	t.Run("FeatureSet and MetricSet", func(t *testing.T) {
		rule := AccessRule{
			AllowedFeatures: []string{"is_online", "uptime"},
			AllowedMetrics:  []string{"agent_cpu_usage"},
			MaxDays:         30,
		}

		fSet := rule.FeatureSet()
		if len(fSet) != 2 {
			t.Errorf("expected FeatureSet size 2, got %d", len(fSet))
		}
		if _, ok := fSet["is_online"]; !ok {
			t.Error("expected is_online to be in FeatureSet")
		}

		mSet := rule.MetricSet()
		if len(mSet) != 1 {
			t.Errorf("expected MetricSet size 1, got %d", len(mSet))
		}
		if _, ok := mSet["agent_cpu_usage"]; !ok {
			t.Error("expected agent_cpu_usage to be in MetricSet")
		}
	})

	t.Run("ToString", func(t *testing.T) {
		rule := AccessRule{
			AllowedFeatures: []string{"is_online"},
			AllowedMetrics:  []string{"agent_cpu_usage"},
			MaxDays:         7,
		}

		str, err := rule.ToString()
		if err != nil {
			t.Fatalf("unexpected error from ToString: %v", err)
		}

		// Ensure it parses back to identical rule
		parsed, err := Parse(str)
		if err != nil {
			t.Fatalf("failed to parse generated ToString json: %v", err)
		}

		if parsed.MaxDays != rule.MaxDays {
			t.Errorf("expected max days %d, got %d", rule.MaxDays, parsed.MaxDays)
		}
		if len(parsed.AllowedFeatures) != 1 || parsed.AllowedFeatures[0] != "is_online" {
			t.Errorf("features mismatch: expected %v, got %v", rule.AllowedFeatures, parsed.AllowedFeatures)
		}
	})
}
