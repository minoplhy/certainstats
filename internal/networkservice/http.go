package networkservice

import (
	csctx "certainstats/internal/context"
	"certainstats/internal/metrics"
	nm "certainstats/internal/networkmonitor"
	api "certainstats/internal/response"
	"certainstats/internal/store"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
)

const (
	maxRequestBytes = 64 << 10
	maxPage         = 1_000_000
	// historyTTL is how long a built history response stays in the response cache.
	historyTTL = 60 * time.Second
)

func requestUserID(r *http.Request) string {
	userID, _ := r.Context().Value(csctx.UserIDKey).(string)
	return userID
}

// positiveQuery returns the query parameter as a positive integer, or def.
func positiveQuery(r *http.Request, key string, def int) int {
	value, err := strconv.Atoi(r.URL.Query().Get(key))
	if err != nil || value < 1 {
		return def
	}
	return value
}

// decode reads exactly one JSON object and rejects unknown fields.
func decode(w http.ResponseWriter, r *http.Request, v any) error {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxRequestBytes))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(v); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return fmt.Errorf("unexpected trailing JSON")
	}
	return nil
}

// failure maps store and validation errors to API responses.
func failure(w http.ResponseWriter, err error) {
	var capability *nm.CapabilityError
	var validation *nm.ValidationError
	switch {
	case errors.Is(err, sql.ErrNoRows):
		api.Error(w, http.StatusNotFound, "Not found")
	case errors.Is(err, nm.ErrConflict):
		api.Error(w, http.StatusConflict, err.Error())
	case errors.As(err, &capability):
		api.JSON(w, http.StatusBadRequest, map[string]any{"error": err.Error(), "details": capability})
	case errors.As(err, &validation):
		api.Error(w, http.StatusBadRequest, err.Error())
	default:
		api.Error(w, http.StatusInternalServerError, "Network monitoring operation failed")
	}
}

func networkFilter(r *http.Request) (store.NetworkListFilter, error) {
	query := r.URL.Query()

	state := query.Get("state")
	switch state {
	case "", nm.StateActive, nm.StatePaused, nm.StateArchived, "all":
	default:
		return store.NetworkListFilter{}, nm.Invalid("Invalid state")
	}
	protocol := query.Get("protocol")
	switch protocol {
	case "", nm.ProtocolICMP, nm.ProtocolTCP, nm.ProtocolHTTP, nm.ProtocolDNS:
	default:
		return store.NetworkListFilter{}, nm.Invalid("Invalid protocol")
	}

	return store.NetworkListFilter{
		AgentID:     query.Get("agent_id"),
		Query:       query.Get("q"),
		Target:      query.Get("target"),
		TargetExact: query.Get("target_exact"),
		Protocol:    protocol,
		State:       state,
	}, nil
}

func (s *Service) Targets(w http.ResponseWriter, r *http.Request) {
	userID := requestUserID(r)
	if userID == "" {
		api.Error(w, http.StatusUnauthorized, "Unauthorized")
		return
	}
	filter, err := networkFilter(r)
	if err != nil {
		failure(w, err)
		return
	}
	targets, err := s.Store.NetworkTargets(r.Context(), userID, filter)
	if err != nil {
		api.Error(w, http.StatusInternalServerError, "Failed to load target groups")
		return
	}
	api.JSON(w, http.StatusOK, map[string]any{"items": targets})
}

func (s *Service) List(w http.ResponseWriter, r *http.Request) {
	userID := requestUserID(r)
	if userID == "" {
		api.Error(w, http.StatusUnauthorized, "Unauthorized")
		return
	}
	filter, err := networkFilter(r)
	if err != nil {
		failure(w, err)
		return
	}
	page := min(positiveQuery(r, "page", 1), maxPage)
	limit := min(positiveQuery(r, "limit", nm.DefaultPageSize), nm.MaxPageSize)
	items, total, err := s.Store.NetworkList(r.Context(), userID, filter, page, limit)
	if err != nil {
		api.Error(w, http.StatusInternalServerError, "Failed to load monitors")
		return
	}
	api.JSON(w, http.StatusOK, map[string]any{
		"items": monitorsJSON(items),
		"total": total,
		"page":  page,
		"limit": limit,
	})
}

func (s *Service) Get(w http.ResponseWriter, r *http.Request) {
	m, err := s.Store.NetworkGet(r.Context(), requestUserID(r), chi.URLParam(r, "id"))
	if err != nil {
		failure(w, err)
		return
	}
	api.JSON(w, http.StatusOK, monitorJSON(*m))
}

func (s *Service) Create(w http.ResponseWriter, r *http.Request) {
	var body struct {
		AgentIDs []string `json:"agent_ids"`
		Target   string   `json:"target"`
		Protocol string   `json:"protocol"`
		Port     int      `json:"port"`
		Interval int      `json:"interval_seconds"`
		Server   string   `json:"dns_server"`
		Enabled  *bool    `json:"enabled"`
	}
	if err := decode(w, r, &body); err != nil {
		api.Error(w, http.StatusBadRequest, "Invalid monitor configuration")
		return
	}
	// Zero selects the protocol default port.
	if body.Port < 0 || body.Port > 65535 {
		api.Error(w, http.StatusBadRequest, "Invalid port")
		return
	}
	if body.Interval == 0 {
		body.Interval = nm.DefaultIntervalSeconds
	}
	if err := nm.ValidateInterval(body.Interval); err != nil {
		failure(w, err)
		return
	}

	config := nm.Config{
		Target:   body.Target,
		Protocol: body.Protocol,
		Port:     uint16(body.Port),
		Interval: uint16(body.Interval),
		Server:   body.Server,
	}
	enabled := body.Enabled == nil || *body.Enabled
	items, err := s.Store.NetworkCreate(r.Context(), requestUserID(r), body.AgentIDs, config, enabled)
	if err != nil {
		failure(w, err)
		return
	}
	for _, m := range items {
		s.Changed(m.UserID, m.AgentID, &nm.Operation{Action: nm.ActionUpsert, Config: m.Config, RunNow: enabled})
	}
	api.JSON(w, http.StatusCreated, map[string]any{"items": monitorsJSON(items)})
}

