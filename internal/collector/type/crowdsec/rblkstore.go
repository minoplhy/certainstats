package crowdsec

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"time"

	"certainstats/internal/collector/type/crowdsec/wire"
	"certainstats/internal/rblk"
)

// Engines for alerts and decisions (CROWDSEC_ENGINE).
const (
	// EngineSQL keeps them in crowdsec.db (the default).
	EngineSQL = "sql"
	// EngineRblk keeps them in the rblk record block store, in its own
	// directory. The hourly rollup, health and components stay in crowdsec.db.
	EngineRblk = "rblk"
)

// rbCols are the alert columns, in Record order (the rc* indexes below).
var rbCols = []rblk.Column{
	{Name: "collector", Kind: rblk.Dict},
	{Name: "uuid", Kind: rblk.ID},
	{Name: "lapi_id", Kind: rblk.Int},
	{Name: "machine", Kind: rblk.Dict},
	{Name: "scenario", Kind: rblk.Dict, Index: true},
	{Name: "message", Kind: rblk.Text},
	{Name: "kind", Kind: rblk.Dict},
	{Name: "ip", Kind: rblk.Dict, Index: true},
	{Name: "range", Kind: rblk.Dict},
	{Name: "scope", Kind: rblk.Dict},
	{Name: "value", Kind: rblk.Dict, SameAs: "ip"},
	{Name: "country", Kind: rblk.Dict},
	{Name: "asn", Kind: rblk.Dict},
	{Name: "as_name", Kind: rblk.Dict},
	{Name: "action", Kind: rblk.Dict},
	{Name: "events", Kind: rblk.Int},
	{Name: "simulated", Kind: rblk.Bool},
	{Name: "stopped", Kind: rblk.Int},
	{Name: "created", Kind: rblk.Int},
	{Name: "received", Kind: rblk.Int},
}

const (
	rcCollector = iota
	rcUUID
	rcLapi
	rcMachine
	rcScenario
	rcMessage
	rcKind
	rcIP
	rcRange
	rcScope
	rcValue
	rcCountry
	rcASN
	rcASName
	rcAction
	rcEvents
	rcSimulated
	rcStopped
	rcCreated
	rcReceived
)

// Decision columns, in Child order.
const (
	rdUUID = iota
	rdType
	rdOrigin
	rdScope
	rdValue
	rdDuration
	rdUntil
)

// alertMessage is the message CrowdSec writes for an overflow, e.g.
// "Ip 203.0.113.7 performed 'crowdsecurity/ssh-bf' (6 events over 2s)".
// A message it rebuilds exactly is not stored; any other is kept as sent, so
// the template saves space but never changes an answer.
func alertMessage(scope, value, scenario string, events int64, over time.Duration) string {
	return fmt.Sprintf("%s %s performed '%s' (%d events over %s)", scope, value, scenario, events, over)
}

func rbSchema() rblk.Schema {
	return rblk.Schema{
		Columns: rbCols,
		Dedupe:  "uuid",
		Templates: map[string]func(r *rblk.Record) string{
			"message": func(r *rblk.Record) string {
				return alertMessage(r.S[rcScope], r.S[rcValue], r.S[rcScenario], r.N[rcEvents], time.Duration(r.N[rcStopped]-r.Time)*time.Millisecond)
			},
		},
		Children: []rblk.Column{
			{Name: "uuid", Kind: rblk.ID},
			{Name: "type", Kind: rblk.Dict},
			{Name: "origin", Kind: rblk.Dict},
			{Name: "scope", Kind: rblk.Dict},
			{Name: "value", Kind: rblk.Dict, SameAs: "^value"},
			{Name: "duration", Kind: rblk.Dict},
			{Name: "until", Kind: rblk.Int, Active: true},
		},
	}
}

// fieldCol maps a filter field (filter.go) to its record column.
var fieldCol = map[string]string{
	"ip": "ip", "range": "range", "scenario": "scenario", "country": "country",
	"machine": "machine", "asn": "asn", "action": "action", "collector": "collector",
}

