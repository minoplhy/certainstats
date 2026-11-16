package crowdsec

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"certainstats/internal/collector/type/crowdsec/wire"
	apiresponse "certainstats/internal/response"
	"certainstats/internal/store"

	"github.com/go-chi/chi/v5"
)

// Health states of one collector, for the page.
const (
	healthOn      = "on"      // reporting
	healthOff     = "off"     // connected, but the host has not switched health on
	healthError   = "error"   // switched on, but CrowdSec's database could not be read
	healthUnknown = "unknown" // the collector is not connected
)

type findingView struct {
	Finding
	Label string `json:"label"`
}

// firingView is an alert rule with an open incident on the component.
type firingView struct {
	AlertID   string `json:"alert_id"`
	Rule      string `json:"rule"`
	Check     string `json:"check"`
	HistoryID string `json:"history_id"`
	// Notified is how the notification went: pending, success, failed, unknown.
	Notified string `json:"notified"`
	Error    string `json:"error,omitempty"`
}

type componentView struct {
	ID          int64         `json:"id"`
	CollectorID string        `json:"collector_id"`
	Collector   string        `json:"collector"`
	Kind        string        `json:"kind"`
	Name        string        `json:"name"`
	Group       string        `json:"group"`
	Version     string        `json:"version"`
	IP          string        `json:"ip"`
	Type        string        `json:"type"`
	OS          string        `json:"os"`
	State       string        `json:"state"`
	Muted       bool          `json:"muted"`
	LastSeenMs  int64         `json:"last_seen_ms"`
	Validated   bool          `json:"validated"`
	Revoked     bool          `json:"revoked"`
	Findings    []findingView `json:"findings"`
	Firing      []firingView  `json:"firing"`
	// Held is what a bouncer that sends usage metrics says it holds, by origin;
	// Active is the Local API's active decisions for the same origins.
	MetricsMs int64            `json:"metrics_ms"`
	Held      map[string]int64 `json:"held"`
	Active    map[string]int64 `json:"active"`
	Dropped   int64            `json:"dropped"`
	Processed int64            `json:"processed"`
}

type unavailableView struct {
	Check string `json:"check"`
	Label string `json:"label"`
	Needs string `json:"needs"`
}

type collectorHealthView struct {
	CollectorID string `json:"collector_id"`
	Name        string `json:"name"`
	Health      string `json:"health"`
	Error       string `json:"error"`
	ReportedMs  int64  `json:"reported_ms"`
	// Unavailable lists the checks whose data the host does not let the collector read.
	Unavailable []unavailableView `json:"unavailable"`
}

// collectorInfo is what the views need about one collector with CrowdSec on.
type collectorInfo struct {
	name   string
	self   string
	health *CollectorHealth
	judged bool
}

// crowdsecCollectors lists the owner's collectors with CrowdSec enabled, in order.
func (t *Type) crowdsecCollectors(ctx context.Context, user string, now int64) ([]store.Collector, map[string]collectorInfo, error) {
	cols, err := t.Collectors.Store.CollectorList(ctx, user)
	if err != nil {
		return nil, nil, err
	}
	var out []store.Collector
	info := map[string]collectorInfo{}
	for i := range cols {
		c := &cols[i]
		if row, ok := c.Type(wire.TypeName); !ok || !row.Enabled {
			continue
		}
		h, err := t.Store.Health(ctx, user, c.CollectorID)
		if err != nil {
			return nil, nil, err
		}
		info[c.CollectorID] = collectorInfo{name: c.Name, self: readState(c).Machine, health: h, judged: t.judged(h, now)}
		out = append(out, *c)
	}
	return out, info, nil
}