func (s *Service) Update(w http.ResponseWriter, r *http.Request) {
	old, err := s.Store.NetworkGet(r.Context(), requestUserID(r), chi.URLParam(r, "id"))
	if err != nil {
		failure(w, err)
		return
	}
	var patch struct {
		Target   *string `json:"target"`
		Protocol *string `json:"protocol"`
		Port     *int    `json:"port"`
		Interval *int    `json:"interval_seconds"`
		Server   *string `json:"dns_server"`
		Enabled  *bool   `json:"enabled"`
	}
	if err = decode(w, r, &patch); err != nil {
		api.Error(w, http.StatusBadRequest, "Invalid monitor configuration")
		return
	}

	config := old.Config
	enabled := old.Enabled
	if patch.Target != nil {
		config.Target = *patch.Target
	}
	if patch.Protocol != nil {
		config.Protocol = *patch.Protocol
	}
	if patch.Server != nil {
		config.Server = *patch.Server
	}
	if patch.Enabled != nil {
		enabled = *patch.Enabled
	}
	if patch.Port != nil {
		if *patch.Port < 1 || *patch.Port > 65535 {
			api.Error(w, http.StatusBadRequest, "Port must be between 1 and 65535")
			return
		}
		config.Port = uint16(*patch.Port)
	}
	if patch.Interval != nil {
		if err = nm.ValidateInterval(*patch.Interval); err != nil {
			failure(w, err)
			return
		}
		config.Interval = uint16(*patch.Interval)
	}

	m, err := s.Store.NetworkUpdate(r.Context(), requestUserID(r), old.ID, config, enabled)
	if err != nil {
		failure(w, err)
		return
	}
	op := nm.Operation{Action: nm.ActionUpsert, Config: m.Config}
	if enabled {
		// Probe immediately when monitoring resumes or a new identity starts.
		op.RunNow = !old.Enabled || m.ID != old.ID
	} else {
		op.Action = nm.ActionDelete
	}
	s.Changed(m.UserID, m.AgentID, &op)
	api.JSON(w, http.StatusOK, monitorJSON(*m))
}

func (s *Service) Archive(w http.ResponseWriter, r *http.Request) {
	m, err := s.Store.NetworkGet(r.Context(), requestUserID(r), chi.URLParam(r, "id"))
	if err != nil {
		failure(w, err)
		return
	}
	if err = s.Store.NetworkArchive(r.Context(), requestUserID(r), m.ID); err != nil {
		failure(w, err)
		return
	}
	s.Changed(m.UserID, m.AgentID, &nm.Operation{Action: nm.ActionDelete, Config: m.Config})
	w.WriteHeader(http.StatusNoContent)
}

type historyResponse struct {
	MonitorID string     `json:"monitor_id"`
	Target    string     `json:"target"`
	AgentName string     `json:"agent_name"`
	Points    []nm.Point `json:"points"`
	Start     int64      `json:"start"`
	End       int64      `json:"end"`
	Semantics string     `json:"semantics"`
}

// History serves one monitor's aggregated history. Like /api/metrics, each
// series is its own request; the browser fetches several in parallel.
func (s *Service) History(w http.ResponseWriter, r *http.Request) {
	userID := requestUserID(r)
	m, err := s.Store.NetworkGet(r.Context(), userID, chi.URLParam(r, "id"))
	if err != nil {
		failure(w, err)
		return
	}
	timeRange, ok := metrics.ParsePrivateTimeRange(r)
	if !ok {
		api.Error(w, http.StatusBadRequest, "Invalid time range")
		return
	}
	start, end := timeRange.StartMs, timeRange.EndMs
	relative := r.URL.Query().Get("start") == ""

	key := s.historyKey(userID, *m, start, end, relative)
	if s.Cache != nil {
		if entry, hit := csctx.GetCacheEntry(&csctx.MetricsCache, key); hit {
			entry.Serve(w, r, "application/json", http.StatusOK)
			return
		}
		finish, builder := s.beginHistory(w, r, key)
		if !builder {
			return
		}
		defer finish()
		// Another builder may have published between the first lookup and election.
		if entry, hit := csctx.GetCacheEntry(&csctx.MetricsCache, key); hit {
			entry.Serve(w, r, "application/json", http.StatusOK)
			return
		}
	}

	points, err := s.history(r.Context(), *m, start, end, relative)
	if err != nil {
		api.Error(w, http.StatusInternalServerError, "Historical query failed")
		return
	}
	payload, err := json.Marshal(historyResponse{
		MonitorID: m.ID,
		Target:    m.Target,
		AgentName: m.AgentName,
		Points:    points,
		Start:     start,
		End:       end,
		Semantics: "reported_windows",
	})
	if err != nil {
		api.Error(w, http.StatusInternalServerError, "Historical query failed")
		return
	}

	entry := csctx.NewCacheEntry(payload, historyTTL)
	if s.Cache != nil {
		// Publish only if no commit changed the monitor's data during the build.
		s.dataMu.RLock()
		if s.historyKey(userID, *m, start, end, relative) == key {
			csctx.MetricsCache.Store(key, entry)
		}
		s.dataMu.RUnlock()
	}
	entry.Serve(w, r, "application/json", http.StatusOK)
}
