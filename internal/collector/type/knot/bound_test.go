package knot

import (
	"context"
	"database/sql"
	"net/netip"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"certainstats/internal/collector/type/knot/wire"
)

// boundMinute is a minute from a collector that cuts with the bound rule:
// per-name counts as given (already cut, with err as the cut), name rows one
// per name and qtype A, and matching totals.
func boundMinute(at int64, err uint64, names map[string]uint64) wire.Dnstap {
	d := wire.Dnstap{MinuteMs: at, Bounded: true, QNameErr: err, NameErr: err}
	var sum uint64
	for n, c := range names {
		d.QNames = append(d.QNames, wire.QName{Zone: "example.com.", QName: n, Count: c})
		d.Names = append(d.Names, wire.Name{Zone: "example.com.", QName: n, QType: "A", RCode: "NOERROR", Count: c})
		sum += c
	}
	d.Totals = []wire.Total{{Zone: "example.com.", QType: "A", RCode: "NOERROR", Count: sum}}
	return d
}

type nameStore interface {
	SaveDnstap(ctx context.Context, userID, collectorID string, d wire.Dnstap, now int64) error
	Dnstap(ctx context.Context, q Query, dim string, limit int) (Breakdown, error)
	Rollup(ctx context.Context, now int64) (int, error)
}

// TestNameErrorsAddUp: on both engines, minute answers carry the collector's
// cuts, and the hour rollup cuts names with the bound rule (ties drop
// together) and adds its own cut to the error.
func TestNameErrorsAddUp(t *testing.T) {
	ctx := context.Background()
	sqlStore := openStore(t)
	sqlStore.TopHourly = 2
	blocks, _ := openTopNStore(t, 2)
	h := time.Date(2026, 11, 16, 10, 0, 0, 0, time.UTC).UnixMilli()
	for name, s := range map[string]nameStore{"sql": sqlStore, "blocks": blocks} {
		if err := s.SaveDnstap(ctx, "u1", "c1", boundMinute(h, 0, map[string]uint64{"a.example.com.": 10, "b.example.com.": 8, "c.example.com.": 3}), h); err != nil {
			t.Fatal(err)
		}
		if err := s.SaveDnstap(ctx, "u1", "c1", boundMinute(h+minuteMs, 2, map[string]uint64{"a.example.com.": 1, "c.example.com.": 9, "d.example.com.": 9}), h); err != nil {
			t.Fatal(err)
		}
		mins := Query{UserID: "u1", Start: h, End: h + hourMs, MinuteFrom: h}
		b, err := s.Dnstap(ctx, mins, "qname", 10)
		if err != nil {
			t.Fatal(err)
		}
		if !b.Bounded || b.ErrBound != 2 || len(b.Rows) != 4 || b.Rows[0] != (Row{"c.example.com.", 12}) {
			t.Fatalf("%s minutes: %+v", name, b)
		}
		for {
			n, err := s.Rollup(ctx, h+4*hourMs)
			if err != nil {
				t.Fatal(err)
			}
			if n == 0 {
				break
			}
		}
		// The hour: c 12, a 11, d 9, b 8. With a cap of 2 the cut is 9, so c
		// and a stay; the error is the minutes' 2 plus the hour's 9.
		hours := Query{UserID: "u1", Start: h, End: h + hourMs, MinuteFrom: h + hourMs}
		b, err = s.Dnstap(ctx, hours, "qname", 10)
		if err != nil {
			t.Fatal(err)
		}
		if !b.Bounded || b.ErrBound != 11 || len(b.Rows) != 2 || b.Rows[0] != (Row{"c.example.com.", 12}) || b.Rows[1] != (Row{"a.example.com.", 11}) || b.Other != 17 || b.Total != 40 {
			t.Fatalf("%s hours: %+v", name, b)
		}
		// A qtype filter reads the split rows, with their own cut.
		b, err = s.Dnstap(ctx, Query{UserID: "u1", QType: "A", Start: h, End: h + hourMs, MinuteFrom: h + hourMs}, "qname", 10)
		if err != nil || !b.Bounded || b.ErrBound != 11 || len(b.Rows) != 2 {
			t.Fatalf("%s hours by qtype: %+v %v", name, b, err)
		}
	}
}

