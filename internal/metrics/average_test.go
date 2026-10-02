package metrics

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/tsdb"
)

func TestGetAverageMetric_SumsSeries(t *testing.T) {
	db, err := tsdb.Open(t.TempDir(), nil, nil, tsdb.DefaultOptions(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	ctx := context.Background()
	app := db.Appender(ctx)
	now := time.Now()
	lbl := func(name, path string) labels.Labels {
		return labels.FromStrings("__name__", name, "agent_id", "agt_1", "path", path)
	}
	// "/" averages 40, "/data" averages 100 → summed 140
	// Samples must be appended in time order or the TSDB drops them.
	for i, v := range []struct{ root, data float64 }{{30, 90}, {50, 110}} {
		ts := now.Add(time.Duration(i-1) * time.Minute).UnixMilli()
		app.Append(0, lbl("agent_disk_used", "/"), ts, v.root)
		app.Append(0, lbl("agent_disk_used", "/data"), ts, v.data)
	}
	// Single-series metric is unaffected: plain average.
	cpu := labels.FromStrings("__name__", "agent_cpu_usage", "agent_id", "agt_1")
	app.Append(0, cpu, now.Add(-time.Minute).UnixMilli(), 40)
	app.Append(0, cpu, now.UnixMilli(), 20)
	if err := app.Commit(); err != nil {
		t.Fatal(err)
	}

	cases := map[string]float64{"agent_disk_used": 140, "agent_cpu_usage": 30, "agent_ram_used": 0}
	for metric, want := range cases {
		got, err := GetAverageMetric(ctx, db, "agt_1", metric, 10*time.Minute)
		if err != nil {
			t.Fatalf("%s: %v", metric, err)
		}
		if math.Abs(got-want) > 1e-9 {
			t.Errorf("%s = %v, want %v", metric, got, want)
		}
	}
}
