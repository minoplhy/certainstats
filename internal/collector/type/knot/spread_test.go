package knot

import (
	"math"
	"testing"
)

func sum(b []float64) float64 {
	s := 0.0
	for _, v := range b {
		s += v
	}
	return s
}

// An integer increase inside one bucket must come back exactly, whatever the
// gap between readings (d/x*x is not always d in floating point).
func TestSpreadSingleBucketExact(t *testing.T) {
	const start, step = int64(0), int64(3_600_000)
	for _, d := range []float64{25, 250, 12_000_000} {
		for gap := int64(1); gap <= 5000; gap++ {
			b := make([]float64, 1)
			t0 := int64(1000)
			spread(b, start, step, start+step, t0, t0+gap, d)
			if b[0] != d {
				t.Fatalf("d=%v gap=%dms: got %v", d, gap, b[0])
			}
		}
	}
}

func TestSpreadManyBucketsExactAndProportional(t *testing.T) {
	const minute = int64(60_000)
	start, end := int64(0), 10*minute
	for _, d := range []float64{7, 250, 1_000_003} {
		for _, iv := range [][2]int64{{13, 3*minute + 7}, {minute - 1, 9*minute + 31}, {17, 10*minute - 3}} {
			b := make([]float64, 10)
			spread(b, start, minute, end, iv[0], iv[1], d)
			if got := sum(b); got != d {
				t.Fatalf("d=%v %v: buckets sum to %v", d, iv, got)
			}
			for i, v := range b {
				bs, be := start+int64(i)*minute, start+int64(i+1)*minute
				ov := min(be, iv[1]) - max(bs, iv[0])
				want := 0.0
				if ov > 0 {
					want = d * float64(ov) / float64(iv[1]-iv[0])
				}
				if math.Abs(v-want) > 1e-9*math.Max(1, d) {
					t.Fatalf("d=%v %v bucket %d: got %v want %v", d, iv, i, v, want)
				}
			}
		}
	}
}

func TestSpreadCountsOnlyInWindowShare(t *testing.T) {
	const minute = int64(60_000)
	b := make([]float64, 2)
	// Interval [-minute, 2*minute) against window [0, 2*minute): 2/3 inside.
	spread(b, 0, minute, 2*minute, -minute, 2*minute, 300)
	if got := sum(b); got != 200 {
		t.Fatalf("in-window share %v, want 200", got)
	}
	b = make([]float64, 2)
	spread(b, 0, minute, 2*minute, 3*minute, 4*minute, 300)
	if got := sum(b); got != 0 {
		t.Fatalf("interval after the window counted %v", got)
	}
}
