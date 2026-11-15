// Package crowdsec is the CrowdSec type of certainstats-collector. It reads
// alerts from CrowdSec's Local API as a dedicated machine and sends them in
// batches, each acknowledged before the next. The server keeps the cursor.
package crowdsec

import (
	log "certainstats/internal/logger"
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strconv"
	"time"

	cwire "certainstats/internal/collector/wire"
	"certainstats/internal/collectoragent/module"
	"certainstats/internal/crowdsec/wire"

	"github.com/fxamacker/cbor/v2"
)

// DefaultCredentials is where the setup steps put the machine login.
const DefaultCredentials = "/etc/certainstats/crowdsec-lapi.yaml"

const (
	pageLimit  = 1000
	minWindow  = time.Minute
	ackTimeout = time.Minute
	backfill   = 10 * 365 * 24 * time.Hour
	// maxDecisions caps the decisions sent with one alert, so it always fits a frame.
	maxDecisions = 500
	// batchBudget leaves room in a frame for the envelope and the batch id.
	batchBudget = cwire.MaxFrameBytes - 4096
)

type Module struct {
	credsPath string
	version   string
	client    *lapi
	// Now is replaceable in tests.
	Now func() time.Time
	// started is when the process began; reached is set once the Local API answered.
	// Together they tell a Local API still starting up from one that is down.
	started time.Time
	reached bool
	// health reads machines and bouncers from CrowdSec's database. It is nil
	// unless the host set CROWDSEC_HEALTH=true, and then nothing touches the database.
	health *health
}

var _ module.Module = (*Module)(nil)

func New(getenv func(string) string, version string) *Module {
	path := getenv("CROWDSEC_LAPI_CREDENTIALS")
	if path == "" {
		path = DefaultCredentials
	}
	m := &Module{credsPath: path, version: version, Now: time.Now, started: time.Now()}
	if cfg := healthFromEnv(getenv); cfg.on {
		m.health = &health{cfg: cfg}
	}
	return m
}

func (m *Module) Name() string   { return wire.TypeName }
func (m *Module) Prefix() string { return wire.Prefix }

func (m *Module) ensureClient() error {
	if m.client != nil {
		return nil
	}
	creds, err := readCredentials(m.credsPath)
	if err != nil {
		return err
	}
	m.client = &lapi{creds: creds, http: &http.Client{Timeout: 30 * time.Second}, userAgent: "certainstats-collector/" + m.version}
	return nil
}

func (m *Module) Check(ctx context.Context) error {
	if info, err := os.Stat(m.credsPath); err == nil && info.Mode().Perm()&0o077 != 0 {
		log.Printf("warning: %s is readable by other users; run chmod 600 %s", m.credsPath, m.credsPath)
	}
	if err := m.ensureClient(); err != nil {
		return err
	}
	if _, err := m.client.login(ctx); err != nil {
		return err
	}
	if _, err := m.client.alerts(ctx, m.Now().Add(-time.Minute), m.Now(), 1); err != nil {
		return err
	}
	if m.health != nil {
		// Health is optional: a database that cannot be read is a warning, not a failure.
		fmt.Println(m.health.check(ctx))
	}
	return nil
}

// runner is the state of one connection.
type runner struct {
	m       *Module
	send    module.Send
	inbox   <-chan cwire.Envelope
	cfg     wire.Config
	cursor  int64
	sent    map[string]int64 // uuid → start, alerts already acknowledged on this connection
	last    *wire.Status
	pending []cwire.Envelope // frames that arrived while waiting for an ack
	batch   int
	once    bool
	// failures counts passes in a row that could not read the Local API.
	failures int
	// healthAt is when machines and bouncers were last read on this connection.
	healthAt time.Time
}

// startGrace is how long after start an unreachable Local API counts as still
// starting (CrowdSec can take a while) rather than as an error.
const startGrace = 2 * time.Minute

// retryDelay is the wait after the nth failed pass in a row: 5 s, doubling, up to poll.
func retryDelay(n int, poll time.Duration) time.Duration {
	d := 5 * time.Second
	for i := 1; i < n && d < poll; i++ {
		d *= 2
	}
	return min(d, poll)
}

func (r *runner) nextWait() time.Duration {
	poll := time.Duration(r.cfg.PollSeconds) * time.Second
	if r.failures > 0 {
		return retryDelay(r.failures, poll)
	}
	return poll
}

func (m *Module) Run(ctx context.Context, send module.Send, inbox <-chan cwire.Envelope, once bool) error {
	r := &runner{m: m, send: send, inbox: inbox, sent: map[string]int64{}, once: once}
	if err := r.waitConfig(ctx); err != nil {
		return err
	}
	if m.health != nil {
		defer m.health.close()
	}
	for {
		r.pass(ctx, r.cursor-int64(r.cfg.OverlapSeconds)*1000)
		r.healthPass(ctx)
		if once || ctx.Err() != nil {
			return nil
		}
		since, ok := r.wait(ctx, r.nextWait())
		if !ok {
			return nil
		}
		if since >= 0 {
			log.Printf("crowdsec: re-reading alerts since %s", time.UnixMilli(since).UTC().Format(time.RFC3339))
			r.sent = map[string]int64{}
			r.pass(ctx, since)
		}
	}
}

