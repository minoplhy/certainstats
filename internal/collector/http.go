package collector

import (
	ctxkeys "certainstats/internal/context"
	log "certainstats/internal/logger"
	apiresponse "certainstats/internal/response"
	"certainstats/internal/store"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
)

// Routes mounts the collector API on r (the panel's /api/collectors). auth wraps
// every route except the collector WebSocket, which authenticates by token.
func (s *Service) Routes(r chi.Router, auth func(http.Handler) http.Handler) {
	r.Get("/connect", s.Connect)
	r.Group(func(a chi.Router) {
		a.Use(auth)
		a.Get("/", s.list)
		a.Post("/", s.create)
		for _, t := range s.Types() {
			a.Route("/"+t.Name(), t.DataRoutes)
		}
		a.Route("/{collectorID}", func(one chi.Router) {
			one.Use(s.resolve)
			one.Get("/", s.get)
			one.Patch("/", s.update)
			one.Delete("/", s.revoke)
			one.Post("/token", s.resetToken)
			for _, t := range s.Types() {
				name := t.Name()
				one.Route("/"+name, func(tr chi.Router) {
					tr.Put("/", s.setEnabled(name, true))
					tr.Delete("/", s.setEnabled(name, false))
					t.CollectorRoutes(tr)
				})
			}
		})
	})
}

func userID(r *http.Request) string {
	id, _ := r.Context().Value(ctxkeys.UserIDKey).(string)
	return id
}

func (s *Service) resolve(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := s.Store.CollectorGet(r.Context(), chi.URLParam(r, "collectorID"), userID(r))
		if errors.Is(err, sql.ErrNoRows) {
			apiresponse.Error(w, http.StatusNotFound, "Collector not found")
			return
		}
		if err != nil {
			log.Printf("[Collector] load: %v", err)
			apiresponse.Error(w, http.StatusInternalServerError, "Failed to load collector")
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), collectorKey{}, c)))
	})
}

type typeView struct {
	store.CollectorType
	Label string `json:"label"`
}

type collectorView struct {
	store.Collector
	Types          []typeView `json:"types"`
	AgentName      string     `json:"agent_name"`
	Connected      bool       `json:"connected"`
	ConnectedSince int64      `json:"connected_since"`
	Token          string     `json:"token,omitempty"`
}

func (s *Service) view(c store.Collector, agentNames map[string]string) collectorView {
	v := collectorView{Collector: c, AgentName: agentNames[c.AgentID], Types: []typeView{}}
	for _, t := range c.Types {
		label := t.Type
		if reg := s.types[t.Type]; reg != nil {
			label = reg.Label()
		}
		v.Types = append(v.Types, typeView{CollectorType: t, Label: label})
	}
	if conn := s.conns.get(c.CollectorID); conn != nil {
		v.Connected, v.ConnectedSince = true, conn.Since.UnixMilli()
		if ver := conn.Version(); ver != "" {
			v.Version = ver
		}
	}
	return v
}

func (s *Service) agentNames(r *http.Request) map[string]string {
	names := map[string]string{}
	agents, err := s.Agents.AgentList(r.Context(), userID(r))
	if err != nil {
		log.Printf("[Collector] agent names: %v", err)
		return names
	}
	for _, a := range agents {
		names[a.AgentID] = a.Nickname
	}
	return names
}

func (s *Service) list(w http.ResponseWriter, r *http.Request) {
	list, err := s.Store.CollectorList(r.Context(), userID(r))
	if err != nil {
		log.Printf("[Collector] list: %v", err)
		apiresponse.Error(w, http.StatusInternalServerError, "Failed to list collectors")
		return
	}
	names := s.agentNames(r)
	items := make([]collectorView, 0, len(list))
	for _, c := range list {
		items = append(items, s.view(c, names))
	}
	types := []map[string]string{}
	for _, t := range s.Types() {
		types = append(types, map[string]string{"type": t.Name(), "label": t.Label()})
	}
	apiresponse.JSON(w, http.StatusOK, map[string]any{"items": items, "types": types})
}

func (s *Service) get(w http.ResponseWriter, r *http.Request) {
	apiresponse.JSON(w, http.StatusOK, s.view(*FromRequest(r), s.agentNames(r)))
}

type editRequest struct {
	Name    *string  `json:"name"`
	AgentID *string  `json:"agent_id"`
	Types   []string `json:"types"`
}

