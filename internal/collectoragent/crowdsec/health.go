package crowdsec

import (
	log "certainstats/internal/logger"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"certainstats/internal/crowdsec/wire"
)

const (
	// slowEvery is how often the heavier parts (decision counts, bouncer metrics)
	// are read. Bouncers push metrics every 15 minutes, so nothing is lost.
	slowEvery = 5 * time.Minute
	// metricsRows bounds how far back the newest metrics row of each bouncer is looked for.
	metricsRows = 3000
	// activeOrigins bounds the origin groups counted; real setups have a handful.
	activeOrigins = 1000
)

// health reads machines and bouncers from CrowdSec's database. It exists only
// on a host that set CROWDSEC_HEALTH=true.
type health struct {
	cfg healthConfig
	db  *healthDB
	// The slow parts, kept between passes.
	slowAt  time.Time
	active  map[string]int64
	metrics map[string]bouncerMetrics
	slowOK  map[string]bool
}

type bouncerMetrics struct {
	at        int64
	held      map[string]int64
	dropped   int64
	processed int64
}

func (h *health) close() {
	if h.db != nil {
		h.db.close()
		h.db = nil
	}
}

// read returns one report. A failure to open or to read machines and bouncers
// is the report's Error; a missing optional table only drops that part.
func (h *health) read(ctx context.Context, now time.Time) wire.Health {
	out := wire.Health{Parts: []string{}, Components: []wire.Component{}}
	fail := func(err error) wire.Health {
		h.close()
		return wire.Health{Error: err.Error(), Parts: []string{}, Components: []wire.Component{}}
	}
	if h.cfg.err != nil {
		return fail(h.cfg.err)
	}
	if h.db == nil {
		db, err := openHealthDB(h.cfg.dsn)
		if err != nil {
			return fail(err)
		}
		h.db = db
	}
	if h.cfg.reads(wire.PartMachines) {
		list, err := h.machines(ctx)
		if err != nil {
			return fail(fmt.Errorf("cannot read CrowdSec's machines table: %w", err))
		}
		out.Components = append(out.Components, list...)
		out.Parts = append(out.Parts, wire.PartMachines)
	}
	var bouncers []wire.Component
	if h.cfg.reads(wire.PartBouncers) {
		list, err := h.bouncers(ctx)
		if err != nil {
			return fail(fmt.Errorf("cannot read CrowdSec's bouncers table: %w", err))
		}
		bouncers = list
		out.Parts = append(out.Parts, wire.PartBouncers)
	}

	if now.Sub(h.slowAt) >= slowEvery || h.slowOK == nil {
		h.slowAt, h.slowOK = now, map[string]bool{}
		h.active, h.metrics = nil, nil
		if h.cfg.reads(wire.PartDecisions) {
			if active, err := h.activeDecisions(ctx, now); err != nil {
				log.Printf("crowdsec: health: decisions not read: %v", err)
			} else {
				h.active, h.slowOK[wire.PartDecisions] = active, true
			}
		}
		if h.cfg.reads(wire.PartMetrics) && len(bouncers) > 0 {
			// The table is missing before CrowdSec 1.6.3.
			if m, err := h.bouncerMetrics(ctx); err != nil {
				log.Printf("crowdsec: health: bouncer metrics not read: %v", err)
			} else {
				h.metrics, h.slowOK[wire.PartMetrics] = m, true
			}
		}
	}
	for _, part := range []string{wire.PartMetrics, wire.PartDecisions} {
		if h.slowOK[part] {
			out.Parts = append(out.Parts, part)
		}
	}
	out.Active = h.active
	for i := range bouncers {
		if m, ok := h.metrics[bouncers[i].Name]; ok {
			bouncers[i].MetricsMs, bouncers[i].Held, bouncers[i].Dropped, bouncers[i].Processed = m.at, m.held, m.dropped, m.processed
		}
	}
	out.Components = append(out.Components, bouncers...)
	if len(out.Components) > wire.MaxComponents {
		log.Printf("crowdsec: health: %d machines and bouncers; reporting the first %d", len(out.Components), wire.MaxComponents)
		out.Components = out.Components[:wire.MaxComponents]
	}
	return out
}

func (h *health) machines(ctx context.Context) ([]wire.Component, error) {
	var list []wire.Component
	scan := func(withOS bool) func(*sql.Rows) error {
		return func(rows *sql.Rows) error {
			var name, version, ip, osName, osVersion sql.NullString
			var seen, validated any
			dest := []any{&name, &seen, &version, &ip, &validated}
			if withOS {
				dest = append(dest, &osName, &osVersion)
			}
			if err := rows.Scan(dest...); err != nil {
				return err
			}
			list = append(list, wire.Component{Kind: wire.KindMachine, Name: name.String, Version: version.String, IP: ip.String,
				OS: strings.TrimSpace(osName.String + " " + osVersion.String), LastSeenMs: dbTimeMs(seen), Validated: dbBool(validated)})
			return nil
		}
	}
	err := h.db.query(ctx, scan(true), h.db.q.machinesOS, wire.MaxComponents+1)
	if err != nil {
		// The first error is dropped on purpose: older CrowdSec has no OS columns,
		// and the read without them reports any real problem.
		list = nil
		err = h.db.query(ctx, scan(false), h.db.q.machines, wire.MaxComponents+1)
	}
	return list, err
}

