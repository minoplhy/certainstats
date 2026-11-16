package web

import (
	log "certainstats/internal/logger"
	"net/http"
)

type nodeOption struct {
	AgentID  string
	Nickname string
}

// CollectorTypePage serves a collector type's own page, the template <name>.html
// under the nav entry <name>. Its data loads from /api/collectors/<name>, so the
// handler knows nothing about the type.
func (h *WebHandler) CollectorTypePage(name, label string) http.HandlerFunc {
	page := name + ".html"
	return func(w http.ResponseWriter, r *http.Request) {
		pd := h.newPageData(r, label, name, nil)
		h.Renderer.RenderHTTP(w, http.StatusOK, page, pd)
	}
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
