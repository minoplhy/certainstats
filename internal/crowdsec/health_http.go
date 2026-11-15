package crowdsec

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"certainstats/internal/collector"
	"certainstats/internal/crowdsec/wire"
	apiresponse "certainstats/internal/response"

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
	// Notify is how the incident's notification went: sent, pending, failed, none; empty for a caution.
	Notify      string `json:"notify,omitempty"`
	NotifyError string `json:"notify_error,omitempty"`
}

type componentView struct {
	ID          int64         `json:"id"`
	CollectorID string        `json:"collector_id"`
	Collector   string        `json:"collector"`
	Kind        string        `json:"kind"`
	Name        string        `json:"name"`
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
	// Held and Active are set for a bouncer that reports what it holds: its
	// decisions, and the Local API's active ones for the same origins.
	MetricsMs int64  `json:"metrics_ms"`
	Held      *int64 `json:"held"`
	Active    *int64 `json:"active"`
	Dropped   int64  `json:"dropped"`
	Processed int64  `json:"processed"`
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
	// Unavailable lists the checks that are on but whose data the host does not let the collector read.
	Unavailable []unavailableView `json:"unavailable"`
}

// components lists the owner's machines and bouncers with their state, and
// says per collector whether health is being reported at all.
func (t *Type) components(w http.ResponseWriter, r *http.Request) {
	ctx, user, now := r.Context(), userID(r), time.Now().UnixMilli()
	cols, err := t.Collectors.Store.CollectorList(ctx, user)
	if err != nil {
		fail(w, "components", err)
		return
	}
	list, err := t.Store.Components(ctx, user)
	if err != nil {
		fail(w, "components", err)
		return
	}
	open, err := t.Store.events(ctx, `user_id = ? AND resolved_ms = 0`, user)
	if err != nil {
		fail(w, "components", err)
		return
	}
	type key struct {
		component int64
		check     string
	}
	events := map[key]*HealthEvent{}
	for _, e := range open {
		events[key{e.ComponentID, e.Check}] = e
	}

	out := struct {
		Collectors []collectorHealthView `json:"collectors"`
		Items      []componentView       `json:"items"`
	}{Collectors: []collectorHealthView{}, Items: []componentView{}}
	type known struct {
		name   string
		self   string
		health *CollectorHealth
		judged bool
	}
	byID := map[string]known{}
	for i := range cols {
		c := &cols[i]
		row, ok := c.Type(wire.TypeName)
		if !ok || !row.Enabled {
			continue
		}
		h, err := t.Store.Health(ctx, user, c.CollectorID)
		if err != nil {
			fail(w, "components", err)
			return
		}
		k := known{name: c.Name, self: readState(c).Machine, health: h, judged: t.judged(h, now)}
		byID[c.CollectorID] = k
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
				if check.setting(h.Checks).Level == LevelOff {
					continue
				}
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
		k, ok := byID[c.CollectorID]
		if !ok {
			continue
		}
		db := c.db()
		v := componentView{ID: c.ID, CollectorID: c.CollectorID, Collector: k.name, Kind: c.Kind, Name: c.Name, Version: c.Version, IP: c.IP, Type: c.Type, OS: c.OS,
			State: c.state(k.judged, k.self), Muted: c.Muted, LastSeenMs: db.LastSeenMs, Validated: db.Validated, Revoked: db.Revoked,
			Findings: []findingView{}, MetricsMs: db.MetricsMs, Dropped: db.Dropped, Processed: db.Processed}
		if db.Held != nil {
			var held, active int64
			for origin, n := range db.Held {
				held += n
				active += k.health.Active[origin]
			}
			v.Held, v.Active = &held, &active
		}
		// Findings of a component that is not being judged are stale; leave them out.
		if v.State == StateAlert || v.State == StateCaution {
			for _, f := range c.Findings {
				fv := findingView{Finding: f, Label: f.Check}
				if check := checks[f.Check]; check != nil {
					fv.Label = check.Label
				}
				if e := events[key{c.ID, f.Check}]; e != nil {
					fv.Notify, fv.NotifyError = e.Firing, e.Error
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

type targetView struct {
	TargetID string `json:"target_id"`
	Name     string `json:"name"`
}

type healthSettingsView struct {
	TargetID string                  `json:"target_id"`
	Checks   map[string]CheckSetting `json:"checks"`
	// Registry describes every check, so the form is built from it.
	Registry []*Check     `json:"registry"`
	Targets  []targetView `json:"targets"`
}

func (t *Type) settingsView(r *http.Request, h *CollectorHealth) (healthSettingsView, error) {
	v := healthSettingsView{TargetID: h.TargetID, Checks: map[string]CheckSetting{}, Registry: Checks(), Targets: []targetView{}}
	for _, check := range v.Registry {
		v.Checks[check.Name] = check.setting(h.Checks)
	}
	if t.Targets != nil {
		targets, err := t.Targets.TargetList(r.Context(), userID(r))
		if err != nil {
			return v, err
		}
		for _, target := range targets {
			v.Targets = append(v.Targets, targetView{TargetID: target.TargetID, Name: target.Name})
		}
	}
	return v, nil
}

func (t *Type) healthSettings(w http.ResponseWriter, r *http.Request) {
	c := collector.FromRequest(r)
	h, err := t.Store.Health(r.Context(), userID(r), c.CollectorID)
	var v healthSettingsView
	if err == nil {
		v, err = t.settingsView(r, h)
	}
	if err != nil {
		fail(w, "health settings", err)
		return
	}
	apiresponse.JSON(w, http.StatusOK, v)
}

func (t *Type) saveHealthSettings(w http.ResponseWriter, r *http.Request) {
	c := collector.FromRequest(r)
	var body struct {
		TargetID string                  `json:"target_id"`
		Checks   map[string]CheckSetting `json:"checks"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&body); err != nil {
		apiresponse.Error(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	body.TargetID = strings.TrimSpace(body.TargetID)
	if body.TargetID != "" {
		if t.Targets == nil {
			apiresponse.Error(w, http.StatusBadRequest, "Notification targets are not available")
			return
		}
		if _, err := t.Targets.TargetGetByID(r.Context(), body.TargetID, userID(r)); err != nil {
			apiresponse.Error(w, http.StatusBadRequest, "Unknown notification target")
			return
		}
	}
	for name, setting := range body.Checks {
		check := checks[name]
		if check == nil {
			apiresponse.Error(w, http.StatusBadRequest, "Unknown check: "+name)
			return
		}
		if err := check.validate(setting); err != nil {
			apiresponse.Error(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	if err := t.Store.SaveHealthSettings(r.Context(), userID(r), c.CollectorID, body.TargetID, body.Checks); err != nil {
		fail(w, "health settings", err)
		return
	}
	t.healthSettings(w, r)
}