// TestOldCollectorIsUnbounded: a collector that does not report its cuts
// still gets per-name counts (summed from its rows), marked unbounded.
func TestOldCollectorIsUnbounded(t *testing.T) {
	ctx := context.Background()
	sqlStore := openStore(t)
	blocks, _ := openTopNStore(t, 1000)
	at := time.Date(2026, 11, 16, 10, 0, 0, 0, time.UTC).UnixMilli()
	d := wire.Dnstap{MinuteMs: at,
		Totals: []wire.Total{{Zone: "example.com.", QType: "A", RCode: "NOERROR", Count: 7}, {Zone: "example.com.", QType: "AAAA", RCode: "NOERROR", Count: 3}},
		Names: []wire.Name{
			{Zone: "example.com.", QName: "www.example.com.", QType: "A", RCode: "NOERROR", Count: 5},
			{Zone: "example.com.", QName: "www.example.com.", QType: "AAAA", RCode: "NOERROR", Count: 3},
			{Zone: "example.com.", QName: "api.example.com.", QType: "A", RCode: "NOERROR", Count: 2},
		}}
	for name, s := range map[string]nameStore{"sql": sqlStore, "blocks": blocks} {
		if err := s.SaveDnstap(ctx, "u1", "c1", d, at); err != nil {
			t.Fatal(err)
		}
		b, err := s.Dnstap(ctx, Query{UserID: "u1", Start: at, End: at + minuteMs, MinuteFrom: at}, "qname", 10)
		if err != nil {
			t.Fatal(err)
		}
		if b.Bounded || len(b.Rows) != 2 || b.Rows[0] != (Row{"www.example.com.", 8}) || b.Rows[1] != (Row{"api.example.com.", 2}) {
			t.Fatalf("%s: %+v", name, b)
		}
	}
}

// TestKnotDBVersion2Keeps: opening a version 2 knot.db adds the new tables
// and keeps the dnstap history.
func TestKnotDBVersion2Keeps(t *testing.T) {
	path := filepath.Join(t.TempDir(), "knot.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	s.Close()
	db, err := sql.Open("sqlite", dsn(path))
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`DROP TABLE dnstap_qnames_minute`, `DROP TABLE dnstap_bounds_minute`,
		`INSERT INTO dnstap_totals_minute (user_id, collector_id, bucket, zone, qtype, rcode, count) VALUES ('u1', 'c1', 60000, 'example.com.', 'A', 'NOERROR', 5)`,
		`PRAGMA user_version = 2`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	db.Close()
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var n, v int
	if err := s.read.QueryRow(`SELECT COUNT(*) FROM dnstap_totals_minute`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("history after upgrade: %d rows (%v)", n, err)
	}
	for _, table := range []string{"dnstap_qnames_minute", "dnstap_nets_minute", "dnstap_netnames_hourly", "dnstap_netcodes_minute"} {
		if err := s.read.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&n); err != nil {
			t.Fatalf("%s missing: %v", table, err)
		}
	}
	if err := s.read.QueryRow(`PRAGMA user_version`).Scan(&v); err != nil || v != schemaVersion {
		t.Fatalf("version %d (%v)", v, err)
	}
}

// netMinute is a minute from a bounded collector with client lists.
func netMinute(at int64, netErr uint64, nets []wire.Net, names []wire.NetName, codes []wire.NetCode) wire.Dnstap {
	d := wire.Dnstap{MinuteMs: at, Bounded: true, Nets: nets, NetNames: names, NetCodes: codes, NetErr: netErr, NetNameErr: netErr, NetCodeErr: netErr}
	sum := map[[2]string]uint64{}
	for _, c := range codes {
		k := [2]string{c.QType, c.RCode}
		if c.QType == wire.Other {
			k = [2]string{"A", "NOERROR"} // totals never fold; a collector counts these exactly
		}
		sum[k] += c.Count
	}
	for k, n := range sum {
		d.Totals = append(d.Totals, wire.Total{Zone: "example.com.", QType: k[0], RCode: k[1], Count: n})
	}
	// Name rows for name-filtered totals (qtypes is their source).
	for _, r := range names {
		d.Names = append(d.Names, wire.Name{Zone: r.Zone, QName: r.QName, QType: "A", RCode: "NOERROR", Count: r.Count})
	}
	return d
}

