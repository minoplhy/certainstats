package tsdb

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/storage"
	"github.com/prometheus/prometheus/tsdb/chunkenc"
)

func TestAppendQueryAndDelete(t *testing.T) {
	db, err := Open(t.TempDir(), Options{RetentionMs: DefaultRetention.Milliseconds()})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	now := time.Now().UnixMilli()
	app := db.Appender(ctx)
	for _, u := range []string{"u1", "u2"} {
		if _, err := app.Append(0, labels.FromStrings("__name__", "m", "user_id", u), now, 7); err != nil {
			t.Fatal(err)
		}
	}
	if err := app.Commit(); err != nil {
		t.Fatal(err)
	}
	read := func(user string) []float64 {
		var vals []float64
		err := db.Query(ctx, now-1000, now+1000, func(q storage.Querier) error {
			set := q.Select(ctx, false, nil, labels.MustNewMatcher(labels.MatchEqual, "user_id", user))
			for set.Next() {
				it := set.At().Iterator(nil)
				for it.Next() != chunkenc.ValNone {
					_, v := it.At()
					vals = append(vals, v)
				}
			}
			return set.Err()
		})
		if err != nil {
			t.Fatal(err)
		}
		return vals
	}
	if got := read("u1"); len(got) != 1 || got[0] != 7 {
		t.Fatalf("read %v", got)
	}
	owners, err := db.LabelValues(ctx, "user_id")
	if err != nil || len(owners) != 2 {
		t.Fatalf("owners %v %v", owners, err)
	}
	if err := db.Delete(ctx, labels.MustNewMatcher(labels.MatchEqual, "user_id", "u2")); err != nil {
		t.Fatal(err)
	}
	if got := read("u2"); len(got) != 0 {
		t.Fatalf("deleted series still read %v", got)
	}
}

func TestLimiterRefusesWhenFull(t *testing.T) {
	db, err := Open(t.TempDir(), Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for i := 0; i < querySlots; i++ {
		db.slots <- struct{}{}
	}
	start := time.Now()
	err = db.Query(context.Background(), 0, 1, func(storage.Querier) error { return nil })
	if !errors.Is(err, ErrBusy) || time.Since(start) < queueWait {
		t.Fatalf("got %v after %s", err, time.Since(start))
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := db.Query(ctx, 0, 1, func(storage.Querier) error { return nil }); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled request: %v", err)
	}
	<-db.slots
	if err := db.Query(context.Background(), 0, 1, func(storage.Querier) error { return nil }); err != nil {
		t.Fatalf("free slot: %v", err)
	}
}
