package crowdsec

import (
	"context"
	"database/sql"
)

type Decision struct {
	Type     string `json:"type"`
	Origin   string `json:"origin"`
	Scope    string `json:"scope"`
	Value    string `json:"value"`
	Duration string `json:"duration"`
	UntilMs  int64  `json:"until"`
}

type Alert struct {
	ID          int64      `json:"id"`
	CollectorID string     `json:"collector_id"`
	Collector   string     `json:"collector"`
	UUID        string     `json:"uuid"`
	Machine     string     `json:"machine"`
	Scenario    string     `json:"scenario"`
	Message     string     `json:"message"`
	IP          string     `json:"ip"`
	Range       string     `json:"range"`
	Scope       string     `json:"scope"`
	Value       string     `json:"value"`
	Country     string     `json:"country"`
	ASN         string     `json:"asn"`
	ASName      string     `json:"as_name"`
	Action      string     `json:"action"`
	Events      int64      `json:"events"`
	Simulated   bool       `json:"simulated"`
	StartedAt   int64      `json:"started_at"`
	StoppedAt   int64      `json:"stopped_at"`
	ReceivedAt  int64      `json:"received_at"`
	Decisions   []Decision `json:"decisions,omitempty"`
}

const alertColumns = `id, collector_id, uuid, machine_id, scenario, message, ip, ip_range, scope, value, country, asn, as_name, action, events_count, simulated, started_at, stopped_at, received_at`

func scanAlert(row interface{ Scan(...any) error }) (Alert, error) {
	var a Alert
	err := row.Scan(&a.ID, &a.CollectorID, &a.UUID, &a.Machine, &a.Scenario, &a.Message, &a.IP, &a.Range, &a.Scope, &a.Value,
		&a.Country, &a.ASN, &a.ASName, &a.Action, &a.Events, &a.Simulated, &a.StartedAt, &a.StoppedAt, &a.ReceivedAt)
	return a, err
}

// sortColumns maps the table's sort keys to columns.
var sortColumns = map[string]string{
	"time": "started_at", "ip": "ip", "scenario": "scenario", "country": "country",
	"machine": "machine_id", "action": "action", "events": "events_count", "asn": "asn",
}

type AlertPage struct {
	Coverage
	Items []Alert `json:"items"`
	Total int64   `json:"total"`
	Page  int     `json:"page"`
	Limit int     `json:"limit"`
}

// Alerts is the paged table. It always reads raw alerts.
func (s *Store) Alerts(ctx context.Context, userID string, f Filter, sortKey string, desc bool, page, limit int) (*AlertPage, error) {
	cov, err := s.plan(ctx, userID, f, true)
	if err != nil {
		return nil, err
	}
	col, ok := sortColumns[sortKey]
	if !ok {
		col = "started_at"
	}
	dir := " ASC"
	if desc {
		dir = " DESC"
	}
	out := &AlertPage{Coverage: cov, Items: []Alert{}, Page: page, Limit: limit}
	where, args := f.where(userID, false, true, "")
	if err := s.read.QueryRowContext(ctx, `SELECT COUNT(*) FROM alerts WHERE `+where, args...).Scan(&out.Total); err != nil {
		return nil, err
	}
	rows, err := s.read.QueryContext(ctx, `SELECT `+alertColumns+` FROM alerts WHERE `+where+` ORDER BY `+col+dir+`, id`+dir+` LIMIT ? OFFSET ?`,
		append(args, limit, (page-1)*limit)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		a, err := scanAlert(rows)
		if err != nil {
			return nil, err
		}
		out.Items = append(out.Items, a)
	}
	return out, rows.Err()
}

