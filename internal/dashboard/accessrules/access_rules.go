package accessrules

import (
	"encoding/json"
)

// featuresSet and metricsSet are pre-built O(1) lookup sets derived from the
// lists above.  Populated once at init so Validate() never allocates a map.
var (
	featuresSet = toSet(FeaturesList)
	metricsSet  = toSet(MetricsList)
)

var allowedDaysSet = map[uint]struct{}{
	1:   {},
	2:   {},
	7:   {},
	30:  {},
	90:  {},
	180: {},
	365: {},
	730: {},
}

func toSet(s []string) map[string]struct{} {
	m := make(map[string]struct{}, len(s))
	for _, v := range s {
		m[v] = struct{}{}
	}
	return m
}

// IsEmpty reports whether a validated rule has anything to show.
func (r AccessRule) IsEmpty() bool {
	return len(r.AllowedFeatures) == 0 && len(r.AllowedMetrics) == 0
}

// FeatureSet returns AllowedFeatures as an O(1) lookup map, useful when
// iterating over agent rows and deciding which columns to include.
func (r AccessRule) FeatureSet() map[string]struct{} {
	return toSet(r.AllowedFeatures)
}

// MetricSet returns AllowedMetrics as an O(1) lookup map.
func (r AccessRule) MetricSet() map[string]struct{} {
	return toSet(r.AllowedMetrics)
}

// intersect returns only the elements of slice that exist in the allowlist set.
func intersect(slice []string, allow map[string]struct{}) []string {
	out := make([]string, 0, len(slice))
	for _, v := range slice {
		if _, ok := allow[v]; ok {
			out = append(out, v)
		}
	}
	return out
}

func (a AccessRule) ToString() (string, error) {
	b, err := json.Marshal(a)
	return string(b), err
}