// checkEdit trims and validates a name and node link.
func (s *Service) checkEdit(r *http.Request, req *editRequest) string {
	if req.Name != nil {
		name := strings.TrimSpace(*req.Name)
		if name == "" || len([]rune(name)) > 64 {
			return "Name must be 1 to 64 characters"
		}
		req.Name = &name
	}
	if req.AgentID != nil && *req.AgentID != "" {
		if _, err := s.Agents.AgentGetByID(r.Context(), *req.AgentID, userID(r)); err != nil {
			return "Node not found"
		}
	}
	return ""
}

func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		apiresponse.Error(w, http.StatusBadRequest, "Invalid JSON body")
		return false
	}
	return true
}

func (s *Service) create(w http.ResponseWriter, r *http.Request) {
	var req editRequest
	if !decode(w, r, &req) {
		return
	}
	if req.Name == nil {
		empty := ""
		req.Name = &empty
	}
	if msg := s.checkEdit(r, &req); msg != "" {
		apiresponse.Error(w, http.StatusBadRequest, msg)
		return
	}
	seen := map[string]bool{}
	for _, t := range req.Types {
		if s.types[t] == nil || seen[t] {
			apiresponse.Error(w, http.StatusBadRequest, "Unknown collector type: "+t)
			return
		}
		seen[t] = true
	}
	if len(req.Types) == 0 {
		apiresponse.Error(w, http.StatusBadRequest, "Choose at least one type to collect")
		return
	}
	c := store.Collector{CollectorID: newID(), UserID: userID(r), Name: *req.Name, CreatedAt: time.Now().UnixMilli()}
	if req.AgentID != nil {
		c.AgentID = *req.AgentID
	}
	token := newToken()
	if err := s.Store.CollectorCreate(r.Context(), c, HashToken(token), req.Types); err != nil {
		log.Printf("[Collector] create: %v", err)
		apiresponse.Error(w, http.StatusInternalServerError, "Failed to create collector")
		return
	}
	created, err := s.Store.CollectorGet(r.Context(), c.CollectorID, c.UserID)
	if err != nil {
		apiresponse.Error(w, http.StatusInternalServerError, "Failed to load collector")
		return
	}
	v := s.view(*created, s.agentNames(r))
	v.Token = token
	apiresponse.JSON(w, http.StatusCreated, v)
}

func (s *Service) update(w http.ResponseWriter, r *http.Request) {
	var req editRequest
	if !decode(w, r, &req) {
		return
	}
	if msg := s.checkEdit(r, &req); msg != "" {
		apiresponse.Error(w, http.StatusBadRequest, msg)
		return
	}
	c := FromRequest(r)
	if err := s.Store.CollectorUpdate(r.Context(), c.CollectorID, userID(r), req.Name, req.AgentID); err != nil {
		apiresponse.Error(w, http.StatusInternalServerError, "Failed to update collector")
		return
	}
	updated, err := s.Store.CollectorGet(r.Context(), c.CollectorID, userID(r))
	if err != nil {
		apiresponse.Error(w, http.StatusInternalServerError, "Failed to load collector")
		return
	}
	apiresponse.JSON(w, http.StatusOK, s.view(*updated, s.agentNames(r)))
}

func (s *Service) revoke(w http.ResponseWriter, r *http.Request) {
	c := FromRequest(r)
	if err := s.Store.CollectorRevoke(r.Context(), c.CollectorID, userID(r)); err != nil {
		apiresponse.Error(w, http.StatusInternalServerError, "Failed to remove collector")
		return
	}
	s.Disconnect(c.CollectorID)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Service) resetToken(w http.ResponseWriter, r *http.Request) {
	c := FromRequest(r)
	token := newToken()
	if err := s.Store.CollectorResetToken(r.Context(), c.CollectorID, userID(r), HashToken(token)); err != nil {
		apiresponse.Error(w, http.StatusInternalServerError, "Failed to reset token")
		return
	}
	s.Disconnect(c.CollectorID)
	apiresponse.JSON(w, http.StatusOK, map[string]string{"token": token})
}

// setEnabled turns a type on or off for one collector. The live connection is
// closed so the collector reconnects and receives a fresh hello.
func (s *Service) setEnabled(typ string, enabled bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := FromRequest(r)
		if err := s.Store.CollectorTypeSetEnabled(r.Context(), c.CollectorID, userID(r), typ, enabled); err != nil {
			apiresponse.Error(w, http.StatusInternalServerError, "Failed to update collector")
			return
		}
		s.Disconnect(c.CollectorID)
		w.WriteHeader(http.StatusNoContent)
	}
}