// components lists the owner's machines and bouncers with their state, and
// says per collector whether health is being reported at all.
func (t *Type) components(w http.ResponseWriter, r *http.Request) {
	ctx, user, now := r.Context(), userID(r), time.Now().UnixMilli()
	cols, info, err := t.crowdsecCollectors(ctx, user, now)
	if err != nil {
		fail(w, "components", err)
		return
	}
	list, err := t.Store.Components(ctx, user)
	if err != nil {
		fail(w, "components", err)
		return
	}
	firing := map[string][]firingView{}
	if t.Rules != nil {
		open, err := t.Rules.CrowdSecOpenIncidents(ctx, user)
		if err != nil {
			fail(w, "components", err)
			return
		}
		for _, i := range open {
			check := strings.TrimPrefix(string(i.Trigger), "crowdsec_")
			firing[i.SubjectID] = append(firing[i.SubjectID], firingView{AlertID: i.AlertID, Rule: i.AlertNickname, Check: check, HistoryID: i.HistoryID, Notified: i.Notified, Error: i.Error})
		}
	}

	out := struct {
		Collectors []collectorHealthView `json:"collectors"`
		Items      []componentView       `json:"items"`
	}{Collectors: []collectorHealthView{}, Items: []componentView{}}
	for _, c := range cols {
		k := info[c.CollectorID]
		h := k.health
		v := collectorHealthView{CollectorID: c.CollectorID, Name: c.Name, Error: h.Error, ReportedMs: h.ReportedMs, Unavailable: []unavailableView{}}
		since, connected := t.connected(c.CollectorID)
		fresh := h.ReportedMs > 0 && now-h.ReportedMs <= reportFresh.Milliseconds()
		switch {
		case !connected:
			v.Health = healthUnknown
		case fresh && h.Error != "":
			v.Health = healthError
		case fresh:
			v.Health = healthOn
		case now-since.UnixMilli() < 2*time.Minute.Milliseconds():
			// Just connected: a first report may still be on its way.
			v.Health = healthUnknown
		default:
			v.Health = healthOff
		}
		if v.Health == healthOn {
			parts := h.partSet()
			for _, check := range Checks() {
				var missing []string
				for _, p := range check.Needs {
					if !parts[p] {
						missing = append(missing, p)
					}
				}
				if len(missing) > 0 {
					v.Unavailable = append(v.Unavailable, unavailableView{Check: check.Name, Label: check.Label, Needs: strings.Join(missing, ", ")})
				}
			}
		}
		out.Collectors = append(out.Collectors, v)
	}
	for _, c := range list {
		k, ok := info[c.CollectorID]
		if !ok {
			continue
		}
		db := c.db()
		fv := firing[c.key()]
		if fv == nil {
			fv = []firingView{}
		}
		v := componentView{ID: c.ID, CollectorID: c.CollectorID, Collector: k.name, Kind: c.Kind, Name: c.Name, Version: c.Version, IP: c.IP, Type: c.Type, OS: c.OS,
			State: c.state(k.judged, k.self, len(fv) > 0), Muted: c.Muted, LastSeenMs: db.LastSeenMs, Validated: db.Validated, Revoked: db.Revoked,
			Findings: []findingView{}, Firing: fv, MetricsMs: db.MetricsMs, Held: db.Held, Dropped: db.Dropped, Processed: db.Processed}
		if c.Kind == wire.KindBouncer {
			v.Group = BaseName(c.Name)
		}
		if db.Held != nil {
			v.Active = map[string]int64{}
			for origin := range db.Held {
				v.Active[origin] = k.health.Active[origin]
			}
		}
		// Findings of a component that is not being judged are stale; leave them out.
		if k.judged && !c.Muted {
			for _, f := range c.Findings {
				fv := findingView{Finding: f, Label: f.Check}
				if check := checks[f.Check]; check != nil {
					fv.Label = check.Label
				}
				v.Findings = append(v.Findings, fv)
			}
		}
		out.Items = append(out.Items, v)
	}
	apiresponse.JSON(w, http.StatusOK, out)
}

func (t *Type) mute(muted bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err == nil {
			err = t.Store.SetMuted(r.Context(), userID(r), id, muted)
		} else {
			err = sql.ErrNoRows
		}
		if errors.Is(err, sql.ErrNoRows) {
			apiresponse.Error(w, http.StatusNotFound, "Machine or bouncer not found")
			return
		}
		if err != nil {
			fail(w, "component", err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// SubjectGroup is one shared bouncer key: its base name and its entries.
type SubjectGroup struct {
	Name    string   `json:"name"`
	Entries []string `json:"entries"`
}

// CollectorSubjects is what an alert rule can watch on one collector.
type CollectorSubjects struct {
	CollectorID string         `json:"collector_id"`
	Name        string         `json:"name"`
	Machines    []string       `json:"machines"`
	Groups      []SubjectGroup `json:"groups"`   // keys used from several addresses
	Bouncers    []string       `json:"bouncers"` // bouncers with a key of their own
}

// Subjects lists, per collector with CrowdSec enabled, the machines, shared
// bouncer keys and single bouncers an alert rule can watch.
func (t *Type) Subjects(ctx context.Context, user string) ([]CollectorSubjects, error) {
	cols, info, err := t.crowdsecCollectors(ctx, user, time.Now().UnixMilli())
	if err != nil {
		return nil, err
	}
	list, err := t.Store.Components(ctx, user)
	if err != nil {
		return nil, err
	}
	byCollector := map[string][]*Component{}
	for _, c := range list {
		byCollector[c.CollectorID] = append(byCollector[c.CollectorID], c)
	}
	out := []CollectorSubjects{}
	for _, col := range cols {
		s := CollectorSubjects{CollectorID: col.CollectorID, Name: col.Name, Machines: []string{}, Groups: []SubjectGroup{}, Bouncers: []string{}}
		self := info[col.CollectorID].self
		for _, c := range byCollector[col.CollectorID] {
			if c.Kind == wire.KindMachine && c.Name != self {
				s.Machines = append(s.Machines, c.Name)
			}
		}
		for base, members := range groupBouncers(byCollector[col.CollectorID]) {
			if len(members) == 1 && members[0].Name == base {
				s.Bouncers = append(s.Bouncers, base)
				continue
			}
			g := SubjectGroup{Name: base}
			for _, m := range members {
				g.Entries = append(g.Entries, m.Name)
			}
			sort.Strings(g.Entries)
			s.Groups = append(s.Groups, g)
		}
		sort.Strings(s.Machines)
		sort.Strings(s.Bouncers)
		sort.Slice(s.Groups, func(i, j int) bool { return s.Groups[i].Name < s.Groups[j].Name })
		out = append(out, s)
	}
	return out, nil
}

func (t *Type) subjects(w http.ResponseWriter, r *http.Request) {
	out, err := t.Subjects(r.Context(), userID(r))
	if err != nil {
		fail(w, "machines and bouncers", err)
		return
	}
	apiresponse.JSON(w, http.StatusOK, map[string]any{"collectors": out})
}
