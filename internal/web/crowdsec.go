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
// Nodes fills the collectors dialog's node picker; it is not part of the page bootstrap.
func (h *WebHandler) CrowdSecHandler(w http.ResponseWriter, r *http.Request) {
	agents, err := h.Store.AgentList(r.Context(), getUserID(r))
	if err != nil {
		log.Printf("crowdsec page: %v", err)
		http.Error(w, "Failed to load agents", http.StatusInternalServerError)
		return
	}
	nodes := make([]nodeOption, 0, len(agents))
	for _, a := range agents {
		nodes = append(nodes, nodeOption{AgentID: a.AgentID, Nickname: a.Nickname})
	}
	pd := h.newPageData(r, "CrowdSec", "crowdsec", map[string]any{"Nodes": nodes})
	h.Renderer.RenderHTTP(w, http.StatusOK, "crowdsec.html", pd)
}
