//go:build knotreport

// Knot dnstap on the generic top-N store (internal/tblk), for the storage
// report: KNOTREPORT_STORE=topn-sql or topn-blocks. knotTopN wraps the
// production TopN (topnstore.go), so the report and TestTopNMatchesStore
// compare it with today's Store.
package knot

import (
	"context"
	"fmt"
	"net/netip"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"certainstats/internal/collector/type/knot/wire"
	"certainstats/internal/tblk"
)

// knotTopN is the production TopN with the report's extras.
type knotTopN struct {
	*TopN
	top int
}

func openKnotTopN(t *testing.T, dir string, engine tblk.Engine, top int) *knotTopN {
	t.Helper()
	registerTracer(t)
	keep := Retention{MinuteMs: (7 * 24 * time.Hour).Milliseconds(), HourlyMs: DefaultHourlyRetention.Milliseconds(), DailyMs: DefaultDailyRetention.Milliseconds()}
	k, err := openTopN(dir, nil, top, keep, 0, tblk.Options{Engine: engine, Driver: "sqlite-knotreport"})
	if err != nil {
		t.Fatal(err)
	}
	return &knotTopN{TopN: k, top: top}
}

func (k *knotTopN) reportTop() int { return k.top }

func (k *knotTopN) tier(ctx context.Context, g tblk.Grain, user, col string, d wire.Dnstap) error {
	for fam, rows := range knotRows(d) {
		if fam == "minutes" {
			continue
		}
		if err := k.s.AppendTier(ctx, g, user, col, fam, d.MinuteMs, rows); err != nil {
			return err
		}
	}
	return nil
}

func (k *knotTopN) reportHourly(ctx context.Context, user, col string, d wire.Dnstap) error {
	return k.tier(ctx, tblk.Hour, user, col, d)
}

func (k *knotTopN) reportDaily(ctx context.Context, user, col string, d wire.Dnstap) error {
	return k.tier(ctx, tblk.Day, user, col, d)
}

func (k *knotTopN) reportFlush(ctx context.Context) error { return k.s.FlushTier(ctx) }

func (k *knotTopN) reportOther(ctx context.Context, user string) (map[string]int64, error) {
	st, err := k.s.Stats(ctx)
	if err != nil {
		return nil, err
	}
	return map[string]int64{"names_minute": st.FoldedRows["qtypes"][tblk.Minute], "clients_minute": st.FoldedRows["netnames"][tblk.Minute]}, nil
}

func (k *knotTopN) reportFocus(ctx context.Context, user string, since int64) (zone, qname, netKey string) {
	top := func(fam, by string) string {
		r, err := k.s.Query(ctx, tblk.Query{Family: fam, Owner: user, Start: since, End: since + dayMs, By: by, Limit: 1, MinuteFrom: since})
		if err != nil || len(r.Rows) == 0 {
			return ""
		}
		return r.Rows[0].Key
	}
	zone, qname = top("totals", "zone"), top("qnames", "qname")
	if n := top("nets", "net"); n != "" {
		netKey, _ = netKeyOf(n)
	}
	return zone, qname, netKey
}