func (r *runner) waitConfig(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case env := <-r.inbox:
			if env.Type != wire.MsgConfig {
				continue
			}
			if err := cbor.Unmarshal(env.Body, &r.cfg); err != nil {
				return err
			}
			r.cursor = r.cfg.CursorMs
			r.cfg.PollSeconds = max(r.cfg.PollSeconds, 10)
			if r.cfg.MaxBatch <= 0 || r.cfg.MaxBatch > wire.MaxBatch {
				r.cfg.MaxBatch = wire.MaxBatch
			}
			r.cfg.OverlapSeconds = max(r.cfg.OverlapSeconds, 0)
			return nil
		}
	}
}

// wait sleeps for d, returning early with a resync start time; since is -1 when
// the poll interval simply elapsed. ok is false when the connection ended.
func (r *runner) wait(ctx context.Context, d time.Duration) (since int64, ok bool) {
	for len(r.pending) > 0 {
		env := r.pending[0]
		r.pending = r.pending[1:]
		if s, is := resyncOf(env); is {
			return s, true
		}
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return 0, false
		case <-timer.C:
			return -1, true
		case env := <-r.inbox:
			if s, is := resyncOf(env); is {
				return s, true
			}
		}
	}
}

func resyncOf(env cwire.Envelope) (int64, bool) {
	if env.Type != wire.MsgResync {
		return 0, false
	}
	var rs wire.Resync
	// A resync this collector cannot read is ignored; the regular poll continues.
	if cbor.Unmarshal(env.Body, &rs) != nil {
		return 0, false
	}
	return max(rs.SinceMs, 0), true
}

// starting reports whether a failure to reach the Local API should be treated as
// CrowdSec still starting up: never reached yet, within startGrace, not --once.
func (r *runner) starting() bool {
	return !r.once && !r.m.reached && r.m.Now().Sub(r.m.started) < startGrace
}

// reachErr records the outcome of reading the Local API and reports it, keeping
// quiet while CrowdSec may still be starting.
func (r *runner) reachErr(err error) {
	if err == nil {
		r.failures = 0
		r.m.reached = true
		r.report(nil)
		return
	}
	r.failures++
	if r.starting() {
		log.Printf("crowdsec: waiting for the Local API (%v); retrying in %s", err, r.nextWait())
		return
	}
	r.report(err)
}

// report sends cs_status when it changes.
func (r *runner) report(err error) {
	st := wire.Status{Reachable: err == nil}
	if r.m.client != nil {
		st.Machine, st.LAPIURL = r.m.client.creds.Login, r.m.client.creds.URL
	}
	if err != nil {
		st.Error = err.Error()
	}
	if r.last != nil && *r.last == st {
		return
	}
	// A failed send leaves last unset, so the status is sent again on the next pass;
	// the connection error itself surfaces in the session.
	if r.send(wire.MsgStatus, st) == nil {
		r.last = &st
	}
	if err != nil {
		log.Printf("crowdsec: %v", err)
	}
}

// healthPass reads machines and bouncers from CrowdSec's database and reports
// them, at most once per pass and no more often than the server asked. It does
// nothing on a host that has not switched health on.
func (r *runner) healthPass(ctx context.Context) {
	h := r.m.health
	if h == nil || ctx.Err() != nil {
		return
	}
	now := r.m.Now()
	// A few seconds of slack, so a pass that starts slightly early still reads.
	if !r.healthAt.IsZero() && now.Sub(r.healthAt) < time.Duration(r.cfg.HealthSeconds)*time.Second-5*time.Second {
		return
	}
	r.healthAt = now
	rep := h.read(ctx, now)
	if rep.Error != "" {
		log.Printf("crowdsec: health: %s", rep.Error)
	}
	if err := r.send(wire.MsgHealth, rep); err != nil {
		log.Printf("crowdsec: health: %v", err)
	}
}

