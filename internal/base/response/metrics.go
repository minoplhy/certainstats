package response

import (
	m "certainstats/internal/base/metrics"
)

type MetricSeries struct {
	Labels map[string]string `json:"labels"`
	Data   []m.DataPoint     `json:"data"`
}

type MetricResponse struct {
	Metric string         `json:"metric"`
	Series []MetricSeries `json:"series"`
}
