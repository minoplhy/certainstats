package knot

import (
	"context"
	"encoding/json"
	"math"
	"path/filepath"
	"strings"
	"testing"
	"time"

	ctsdb "certainstats/internal/collector/tsdb"
	"certainstats/internal/collector/type/knot/wire"
)

func openTSDB(t *testing.T) *ctsdb.DB {
	t.Helper()
	db, err := ctsdb.Open(filepath.Join(t.TempDir(), "tsdb"), ctsdb.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func openStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "knot.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func udp(v uint64) []wire.Counter {
	return []wire.Counter{{Section: "mod-stats", Item: "request-protocol", ID: "udp4", Value: v}}
}

func near(a, b float64) bool { return math.Abs(a-b) < 1e-6 }

func TestIncreasesStartAtZeroSurviveRestartsAndSpreadGaps(t *testing.T) {
	db := openTSDB(t)
	ctx := context.Background()
	min := time.Minute.Milliseconds()
	t0 := time.Date(2026, 11, 16, 10, 0, 0, 0, time.UTC).UnixMilli()
	// Another owner and a per-zone series must not leak into u1's server-wide totals.
	writeCounters(ctx, db, "u2", "c9", udp(1), t0+min)
	writeCounters(ctx, db, "u2", "c9", udp(999_999), t0+2*min)
	writeCounters(ctx, db, "u1", "c1", []wire.Counter{{Section: "mod-stats", Item: "request-protocol", ID: "udp4", Zone: "example.com.", Value: 5}}, t0+min)
	writeCounters(ctx, db, "u1", "c1", []wire.Counter{{Section: "mod-stats", Item: "request-protocol", ID: "udp4", Zone: "example.com.", Value: 9}}, t0+2*min)

	// Knot had served 12M queries before the collector's first reading: that is a baseline.
	for _, s := range []struct {
		at int64
		v  uint64
	}{{t0, 12_000_000}, {t0 + min, 12_000_100}, {t0 + 2*min, 40}, {t0 + 62*min, 640}} {
		if err := writeCounters(ctx, db, "u1", "c1", udp(s.v), s.at); err != nil {
			t.Fatal(err)
		}
	}
	inc, err := readIncreases(ctx, db, counterQuery{UserID: "u1", Items: []string{"request-protocol"}, Start: t0, End: t0 + 62*min, Step: 10 * min})
	if err != nil {
		t.Fatal(err)
	}
	if got := inc.sumItem("request-protocol"); !near(got, 740) {
		t.Fatalf("total %v, want 100 + 40 after the restart + 600 over the gap", got)
	}
	if inc.Pairs != 3 || inc.From != t0 {
		t.Fatalf("pairs %d from %d", inc.Pairs, inc.From)
	}
	b := inc.Series[seriesKey{"request-protocol", "udp4"}]
	// 600 accrued evenly over 60 minutes: 10 per minute, so 80 lands in the first bucket (2..10 min)
	// beside the 140 from the first two readings, and 100 in each full bucket after.
	if !near(b[0], 140+80) || !near(b[1], 100) || !near(b[5], 100) {
		t.Fatalf("buckets %v", b)
	}

	zone, err := readIncreases(ctx, db, counterQuery{UserID: "u1", Zone: "example.com.", Items: []string{"request-protocol"}, Start: t0, End: t0 + 62*min, Step: 62 * min})
	if err != nil || !near(zone.sumItem("request-protocol"), 4) {
		t.Fatalf("zone total %v %v", zone.sumItem("request-protocol"), err)
	}

	// A fresh collector with one reading so far shows nothing, and says it is collecting.
	writeCounters(ctx, db, "u3", "c3", udp(5_000_000), t0)
	fresh, err := readIncreases(ctx, db, counterQuery{UserID: "u3", Start: t0 - min, End: t0 + min, Step: min})
	if err != nil || fresh.Pairs != 0 || fresh.sumItem("request-protocol") != 0 {
		t.Fatalf("fresh %+v %v", fresh, err)
	}
}

func TestSpreadClipsToRange(t *testing.T) {
	b := make([]float64, 3)
	spread(b, 100, 10, 130, 90, 120, 30) // 1 per ms-unit; 10 before the range is dropped
	if !near(b[0], 10) || !near(b[1], 10) || !near(b[2], 0) {
		t.Fatalf("%v", b)
	}
}

func TestZonesKeepSerialChangeTimes(t *testing.T) {
	s := openStore(t)
	ctx := context.Background()
	z := func(name string, serial uint32) wire.Zone {
		return wire.Zone{Name: name, Role: "primary", Serial: serial, HasSerial: true}
	}
	if err := s.SaveZones(ctx, "u1", "c1", []wire.Zone{z("a.", 1), z("b.", 1)}, 1000); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveZones(ctx, "u1", "c1", []wire.Zone{z("a.", 1), z("c.", 3)}, 2000); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveZones(ctx, "u1", "c1", []wire.Zone{z("a.", 2), z("c.", 3)}, 3000); err != nil {
		t.Fatal(err)
	}
	rows, err := s.Zones(ctx, "u1", "")
	if err != nil || len(rows) != 2 {
		t.Fatalf("rows %+v %v", rows, err)
	}
	if rows[0].Zone != "a." || rows[0].Serial != 2 || rows[0].SerialChangedAt != 3000 || rows[0].SeenAt != 3000 {
		t.Fatalf("a: %+v", rows[0])
	}
	if rows[1].Zone != "c." || rows[1].SerialChangedAt != 2000 {
		t.Fatalf("c: %+v", rows[1])
	}
	if other, _ := s.Zones(ctx, "u2", ""); len(other) != 0 {
		t.Fatal("another owner sees u1's zones")
	}
}

func bucketAt(minute int64) wire.Dnstap {
	return wire.Dnstap{
		MinuteMs: minute,
		Totals: []wire.Total{
			{Zone: "example.com.", QType: "A", RCode: "NOERROR", Count: 70},
			{Zone: "example.com.", QType: "AAAA", RCode: "NXDOMAIN", Count: 30},
		},
		Names: []wire.Name{
			{Zone: "example.com.", QName: "www.example.com.", QType: "A", RCode: "NOERROR", Count: 60},
			{Zone: "example.com.", QName: "mail.example.com.", QType: "A", RCode: "NOERROR", Count: 10},
			{Zone: "example.com.", QName: "nope.example.com.", QType: "AAAA", RCode: "NXDOMAIN", Count: 20},
			{Zone: "example.com.", QName: wire.Other, QType: "AAAA", RCode: "NXDOMAIN", Count: 10},
		},
		Clients: []wire.Client{{Subnet: "192.0.2.0/24", Count: 100}},
		Sizes:   []wire.Size{{UpTo: 128, Count: 90}, {UpTo: 0, Count: 10}},
	}
}

func TestDnstapStoreQueriesRollupAndPrune(t *testing.T) {
	s := openStore(t)
	s.TopHourly = 1
	ctx := context.Background()
	now := time.Date(2026, 11, 16, 12, 30, 0, 0, time.UTC).UnixMilli()
	old := hourOf(now) - 5*hourMs
	for _, m := range []int64{old, old + minuteMs, hourOf(now) - minuteMs} {
		if err := s.SaveDnstap(ctx, "u1", "c1", bucketAt(m), now); err != nil {
			t.Fatal(err)
		}
	}
	// A resent part replaces rows instead of adding to them.
	if err := s.SaveDnstap(ctx, "u1", "c1", bucketAt(old), now); err != nil {
		t.Fatal(err)
	}
	q := Query{UserID: "u1", Start: old, End: now, Step: hourMs}
	b, err := s.Dnstap(ctx, q, "rcode", 10)
	if err != nil {
		t.Fatal(err)
	}
	if b.Total != 300 || !b.Exact || len(b.Rows) != 2 || b.Rows[0] != (Row{"NOERROR", 210}) {
		t.Fatalf("rcode: %+v", b)
	}
	b, err = s.Dnstap(ctx, Query{UserID: "u1", QType: "AAAA", Start: old, End: now}, "qname", 10)
	if err != nil {
		t.Fatal(err)
	}
	if b.Total != 90 || !b.Exact || len(b.Rows) != 1 || b.Rows[0] != (Row{"nope.example.com.", 60}) || b.Other != 30 || !b.Approximate {
		t.Fatalf("qname by qtype: %+v", b)
	}
	b, err = s.Dnstap(ctx, Query{UserID: "u1", QName: "www.example.com.", Start: old, End: now}, "qtype", 10)
	if err != nil || b.Total != 180 || b.Exact {
		t.Fatalf("filtered by name: %+v %v", b, err)
	}
	b, err = s.Dnstap(ctx, Query{UserID: "u1", Zone: "example.com.", Start: old, End: now}, "client", 10)
	if err != nil || len(b.Ignored) != 1 || b.Ignored[0] != "zone" || b.Rows[0].Count != 300 {
		t.Fatalf("clients: %+v %v", b, err)
	}
	if b, _ := s.Dnstap(ctx, Query{UserID: "u2", Start: old, End: now}, "rcode", 10); b.Total != 0 {
		t.Fatal("another owner sees u1's dnstap")
	}
	if _, err := s.Dnstap(ctx, q, "bogus", 10); err == nil || !strings.Contains(err.Error(), "unknown dimension") {
		t.Fatal("bogus dimension accepted")
	}

	// Rolling settles hours older than the lag; the newest hour stays in minutes.
	n, err := s.Rollup(ctx, now)
	if err != nil || n == 0 {
		t.Fatalf("rollup %d %v", n, err)
	}
	rolled, _ := s.rolledUntil(ctx)
	if rolled != hourOf(now-rollLag.Milliseconds()) {
		t.Fatalf("rolled until %d", rolled)
	}
	// A range starting before minute retention reads the hourly rows, which must match.
	hq := q
	hq.MinuteFrom = now
	hb, err := s.Dnstap(ctx, hq, "rcode", 10)
	if err != nil || hb.Total != 300 {
		t.Fatalf("hourly total %+v %v", hb, err)
	}
	names, err := s.Dnstap(ctx, hq, "qname", 10)
	if err != nil {
		t.Fatal(err)
	}
	var sum int64
	for _, r := range names.Rows {
		sum += r.Count
	}
	if sum+names.Other != 300 || len(names.Rows) > 3 {
		t.Fatalf("hourly names do not add up: %+v", names)
	}

	// Retention drops old minutes; an unknown account loses everything.
	if _, err := s.Prune(ctx, Retention{MinuteMs: 2 * hourMs}, now, []string{"u1"}); err != nil {
		t.Fatal(err)
	}
	mb, _ := s.Dnstap(ctx, Query{UserID: "u1", Start: old, End: now}, "rcode", 10)
	if mb.Total != 100 {
		t.Fatalf("after minute retention %d", mb.Total)
	}
	s.SaveZones(ctx, "gone", "c2", []wire.Zone{{Name: "x."}}, now)
	s.SaveDnstap(ctx, "gone", "c2", bucketAt(old), now)
	res, err := s.Prune(ctx, Retention{}, now, []string{"u1"})
	if err != nil || res.Zones != 1 || res.Minute == 0 {
		t.Fatalf("prune removed account: %+v %v", res, err)
	}
}

func TestSerialArithmetic(t *testing.T) {
	if !serialLess(1, 2) || serialLess(2, 1) || serialLess(5, 5) {
		t.Fatal("plain order")
	}
	if !serialLess(4294967295, 1) || serialLess(1, 4294967295) {
		t.Fatal("wraparound")
	}
}

func TestDriftNeedsTimeToSettle(t *testing.T) {
	now := int64(10 * 3600 * 1000)
	rows := []ZoneRow{
		{CollectorID: "p", Zone: "a.", Role: "primary", Serial: 10, HasSerial: true, SerialChangedAt: now - 20*minuteMs},
		{CollectorID: "s", Zone: "a.", Role: "secondary", Serial: 9, HasSerial: true, ExpireAt: now + hourMs},
		{CollectorID: "p", Zone: "b.", Role: "primary", Serial: 7, HasSerial: true, SerialChangedAt: now - 5*minuteMs},
		{CollectorID: "s", Zone: "b.", Role: "secondary", Serial: 6, HasSerial: true},
		{CollectorID: "gone", Zone: "c.", Role: "primary", Serial: 1, HasSerial: true},
	}
	views := zoneViews(rows, map[string]string{"p": "ns1", "s": "ns2"}, now)
	if len(views) != 2 {
		t.Fatalf("views %+v", views)
	}
	if !views[0].Drift || !views[0].Rows[1].Behind || *views[0].PrimarySerial != 10 || views[0].ExpireAt != now+hourMs {
		t.Fatalf("a: %+v", views[0])
	}
	if views[1].Drift {
		t.Fatal("b drifted before the primary's change could reach the secondary")
	}
}

func TestChecks(t *testing.T) {
	db := openTSDB(t)
	ctx := context.Background()
	now := time.Now()
	nowMs := now.UnixMilli()
	rc := func(nx, fail uint64) []wire.Counter {
		return []wire.Counter{
			{Section: "mod-stats", Item: "response-code", ID: "NOERROR", Value: nx},
			{Section: "mod-stats", Item: "response-code", ID: "SERVFAIL", Value: fail},
		}
	}
	writeCounters(ctx, db, "u1", "c1", rc(0, 0), nowMs-10*minuteMs)
	writeCounters(ctx, db, "u1", "c1", rc(950, 50), nowMs-5*minuteMs)
	typ := &Type{TSDB: db}
	servers := []Server{
		{CollectorID: "c1", Name: "ns1", Enabled: true, Connected: true, ConnectedSince: nowMs - hourMs, FirstSeen: 1, ReportedOn: true, Status: "ok", ModStats: true, LastStatsMs: nowMs},
		{CollectorID: "c2", Name: "ns2", Enabled: true, FirstSeen: 1, LastSeen: nowMs - hourMs},
		{CollectorID: "c3", Name: "ns3", Enabled: true, Connected: true, ReportedOn: true, FirstSeen: 1, Status: "error", Error: "cannot open Knot's control socket"},
		{CollectorID: "c4", Name: "ns4", Enabled: true, Connected: true, ConnectedSince: nowMs - hourMs, FirstSeen: 1, ReportedOn: true, Status: "ok",
			LastStatsMs: nowMs, Dnstap: &wire.DnstapState{Socket: "/run/x.sock", Listening: true}},
	}
	zones := []ZoneView{{Zone: "a.", Rows: []ZoneSide{
		{ZoneRow: ZoneRow{CollectorID: "c1", Role: "secondary", ExpireAt: nowMs + hourMs}, Server: "ns1"},
		{ZoneRow: ZoneRow{CollectorID: "c4", Role: "secondary", ExpireAt: nowMs - minuteMs}, Server: "ns4"},
	}}}
	got := map[string]string{}
	for _, c := range typ.checks(ctx, "u1", servers, zones, now) {
		got[c.Kind+"/"+c.Server] = c.Level
	}
	want := map[string]string{
		"servfail/ns1": "warn", "offline/ns2": "bad", "unreachable/ns3": "bad",
		"no_mod_stats/ns4": "warn", "dnstap_quiet/ns4": "warn", "expiring/ns1": "warn", "expired/ns4": "bad",
	}
	for k, lvl := range want {
		if got[k] != lvl {
			t.Errorf("%s: got %q, want %q (all: %v)", k, got[k], lvl, got)
		}
	}
	if len(got) != len(want) {
		t.Errorf("unexpected checks: %v", got)
	}
}

// The page reads dnstap state by these names (dnstap.js); Go's default
// capitalised keys hid the Client networks card.
func TestDnstapStateJSONNames(t *testing.T) {
	raw, err := json.Marshal(Server{Dnstap: &wire.DnstapState{Socket: "/s", Listening: true, LastFrameMs: 5, Clients: true}})
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Dnstap map[string]any `json:"dnstap"`
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"socket", "listening", "connected", "last_frame_ms", "dropped", "clients"} {
		if _, ok := got.Dnstap[k]; !ok {
			t.Errorf("dnstap has no %q: %s", k, raw)
		}
	}
	if got.Dnstap["clients"] != true {
		t.Errorf("clients: %s", raw)
	}
}