// sortCol maps the table's sort keys to record columns ("" is time).
var sortCol = map[string]string{
	"time": "", "ip": "ip", "scenario": "scenario", "country": "country",
	"machine": "machine", "action": "action", "events": "events", "asn": "asn",
}

// searchCols are the columns a text search looks in, as Filter.where's q.
var searchCols = []string{"ip", "scenario", "machine", "country", "as_name", "asn", "value", "range"}

// recStore answers the raw-alert reads from rblk. Every method mirrors one
// SQL path in query.go or detail.go and returns the same answer.
type recStore struct{ s *rblk.Store }

// UseRecords keeps alerts and decisions in an rblk store in dir instead of
// crowdsec.db. Call it once, right after Open.
func (s *Store) UseRecords(dir string) error {
	rs, err := rblk.Open(dir, rbSchema(), rblk.Blocks)
	if err != nil {
		return fmt.Errorf("crowdsec records: %w", err)
	}
	s.rec = &recStore{rs}
	return nil
}

// Seal moves settled alerts from the record store's head into blocks. It
// runs on the hourly maintenance; with crowdsec.db alone it does nothing.
func (s *Store) Seal(ctx context.Context, now int64) error {
	if s.rec == nil {
		return nil
	}
	return s.rec.s.Maintain(ctx, now)
}

// toRecords normalizes alerts exactly as the SQL Insert does.
func toRecords(collector string, rows []alertRow, receivedAt int64) []rblk.Record {
	out := make([]rblk.Record, 0, len(rows))
	for _, row := range rows {
		r := rblk.Record{Time: row.StartedAt, S: make([]string, len(rbCols)), N: make([]int64, len(rbCols))}
		r.S[rcCollector], r.S[rcUUID], r.N[rcLapi] = collector, row.UUID, row.LapiID
		r.S[rcMachine], r.S[rcScenario], r.S[rcMessage], r.S[rcKind] = row.Machine, row.Scenario, row.Message, row.Kind
		r.S[rcIP], r.S[rcRange], r.S[rcScope], r.S[rcValue] = row.IP, row.Range, row.Scope, row.Value
		r.S[rcCountry], r.S[rcASN], r.S[rcASName], r.S[rcAction] = row.Country, row.ASN, row.ASName, row.Action
		r.N[rcEvents] = row.Events
		if row.Simulated {
			r.N[rcSimulated] = 1
		}
		r.N[rcStopped], r.N[rcCreated], r.N[rcReceived] = row.StoppedAt, row.CreatedAt, receivedAt
		for _, d := range row.Decisions {
			r.Children = append(r.Children, rblk.Child{
				S: []string{d.UUID, d.Type, d.Origin, d.Scope, d.Value, d.Duration, ""},
				N: []int64{0, 0, 0, 0, 0, 0, d.UntilMs},
			})
		}
		out = append(out, r)
	}
	return out
}

// insertRec stores a batch in the record store. It is one write: rblk
// dedupes by uuid, so a resend after a crash only reports duplicates. No
// hourly rollup is kept; long ranges count the records themselves.
func (s *Store) insertRec(ctx context.Context, userID, collectorID string, alerts []wire.Alert, receivedAt int64) (stored, duplicates int, newest int64, err error) {
	rows := make([]alertRow, len(alerts))
	for i := range alerts {
		rows[i] = normalize(collectorID, &alerts[i], receivedAt)
		newest = max(newest, rows[i].StartedAt)
	}
	ids, err := s.rec.s.Insert(ctx, userID, toRecords(collectorID, rows, receivedAt))
	if err != nil {
		return 0, 0, 0, err
	}
	for _, id := range ids {
		if id == 0 {
			duplicates++
		} else {
			stored++
		}
	}
	return stored, duplicates, newest, nil
}

