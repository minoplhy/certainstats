package web

import "net/http"

// NetworkMonitorsHandler serves the Network view, which is part of the agents SPA page.
func (h *WebHandler) NetworkMonitorsHandler(w http.ResponseWriter, r *http.Request) {
	h.AgentsListHandler(w, r)
}
