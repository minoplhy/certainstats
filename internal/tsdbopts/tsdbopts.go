// Package tsdbopts builds the Prometheus TSDB options both CertainStats TSDBs use.
//
// The library default sets the maximum block duration equal to the minimum
// (2h), so blocks are never compacted together: every 2h a new block is
// written and kept open for good, and open files, mapped memory and the cost
// of long-range queries grow without bound. This follows the Prometheus
// server's rule instead: blocks merge up to a tenth of retention, 31 days at
// most.
package tsdbopts

import (
	"time"

	"github.com/prometheus/prometheus/tsdb"
)

const (
	day = int64(24 * time.Hour / time.Millisecond)
	// maxBlock is the Prometheus server's upper bound on block duration.
	maxBlock = 31 * day
	// sizeLimitedBlock caps blocks when a size limit is set, so reaching the
	// limit removes about a week of the oldest history rather than a month.
	sizeLimitedBlock = 7 * day
)

// MaxBlockDuration is how far blocks may merge, in milliseconds, for a
// retention (0: unlimited) and a size limit in bytes (0: none).
func MaxBlockDuration(retentionMs, maxBytes int64) int64 {
	d := maxBlock
	if retentionMs > 0 {
		d = min(d, retentionMs/10)
	}
	if maxBytes > 0 {
		d = min(d, sizeLimitedBlock)
	}
	return max(d, tsdb.DefaultBlockDuration)
}

// Options returns the library defaults with retention, size limit and block
// merging set.
func Options(retentionMs, maxBytes int64) *tsdb.Options {
	opts := tsdb.DefaultOptions()
	opts.RetentionDuration = retentionMs
	opts.MaxBytes = maxBytes
	opts.MaxBlockDuration = MaxBlockDuration(retentionMs, maxBytes)
	return opts
}
