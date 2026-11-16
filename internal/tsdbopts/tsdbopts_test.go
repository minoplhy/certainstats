package tsdbopts

import (
	"context"
	"log/slog"
	"math"
	"testing"
	"time"

	"github.com/prometheus/prometheus/model/histogram"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/storage"
	"github.com/prometheus/prometheus/tsdb"
	"github.com/prometheus/prometheus/tsdb/chunkenc"
	"github.com/prometheus/prometheus/tsdb/chunks"
)

func TestMaxBlockDuration(t *testing.T) {
	h := int64(time.Hour / time.Millisecond)
	const gb = int64(10) << 30
	for _, c := range []struct {
		retention, bytes, want int64
	}{
		{0, 0, 31 * day},
		{2160 * h, 0, 216 * h},
		{30 * day, 0, 3 * day},
		{10 * h, 0, 2 * h},
		{1 * h, 0, 2 * h},
		{0, gb, 7 * day},
		{2160 * h, gb, 7 * day},
		{30 * day, gb, 3 * day},
	} {
		if got := MaxBlockDuration(c.retention, c.bytes); got != c.want {
			t.Errorf("MaxBlockDuration(%d, %d) = %d, want %d", c.retention, c.bytes, got, c.want)
		}
	}
}

type fsample struct {
	t int64
	f float64
}

func (s fsample) T() int64                      { return s.t }
func (s fsample) ST() int64                     { return 0 }
func (s fsample) F() float64                    { return s.f }
func (s fsample) H() *histogram.Histogram       { return nil }
func (s fsample) FH() *histogram.FloatHistogram { return nil }
func (s fsample) Type() chunkenc.ValueType      { return chunkenc.ValFloat }
func (s fsample) Copy() chunks.Sample           { return s }

const (
	blocks       = 27 // 54h of 2h blocks
	seriesCount  = 3
	samplesEvery = int64(time.Minute / time.Millisecond)
)

// writeBlocks writes consecutive 2h blocks, as the library default leaves them.
func writeBlocks(t *testing.T, dir string) {
	t.Helper()
	for b := int64(0); b < blocks; b++ {
		lo := b * tsdb.DefaultBlockDuration
		var series []storage.Series
		for s := 0; s < seriesCount; s++ {
			var samples []chunks.Sample
			for ts := lo; ts < lo+tsdb.DefaultBlockDuration; ts += samplesEvery {
				samples = append(samples, fsample{ts, float64(ts)})
			}
			series = append(series, storage.NewListSeries(labels.FromStrings("__name__", "m", "s", string(rune('a'+s))), samples))
		}
		if _, err := tsdb.CreateBlock(series, dir, tsdb.DefaultBlockDuration, slog.New(slog.DiscardHandler)); err != nil {
			t.Fatal(err)
		}
	}
}

func compactedBlocks(t *testing.T, opts *tsdb.Options) (int, int) {
	t.Helper()
	dir := t.TempDir()
	writeBlocks(t, dir)
	db, err := tsdb.Open(dir, nil, nil, opts, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Compact(context.Background()); err != nil {
		t.Fatal(err)
	}
	q, err := db.Querier(math.MinInt64, math.MaxInt64)
	if err != nil {
		t.Fatal(err)
	}
	defer q.Close()
	n := 0
	set := q.Select(context.Background(), false, nil, labels.MustNewMatcher(labels.MatchEqual, "__name__", "m"))
	for set.Next() {
		it := set.At().Iterator(nil)
		for it.Next() != chunkenc.ValNone {
			n++
		}
	}
	if err := set.Err(); err != nil {
		t.Fatal(err)
	}
	return len(db.Blocks()), n
}

func TestOptionsMergeBlocks(t *testing.T) {
	want := int(blocks * seriesCount * (tsdb.DefaultBlockDuration / samplesEvery))
	merged, samples := compactedBlocks(t, Options(0, 0))
	if merged > blocks/3 {
		t.Fatalf("%d blocks after compaction, want far fewer than %d", merged, blocks)
	}
	if samples != want {
		t.Fatalf("%d samples after compaction, want %d", samples, want)
	}
	t.Logf("%d blocks merged into %d", blocks, merged)
}

// Documents the bug: the library default never merges 2h blocks.
func TestLibraryDefaultNeverMerges(t *testing.T) {
	if n, _ := compactedBlocks(t, tsdb.DefaultOptions()); n != blocks {
		t.Fatalf("library default left %d blocks, expected all %d unmerged", n, blocks)
	}
}
