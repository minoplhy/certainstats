package crowdsec

import (
	"certainstats/internal/collector"
	"certainstats/internal/collector/type/crowdsec/wire"
	ctxkeys "certainstats/internal/context"
	log "certainstats/internal/logger"
	apiresponse "certainstats/internal/response"
	"database/sql"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
)

func userID(r *http.Request) string {
	id, _ := r.Context().Value(ctxkeys.UserIDKey).(string)
	return id
}

// CollectorRoutes is mounted at /collectors/{id}/crowdsec.
func (t *Type) CollectorRoutes(r chi.Router) {
	r.Get("/", t.collectorStatus)
	r.Post("/resync", t.resync)
}

// DataRoutes is mounted at /collectors/crowdsec.
func (t *Type) DataRoutes(r chi.Router) {
	r.Get("/summary", t.summary)
	r.Get("/timeline", t.timeline)
	r.Get("/groups/{field}", t.groups)
	r.Get("/alerts", t.alertList)
	r.Get("/alerts/{id}", t.alertOne)
	r.Get("/ips/{ip}", t.ip)
	r.Get("/components", t.components)
	r.Get("/subjects", t.subjects)
	r.Put("/components/{id}/mute", t.mute(true))
	r.Delete("/components/{id}/mute", t.mute(false))
}

func fail(w http.ResponseWriter, what string, err error) {
	log.Printf("[CrowdSec] %s: %v", what, err)
	apiresponse.Error(w, http.StatusInternalServerError, "Failed to load "+what)
}

func (t *Type) filter(w http.ResponseWriter, r *http.Request) (Filter, bool) {
	f, err := ParseFilter(r.URL.Query(), time.Now())
	if err != nil {
		apiresponse.Error(w, http.StatusBadRequest, err.Error())
		return f, false
	}
	return f, true
}

func (t *Type) collectorStatus(w http.ResponseWriter, r *http.Request) {
	c := collector.FromRequest(r)
	row, ok := c.Type(wire.TypeName)
	if !ok {
		apiresponse.Error(w, http.StatusNotFound, "CrowdSec is not enabled for this collector")
		return
	}
	st := readState(c)
	apiresponse.JSON(w, http.StatusOK, map[string]any{
		"enabled": row.Enabled, "reported_on": row.ReportedOn, "status": row.Status, "error": row.Error,
		"cursor": st.CursorMs, "machine": st.Machine, "lapi_url": st.LAPIURL,
		"connected": t.Collectors.Conn(c.CollectorID) != nil,
	})
}

func (t *Type) resync(w http.ResponseWriter, r *http.Request) {
	c := collector.FromRequest(r)
	var body struct {
		SinceMs int64 `json:"since_ms"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.SinceMs < 0 || body.SinceMs > time.Now().UnixMilli() {
		apiresponse.Error(w, http.StatusBadRequest, "since_ms must be a past Unix time in milliseconds")
		return
	}
	conn := t.Collectors.Conn(c.CollectorID)
	if conn == nil || !conn.Enabled(wire.TypeName) {
		apiresponse.Error(w, http.StatusConflict, "The collector is not connected with CrowdSec enabled")
		return
	}
	if err := conn.Send(wire.MsgResync, wire.Resync{SinceMs: body.SinceMs}); err != nil {
		apiresponse.Error(w, http.StatusBadGateway, "Could not reach the collector")
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

func (t *Type) summary(w http.ResponseWriter, r *http.Request) {
	f, ok := t.filter(w, r)
	if !ok {
		return
	}
	out, err := t.Store.Summary(r.Context(), userID(r), f, time.Now().UnixMilli())
	if err != nil {
		fail(w, "summary", err)
		return
	}
	apiresponse.JSON(w, http.StatusOK, out)
}

func (t *Type) timeline(w http.ResponseWriter, r *http.Request) {
	f, ok := t.filter(w, r)
	if !ok {
		return
	}
	out, err := t.Store.Timeline(r.Context(), userID(r), f)
	if err != nil {
		fail(w, "timeline", err)
		return
	}
	apiresponse.JSON(w, http.StatusOK, out)
}

func (t *Type) groups(w http.ResponseWriter, r *http.Request) {
	fd, ok := fieldByParam(chi.URLParam(r, "field"))
	if !ok {
		apiresponse.Error(w, http.StatusBadRequest, "Unknown field")
		return
	}
	f, ok := t.filter(w, r)
	if !ok {
		return
	}
	limit := 10
	if raw := r.URL.Query().Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > 100 {
			apiresponse.Error(w, http.StatusBadRequest, "limit must be 1 to 100")
			return
		}
		limit = n
	}
	out, err := t.Store.Groups(r.Context(), userID(r), f, fd, limit)
	if err != nil {
		fail(w, "groups", err)
		return
	}
	if fd.Param == "collector" {
		names, err := t.Collectors.Store.CollectorNames(r.Context(), userID(r))
		if err != nil {
			fail(w, "groups", err)
			return
		}
		for i := range out.Items {
			out.Items[i].Label = names[out.Items[i].Value]
		}
	}
	apiresponse.JSON(w, http.StatusOK, out)
}

func (t *Type) named(r *http.Request, items []Alert) error {
	names, err := t.Collectors.Store.CollectorNames(r.Context(), userID(r))
	if err != nil {
		return err
	}
	for i := range items {
		items[i].Collector = names[items[i].CollectorID]
	}
	return nil
}

func (t *Type) alertList(w http.ResponseWriter, r *http.Request) {
	f, ok := t.filter(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	page, err := strconv.Atoi(q.Get("page"))
	if err != nil || page < 1 {
		page = 1
	}
	limit := 25
	switch q.Get("limit") {
	case "50":
		limit = 50
	case "100":
		limit = 100
	}
	if page > 10000 {
		apiresponse.Error(w, http.StatusBadRequest, "page too large")
		return
	}
	out, err := t.Store.Alerts(r.Context(), userID(r), f, q.Get("sort"), q.Get("order") != "asc", page, limit)
	if err == nil {
		err = t.named(r, out.Items)
	}
	if err != nil {
		fail(w, "alerts", err)
		return
	}
	apiresponse.JSON(w, http.StatusOK, out)
}

func (t *Type) alertOne(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		apiresponse.Error(w, http.StatusNotFound, "Alert not found")
		return
	}
	a, err := t.Store.Alert(r.Context(), userID(r), id)
	if errors.Is(err, sql.ErrNoRows) {
		apiresponse.Error(w, http.StatusNotFound, "Alert not found")
		return
	}
	if err == nil {
		list := []Alert{*a}
		err = t.named(r, list)
		a = &list[0]
	}
	if err != nil {
		fail(w, "alert", err)
		return
	}
	apiresponse.JSON(w, http.StatusOK, a)
}

func (t *Type) ip(w http.ResponseWriter, r *http.Request) {
	ip := chi.URLParam(r, "ip")
	if net.ParseIP(ip) == nil {
		apiresponse.Error(w, http.StatusBadRequest, "Not an IP address")
		return
	}
	v, err := t.Store.IP(r.Context(), userID(r), ip)
	if err != nil {
		fail(w, "IP", err)
		return
	}
	apiresponse.JSON(w, http.StatusOK, v)
}
