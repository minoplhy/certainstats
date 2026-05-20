package metrics

import (
	"context"
	"fmt"
	"time"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/tsdb"
	"github.com/prometheus/prometheus/tsdb/chunkenc"
)

func GetAverageMetric(ctx context.Context, tsdb *tsdb.DB, agentID string, metricName string, duration time.Duration) (float64, error) {
	end := time.Now()
	start := end.Add(-duration)

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

	var sum float64
	var count int

	// 4. Iterate through the matching series (usually just 1 series per agent/metric combo)
	for seriesSet.Next() {
		series := seriesSet.At()
		iterator := series.Iterator(nil)

		// 5. Iterate through the actual raw data chunks (time + value)
		// Note: In newer Prometheus versions, Next() returns a chunkenc.ValueType
		for iterator.Next() == chunkenc.ValFloat {
			_, value := iterator.At() // returns (timestamp_ms, float_value)
			sum += value
			count++
		}

		// Handle potential iterator errors
		if err := iterator.Err(); err != nil {
			return 0, fmt.Errorf("error iterating chunks: %w", err)
		}
	}

	if err := seriesSet.Err(); err != nil {
		return 0, fmt.Errorf("error in series selection: %w", err)
	}

	// 6. Return the calculated average
	if count == 0 {
		//return 0, fmt.Errorf("no data points found for %s over the last %s", metricName, duration)
		return 0, nil
	}

	return sum / float64(count), nil
}
