package knot

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	ctxkeys "certainstats/internal/context"
)

func windowOf(t *testing.T, query string, now time.Time, minuteFrom int64) (window, bool) {
	t.Helper()
	return parseWindow(httptest.NewRequest(http.MethodGet, "/x?"+query, nil), now, minuteFrom)
}

func TestWindowUsesTheSharedRangeRules(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	nowMs := now.UnixMilli()

	w, ok := windowOf(t, "hours=6", now, 0)
	if !ok || w.Step%minuteMs != 0 || w.Start%w.Step != 0 || w.End%w.Step != 0 {
		t.Fatalf("hours=6: %+v %v", w, ok)
	}
	if w.End < nowMs || w.End-w.Step > nowMs || w.End-w.Start < 6*hourMs {
		t.Fatalf("hours=6 does not end now and cover six hours: %+v", w)
	}
	if hw, _ := windowOf(t, "hours=1", now, 0); hw.Step != minuteMs {
		t.Fatalf("an hour should be read per minute, got step %d", hw.Step)
	}

	start, end := nowMs-3*hourMs, nowMs-hourMs
	w, ok = windowOf(t, "start="+strconv.FormatInt(start, 10)+"&end="+strconv.FormatInt(end, 10), now, 0)
	if !ok || w.Start > start || w.End < end || w.End-end >= w.Step {
		t.Fatalf("custom range: %+v %v", w, ok)
	}

	// A range reaching back past minute retention reads hourly rows.
	w, ok = windowOf(t, "start="+strconv.FormatInt(nowMs-30*hourMs, 10)+"&end="+strconv.FormatInt(nowMs-29*hourMs, 10), now, nowMs-24*hourMs)
	if !ok || w.Step%hourMs != 0 {
		t.Fatalf("old range should step by hours: %+v %v", w, ok)
	}

	// Relative ranges end at the type's clock, so tests and alerts can move it.
	past := now.Add(-48 * time.Hour)
	if w, _ := windowOf(t, "hours=1", past, 0); w.End > past.UnixMilli()+minuteMs {
		t.Fatalf("relative range ignored the clock: %+v", w)
	}

	for _, bad := range []string{"hours=0", "hours=x", "hours=99999999", "range=7d&hours=-1",
		"start=10", "start=200&end=100", "start=1&end=" + strconv.FormatInt(nowMs+time.Hour.Milliseconds(), 10)} {
		if _, ok := windowOf(t, bad, now, 0); ok {
			t.Errorf("%s should be refused", bad)
		}
	}
}

func TestEndpointsTakeHoursOrStartEnd(t *testing.T) {
	r := newRuleRig(t)
	r.now = time.Now().Truncate(time.Minute)
	nowMs := r.now.UnixMilli()
	get := func(handler http.HandlerFunc, query string) (int, map[string]any) {
		req := httptest.NewRequest(http.MethodGet, "/x?"+query, nil)
		req = req.WithContext(context.WithValue(req.Context(), ctxkeys.UserIDKey, "u1"))
		rec := httptest.NewRecorder()
		handler(rec, req)
		var body map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &body)
		return rec.Code, body
	}
	custom := "start=" + strconv.FormatInt(nowMs-2*hourMs, 10) + "&end=" + strconv.FormatInt(nowMs-hourMs, 10)
	for _, c := range []struct {
		name    string
		handler http.HandlerFunc
		query   string
	}{
		{"series hours", r.typ.series, "group=rcode&hours=24"},
		{"series custom", r.typ.series, "group=rcode&" + custom},
		{"totals hours", r.typ.totals, "hours=6"},
		{"totals custom", r.typ.totals, custom},
		{"queries custom", r.typ.queries, "dim=qtype&" + custom},
	} {
		code, body := get(c.handler, c.query)
		if code != http.StatusOK {
			t.Fatalf("%s: %d %v", c.name, code, body)
		}
		if _, has := body["range"]; has {
			t.Errorf("%s still answers with a range label", c.name)
		}
		if body["start"] == nil || body["end"] == nil {
			t.Errorf("%s: no start or end in %v", c.name, body)
		}
	}
	if code, body := get(r.typ.series, "group=rcode&hours=0"); code != http.StatusBadRequest || body["error"] != badRange {
		t.Fatalf("bad range: %d %v", code, body)
	}
}

func TestDnstapSeriesHasEveryBucket(t *testing.T) {
	m := minuteMs
	got := zeroFill([][2]int64{{2 * m, 5}, {4 * m, 1}}, 0, 6*m, m)
	want := [][2]int64{{0, 0}, {m, 0}, {2 * m, 5}, {3 * m, 0}, {4 * m, 1}, {5 * m, 0}}
	if len(got) != len(want) {
		t.Fatalf("got %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
	if out := zeroFill(nil, 0, (maxFill+1)*m, m); len(out) != 0 {
		t.Fatalf("an oversized window must not be filled, got %d points", len(out))
	}
}
