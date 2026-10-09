package networkservice

import (
	"certainstats/internal/metrics"
	nm "certainstats/internal/networkmonitor"
	"context"
	"math"
	"sort"
	"time"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/tsdb/chunkenc"
)

const (
	// minBucket is the smallest history bucket width.
	minBucket = time.Minute
	// maxPoints bounds the points returned per monitor.
	maxPoints = 1000
	// recentWindow is the range the sample cache can serve.
	recentWindow = 24 * time.Hour
)

// bucket accumulates reported windows that fall into one history point.
type bucket struct {
	attempts, success, sum float64
	fastest, slowest       float64
	hasFastest             bool
}

func networkKey(m nm.Monitor) metrics.NetworkKey {
	return metrics.NetworkKey{Owner: m.UserID, Agent: m.AgentID, Monitor: m.ID}
}

// history returns aggregated points from the sample cache when it covers the
// range, warming it from TSDB for recent ranges, and from TSDB otherwise.
func (s *Service) history(ctx context.Context, m nm.Monitor, start, end int64, relative bool) ([]nm.Point, error) {
	if s.Cache != nil {
		key := networkKey(m)
		if samples, ok := s.Cache.GetNetwork(key, start, end); ok {
			return aggregate(samples, start, end), nil
		}
		if s.TSDB == nil {
			return []nm.Point{}, nil
		}
		s.Cache.NetworkFallback()

		now := time.Now().UnixMilli()
		recentStart := now - recentWindow.Milliseconds()
		// Relative 24h windows are anchored when the HTTP range is resolved,
		// slightly before this read. Include that boundary in the warm snapshot.
		recent := start >= recentStart || relative && end-start <= recentWindow.Milliseconds()
		if recent && end <= now {
			revision := s.Cache.NetworkRevision(key)
			from := min(start, recentStart)
			samples, err := s.readSamples(ctx, m, from, now)
			if err != nil {
				return nil, err
			}
			s.Cache.WarmNetwork(key, samples, from, revision)
			return aggregate(samples, start, end), nil
		}
	}
	if s.TSDB == nil {
		return []nm.Point{}, nil
	}
	samples, err := s.readSamples(ctx, m, start, end)
	if err != nil {
		return nil, err
	}
	return aggregate(samples, start, end), nil
}

// readSamples joins the monitor's TSDB series into samples ordered by time.
func (s *Service) readSamples(ctx context.Context, m nm.Monitor, start, end int64) ([]metrics.NetworkSample, error) {
	if s.TSDB == nil {
		return []metrics.NetworkSample{}, nil
	}
	release, err := metrics.AcquirePrivateQuery(ctx)
	if err != nil {
		return nil, err
	}
	defer release()

	s.dataMu.RLock()
	defer s.dataMu.RUnlock()
	querier, err := s.TSDB.Querier(start, end)
	if err != nil {
		return nil, err
	}
	defer querier.Close()

	byTimestamp := map[int64]*metrics.NetworkSample{}
	for _, name := range tsdbMetrics {
		set := querier.Select(ctx, false, nil,
			labels.MustNewMatcher(labels.MatchEqual, "__name__", "network_monitor_"+name),
			labels.MustNewMatcher(labels.MatchEqual, "user_id", m.UserID),
			labels.MustNewMatcher(labels.MatchEqual, "agent_id", m.AgentID),
			labels.MustNewMatcher(labels.MatchEqual, "monitor_id", m.ID),
		)
		for set.Next() {
			it := set.At().Iterator(nil)
			for it.Next() != chunkenc.ValNone {
				timestamp, value := it.At()
				if math.IsNaN(value) || math.IsInf(value, 0) {
					continue
				}
				sample := byTimestamp[timestamp]
				if sample == nil {
					sample = &metrics.NetworkSample{Timestamp: timestamp}
					byTimestamp[timestamp] = sample
				}
				setSampleField(sample, name, value)
			}
			if err = it.Err(); err != nil {
				return nil, err
			}
		}
		if err = set.Err(); err != nil {
			return nil, err
		}
	}

	samples := make([]metrics.NetworkSample, 0, len(byTimestamp))
	for _, sample := range byTimestamp {
		samples = append(samples, *sample)
	}
	sort.Slice(samples, func(i, j int) bool { return samples[i].Timestamp < samples[j].Timestamp })
	return samples, nil
}

func setSampleField(sample *metrics.NetworkSample, name string, value float64) {
	switch name {
	case "attempt_count":
		sample.Attempts = value
	case "success_count":
		sample.Success = value
	case "response_sum_us":
		sample.Sum = value
	case "response_min_us":
		sample.Min = value
	case "response_max_us":
		sample.Max = value
	}
}

// aggregate is shared by TSDB and memory reads; weights are reported attempts
// and successes rather than the number of reporting windows.
func aggregate(samples []metrics.NetworkSample, start, end int64) []nm.Point {
	step := max(minBucket.Milliseconds(), (end-start+maxPoints-1)/maxPoints)
	buckets := map[int64]*bucket{}
	for _, sample := range samples {
		if sample.Timestamp < start || sample.Timestamp > end {
			continue
		}
		key := start + (sample.Timestamp-start)/step*step
		b := buckets[key]
		if b == nil {
			b = &bucket{}
			buckets[key] = b
		}
		b.attempts += sample.Attempts
		b.success += sample.Success
		b.sum += sample.Sum
		if sample.Success > 0 {
			if !b.hasFastest || sample.Min < b.fastest {
				b.fastest = sample.Min
				b.hasFastest = true
			}
			b.slowest = max(b.slowest, sample.Max)
		}
	}

	points := []nm.Point{}
	for timestamp := start; timestamp <= end && len(points) < maxPoints; timestamp += step {
		point := nm.Point{Timestamp: timestamp}
		if b := buckets[timestamp]; b != nil && b.attempts > 0 {
			loss := 100 * (b.attempts - b.success) / b.attempts
			point.Loss = &loss
			if b.success > 0 {
				// Stored values are microseconds; points are milliseconds.
				avg, fastest, slowest := b.sum/b.success/1000, b.fastest/1000, b.slowest/1000
				point.Avg, point.Min, point.Max = &avg, &fastest, &slowest
			}
		}
		points = append(points, point)
	}
	return points
}