func (s *Store) decisionsOf(ctx context.Context, alertID int64) ([]Decision, error) {
	rows, err := s.read.QueryContext(ctx, `SELECT type, origin, scope, value, duration, until_ms FROM decisions WHERE alert_id = ? ORDER BY rowid`, alertID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list := []Decision{}
	for rows.Next() {
		var d Decision
		if err := rows.Scan(&d.Type, &d.Origin, &d.Scope, &d.Value, &d.Duration, &d.UntilMs); err != nil {
			return nil, err
		}
		list = append(list, d)
	}
	return list, rows.Err()
}

// Alert returns one alert with all its decisions, or sql.ErrNoRows.
func (s *Store) Alert(ctx context.Context, userID string, id int64) (*Alert, error) {
	a, err := scanAlert(s.read.QueryRowContext(ctx, `SELECT `+alertColumns+` FROM alerts WHERE id = ? AND user_id = ?`, id, userID))
	if err != nil {
		return nil, err
	}
	if a.Decisions, err = s.decisionsOf(ctx, a.ID); err != nil {
		return nil, err
	}
	return &a, nil
}

type IPDecision struct {
	Decision
	AlertID   int64  `json:"alert_id"`
	Scenario  string `json:"scenario"`
	StartedAt int64  `json:"started_at"`
}

type IPView struct {
	IP        string       `json:"ip"`
	FirstSeen int64        `json:"first_seen"`
	LastSeen  int64        `json:"last_seen"`
	Alerts    int64        `json:"alerts"`
	Events    int64        `json:"events"`
	Country   string       `json:"country"`
	ASN       string       `json:"asn"`
	ASName    string       `json:"as_name"`
	Range     string       `json:"range"`
	Scenarios []Group      `json:"scenarios"`
	Machines  []Group      `json:"machines"`
	Decisions []IPDecision `json:"decisions"`
	RawSince  int64        `json:"raw_since"`
}

// IP summarises one address over all retained raw alerts. The ip index keeps it cheap.
func (s *Store) IP(ctx context.Context, userID, ip string) (*IPView, error) {
	v := &IPView{IP: ip, Scenarios: []Group{}, Machines: []Group{}, Decisions: []IPDecision{}}
	var first, last sql.NullInt64
	if err := s.read.QueryRowContext(ctx, `SELECT MIN(started_at), MAX(started_at), COUNT(*), COALESCE(SUM(events_count), 0) FROM alerts WHERE user_id = ? AND ip = ?`,
		userID, ip).Scan(&first, &last, &v.Alerts, &v.Events); err != nil {
		return nil, err
	}
	v.FirstSeen, v.LastSeen = first.Int64, last.Int64
	raw, _, err := s.coverage(ctx, userID)
	if err != nil {
		return nil, err
	}
	v.RawSince = raw
	if v.Alerts == 0 {
		return v, nil
	}
	if err := s.read.QueryRowContext(ctx, `SELECT country, asn, as_name, ip_range FROM alerts WHERE user_id = ? AND ip = ? ORDER BY started_at DESC LIMIT 1`,
		userID, ip).Scan(&v.Country, &v.ASN, &v.ASName, &v.Range); err != nil {
		return nil, err
	}
	for _, g := range []struct {
		col string
		dst *[]Group
	}{{"scenario", &v.Scenarios}, {"machine_id", &v.Machines}} {
		rows, err := s.read.QueryContext(ctx, `SELECT `+g.col+`, COUNT(*), SUM(events_count) FROM alerts WHERE user_id = ? AND ip = ? GROUP BY 1 ORDER BY 2 DESC LIMIT 50`, userID, ip)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var item Group
			if err := rows.Scan(&item.Value, &item.Alerts, &item.Events); err != nil {
				rows.Close()
				return nil, err
			}
			*g.dst = append(*g.dst, item)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
	}
	rows, err := s.read.QueryContext(ctx, `SELECT d.type, d.origin, d.scope, d.value, d.duration, d.until_ms, a.id, a.scenario, a.started_at
		FROM alerts a JOIN decisions d ON d.alert_id = a.id WHERE a.user_id = ? AND a.ip = ? ORDER BY a.started_at DESC LIMIT 50`, userID, ip)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var d IPDecision
		if err := rows.Scan(&d.Type, &d.Origin, &d.Scope, &d.Value, &d.Duration, &d.UntilMs, &d.AlertID, &d.Scenario, &d.StartedAt); err != nil {
			return nil, err
		}
		v.Decisions = append(v.Decisions, d)
	}
	return v, rows.Err()
}