func (h *health) bouncers(ctx context.Context) ([]wire.Component, error) {
	var list []wire.Component
	err := h.db.query(ctx, func(rows *sql.Rows) error {
		var name, typ, version, ip sql.NullString
		var seen, revoked any
		if err := rows.Scan(&name, &seen, &typ, &version, &ip, &revoked); err != nil {
			return err
		}
		list = append(list, wire.Component{Kind: wire.KindBouncer, Name: name.String, Type: typ.String, Version: version.String, IP: ip.String,
			LastSeenMs: dbTimeMs(seen), Revoked: dbBool(revoked), Validated: true})
		return nil
	}, h.db.q.bouncers, wire.MaxComponents+1)
	return list, err
}

// activeDecisions counts the Local API's active bans on IPs and ranges by
// origin, the way a firewall bouncer groups them: a list is "lists:<name>".
// Values are counted once, since a bouncer holds each address once.
func (h *health) activeDecisions(ctx context.Context, now time.Time) (map[string]int64, error) {
	out := map[string]int64{}
	err := h.db.query(ctx, func(rows *sql.Rows) error {
		var origin, list sql.NullString
		var n int64
		if err := rows.Scan(&origin, &list, &n); err != nil {
			return err
		}
		key := origin.String
		if list.String != "" {
			key += ":" + list.String
		}
		out[key] += n
		return nil
	}, h.db.q.active, h.db.timeArg(now), activeOrigins)
	return out, err
}

// metricsPayload is the part of a bouncer's usage metrics that is kept. The rest
// of the payload never leaves the host.
type metricsPayload struct {
	Metrics []struct {
		Items []struct {
			Name   string            `json:"name"`
			Unit   string            `json:"unit"`
			Value  float64           `json:"value"`
			Labels map[string]string `json:"labels"`
		} `json:"items"`
	} `json:"metrics"`
}

// bouncerMetrics returns each bouncer's newest usage metrics (CrowdSec 1.6.3+).
func (h *health) bouncerMetrics(ctx context.Context) (map[string]bouncerMetrics, error) {
	out := map[string]bouncerMetrics{}
	err := h.db.query(ctx, func(rows *sql.Rows) error {
		var name sql.NullString
		var at, payload any
		if err := rows.Scan(&name, &at, &payload); err != nil {
			return err
		}
		if _, newer := out[name.String]; newer {
			return nil
		}
		var p metricsPayload
		if err := json.Unmarshal([]byte(dbText(payload)), &p); err != nil {
			// A payload this reader does not understand (another bouncer's format,
			// or a damaged row) only loses that bouncer's numbers; its liveness is still read.
			log.Debugf("crowdsec: health: usage metrics of %s not understood: %v", name.String, err)
			return nil
		}
		m := bouncerMetrics{at: dbTimeMs(at)}
		for _, window := range p.Metrics {
			for _, item := range window.Items {
				switch {
				case item.Name == "active_decisions":
					if m.held == nil {
						m.held = map[string]int64{}
					}
					m.held[item.Labels["origin"]] += int64(item.Value)
				case item.Name == "dropped" && item.Unit == "packet":
					m.dropped += int64(item.Value)
				case item.Name == "processed" && item.Unit == "packet":
					m.processed += int64(item.Value)
				}
			}
		}
		out[name.String] = m
		return nil
	}, h.db.q.metrics, metricsRows)
	return out, err
}

// check tests the database for --check. Health is optional, so a failure is a
// warning, never a failed check.
func (h *health) check(ctx context.Context) string {
	rep := h.read(ctx, time.Now())
	defer h.close()
	if rep.Error != "" {
		return "warning: machine and bouncer health: " + rep.Error
	}
	if h.db.dialect == "postgres" {
		// Asking about a privilege writes nothing.
		var canWrite any
		err := h.db.query(ctx, func(rows *sql.Rows) error { return rows.Scan(&canWrite) }, canWritePostgres)
		if err == nil && dbBool(canWrite) {
			log.Printf("warning: the CROWDSEC_DB user may write to CrowdSec's database; the collector only reads, so give it a SELECT-only user")
		}
	}
	machines := 0
	for _, c := range rep.Components {
		if c.Kind == wire.KindMachine {
			machines++
		}
	}
	return fmt.Sprintf("ok   crowdsec: health reads %s (machines: %d, bouncers: %d)", strings.Join(rep.Parts, ", "), machines, len(rep.Components)-machines)
}
