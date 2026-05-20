package metrics

import (
	m "certainstats/internal/base/metrics"
	"strings"
)

// maxDataPoints is the target number of chart points returned to the frontend.
// Beyond this threshold the backend buckets raw samples into aggregated intervals.
const maxDataPoints = 1000

// minStepMs is the minimum bucket width in milliseconds below which we skip
// downsampling entirely and return every raw point (≤ ~15 s step = sub-minute
// resolution that is already dense enough for a 1-hour view).
const minStepMs = 15_000

// isDeltaMetric returns true for counter / throughput metrics whose raw values
// represent bytes transferred in an interval.  These are summed inside a bucket
// rather than averaged.
func isDeltaMetric(metricName string) bool {
	return strings.Contains(metricName, "bytes")
}

// stepForRange computes the bucket width for a given query duration.
// Returns 0 when no downsampling is needed (short ranges).
func stepForRange(durationMs int64) int64 {
	step := durationMs / maxDataPoints
	if step < minStepMs {
		return 0
	}
	return step
}

// downsamplePoints aggregates a sorted slice of TimeseriesPoint into at most
// maxDataPoints output points using fixed-width time buckets.
//
//   - gauge metrics  → bucket average
//   - delta metrics  → bucket sum   (bytes-per-interval semantics preserved)
//
// When stepMs == 0 (short range) the raw points are returned unchanged.
func downsamplePoints(pts []TimeseriesPoint, stepMs int64, isDelta bool) []m.DataPoint {
	out := make([]m.DataPoint, 0, len(pts))

	if stepMs == 0 {
		for _, pt := range pts {
			out = append(out, m.DataPoint{float64(pt.Timestamp), pt.Value})
		}
		return out
	}

	var (
		bucketStart int64 = -1
		sum         float64
		count       int64
	)

	flush := func() {
		if bucketStart == -1 {
			return
		}
		var v float64
		if isDelta {
			v = sum
		} else {
			v = sum / float64(count)
		}
		out = append(out, m.DataPoint{float64(bucketStart), v})
	}

	for _, pt := range pts {
		if bucketStart == -1 || pt.Timestamp >= bucketStart+stepMs {
			flush()
			bucketStart = pt.Timestamp
			sum = pt.Value
			count = 1
		} else {
			sum += pt.Value
			count++
		}
	}
	flush()

	return out
}
