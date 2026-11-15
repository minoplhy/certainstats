package web

import (
	log "certainstats/internal/logger"
	"net/http"
)

type nodeOption struct {
	AgentID  string
	Nickname string
}

// CrowdSecHandler serves the CrowdSec page. Its data loads from /api/collectors/crowdsec.
func (h *WebHandler) CrowdSecHandler(w http.ResponseWriter, r *http.Request) {
	pd := h.newPageData(r, "CrowdSec", "crowdsec", nil)
	h.Renderer.RenderHTTP(w, http.StatusOK, "crowdsec.html", pd)
}

// CollectorsHandler serves the Collectors page. Its list loads from /api/collectors;
// Nodes fills the add form's node picker and is not part of the page bootstrap.
func (h *WebHandler) CollectorsHandler(w http.ResponseWriter, r *http.Request) {
	agents, err := h.Store.AgentList(r.Context(), getUserID(r))
	if err != nil {
		log.Printf("collectors page: %v", err)
		http.Error(w, "Failed to load agents", http.StatusInternalServerError)
		return
	}
	nodes := make([]nodeOption, 0, len(agents))
	for _, a := range agents {
		nodes = append(nodes, nodeOption{AgentID: a.AgentID, Nickname: a.Nickname})
	}
	pd := h.newPageData(r, "Collectors", "collectors", map[string]any{"Nodes": nodes})
	h.Renderer.RenderHTTP(w, http.StatusOK, "collectors.html", pd)
}