func (k *recStore) query(userID string, f Filter, withTime bool) rblk.Query {
	q := rblk.Query{Owner: userID}
	if withTime {
		q.Start, q.End = f.Start, f.End
	}
	for _, fd := range fields {
		col := fieldCol[fd.Param]
		if v := f.Include[fd.Param]; len(v) > 0 {
			q.Filters = append(q.Filters, rblk.Filter{Col: col, Values: v})
		}
		if v := f.Exclude[fd.Param]; len(v) > 0 {
			q.Filters = append(q.Filters, rblk.Filter{Col: col, Values: v, Not: true})
		}
	}
	if f.Q != "" {
		q.Contains, q.In = f.Q, searchCols
	}
	return q
}

func toAlert(r *rblk.Record) Alert {
	return Alert{ID: r.ID, CollectorID: r.S[rcCollector], UUID: r.S[rcUUID], Machine: r.S[rcMachine], Scenario: r.S[rcScenario],
		Message: r.S[rcMessage], IP: r.S[rcIP], Range: r.S[rcRange], Scope: r.S[rcScope], Value: r.S[rcValue],
		Country: r.S[rcCountry], ASN: r.S[rcASN], ASName: r.S[rcASName], Action: r.S[rcAction], Events: r.N[rcEvents],
		Simulated: r.N[rcSimulated] != 0, StartedAt: r.Time, StoppedAt: r.N[rcStopped], ReceivedAt: r.N[rcReceived]}
}

func toDecision(c *rblk.Child) Decision {
	return Decision{Type: c.S[rdType], Origin: c.S[rdOrigin], Scope: c.S[rdScope], Value: c.S[rdValue], Duration: c.S[rdDuration], UntilMs: c.N[rdUntil]}
}

// oldest is the earliest stored alert, as MIN(started_at); 0 when none.
func (k *recStore) oldest(ctx context.Context, userID string) (int64, error) {
	t, _, err := k.s.Oldest(ctx, userID)
	return t, err
}

func (k *recStore) alerts(ctx context.Context, userID string, f Filter, sortKey string, desc bool, page, limit int, out *AlertPage) error {
	q := k.query(userID, f, true)
	q.Sort, q.Desc, q.Offset, q.Limit = sortCol[sortKey], desc, (page-1)*limit, limit
	rows, total, err := k.s.Find(ctx, q)
	if err != nil {
		return err
	}
	out.Total = total
	for i := range rows {
		out.Items = append(out.Items, toAlert(&rows[i]))
	}
	return nil
}

// alert returns one alert with its decisions, or sql.ErrNoRows as the SQL path.
func (k *recStore) alert(ctx context.Context, userID string, id int64) (*Alert, error) {
	r, err := k.s.Get(ctx, userID, id)
	if errors.Is(err, rblk.ErrNotFound) {
		return nil, sql.ErrNoRows
	}
	if err != nil {
		return nil, err
	}
	a := toAlert(r)
	a.Decisions = []Decision{}
	for i := range r.Children {
		a.Decisions = append(a.Decisions, toDecision(&r.Children[i]))
	}
	return &a, nil
}

func (k *recStore) ip(ctx context.Context, userID, ip string, v *IPView) error {
	q := rblk.Query{Owner: userID, Filters: []rblk.Filter{{Col: "ip", Values: []string{ip}}}}
	all, err := k.s.Aggregate(ctx, q, rblk.Agg{Sum: []string{"events"}, Times: true})
	if err != nil || len(all) == 0 {
		return err
	}
	v.Alerts, v.Events, v.FirstSeen, v.LastSeen = all[0].Count, all[0].Sums[0], all[0].MinTime, all[0].MaxTime
	lq := q
	lq.Desc, lq.Limit = true, 1
	last, _, err := k.s.Find(ctx, lq)
	if err != nil {
		return err
	}
	if len(last) > 0 {
		v.Country, v.ASN, v.ASName, v.Range = last[0].S[rcCountry], last[0].S[rcASN], last[0].S[rcASName], last[0].S[rcRange]
	}
	for _, g := range []struct {
		col string
		dst *[]Group
	}{{"scenario", &v.Scenarios}, {"machine", &v.Machines}} {
		gs, err := k.s.Aggregate(ctx, q, rblk.Agg{By: g.col, Sum: []string{"events"}, Top: ipGroupLimit})
		if err != nil {
			return err
		}
		for _, x := range gs {
			*g.dst = append(*g.dst, Group{Value: x.Key, Alerts: x.Count, Events: x.Sums[0]})
		}
		sortByAlerts(*g.dst)
	}
	// The newest decisions: alerts without decisions do not count, so page
	// through alerts until enough are found.
	dq := q
	dq.Desc, dq.Limit, dq.Children = true, 100, true
	for len(v.Decisions) < ipDecisionLimit {
		rows, total, err := k.s.Find(ctx, dq)
		if err != nil {
			return err
		}
		for i := range rows {
			for j := range rows[i].Children {
				if len(v.Decisions) == ipDecisionLimit {
					break
				}
				v.Decisions = append(v.Decisions, IPDecision{Decision: toDecision(&rows[i].Children[j]), AlertID: rows[i].ID, Scenario: rows[i].S[rcScenario], StartedAt: rows[i].Time})
			}
		}
		dq.Offset += dq.Limit
		if int64(dq.Offset) >= total {
			break
		}
	}
	return nil
}

