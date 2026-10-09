package metrics

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/tsdb"
	"github.com/prometheus/prometheus/tsdb/chunkenc"
)

func GetAverageMetric(ctx context.Context, tsdb *tsdb.DB, agentID string, metricName string, duration time.Duration) (float64, error) {
	end := time.Now()
	start := end.Add(-duration)

	release, err := acquireTSDB(ctx, false)
	if err != nil {
		return 0, err
	}
	defer release()

	// 1. Create a Querier for the specific time block
	// Prometheus uses Unix milliseconds for its timestamps
	querier, err := tsdb.Querier(start.UnixMilli(), end.UnixMilli())
	if err != nil {
		return 0, fmt.Errorf("failed to create querier: %w", err)
	}
	defer querier.Close()

	// 2. Define the exact labels we are looking for (Prometheus matches these)
	matchers := []*labels.Matcher{
		labels.MustNewMatcher(labels.MatchEqual, "__name__", metricName),
		labels.MustNewMatcher(labels.MatchEqual, "agent_id", agentID),
	}

	// 3. Select the series that match our labels
	// false = we want data, not just metadata; nil = no hinting
	seriesSet := querier.Select(ctx, false, nil, matchers...)

	// Multi-series metrics (e.g. one per disk path) are summed: each series is
	// averaged on its own, then the averages are added together.
	var total float64
	anyData := false
	durationValues, err := loadIntervals(ctx, querier, matchers, TimeRange{StartMs: start.UnixMilli(), EndMs: end.UnixMilli()})
	if err != nil {
		return 0, err
	}

	// 4. Iterate through the matching series (usually just 1 series per agent/metric combo)
	for seriesSet.Next() {
		series := seriesSet.At()
		iterator := series.Iterator(nil)
		var sum float64
		var count int
		var seconds float64
		var last int64
		var firstTimestamp, lastTimestamp int64

		// 5. Iterate through the actual raw data chunks (time + value)
		// Note: In newer Prometheus versions, Next() returns a chunkenc.ValueType
		for iterator.Next() == chunkenc.ValFloat {
			timestamp, value := iterator.At() // returns (timestamp_ms, float_value)
			if math.IsNaN(value) || math.IsInf(value, 0) {
				continue
			}
			if isDeltaMetric(metricName) {
				dt := durationValues[timestamp]
				if dt <= 0 {
					if last > 0 {
						dt = float64(timestamp-last) / 1000
					}
					if dt <= 0 {
						dt = 60
					}
				}
				seconds += dt
				last = timestamp
			}
			if firstTimestamp == 0 {
				firstTimestamp = timestamp
			}
			lastTimestamp = timestamp
			sum += value
			count++
		}

		// Handle potential iterator errors
		if err := iterator.Err(); err != nil {
			return 0, fmt.Errorf("error iterating chunks: %w", err)
		}
		if count > 0 && (end.UnixMilli()-lastTimestamp > 3*time.Minute.Milliseconds() || count < 2 || lastTimestamp-firstTimestamp < duration.Milliseconds()/2) {
			return 0, fmt.Errorf("insufficient telemetry coverage")
		}
		if count > 0 {
			anyData = true
			if isDeltaMetric(metricName) {
				total += sum / seconds
			} else {
				total += sum / float64(count)
			}
		}
	}

	if err := seriesSet.Err(); err != nil {
		return 0, fmt.Errorf("error in series selection: %w", err)
	}

	if !anyData {
		return 0, fmt.Errorf("insufficient telemetry")
	}
	return total, nil
}
