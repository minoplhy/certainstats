package networkservice

import (
	nm "certainstats/internal/networkmonitor"
	api "certainstats/internal/response"
	"certainstats/internal/store"
	"net/http"
)

func groupJSON(g *store.NetworkGroup) map[string]any {
	return map[string]any{"items": monitorsJSON(g.Items), "revision": g.Revision}
}

func (s *Service) Group(w http.ResponseWriter, r *http.Request) {
	if requestUserID(r) == "" {
		api.Error(w, http.StatusUnauthorized, "Unauthorized")
		return
	}
	g, err := s.Store.NetworkGroup(r.Context(), requestUserID(r), r.URL.Query().Get("target"))
	if err != nil {
		failure(w, err)
		return
	}
	api.JSON(w, http.StatusOK, groupJSON(g))
}

func (s *Service) EditGroup(w http.ResponseWriter, r *http.Request) {
	if requestUserID(r) == "" {
		api.Error(w, http.StatusUnauthorized, "Unauthorized")
		return
	}
	var body struct {
		OriginalTarget string   `json:"original_target"`
		Revision       string   `json:"revision"`
		Target         string   `json:"target"`
		Protocol       string   `json:"protocol"`
		Port           int      `json:"port"`
		Server         string   `json:"dns_server"`
		Interval       int      `json:"interval_seconds"`
		AgentIDs       []string `json:"agent_ids"`
		Enabled        *bool    `json:"enabled"`
	}
	if err := decode(w, r, &body); err != nil {
		api.Error(w, http.StatusBadRequest, "Invalid group configuration")
		return
	}
	if body.Port < 0 || body.Port > 65535 {
		api.Error(w, http.StatusBadRequest, "Invalid port")
		return
	}
	if err := nm.ValidateInterval(body.Interval); err != nil {
		failure(w, err)
		return
	}
	change, err := s.Store.NetworkEditGroup(r.Context(), requestUserID(r), store.NetworkGroupEdit{
		Target: body.OriginalTarget, Revision: body.Revision, AgentIDs: body.AgentIDs, Enabled: body.Enabled,
		Config: nm.Config{Target: body.Target, Protocol: body.Protocol, Port: uint16(body.Port), Server: body.Server, Interval: uint16(body.Interval)},
	})
	if err != nil {
		failure(w, err)
		return
	}
	// A full sync includes replacements and removals together, once per affected agent.
	agents := map[string]bool{}
	for _, m := range change.Before {
		agents[m.AgentID] = true
	}
	for _, m := range change.Items {
		agents[m.AgentID] = true
	}
	for id := range agents {
		s.Changed(requestUserID(r), id, nil)
	}
	api.JSON(w, http.StatusOK, groupJSON(&change.NetworkGroup))
}