// TestNetListsAddUp: on both engines, client answers read the list that fits
// the filters, carry the collector's cuts from minutes, and the hour rollup
// cuts networks and their names with the bound rule, adding its own cut.
func TestNetListsAddUp(t *testing.T) {
	ctx := context.Background()
	sqlStore := openStore(t)
	sqlStore.TopHourly = 2
	blocks, _ := openTopNStore(t, 2)
	h := time.Date(2026, 11, 16, 10, 0, 0, 0, time.UTC).UnixMilli()
	const z = "example.com."
	A, B, C, D := "10.0.0.0/24", "10.0.1.0/24", "2001:db8:1::/48", "10.0.2.0/24"
	m1 := netMinute(h, 0,
		[]wire.Net{{Subnet: A, Count: 10}, {Subnet: B, Count: 8}, {Subnet: C, Count: 3}},
		[]wire.NetName{{Subnet: A, Zone: z, QName: "a." + z, Count: 10}, {Subnet: B, Zone: z, QName: "b." + z, Count: 8}, {Subnet: C, Zone: z, QName: "c." + z, Count: 3}},
		[]wire.NetCode{{Subnet: A, Zone: z, QType: "A", RCode: "NOERROR", Count: 10}, {Subnet: B, Zone: z, QType: "A", RCode: "NOERROR", Count: 8}, {Subnet: C, Zone: z, QType: "AAAA", RCode: "NXDOMAIN", Count: 3}})
	// The second minute folded 4 queries of A at a cut of 2; folded rows keep
	// only their zone.
	m2 := netMinute(h+minuteMs, 2,
		[]wire.Net{{Subnet: C, Count: 9}, {Subnet: D, Count: 9}, {Subnet: wire.Other, Count: 4}},
		[]wire.NetName{{Subnet: C, Zone: z, QName: "c." + z, Count: 9}, {Subnet: D, Zone: z, QName: "d." + z, Count: 9}, {Subnet: wire.Other, Zone: z, QName: wire.Other, Count: 4}},
		[]wire.NetCode{{Subnet: C, Zone: z, QType: "AAAA", RCode: "NXDOMAIN", Count: 9}, {Subnet: D, Zone: z, QType: "A", RCode: "NOERROR", Count: 9}, {Subnet: wire.Other, Zone: z, QType: wire.Other, RCode: wire.Other, Count: 4}})
	ten, six := netip.MustParsePrefix("10.0.0.0/8"), netip.MustParsePrefix("2001:db8::/32")
	type check struct {
		label string
		q     Query
		dim   string
		rows  []Row
		other int64
		total int64
		err   int64
	}
	answers := map[string][]Breakdown{}
	for name, s := range map[string]nameStore{"sql": sqlStore, "blocks": blocks} {
		for _, d := range []wire.Dnstap{m1, m2} {
			if err := s.SaveDnstap(ctx, "u1", "c1", d, h); err != nil {
				t.Fatal(err)
			}
		}
		mins := Query{UserID: "u1", Start: h, End: h + hourMs, MinuteFrom: h}
		hours := Query{UserID: "u1", Start: h, End: h + hourMs, MinuteFrom: h + hourMs}
		with := func(q Query, f func(*Query)) Query { f(&q); return q }
		run := func(checks []check) {
			for _, c := range checks {
				b, err := s.Dnstap(ctx, c.q, c.dim, 10)
				if err != nil {
					t.Fatal(err)
				}
				if !b.Bounded || b.ErrBound != c.err || !reflect.DeepEqual(b.Rows, c.rows) || b.Other != c.other || b.Total != c.total {
					t.Errorf("%s %s: got rows %v other %d total %d err %d bounded %v; want rows %v other %d total %d err %d",
						name, c.label, b.Rows, b.Other, b.Total, b.ErrBound, b.Bounded, c.rows, c.other, c.total, c.err)
				}
				answers[c.label] = append(answers[c.label], b)
			}
		}
		run([]check{
			{"minutes networks", mins, "client", []Row{{C, 12}, {A, 10}, {D, 9}, {B, 8}}, 4, 43, 2},
			// A network's total comes from the per-network counts.
			{"minutes names of 10/8", with(mins, func(q *Query) { q.Client = ten }), "qname", []Row{{"a." + z, 10}, {"d." + z, 9}, {"b." + z, 8}}, 0, 27, 2},
			{"minutes qtypes of 2001:db8::/32", with(mins, func(q *Query) { q.Client = six }), "qtype", []Row{{"AAAA", 12}}, 0, 12, 2},
			{"minutes networks asking c", with(mins, func(q *Query) { q.QName = "c." + z }), "client", []Row{{C, 12}}, 0, 12, 2},
		})
		for {
			n, err := s.Rollup(ctx, h+4*hourMs)
			if err != nil {
				t.Fatal(err)
			}
			if n == 0 {
				break
			}
		}
		// The hour: networks C 12, A 10, D 9, B 8 and Other 4. A cap of 2 cuts
		// at 9, so D and B fold; the error is the minutes' 2 plus the hour's 9.
		run([]check{
			{"hour networks", hours, "client", []Row{{C, 12}, {A, 10}}, 21, 43, 11},
			// D and B folded out of both lists in the hour, so 10/8 holds A alone.
			{"hour names of 10/8", with(hours, func(q *Query) { q.Client = ten }), "qname", []Row{{"a." + z, 10}}, 0, 10, 11},
		})
		// A client filter with a name and a qtype: the qtype list cannot
		// split by name, so the name is reported as ignored.
		b, err := s.Dnstap(ctx, with(mins, func(q *Query) { q.Client = ten; q.QName = "a." + z }), "qtype", 10)
		if err != nil || !reflect.DeepEqual(b.Ignored, []string{"qname"}) {
			t.Errorf("%s crossed filters: ignored %v (%v)", name, b.Ignored, err)
		}
	}
}

