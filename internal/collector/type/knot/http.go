package knot

import (
	"errors"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"certainstats/internal/collector"
	ctsdb "certainstats/internal/collector/tsdb"
	"certainstats/internal/collector/type/knot/wire"
	ctxkeys "certainstats/internal/context"
	log "certainstats/internal/logger"
	"certainstats/internal/metrics"
	apiresponse "certainstats/internal/response"
	"certainstats/internal/tblk"

	"github.com/go-chi/chi/v5"
)

func userID(r *http.Request) string {
	id, _ := r.Context().Value(ctxkeys.UserIDKey).(string)
	return id
}

func fail(w http.ResponseWriter, what string, err error) {
	if errors.Is(err, ctsdb.ErrBusy) || errors.Is(err, tblk.ErrBusy) {
		apiresponse.Error(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	log.Printf("[Knot] %s: %v", what, err)
	apiresponse.Error(w, http.StatusInternalServerError, "Failed to load "+what)
}

// CollectorRoutes is mounted at /collectors/{id}/knot.
func (t *Type) CollectorRoutes(r chi.Router) {
	r.Get("/", t.collectorStatus)
}

// DataRoutes is mounted at /collectors/knot. Each GET answers one resource;
// filters take a single value each.
func (t *Type) DataRoutes(r chi.Router) {
	r.Get("/", t.overview)
	r.Get("/zones", t.zoneList)
	r.Get("/zones/{zone}", t.zoneOne)
	r.Get("/totals", t.totals)
	r.Get("/series", t.series)
	r.Get("/queries", t.queries)
}

func (t *Type) collectorStatus(w http.ResponseWriter, r *http.Request) {
	c := collector.FromRequest(r)
	row, ok := c.Type(wire.TypeName)
	if !ok {
		apiresponse.Error(w, http.StatusNotFound, "Knot DNS is not enabled for this collector")
		return
	}
	st := readState(c)
	apiresponse.JSON(w, http.StatusOK, map[string]any{
		"enabled": row.Enabled, "reported_on": row.ReportedOn, "status": row.Status, "error": row.Error,
		"version": st.Version, "socket": st.Socket, "zone_stats": st.ZoneStats, "mod_stats": st.ModStats,
		"dnstap": st.Dnstap, "connected": t.Collectors.Conn(c.CollectorID) != nil,
	})
}

// window is a resolved time range: [Start, End) with a step for series.
type window struct {
	Start, End, Step int64
}

// parseWindow reads the range the way every chart does (metrics.ParsePrivateTimeRange:
// hours, or start and end in Unix ms) and takes the step from the shared chart rule,
// rounded up to whole minutes, Knot's finest grain. minuteFrom is the oldest minute
// row kept; a range starting before it reads hourly rows, so its step is whole hours.
func parseWindow(r *http.Request, now time.Time, minuteFrom int64) (window, bool) {
	tr, ok := metrics.ParsePrivateTimeRange(r)
	if !ok {
		return window{}, false
	}
	start, end := tr.StartMs, tr.EndMs
	if !tr.IsCustom {
		// A relative range ends at this type's clock.
		span := end - start
		end = now.UnixMilli()
		start = end - span
	}
	grain := minuteMs
	if minuteFrom > 0 && start < minuteFrom {
		grain = hourMs
	}
	step := max(metrics.StepForRange(end-start), grain)
	step = (step + grain - 1) / grain * grain
	// Align to the step so buckets line up with the clock; the last bucket is partial.
	start -= start % step
	if rem := end % step; rem != 0 {
		end += step - rem
	}
	return window{Start: start, End: end, Step: step}, true
}

// minuteFrom is the oldest dnstap minute still kept, or 0 when minutes are kept for ever.
func (t *Type) minuteFrom(now time.Time) int64 {
	if t.Retention.MinuteMs <= 0 {
		return 0
	}
	return now.UnixMilli() - t.Retention.MinuteMs
}

const badRange = "Invalid time range"

// scope reads the collector and zone filters, checking the collector is one of the owner's Knot servers.
func (t *Type) scope(w http.ResponseWriter, r *http.Request) (collectorID, zone string, servers []Server, ok bool) {
	servers, err := t.servers(r.Context(), userID(r))
	if err != nil {
		fail(w, "servers", err)
		return "", "", nil, false
	}
	collectorID = r.URL.Query().Get("collector")
	if collectorID != "" {
		found := false
		for _, s := range servers {
			found = found || s.CollectorID == collectorID
		}
		if !found {
			apiresponse.Error(w, http.StatusNotFound, "Knot DNS server not found")
			return "", "", nil, false
		}
	}
	zone = r.URL.Query().Get("zone")
	if zone != "" {
		zone = canonical(zone)
	}
	return collectorID, zone, servers, true
}

func canonical(z string) string {
	z = strings.ToLower(strings.TrimSpace(z))
	if z == "" || z == wire.NoZone {
		return z
	}
	if !strings.HasSuffix(z, ".") {
		z += "."
	}
	return z
}

func (t *Type) overview(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := userID(r)
	now := t.now()
	servers, err := t.servers(ctx, uid)
	if err != nil {
		fail(w, "servers", err)
		return
	}
	rows, err := t.Store.Zones(ctx, uid, "")
	if err != nil {
		fail(w, "zones", err)
		return
	}
	last, err := t.Store.LastDnstap(ctx, uid)
	if err != nil {
		fail(w, "dnstap", err)
		return
	}
	counts := map[string]int{}
	for _, z := range rows {
		counts[z.CollectorID]++
	}
	for i := range servers {
		servers[i].ZoneCount = counts[servers[i].CollectorID]
		servers[i].LastDnstapMs = last[servers[i].CollectorID]
	}
	zones := zoneViews(rows, serverNames(servers), now.UnixMilli())
	drift := 0
	for _, z := range zones {
		if z.Drift {
			drift++
		}
	}
	apiresponse.JSON(w, http.StatusOK, map[string]any{
		"servers":     servers,
		"checks":      t.checks(ctx, uid, servers, zones, now),
		"zone_count":  len(zones),
		"drift_count": drift,
		"now":         now.UnixMilli(),
	})
}

func (t *Type) zoneList(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := userID(r)
	servers, err := t.servers(ctx, uid)
	if err != nil {
		fail(w, "servers", err)
		return
	}
	rows, err := t.Store.Zones(ctx, uid, "")
	if err != nil {
		fail(w, "zones", err)
		return
	}
	apiresponse.JSON(w, http.StatusOK, map[string]any{"zones": zoneViews(rows, serverNames(servers), t.now().UnixMilli())})
}

func (t *Type) zoneOne(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := userID(r)
	zone := canonical(chi.URLParam(r, "zone"))
	servers, err := t.servers(ctx, uid)
	if err != nil {
		fail(w, "servers", err)
		return
	}
	rows, err := t.Store.Zones(ctx, uid, zone)
	if err != nil {
		fail(w, "zone", err)
		return
	}
	views := zoneViews(rows, serverNames(servers), t.now().UnixMilli())
	if len(views) == 0 {
		apiresponse.Error(w, http.StatusNotFound, "Zone not found")
		return
	}
	zoneStats := false
	for _, s := range servers {
		zoneStats = zoneStats || s.ZoneStats
	}
	apiresponse.JSON(w, http.StatusOK, map[string]any{"zone": views[0], "zone_stats": zoneStats})
}

// Totals are the headline numbers for a range.
type Totals struct {
	Start      int64   `json:"start"`
	End        int64   `json:"end"`
	From       int64   `json:"from"`
	Collecting bool    `json:"collecting"`
	Requests   float64 `json:"requests"`
	QPS        float64 `json:"qps"`
	Responses  float64 `json:"responses"`
	NXDomain   float64 `json:"nxdomain"`
	ServFail   float64 `json:"servfail"`
	Refused    float64 `json:"refused"`
	Transfers  float64 `json:"transfers"`
	BytesIn    float64 `json:"bytes_in"`
	BytesOut   float64 `json:"bytes_out"`
}

func (t *Type) totals(w http.ResponseWriter, r *http.Request) {
	now := t.now()
	win, ok := parseWindow(r, now, 0)
	if !ok {
		apiresponse.Error(w, http.StatusBadRequest, badRange)
		return
	}
	collectorID, zone, _, ok := t.scope(w, r)
	if !ok {
		return
	}
	inc, err := readIncreases(r.Context(), t.TSDB, counterQuery{UserID: userID(r), CollectorID: collectorID, Zone: zone,
		Items: []string{"request-protocol", "response-code", "server-operation", "request-bytes", "response-bytes"},
		Start: win.Start, End: win.End, Step: win.End - win.Start})
	if err != nil {
		fail(w, "totals", err)
		return
	}
	out := Totals{Start: win.Start, End: win.End, From: inc.From, Collecting: inc.Pairs == 0,
		Requests:  inc.sumItem("request-protocol"),
		Responses: inc.sumItem("response-code"),
		NXDomain:  inc.total(seriesKey{"response-code", "NXDOMAIN"}),
		ServFail:  inc.total(seriesKey{"response-code", "SERVFAIL"}),
		Refused:   inc.total(seriesKey{"response-code", "REFUSED"}),
		Transfers: inc.total(seriesKey{"server-operation", "axfr"}) + inc.total(seriesKey{"server-operation", "ixfr"}),
		BytesIn:   inc.sumItem("request-bytes"),
		BytesOut:  inc.sumItem("response-bytes"),
	}
	// The rate covers only the time CertainStats has been counting.
	if inc.From > 0 {
		if span := float64(min(now.UnixMilli(), win.End)-inc.From) / 1000; span > 0 {
			out.QPS = out.Requests / span
		}
	}
	apiresponse.JSON(w, http.StatusOK, out)
}

// groups maps a series group to the mod-stats items it reads.
var groups = map[string][]string{
	"protocol":  {"request-protocol"},
	"rcode":     {"response-code"},
	"operation": {"server-operation"},
	"qtype":     {"query-type"},
	"bytes":     {"request-bytes", "response-bytes"},
}

// maxSeries is how many ids a chart shows before folding the rest into "other".
const maxSeries = 4

// SeriesLine is one chart line: per-second rates per step.
type SeriesLine struct {
	Name   string       `json:"name"`
	Total  float64      `json:"total"`
	Points [][2]float64 `json:"points"`
}

func (t *Type) series(w http.ResponseWriter, r *http.Request) {
	now := t.now()
	win, ok := parseWindow(r, now, 0)
	if !ok {
		apiresponse.Error(w, http.StatusBadRequest, badRange)
		return
	}
	group := r.URL.Query().Get("group")
	items, ok := groups[group]
	if !ok {
		apiresponse.Error(w, http.StatusBadRequest, "group must be one of protocol, rcode, operation, qtype, bytes")
		return
	}
	collectorID, zone, _, ok := t.scope(w, r)
	if !ok {
		return
	}
	inc, err := readIncreases(r.Context(), t.TSDB, counterQuery{UserID: userID(r), CollectorID: collectorID, Zone: zone,
		Items: items, Start: win.Start, End: win.End, Step: win.Step})
	if err != nil {
		fail(w, "series", err)
		return
	}
	line := func(name string, keys []seriesKey) SeriesLine {
		l := SeriesLine{Name: name, Points: make([][2]float64, inc.Buckets)}
		secs := float64(inc.Step) / 1000
		for b := 0; b < inc.Buckets; b++ {
			var v float64
			for _, k := range keys {
				v += inc.Series[k][b]
			}
			l.Total += v
			l.Points[b] = [2]float64{float64(inc.Start + int64(b)*inc.Step), v / secs}
		}
		return l
	}
	lines := []SeriesLine{}
	if group == "bytes" {
		for _, side := range []struct{ name, item string }{{"received", "request-bytes"}, {"sent", "response-bytes"}} {
			var keys []seriesKey
			for k := range inc.Series {
				if k.item == side.item {
					keys = append(keys, k)
				}
			}
			lines = append(lines, line(side.name, keys))
		}
	} else {
		ids := inc.topIDs(items[0])
		var rest []seriesKey
		for i, id := range ids {
			k := seriesKey{items[0], id}
			if i < maxSeries {
				lines = append(lines, line(id, []seriesKey{k}))
			} else {
				rest = append(rest, k)
			}
		}
		if len(rest) > 0 {
			lines = append(lines, line("other", rest))
		}
		sort.SliceStable(lines, func(i, j int) bool { return lines[i].Total > lines[j].Total })
	}
	apiresponse.JSON(w, http.StatusOK, map[string]any{
		"group": group, "start": win.Start, "end": win.End, "step": win.Step,
		"collecting": inc.Pairs == 0, "series": lines,
	})
}

func (t *Type) queries(w http.ResponseWriter, r *http.Request) {
	now := t.now()
	win, ok := parseWindow(r, now, t.minuteFrom(now))
	if !ok {
		apiresponse.Error(w, http.StatusBadRequest, badRange)
		return
	}
	collectorID, zone, _, ok := t.scope(w, r)
	if !ok {
		return
	}
	qv := r.URL.Query()
	dim := qv.Get("dim")
	if dim == "" {
		dim = "qname"
	}
	limit := 50
	if raw := qv.Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > 500 {
			apiresponse.Error(w, http.StatusBadRequest, "limit must be 1 to 500")
			return
		}
		limit = n
	}
	q := Query{UserID: userID(r), CollectorID: collectorID, Zone: zone,
		QName: normalizeQName(qv.Get("qname")), QType: strings.ToUpper(strings.TrimSpace(qv.Get("qtype"))),
		RCode: strings.ToUpper(strings.TrimSpace(qv.Get("rcode"))), Start: win.Start, End: win.End, Step: win.Step}
	q.MinuteFrom = t.minuteFrom(now)
	var widened bool
	if raw := strings.TrimSpace(qv.Get("client")); raw != "" {
		p, wide, err := clientFilter(raw)
		if err != nil {
			apiresponse.Error(w, http.StatusBadRequest, "client must be an IP address or CIDR")
			return
		}
		q.Client, widened = p, wide
	}
	b, err := t.dnstapRows().Dnstap(r.Context(), q, dim, limit)
	if err != nil {
		if strings.HasPrefix(err.Error(), "unknown dimension") {
			apiresponse.Error(w, http.StatusBadRequest, "dim must be one of qname, zone, qtype, rcode, client, size")
			return
		}
		fail(w, "queries", err)
		return
	}
	out := map[string]any{"start": win.Start, "end": win.End, "step": win.Step, "breakdown": b}
	// Client rows go no finer than /24 or /48; a narrower filter says what it matched.
	if widened {
		out["widened"] = q.Client.String()
	}
	apiresponse.JSON(w, http.StatusOK, out)
}

func normalizeQName(n string) string {
	n = strings.ToLower(strings.TrimSpace(n))
	if n == "" || n == wire.Other {
		return n
	}
	if !strings.HasSuffix(n, ".") {
		n += "."
	}
	return n
}
