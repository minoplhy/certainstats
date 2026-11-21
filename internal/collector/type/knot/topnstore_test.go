package knot

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"certainstats/internal/collector/type/knot/wire"
	ctxkeys "certainstats/internal/context"
)

// openTopNStore opens knot.db and the block store beside it, as KNOT_ENGINE=topn does.
func openTopNStore(t *testing.T, top int) (*TopN, *Store) {
	t.Helper()
	meta := openStore(t)
	keep := Retention{MinuteMs: DefaultMinuteRetention.Milliseconds(), HourlyMs: DefaultHourlyRetention.Milliseconds(), DailyMs: DefaultDailyRetention.Milliseconds()}
	k, err := OpenTopN(filepath.Join(t.TempDir(), "knot-topn"), meta, top, keep, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { k.Close() })
	return k, meta
}

// The same story as TestDnstapStoreQueriesRollupAndPrune, on the block engine.
func TestTopNQueriesRollupAndPrune(t *testing.T) {
	k, meta := openTopNStore(t, 1)
	ctx := context.Background()
	now := time.Date(2026, 11, 16, 12, 30, 0, 0, time.UTC).UnixMilli()
	old := hourOf(now) - 5*hourMs
	last := hourOf(now) - minuteMs
	for _, m := range []int64{old, old + minuteMs, last} {
		if err := k.SaveDnstap(ctx, "u1", "c1", bucketAt(m), now); err != nil {
			t.Fatal(err)
		}
	}
	// A resent part replaces rows instead of adding to them.
	if err := k.SaveDnstap(ctx, "u1", "c1", bucketAt(old), now); err != nil {
		t.Fatal(err)
	}
	// Freshness comes from knot.db with either engine.
	if seen, err := meta.LastDnstap(ctx, "u1"); err != nil || seen["c1"] != last {
		t.Fatalf("last dnstap %v %v", seen, err)
	}
	q := Query{UserID: "u1", Start: old, End: now, Step: hourMs}
	b, err := k.Dnstap(ctx, q, "rcode", 10)
	if err != nil {
		t.Fatal(err)
	}
	if b.Total != 300 || !b.Exact || len(b.Rows) != 2 || b.Rows[0] != (Row{"NOERROR", 210}) {
		t.Fatalf("rcode: %+v", b)
	}
	b, err = k.Dnstap(ctx, Query{UserID: "u1", QType: "AAAA", Start: old, End: now}, "qname", 10)
	if err != nil {
		t.Fatal(err)
	}
	if b.Total != 90 || !b.Exact || len(b.Rows) != 1 || b.Rows[0] != (Row{"nope.example.com.", 60}) || b.Other != 30 || !b.Approximate {
		t.Fatalf("qname by qtype: %+v", b)
	}
	b, err = k.Dnstap(ctx, Query{UserID: "u1", QName: "www.example.com.", Start: old, End: now}, "qtype", 10)
	if err != nil || b.Total != 180 || b.Exact {
		t.Fatalf("filtered by name: %+v %v", b, err)
	}
	b, err = k.Dnstap(ctx, Query{UserID: "u1", Zone: "example.com.", Start: old, End: now}, "client", 10)
	if err != nil || len(b.Ignored) != 0 || len(b.Rows) != 2 || b.Rows[0] != (Row{"192.0.2.0/24", 210}) || b.Rows[1] != (Row{"2001:db8:1::/48", 90}) {
		t.Fatalf("clients: %+v %v", b, err)
	}
	if b, _ := k.Dnstap(ctx, Query{UserID: "u2", Start: old, End: now}, "rcode", 10); b.Total != 0 {
		t.Fatal("another owner sees u1's dnstap")
	}
	if _, err := k.Dnstap(ctx, q, "bogus", 10); err == nil || !strings.Contains(err.Error(), "unknown dimension") {
		t.Fatal("bogus dimension accepted")
	}

	// Rolling settles hours older than the lag; the newest hour stays in minutes.
	if n, err := k.Rollup(ctx, now); err != nil || n == 0 {
		t.Fatalf("rollup %d %v", n, err)
	}
	hq := q
	hq.MinuteFrom = now
	if hb, err := k.Dnstap(ctx, hq, "rcode", 10); err != nil || hb.Total != 300 {
		t.Fatalf("hourly total %+v %v", hb, err)
	}
	names, err := k.Dnstap(ctx, hq, "qname", 10)
	if err != nil {
		t.Fatal(err)
	}
	var sum int64
	for _, r := range names.Rows {
		sum += r.Count
	}
	if sum+names.Other != 300 {
		t.Fatalf("hourly names do not add up: %+v", names)
	}
	// The cap of 1 cuts networks with the bound rule; the sums still match.
	if hc, err := k.Dnstap(ctx, hq, "client", 10); err != nil || hc.Rows[0] != (Row{"192.0.2.0/24", 210}) || sumRows(hc)+hc.Other != 300 || hc.ErrBound == 0 || hc.Bounded {
		t.Fatalf("hourly clients: %+v %v", hc, err)
	}

	// Retention drops old minutes; the newest minute is still in the head.
	if _, err := k.Prune(ctx, Retention{MinuteMs: 2 * hourMs}, now, []string{"u1"}); err != nil {
		t.Fatal(err)
	}
	if mb, _ := k.Dnstap(ctx, Query{UserID: "u1", Start: old, End: now}, "rcode", 10); mb.Total != 100 {
		t.Fatalf("after minute retention %d", mb.Total)
	}

	// An unknown account loses its zones, minutes and every tier of rows.
	meta.SaveZones(ctx, "gone", "c2", []wire.Zone{{Name: "x."}}, now)
	if err := k.SaveDnstap(ctx, "gone", "c2", bucketAt(old), now); err != nil {
		t.Fatal(err)
	}
	if _, err := k.Rollup(ctx, now); err != nil {
		t.Fatal(err)
	}
	res, err := k.Prune(ctx, Retention{}, now, []string{"u1"})
	if err != nil || res.Zones != 1 {
		t.Fatalf("prune removed account: %+v %v", res, err)
	}
	if b, _ := k.Dnstap(ctx, Query{UserID: "gone", Start: old, End: now, MinuteFrom: now}, "rcode", 10); b.Total != 0 {
		t.Fatalf("removed account still has rows: %+v", b)
	}
	owners, err := k.s.Owners(ctx)
	if err != nil || len(owners) != 1 || owners[0] != "u1" {
		t.Fatalf("owners after removal %v %v", owners, err)
	}
	if seen, _ := meta.LastDnstap(ctx, "gone"); len(seen) != 0 {
		t.Fatalf("removed account still has minutes: %v", seen)
	}
}