// sortByAlerts orders groups by alerts, then value, as ORDER BY 2 DESC, 1.
func sortByAlerts(gs []Group) {
	sort.SliceStable(gs, func(i, j int) bool {
		if gs[i].Alerts != gs[j].Alerts {
			return gs[i].Alerts > gs[j].Alerts
		}
		return gs[i].Value < gs[j].Value
	})
}

// summary fills the raw counts: alerts, events and distinct IPs.
func (k *recStore) summary(ctx context.Context, userID string, f Filter, out *Summary) error {
	gs, err := k.s.Aggregate(ctx, k.query(userID, f, true), rblk.Agg{Sum: []string{"events"}, Distinct: "ip"})
	if err != nil {
		return err
	}
	var ips int64
	if len(gs) > 0 {
		out.Alerts, out.Events, ips = gs[0].Count, gs[0].Sums[0], gs[0].Distinct
	}
	out.UniqueIPs = &ips
	return nil
}

// active counts distinct values of active decisions per type; filters apply,
// the time range does not.
func (k *recStore) active(ctx context.Context, userID string, f Filter, now int64, out map[string]int64) error {
	act, err := k.s.Active(ctx, k.query(userID, f, false), now)
	if err != nil {
		return err
	}
	values := map[string]map[string]bool{}
	for _, r := range act {
		for _, c := range r.Children {
			t := c.S[rdType]
			if values[t] == nil {
				values[t] = map[string]bool{}
			}
			values[t][c.S[rdValue]] = true
		}
	}
	for t, set := range values {
		out[t] = int64(len(set))
	}
	return nil
}

// timeline counts alerts and the decisions they issued per bucket.
func (k *recStore) timeline(ctx context.Context, userID string, f Filter, w, off int64, counts map[int64][2]int64) error {
	gs, err := k.s.Aggregate(ctx, k.query(userID, f, true), rblk.Agg{Width: w, Offset: off, Sum: []string{"#children"}})
	if err != nil {
		return err
	}
	for _, g := range gs {
		counts[g.Bucket] = [2]int64{g.Count, g.Sums[0]}
	}
	return nil
}

func (k *recStore) groups(ctx context.Context, userID string, f Filter, fd field, limit int, out *Groups) error {
	a := rblk.Agg{By: fieldCol[fd.Param], Sum: []string{"events"}, Top: limit}
	if fd.Param == "asn" {
		a.Max = []string{"as_name"}
	}
	gs, err := k.s.Aggregate(ctx, k.query(userID, f, true), a)
	if err != nil {
		return err
	}
	for _, g := range gs {
		item := Group{Value: g.Key, Alerts: g.Count, Events: g.Sums[0]}
		if fd.Param == "asn" {
			item.Label = g.Max[0]
		}
		out.Items = append(out.Items, item)
		out.Total = g.Total
	}
	sortByAlerts(out.Items)
	return nil
}
