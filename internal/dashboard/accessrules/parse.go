package accessrules

import (
	"encoding/json"
	"fmt"
)

func ParseRules(in string) (AccessRules, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(in), &raw); err != nil {
		return nil, err
	}

	rules := make(AccessRules, len(raw))
	for role, data := range raw {
		rule, err := Parse(string(data))
		if err != nil {
			return nil, fmt.Errorf("accessrules: role %q: %w", role, err)
		}
		rules[role] = rule
	}
	return rules, nil
}

// Parse unmarshals a single AccessRule from JSON and validates it
func Parse(in string) (AccessRule, error) {
	var rule AccessRule
	if err := json.Unmarshal([]byte(in), &rule); err != nil {
		return AccessRule{}, err
	}
	if rule.MaxDays == 0 {
		return AccessRule{}, fmt.Errorf("accessrules: max_days must be greater than 0")
	}
	if _, ok := allowedDaysSet[rule.MaxDays]; !ok {
		return AccessRule{}, fmt.Errorf("accessrules: max_days must be one of the selective quick ranges: 1, 2, 7, 30, 90, 180, 365, 730")
	}
	for _, f := range rule.AllowedFeatures {
		if _, ok := featuresSet[f]; !ok {
			return AccessRule{}, fmt.Errorf("accessrules: unknown feature %q", f)
		}
	}
	for _, m := range rule.AllowedMetrics {
		if _, ok := metricsSet[m]; !ok {
			return AccessRule{}, fmt.Errorf("accessrules: unknown metric %q", m)
		}
	}
	return rule, nil
}