// TestTopNMatchesStore feeds the same minutes to Store and to Knot on both
// topn engines, and checks every drill-down gives the same answer.
func TestTopNMatchesStore(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	cur := openTraced(t, filepath.Join(dir, "current.db"))
	defer cur.Close()
	tsql := openKnotTopN(t, filepath.Join(dir, "topn-sql"), tblk.SQL, 100)
	defer tsql.Close()
	tblk := openKnotTopN(t, filepath.Join(dir, "topn-blocks"), tblk.Blocks, 100)
	defer tblk.Close()
	cur.TopHourly = 100

	g := newGenerator(1, reportCfg{Zones: 5, Names: 2000, Nets: 400, Fold: "subnet"})
	const user, col = "5b0c8a2e-3f6d-4c1b-9e7a-2d4f6a8c0e11", "9d3e7f10-0000-4a5b-8c6d-0e1f2a3b4c5d"
	end := time.Date(2026, 11, 18, 0, 0, 0, 0, time.UTC).UnixMilli()
	first := end - 3*hourMs
	for m := first; m < end; m += minuteMs {
		d := g.sample(3000).bucket(m, 50, 20, 50, 1)
		d.QueriesOnly = m == first+30*minuteMs
		for _, s := range []reportStore{cur, tsql, tblk} {
			if err := s.SaveDnstap(ctx, user, col, d, m+minuteMs); err != nil {
				t.Fatal(err)
			}
		}
	}
	zone, qname, netKey := cur.reportFocus(ctx, user, end-dayMs)
	for _, s := range []reportStore{tsql, tblk} {
		if z, q, n := s.reportFocus(ctx, user, end-dayMs); z != zone || q != qname || n != netKey {
			t.Fatalf("focus differs: current %s %s %s, topn %s %s %s", zone, qname, netKey, z, q, n)
		}
	}
	p, err := netip.ParsePrefix(netLabel(netKey))
	if err != nil {
		t.Fatal(err)
	}
	client := netip.PrefixFrom(p.Addr(), 16).Masked()
	if p.Addr().Is6() {
		client = netip.PrefixFrom(p.Addr(), 32).Masked()
	}
	type filter struct {
		name string
		set  func(*Query)
	}
	filtersList := []filter{
		{"none", func(*Query) {}},
		{"zone", func(q *Query) { q.Zone = zone }},
		{"qname", func(q *Query) { q.QName = qname }},
		{"client", func(q *Query) { q.Client = client }},
		{"collector+rcode", func(q *Query) { q.CollectorID = col; q.RCode = "NXDOMAIN" }},
		{"missing zone", func(q *Query) { q.Zone = "nowhere.example." }},
	}
	allDims := []string{"qname", "zone", "qtype", "rcode", "client", "size"}
	compare := func(label string, a, b reportStore, base Query, flist []filter, dims []string) {
		data := 0
		for _, f := range flist {
			for _, dim := range dims {
				q := base
				f.set(&q)
				want, err := a.Dnstap(ctx, q, dim, 50)
				if err != nil {
					t.Fatal(err)
				}
				got, err := b.Dnstap(ctx, q, dim, 50)
				if err != nil {
					t.Fatal(err)
				}
				if want.Total > 0 {
					data++
				}
				if !reflect.DeepEqual(want, got) {
					t.Errorf("%s %s by %s differs:\nwant total %d other %d rows %v flags %v/%v/%v ignored %v\ngot  total %d other %d rows %v flags %v/%v/%v ignored %v",
						label, f.name, dim, want.Total, want.Other, want.Rows, want.Approximate, want.Exact, want.QueriesOnly, want.Ignored,
						got.Total, got.Other, got.Rows, got.Approximate, got.Exact, got.QueriesOnly, got.Ignored)
				}
			}
		}
		if data == 0 {
			t.Errorf("%s compared no traffic", label)
		}
	}
	for _, span := range []int64{hourMs, 2 * hourMs} {
		q := Query{UserID: user, Start: end - span, End: end, Step: 5 * minuteMs, MinuteFrom: end - 7*dayMs}
		compare(fmt.Sprintf("store/sql minutes %dh", span/hourMs), cur, tsql, q, filtersList, allDims)
		compare(fmt.Sprintf("sql/blocks minutes %dh", span/hourMs), tsql, tblk, q, filtersList, allDims)
	}
	for _, s := range []reportStore{cur, tsql, tblk} {
		for {
			n, err := s.Rollup(ctx, end)
			if err != nil {
				t.Fatal(err)
			}
			if n == 0 {
				break
			}
		}
	}
	// Hourly reads: every store cuts names and client lists with the same rule.
	hourly := Query{UserID: user, Start: first, End: first + hourMs, Step: hourMs, MinuteFrom: end}
	mixed := Query{UserID: user, Start: first, End: end, Step: hourMs, MinuteFrom: end}
	compare("store/sql hourly", cur, tsql, hourly, filtersList, allDims)
	compare("store/sql hourly+minutes", cur, tsql, mixed, filtersList, allDims)
	compare("sql/blocks hourly", tsql, tblk, hourly, filtersList, allDims)
	compare("sql/blocks hourly+minutes", tsql, tblk, mixed, filtersList, allDims)
	// Name answers carry the cut errors: minutes cut at 50 rows and hours at
	// 100, so the window has an error, and every minute came bounded.
	for _, s := range []reportStore{cur, tsql, tblk} {
		b, err := s.Dnstap(ctx, mixed, "qname", 50)
		if err != nil {
			t.Fatal(err)
		}
		if !b.Bounded || b.ErrBound <= 0 || b.ErrBound > b.Total/50 {
			t.Fatalf("name answer bounds: bounded %v, error %d of %d", b.Bounded, b.ErrBound, b.Total)
		}
		// Networks were cut at 50 a minute from 400, so the network list has
		// an error too, and still adds up to the total.
		c, err := s.Dnstap(ctx, mixed, "client", 0)
		if err != nil {
			t.Fatal(err)
		}
		var listed int64
		for _, r := range c.Rows {
			listed += r.Count
		}
		if !c.Bounded || c.ErrBound <= 0 || c.ErrBound > c.Total/50 || listed+c.Other != c.Total {
			t.Fatalf("network answer: bounded %v, error %d, listed %d + other %d of %d", c.Bounded, c.ErrBound, listed, c.Other, c.Total)
		}
	}
}
