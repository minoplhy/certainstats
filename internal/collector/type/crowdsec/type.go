package crowdsec

import (
	"certainstats/internal/collector"
	"certainstats/internal/collector/type/crowdsec/wire"
	log "certainstats/internal/logger"
	"certainstats/internal/store"
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/fxamacker/cbor/v2"
)

// Type is the CrowdSec collector type, registered with the collector parent.
type Type struct {
	Store      *Store
	Collectors *collector.Service
	Retention  Retention
	// Rules holds CrowdSec alert rules and their incidents (the main database); nil alerts nothing.
	Rules Rules
	// Deliver sends a queued notification in the background (alertdelivery.Go).
	Deliver func(eventID string)
	// live overrides how a collector's live connection is found; tests set it.
	live func(collectorID string) (since time.Time, ok bool)
}

var _ collector.Type = (*Type)(nil)

func (t *Type) Name() string   { return wire.TypeName }
func (t *Type) Label() string  { return "CrowdSec" }
func (t *Type) Prefix() string { return wire.Prefix }

// credentialsPath is where the setup steps put the collector's machine login.
const credentialsPath = "/etc/certainstats/crowdsec-lapi.yaml"

var _ collector.Setup = (*Type)(nil)

// Setup is CrowdSec's part of the install steps.
func (t *Type) Setup() collector.TypeSetup {
	return collector.TypeSetup{
		Env: [][2]string{{"COLLECT_CROWDSEC", "true"}, {"CROWDSEC_LAPI_CREDENTIALS", credentialsPath}},
		Prepare: []collector.Step{{
			Label:   "Register a machine login for the collector on the CrowdSec server (as root)",
			Command: "mkdir -p /etc/certainstats\ncscli machines add certainstats --auto -f " + credentialsPath + "\nchmod 600 " + credentialsPath,
		}},
		Volumes: []string{credentialsPath + ":" + credentialsPath + ":ro"},
		Note: "Install the collector on the machine that runs the CrowdSec Local API. If CrowdSec runs in Docker, the collector must share its network, " +
			"the Local API must listen on that network, and url in " + credentialsPath + " must name the container, such as http://crowdsec:8080. " +
			"Machine and bouncer health is optional and off by default: it reads CrowdSec's database, read-only, only when the host sets CROWDSEC_HEALTH=true. Alert rules for machines and bouncers are set up on the Alerts page.",
	}
}

// state is the type's small per-collector state, kept in collector_types.state_json.
type state struct {
	CursorMs int64  `json:"cursor_ms"`
	Machine  string `json:"machine,omitempty"`
	LAPIURL  string `json:"lapi_url,omitempty"`
}

func readState(c *store.Collector) state {
	var st state
	if row, ok := c.Type(wire.TypeName); ok && row.State != "" {
		_ = json.Unmarshal([]byte(row.State), &st) // a damaged state restarts from a full backfill
	}
	return st
}

func (t *Type) loadState(ctx context.Context, c *collector.Conn) (state, error) {
	col, err := t.Collectors.Store.CollectorGet(ctx, c.Collector.CollectorID, c.Collector.UserID)
	if err != nil {
		return state{}, err
	}
	return readState(col), nil
}

func (t *Type) saveState(ctx context.Context, collectorID string, st state) error {
	raw, err := json.Marshal(st)
	if err != nil {
		return err
	}
	return t.Collectors.Store.CollectorTypeSaveState(ctx, collectorID, wire.TypeName, string(raw))
}

func (t *Type) OnConnect(ctx context.Context, c *collector.Conn) error {
	st, err := t.loadState(ctx, c)
	if err != nil {
		return err
	}
	return c.Send(wire.MsgConfig, wire.Config{CursorMs: st.CursorMs, PollSeconds: wire.PollSecond, MaxBatch: wire.MaxBatch, OverlapSeconds: wire.OverlapSeconds, HealthSeconds: wire.HealthSecond})
}

func (t *Type) OnDisconnect(*collector.Conn) {}

func (t *Type) OnMessage(ctx context.Context, c *collector.Conn, msgType string, body cbor.RawMessage) error {
	switch msgType {
	case wire.MsgStatus:
		var m wire.Status
		if err := cbor.Unmarshal(body, &m); err != nil {
			return err
		}
		return t.status(ctx, c, m)
	case wire.MsgAlerts:
		var m wire.Alerts
		if err := cbor.Unmarshal(body, &m); err != nil {
			return err
		}
		if len(m.Alerts) > wire.MaxBatch {
			return fmt.Errorf("batch of %d alerts exceeds %d", len(m.Alerts), wire.MaxBatch)
		}
		return t.alerts(ctx, c, m)
	case wire.MsgHealth:
		var m wire.Health
		if err := cbor.Unmarshal(body, &m); err != nil {
			return err
		}
		return t.health(ctx, c, m)
	}
	return nil
}

func (t *Type) status(ctx context.Context, c *collector.Conn, m wire.Status) error {
	status, message := "ok", ""
	if !m.Reachable {
		status, message = "error", m.Error
		if message == "" {
			message = "Local API unreachable"
		}
	}
	if err := t.Collectors.Store.CollectorTypeStatus(ctx, c.Collector.CollectorID, wire.TypeName, status, message); err != nil {
		return err
	}
	st, err := t.loadState(ctx, c)
	if err != nil {
		return err
	}
	if m.Machine != "" {
		st.Machine = m.Machine
	}
	if m.LAPIURL != "" {
		st.LAPIURL = m.LAPIURL
	}
	return t.saveState(ctx, c.Collector.CollectorID, st)
}

// alerts stores a batch and acknowledges it. A store failure is answered, not
// fatal: the cursor stays put and the collector resends later.
func (t *Type) alerts(ctx context.Context, c *collector.Conn, m wire.Alerts) error {
	st, err := t.loadState(ctx, c)
	if err != nil {
		return err
	}
	stored, dup, newest, err := t.Store.Insert(ctx, c.Collector.UserID, c.Collector.CollectorID, m.Alerts, time.Now().UnixMilli())
	if err != nil {
		log.Printf("[CrowdSec] %s: store batch: %v", c.Collector.CollectorID, err)
		return c.Send(wire.MsgAck, wire.Ack{BatchID: m.BatchID, CursorMs: st.CursorMs, Error: "storage failed; resend later", RetryAfterSeconds: 30})
	}
	if newest > st.CursorMs {
		st.CursorMs = newest
		if err := t.saveState(ctx, c.Collector.CollectorID, st); err != nil {
			return err
		}
	}
	return c.Send(wire.MsgAck, wire.Ack{BatchID: m.BatchID, Stored: stored, Duplicates: dup, CursorMs: st.CursorMs})
}

// Maintain applies retention and logs table sizes; it runs on the hourly cleanup.
func (t *Type) Maintain(ctx context.Context) {
	users, err := t.Collectors.Store.UserIDs(ctx)
	if err != nil {
		log.Printf("[CrowdSec] maintenance: %v", err)
		return
	}
	res, err := t.Store.Prune(ctx, t.Retention, time.Now().UnixMilli(), users)
	if err != nil {
		log.Printf("[CrowdSec] maintenance: %v", err)
		return
	}
	log.Printf("[Maintenance] %s", res)
	if err := t.Store.pruneHealth(ctx, users); err != nil {
		log.Printf("[CrowdSec] health maintenance: %v", err)
	}
}