// pass reads every alert that started after fromMs and sends what is new.
func (r *runner) pass(ctx context.Context, fromMs int64) {
	if err := r.m.ensureClient(); err != nil {
		// A missing or unreadable credentials file is not a start-up race: report at once.
		r.failures++
		r.report(err)
		return
	}
	now := r.m.Now()
	from := time.UnixMilli(fromMs)
	if fromMs <= 0 {
		from = now.Add(-backfill)
	}
	alerts, err := r.fetch(ctx, from, now, 0)
	r.reachErr(err)
	if err != nil {
		return
	}
	fresh := alerts[:0]
	for _, a := range alerts {
		if _, done := r.sent[a.UUID]; a.UUID != "" && done {
			continue
		}
		if len(a.Decisions) > maxDecisions {
			log.Printf("crowdsec: alert %s has %d decisions; sending the first %d", a.UUID, len(a.Decisions), maxDecisions)
			a.Decisions = a.Decisions[:maxDecisions]
		}
		fillUntil(&a, now)
		fresh = append(fresh, a)
	}
	// Oldest first by real time: the server's cursor is the newest start it has
	// acknowledged, so sending out of order could skip alerts if a batch fails.
	sort.SliceStable(fresh, func(i, j int) bool { return alertMs(fresh[i]) < alertMs(fresh[j]) })
	for len(fresh) > 0 && ctx.Err() == nil {
		n := fit(fresh[:min(len(fresh), r.cfg.MaxBatch)], batchBudget)
		if err := r.deliver(ctx, fresh[:n]); err != nil {
			log.Printf("crowdsec: %v", err)
			return
		}
		fresh = fresh[n:]
	}
	r.forget()
}

// fit returns how many leading alerts encode within budget bytes, halving until
// they do. One alert always goes; maxDecisions keeps it small enough.
func fit(alerts []wire.Alert, budget int) int {
	n := len(alerts)
	for n > 1 {
		if b, err := cbor.Marshal(alerts[:n]); err == nil && len(b) <= budget {
			break
		}
		n /= 2
	}
	return n
}

// fetch narrows a window that fills a page, so nothing is skipped.
func (r *runner) fetch(ctx context.Context, from, to time.Time, depth int) ([]wire.Alert, error) {
	list, err := r.m.client.alerts(ctx, from, to, pageLimit)
	if err != nil || len(list) < pageLimit || to.Sub(from) <= minWindow || depth >= 24 {
		if err == nil && len(list) >= pageLimit {
			log.Printf("crowdsec: more than %d alerts started within %s; some may be missing", pageLimit, to.Sub(from))
		}
		return list, err
	}
	mid := from.Add(to.Sub(from) / 2)
	a, err := r.fetch(ctx, from, mid, depth+1)
	if err != nil {
		return nil, err
	}
	b, err := r.fetch(ctx, mid, to, depth+1)
	if err != nil {
		return nil, err
	}
	seen := make(map[string]bool, len(a))
	for _, x := range a {
		seen[x.UUID] = true
	}
	for _, x := range b {
		if x.UUID == "" || !seen[x.UUID] {
			a = append(a, x)
		}
	}
	return a, nil
}

// fillUntil turns the Local API's "time left" into an expiry time, since that
// duration is only meaningful at the moment it was read.
func fillUntil(a *wire.Alert, now time.Time) {
	for i := range a.Decisions {
		d := &a.Decisions[i]
		if d.Until != "" {
			continue
		}
		if dur, err := time.ParseDuration(d.Duration); err == nil {
			d.Until = now.Add(dur).UTC().Format(time.RFC3339)
		}
	}
}

func (r *runner) deliver(ctx context.Context, alerts []wire.Alert) error {
	r.batch++
	id := strconv.FormatInt(r.m.Now().UnixNano(), 36) + "-" + strconv.Itoa(r.batch)
	if err := r.send(wire.MsgAlerts, wire.Alerts{BatchID: id, Alerts: alerts}); err != nil {
		return err
	}
	timer := time.NewTimer(ackTimeout)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
			return errors.New("no acknowledgement from CertainStats")
		case env := <-r.inbox:
			if env.Type != wire.MsgAck {
				r.pending = append(r.pending, env)
				continue
			}
			var ack wire.Ack
			if err := cbor.Unmarshal(env.Body, &ack); err != nil {
				return err
			}
			if ack.BatchID != id {
				continue
			}
			if ack.Error != "" {
				return fmt.Errorf("CertainStats could not store a batch: %s", ack.Error)
			}
			r.cursor = max(r.cursor, ack.CursorMs)
			for _, a := range alerts {
				if a.UUID != "" {
					r.sent[a.UUID] = alertMs(a)
				}
			}
			return nil
		}
	}
}

// forget drops sent uuids that the next pass can no longer return.
func (r *runner) forget() {
	horizon := r.cursor - int64(r.cfg.OverlapSeconds)*1000 - time.Hour.Milliseconds()
	for id, start := range r.sent {
		if start < horizon {
			delete(r.sent, id)
		}
	}
}

// alertMs is the alert's time as the server stores it: start_at, else created_at.
// Both may be RFC 3339 or Go's String() form; zero when neither parses.
func alertMs(a wire.Alert) int64 {
	if t, ok := wire.ParseTime(a.StartAt); ok {
		return t.UnixMilli()
	}
	if t, ok := wire.ParseTime(a.CreatedAt); ok {
		return t.UnixMilli()
	}
	return 0
}
