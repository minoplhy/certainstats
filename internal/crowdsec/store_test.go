package crowdsec

import (
	"context"
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"certainstats/internal/crowdsec/wire"

	"github.com/fxamacker/cbor/v2"
)

func openTest(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "crowdsec.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func fixture(t *testing.T) []wire.Alert {
	t.Helper()
	raw, err := os.ReadFile("testdata/lapi_alerts.json")
	if err != nil {
		t.Fatal(err)
	}
	var list []wire.Alert
	if err := json.Unmarshal(raw, &list); err != nil {
		t.Fatal(err)
	}
	return list
}

func ms(v string) int64 {
	t, err := time.Parse(time.RFC3339Nano, v)
	if err != nil {
		panic(err)
	}
	return t.UnixMilli()
}

func filterFor(t *testing.T, q string) Filter {
	t.Helper()
	v, err := url.ParseQuery(q)
	if err != nil {
		t.Fatal(err)
	}
	f, err := ParseFilter(v, time.Date(2026, 11, 14, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	return f
}

const day = "start=1794268800000&end=1794355200000" // 2026-11-10 00:00 to 2026-11-11 00:00 UTC

func TestFixtureSurvivesTheWire(t *testing.T) {
	list := fixture(t)
	raw, err := cbor.Marshal(wire.Alerts{BatchID: "b", Alerts: list})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "Failed password") || strings.Contains(string(raw), "target_user") {
		t.Fatal("events and meta must not be encoded")
	}
	var back wire.Alerts
	if err := cbor.Unmarshal(raw, &back); err != nil {
		t.Fatal(err)
	}
	if back.Alerts[0].Source.ASName != `Example "Quoted" Net` || len(back.Alerts[0].Decisions) != 2 {
		t.Fatalf("round trip lost data: %+v", back.Alerts[0])
	}
}

func TestNormalize(t *testing.T) {
	list := fixture(t)
	now := ms("2026-11-10T13:00:00Z")
	a := normalize("col_a", &list[0], now)
	if a.Action != "ban+captcha" || a.Country != "NL" || a.ASN != "64500" || a.IP != "203.0.113.7" {
		t.Fatalf("unexpected row %+v", a)
	}
	if a.StartedAt != ms("2026-11-10T10:14:58.123Z") {
		t.Fatalf("alert time must be start_at, got %d", a.StartedAt)
	}
	if a.Decisions[0].UntilMs != ms("2026-11-10T14:15:00Z") || a.Decisions[1].UntilMs != now+59*60_000 {
		t.Fatalf("decision expiry wrong: %+v", a.Decisions)
	}
	b := normalize("col_a", &list[1], now)
	if b.Action != "none" || !strings.HasPrefix(b.UUID, "h:") || !b.Simulated {
		t.Fatalf("unexpected row %+v", b)
	}
	if again := normalize("col_a", &list[1], now+1000); again.UUID != b.UUID {
		t.Fatal("fallback uuid must be stable")
	}
}

func TestInsertDeduplicatesAndRollsUp(t *testing.T) {
	s, ctx := openTest(t), context.Background()
	list := fixture(t)
	stored, dup, newest, err := s.Insert(ctx, "u1", "col_a", list, ms("2026-11-10T13:00:00Z"))
	if err != nil || stored != 2 || dup != 0 {
		t.Fatalf("first insert: stored=%d dup=%d err=%v", stored, dup, err)
	}
	if newest != ms("2026-11-10T11:58:00Z") {
		t.Fatalf("newest start wrong: %d", newest)
	}
	stored, dup, _, err = s.Insert(ctx, "u1", "col_b", list, ms("2026-11-10T14:00:00Z"))
	if err != nil || stored != 1 || dup != 1 {
		// The uuid-less alert hashes its collector, so only the real uuid is shared.
		t.Fatalf("redelivery: stored=%d dup=%d err=%v", stored, dup, err)
	}
	stored, dup, _, _ = s.Insert(ctx, "u1", "col_a", list, ms("2026-11-10T14:00:00Z"))
	if stored != 0 || dup != 2 {
		t.Fatalf("same collector redelivery: stored=%d dup=%d", stored, dup)
	}
	var hours, alerts, decisions, raw int64
	if err := s.read.QueryRow(`SELECT COUNT(*), SUM(alerts), SUM(decisions) FROM rollup_hourly WHERE user_id = 'u1'`).Scan(&hours, &alerts, &decisions); err != nil {
		t.Fatal(err)
	}
	if err := s.read.QueryRow(`SELECT COUNT(*) FROM decisions`).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if hours != 2 || alerts != 3 || decisions != raw {
		t.Fatalf("rollup rows=%d alerts=%d decisions=%d (raw %d), want 2, 3 and raw", hours, alerts, decisions, raw)
	}
	var hour int64
	if err := s.read.QueryRow(`SELECT hour FROM rollup_hourly WHERE scenario = 'crowdsecurity/ssh-bf'`).Scan(&hour); err != nil {
		t.Fatal(err)
	}
	if hour != ms("2026-11-10T10:00:00Z") {
		t.Fatal("late alert must land in its start hour")
	}
	// Another owner sees nothing.
	sum, err := s.Summary(ctx, "u2", filterFor(t, day), ms("2026-11-10T13:00:00Z"))
	if err != nil || sum.Alerts != 0 {
		t.Fatalf("owner isolation: %+v %v", sum, err)
	}
}

func TestQueries(t *testing.T) {
	s, ctx := openTest(t), context.Background()
	if _, _, _, err := s.Insert(ctx, "u1", "col_a", fixture(t), ms("2026-11-10T13:00:00Z")); err != nil {
		t.Fatal(err)
	}
	now := ms("2026-11-10T13:00:00Z")
	sum, err := s.Summary(ctx, "u1", filterFor(t, day), now)
	if err != nil || sum.Alerts != 2 || sum.Events != 17 || sum.UniqueIPs == nil || *sum.UniqueIPs != 2 || sum.Source != "raw" {
		t.Fatalf("summary %+v %v", sum, err)
	}
	if sum.Active["ban"] != 1 || sum.Active["captcha"] != 1 {
		t.Fatalf("active decisions %+v", sum.Active)
	}
	sum, _ = s.Summary(ctx, "u1", filterFor(t, day+"&not_country=NL"), now)
	if sum.Alerts != 1 || sum.Active["ban"] != 0 {
		t.Fatalf("exclude filter: %+v", sum)
	}
	sum, _ = s.Summary(ctx, "u1", filterFor(t, day+"&q=113.7"), now)
	if sum.Alerts != 1 {
		t.Fatalf("search: %+v", sum)
	}
	sum, _ = s.Summary(ctx, "u1", filterFor(t, day+"&q=%25"), now)
	if sum.Alerts != 0 {
		t.Fatal("wildcards in search text must be literal")
	}

	tl, err := s.Timeline(ctx, "u1", filterFor(t, day))
	if err != nil {
		t.Fatal(err)
	}
	var total, decisions int64
	for _, p := range tl.Points {
		total += p[1]
		decisions += p[2]
	}
	if total != 2 || decisions != 2 || tl.WidthMs != 5*60_000 || len(tl.Points) != 288 {
		t.Fatalf("timeline total=%d decisions=%d width=%d points=%d", total, decisions, tl.WidthMs, len(tl.Points))
	}

	g, err := s.Groups(ctx, "u1", filterFor(t, day), fields[5], 10) // asn
	if err != nil || g.Total != 2 || len(g.Items) != 2 {
		t.Fatalf("groups %+v %v", g, err)
	}
	for _, item := range g.Items {
		if item.Value == "64500" && item.Label != `Example "Quoted" Net` {
			t.Fatalf("asn label %+v", item)
		}
	}

	page, err := s.Alerts(ctx, "u1", filterFor(t, day), "events", true, 1, 25)
	if err != nil || page.Total != 2 || page.Items[0].Events != 11 {
		t.Fatalf("alerts %+v %v", page, err)
	}
	one, err := s.Alert(ctx, "u1", page.Items[1].ID)
	if err != nil || len(one.Decisions) != 2 {
		t.Fatalf("alert detail %+v %v", one, err)
	}
	if _, err := s.Alert(ctx, "u2", page.Items[1].ID); err == nil {
		t.Fatal("another owner must not read an alert")
	}
	ip, err := s.IP(ctx, "u1", "203.0.113.7")
	if err != nil || ip.Alerts != 1 || len(ip.Decisions) != 2 || ip.Country != "NL" || len(ip.Scenarios) != 1 {
		t.Fatalf("ip view %+v %v", ip, err)
	}
}

func TestRollupAgreesWithRaw(t *testing.T) {
	s, ctx := openTest(t), context.Background()
	if _, _, _, err := s.Insert(ctx, "u1", "col_a", fixture(t), ms("2026-11-10T13:00:00Z")); err != nil {
		t.Fatal(err)
	}
	long := filterFor(t, "start=1790000000000&end=1794355200000") // more than 7 days: rollup
	short := filterFor(t, day)
	a, _ := s.Summary(ctx, "u1", long, 0)
	b, _ := s.Summary(ctx, "u1", short, 0)
	if a.Source != "rollup" || a.Alerts != b.Alerts || a.Events != b.Events || a.UniqueIPs != nil {
		t.Fatalf("rollup %+v raw %+v", a, b)
	}
	sumTimeline := func(f Filter) (alerts, decisions int64, source string) {
		tl, err := s.Timeline(ctx, "u1", f)
		if err != nil {
			t.Fatal(err)
		}
		for _, p := range tl.Points {
			alerts += p[1]
			decisions += p[2]
		}
		return alerts, decisions, tl.Source
	}
	ra, rd, rs := sumTimeline(long)
	sa, sd, _ := sumTimeline(short)
	if rs != "rollup" || ra != sa || rd != sd || rd == 0 {
		t.Fatalf("timeline rollup alerts=%d decisions=%d, raw alerts=%d decisions=%d", ra, rd, sa, sd)
	}
	// An IP filter needs raw alerts even for a long range.
	c, _ := s.Summary(ctx, "u1", filterFor(t, "start=1790000000000&end=1794355200000&ip=203.0.113.7"), 0)
	if c.Source != "raw" || c.Alerts != 1 {
		t.Fatalf("ip filter %+v", c)
	}
}

func TestFilterIsParameterized(t *testing.T) {
	f := filterFor(t, day+"&scenario=x'%20OR%201=1--&not_machine=a&not_machine=b,c")
	where, args := f.where("u1", false, true, "")
	if strings.Contains(where, "OR 1=1") || strings.Contains(where, "'x") {
		t.Fatalf("value leaked into SQL: %s", where)
	}
	if !strings.Contains(where, "machine_id NOT IN (?,?)") {
		t.Fatalf("repeated params must stay separate values, comma lists unsplit: %s", where)
	}
	if len(args) != 6 {
		t.Fatalf("args %v", args)
	}
	for _, bad := range []string{"start=5&end=1", "hours=-1", "tz_offset=9999"} {
		v, _ := url.ParseQuery(bad)
		if _, err := ParseFilter(v, time.Now()); err == nil {
			t.Fatalf("%s must be rejected", bad)
		}
	}
}

func TestPrune(t *testing.T) {
	s, ctx := openTest(t), context.Background()
	list := fixture(t)
	if _, _, _, err := s.Insert(ctx, "u1", "col_a", list, ms("2026-11-10T13:00:00Z")); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := s.Insert(ctx, "gone", "col_x", list[:1], ms("2026-11-10T13:00:00Z")); err != nil {
		t.Fatal(err)
	}
	now := ms("2026-11-10T11:30:00Z")
	res, err := s.Prune(ctx, Retention{RawMs: 3_600_000}, now, []string{"u1"})
	if err != nil {
		t.Fatal(err)
	}
	if res.AlertRows != 1 || res.RollupRows != 2 {
		t.Fatalf("after raw prune: %+v", res)
	}
	var decisions int64
	s.read.QueryRow(`SELECT COUNT(*) FROM decisions`).Scan(&decisions)
	if decisions != 0 {
		t.Fatal("decisions of pruned alerts must go with them")
	}
	sum, err := s.Summary(ctx, "u1", filterFor(t, day+"&ip=203.0.113.7"), now)
	if err != nil || sum.DetailExpiredBefore == 0 {
		t.Fatalf("pruned raw detail must be reported: %+v %v", sum, err)
	}
	res, err = s.Prune(ctx, Retention{RawMs: 3_600_000, RollupMs: 3_600_000}, ms("2026-11-10T12:30:00Z"), []string{"u1"})
	if err != nil || res.RollupRows != 1 {
		t.Fatalf("after rollup prune: %+v %v", res, err)
	}
}

// CrowdSec's Local API writes start_at and stop_at with Go's time.String().
func TestNormalizeGoTimeFormat(t *testing.T) {
	a := fixture(t)[0]
	a.StartAt = "2026-11-10 10:14:58.123456789 +0000 UTC"
	a.StopAt = "2026-11-10 17:15:00 +0700 +07"
	a.CreatedAt = "2026-11-10T13:15:02Z" // stored hours later; must not become the alert time
	row := normalize("col_a", &a, ms("2026-11-10T14:00:00Z"))
	if row.StartedAt != ms("2026-11-10T10:14:58.123Z") || row.StoppedAt != ms("2026-11-10T10:15:00Z") {
		t.Fatalf("times %d %d", row.StartedAt, row.StoppedAt)
	}
}

func TestMigrateBackfillsRollupDecisions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "crowdsec.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, _, _, err := s.Insert(ctx, "u1", "col_a", fixture(t), ms("2026-11-10T13:00:00Z")); err != nil {
		t.Fatal(err)
	}
	// Turn it back into a version 0 database: no decisions column.
	for _, stmt := range []string{`ALTER TABLE rollup_hourly DROP COLUMN decisions`, `PRAGMA user_version = 0`} {
		if _, err := s.write.Exec(stmt); err != nil {
			t.Fatal(err)
		}
	}
	s.Close()
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var rolled, raw int64
	if err := s.read.QueryRow(`SELECT SUM(decisions) FROM rollup_hourly`).Scan(&rolled); err != nil {
		t.Fatal(err)
	}
	if err := s.read.QueryRow(`SELECT COUNT(*) FROM decisions`).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if raw == 0 || rolled != raw {
		t.Fatalf("backfilled decisions=%d, raw=%d", rolled, raw)
	}
}