// A store failure leaves knot.db without the minute, so freshness never runs
// ahead of the rows; a bad client network is skipped, as Store skips it.
func TestTopNSkipsBadClientRows(t *testing.T) {
	k, _ := openTopNStore(t, 1000)
	ctx := context.Background()
	now := time.Date(2026, 11, 16, 12, 30, 0, 0, time.UTC).UnixMilli()
	d := bucketAt(now - 10*minuteMs)
	d.Clients = append(d.Clients, wire.Client{Subnet: "not-a-net", Zone: "example.com.", QName: "x.example.com.", Count: 5})
	if err := k.SaveDnstap(ctx, "u1", "c1", d, now); err != nil {
		t.Fatal(err)
	}
	b, err := k.Dnstap(ctx, Query{UserID: "u1", Start: now - hourMs, End: now}, "client", 10)
	if err != nil || len(b.Rows) != 2 {
		t.Fatalf("clients: %+v %v", b, err)
	}
}

// The drill-down endpoint reads TopN when it is set.
func TestQueriesEndpointReadsTopN(t *testing.T) {
	r := newRuleRig(t)
	r.now = time.Now().Truncate(time.Minute)
	now := r.now
	k, meta := openTopNStore(t, 1000)
	typ := r.typ
	typ.Store, typ.TopN = meta, k
	ctx := context.Background()
	for _, m := range []int64{now.UnixMilli() - 10*minuteMs, now.UnixMilli() - 9*minuteMs} {
		if err := typ.dnstapRows().SaveDnstap(ctx, "u1", "col_p", bucketAt(m), now.UnixMilli()); err != nil {
			t.Fatal(err)
		}
	}
	req := httptest.NewRequest(http.MethodGet, "/x?dim=rcode&hours=1", nil)
	req = req.WithContext(context.WithValue(req.Context(), ctxkeys.UserIDKey, "u1"))
	rec := httptest.NewRecorder()
	typ.queries(rec, req)
	var body struct {
		Breakdown Breakdown `json:"breakdown"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("%d %s %v", rec.Code, rec.Body.String(), err)
	}
	if body.Breakdown.Total != 200 || len(body.Breakdown.Rows) != 2 {
		t.Fatalf("breakdown %+v", body.Breakdown)
	}
	// Nothing was written to knot.db's bucket tables.
	if b, err := meta.Dnstap(ctx, Query{UserID: "u1", Start: now.UnixMilli() - hourMs, End: now.UnixMilli()}, "rcode", 10); err != nil || b.Total != 0 {
		t.Fatalf("knot.db has dnstap rows: %+v %v", b, err)
	}
}
