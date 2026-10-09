package metrics

import (
	m "certainstats/internal/base/metrics"
	"math"
)

// rateSeries preserves interval bytes and durations through downsampling. The
// sum of bytes divided by sum of observed durations avoids averaging rates.
func rateSeries(points []TimeseriesPoint, intervals map[int64]float64, step int64) ([]m.DataPoint, []m.DataPoint, bool) {
	rates := make([]m.DataPoint, 0, len(points))
	durations := make([]m.DataPoint, 0, len(points))
	var start, previous int64
	var bytes, seconds float64
	legacy := false
	flush := func() {
		if seconds > 0 {
			rates = append(rates, m.DataPoint{float64(start), bytes / seconds})
			durations = append(durations, m.DataPoint{float64(start), seconds})
		}
	}
	for _, p := range points {
		if math.IsNaN(p.Value) || math.IsInf(p.Value, 0) {
			continue
		}
		dt := intervals[p.Timestamp]
		if dt <= 0 {
			legacy = true
			if previous > 0 {
				dt = float64(p.Timestamp-previous) / 1000
			}
			if dt <= 0 {
				dt = 60
			}
		}
		previous = p.Timestamp
		if seconds > 0 && (step == 0 || p.Timestamp >= start+step) {
			flush()
			bytes = 0
			seconds = 0
		}
		if seconds == 0 {
			start = p.Timestamp
		}
		bytes += p.Value
		seconds += dt
	}
	flush()
	return rates, durations, legacy
}