// TestRollupKeysWithNUL: a key holding 0x00 survives the hour cut. The sql
// rollup once joined keys with 0x00 and split them back, so a zone holding
// one came back as too many columns.
func TestRollupKeysWithNUL(t *testing.T) {
	ctx := context.Background()
	sqlStore := openStore(t)
	sqlStore.TopHourly = 1
	blocks, _ := openTopNStore(t, 1)
	h := time.Date(2026, 11, 16, 10, 0, 0, 0, time.UTC).UnixMilli()
	zone := "ex\x00ample."
	d := wire.Dnstap{MinuteMs: h, Bounded: true}
	for n, c := range map[string]uint64{"a.\x00x.": 9, "b.": 3, "c.": 2} {
		d.QNames = append(d.QNames, wire.QName{Zone: zone, QName: n, Count: c})
		d.Names = append(d.Names, wire.Name{Zone: zone, QName: n, QType: "A", RCode: "NOERROR", Count: c})
	}
	d.Totals = []wire.Total{{Zone: zone, QType: "A", RCode: "NOERROR", Count: 14}}
	for name, s := range map[string]nameStore{"sql": sqlStore, "blocks": blocks} {
		if err := s.SaveDnstap(ctx, "u1", "c1", d, h); err != nil {
			t.Fatal(err)
		}
		for {
			n, err := s.Rollup(ctx, h+4*hourMs)
			if err != nil {
				t.Fatalf("%s rollup: %v", name, err)
			}
			if n == 0 {
				break
			}
		}
		b, err := s.Dnstap(ctx, Query{UserID: "u1", Zone: zone, Start: h, End: h + hourMs, MinuteFrom: h + hourMs}, "qname", 10)
		if err != nil {
			t.Fatal(err)
		}
		if len(b.Rows) != 1 || b.Rows[0] != (Row{"a.\x00x.", 9}) || b.Other != 5 || b.Total != 14 || b.ErrBound != 3 {
			t.Fatalf("%s hours: %+v", name, b)
		}
	}
}
