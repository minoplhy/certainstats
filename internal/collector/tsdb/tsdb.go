// Package tsdb is the collectors' own Prometheus time-series database,
// dataDir/collector_tsdb. It is separate from the agent TSDB: collector types
// write and read only here, so a busy collector can neither evict agent metrics
// nor take agent query slots, and each database can be sized or wiped alone.
package tsdb

import (
	"certainstats/internal/tsdbopts"
	"context"
	"errors"
	"os"
	"time"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/storage"
	ptsdb "github.com/prometheus/prometheus/tsdb"
)

// DefaultRetention applies when COLLECTOR_RETENTION_DURATION is not set.
const DefaultRetention = 90 * 24 * time.Hour

const (
	// querySlots caps concurrent reads.
	querySlots = 8
	// queueWait is how long a read waits for a slot before it is refused.
	queueWait = 2 * time.Second
)

// ErrBusy is returned when every query slot stays taken for queueWait.
var ErrBusy = errors.New("collector time series are busy; try again")

// Options sizes the database. Zero keeps data without that limit.
type Options struct {
	RetentionMs int64
	MaxBytes    int64
}

// DB is the collector TSDB with its own read limiter.
type DB struct {
	db    *ptsdb.DB
	slots chan struct{}
}

// Open creates dir if needed and opens the database in it.
func Open(dir string, o Options) (*DB, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	db, err := ptsdb.Open(dir, nil, nil, tsdbopts.Options(o.RetentionMs, o.MaxBytes), nil)
	if err != nil {
		return nil, err
	}
	return &DB{db: db, slots: make(chan struct{}, querySlots)}, nil
}

func (d *DB) Close() error { return d.db.Close() }

// BlockCount reports how many persisted blocks are open.
func (d *DB) BlockCount() int { return len(d.db.Blocks()) }

// Appender starts a write; commit or roll it back.
func (d *DB) Appender(ctx context.Context) storage.Appender { return d.db.Appender(ctx) }

// acquire takes a query slot, waiting at most queueWait.
func (d *DB) acquire(ctx context.Context) (func(), error) {
	wait, cancel := context.WithTimeout(ctx, queueWait)
	defer cancel()
	select {
	case d.slots <- struct{}{}:
		return func() { <-d.slots }, nil
	case <-wait.Done():
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, ErrBusy
	}
}

// Query runs fn with a querier over [mint, maxt], holding one query slot.
func (d *DB) Query(ctx context.Context, mint, maxt int64, fn func(storage.Querier) error) error {
	release, err := d.acquire(ctx)
	if err != nil {
		return err
	}
	defer release()
	q, err := d.db.Querier(mint, maxt)
	if err != nil {
		return err
	}
	defer q.Close()
	return fn(q)
}

// LabelValues lists the values of one label across all series.
func (d *DB) LabelValues(ctx context.Context, name string, ms ...*labels.Matcher) ([]string, error) {
	var out []string
	err := d.Query(ctx, 0, time.Now().Add(time.Hour).UnixMilli(), func(q storage.Querier) error {
		vals, _, err := q.LabelValues(ctx, name, nil, ms...)
		out = vals
		return err
	})
	return out, err
}

// Delete removes every sample of the matching series.
func (d *DB) Delete(ctx context.Context, ms ...*labels.Matcher) error {
	return d.db.Delete(ctx, 0, time.Now().Add(time.Hour).UnixMilli(), ms...)
}
