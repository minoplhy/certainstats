package web

import (
	"certainstats/internal/agent"
	agentdata "certainstats/internal/agent_data"
	ctx "certainstats/internal/context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
)

func (h *WebHandler) AgentsListHandler(w http.ResponseWriter, r *http.Request) {
	userID := getUserID(r)
	agents, err := h.Store.AgentList(r.Context(), userID)
	if err != nil {
		http.Error(w, "Failed to load agents", http.StatusInternalServerError)
		return
	}

	online := 0
	for _, a := range agents {
		if a.IsOnline {
			online++
		}
	}

	pd := h.newPageData(r, "Agent Hub", "agents", map[string]any{
		"Agents":       agents,
		"OnlineCount":  online,
		"OfflineCount": len(agents) - online,
	})
	h.Renderer.RenderHTTP(w, http.StatusOK, "agents_list.html", pd)
}

func (h *WebHandler) AgentDetailHandler(w http.ResponseWriter, r *http.Request) {
	agentID := chi.URLParam(r, "id")
	target := h.PanelPath + "/" + agentID
	if h.PanelPath == "" {
		target = "/" + agentID
	}
	http.Redirect(w, r, target, http.StatusMovedPermanently)
}

func (h *WebHandler) AgentManagementHandler(w http.ResponseWriter, r *http.Request) {
	userID := getUserID(r)
	agents, err := h.Store.AgentListManagement(r.Context(), userID)
	if err != nil {
		http.Error(w, "Failed to load agents management list", http.StatusInternalServerError)
		return
	}

	drivers := map[string]bool{}
	for _, a := range agents {
		drivers[a.AgentType] = true
	}

	pd := h.newPageData(r, "Fleet Management", "management", map[string]any{
		"Agents":      agents,
		"DriverCount": len(drivers),
	})
	h.Renderer.RenderHTTP(w, http.StatusOK, "agent_management.html", pd)
}

func (h *WebHandler) AgentProvisionHandler(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid form", http.StatusBadRequest)
		return
	}

	userID := getUserID(r)
	nickname := strings.TrimSpace(r.FormValue("nickname"))
	if nickname == "" {
		nickname = agent.GenerateNickname()
	}
	agentType := strings.TrimSpace(r.FormValue("agent_type"))
	if agentType == "" {
		agentType = "beszel"
	}

	agentID := agentdata.GenerateAgentID()
	token := agentdata.GenerateDeviceToken(agentType)

	if err := h.Store.AgentProvision(r.Context(), agentID, userID, token, nickname, agentType); err != nil {
		http.Error(w, "Failed to provision agent: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if agentType == "beszel" {
		if _, err := agent.GenerateAndSaveSSH(r.Context(), h.Store, agentID, userID); err != nil {
			http.Error(w, "Agent created; SSH key generation failed", 500)
			return
		}
	}

	redir := r.FormValue("redirect_to")
	if redir == "" {
		redir = r.Header.Get("Referer")
	}
	if redir == "" {
		redir = h.PanelPath + "/"
	}
	http.Redirect(w, r, redir, http.StatusSeeOther)
}

func (h *WebHandler) AgentResetTokenHandler(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid form", http.StatusBadRequest)
		return
	}

	userID := getUserID(r)
	agentID := r.FormValue("agent_id")

	tokBytes := make([]byte, 16)
	if _, err := rand.Read(tokBytes); err != nil {
		http.Error(w, "Token generation failed", 500)
		return
	}
	newToken := hex.EncodeToString(tokBytes)

	if err := h.Store.AgentResetToken(r.Context(), agentID, userID, newToken); err != nil {
		http.Error(w, "Failed to persist change", 500)
		return
	}

	redir := r.FormValue("redirect_to")
	if redir == "" {
		redir = h.PanelPath + "/agents/management"
	}
	http.Redirect(w, r, redir, http.StatusSeeOther)
}

func (h *WebHandler) AgentResetSSHHandler(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid form", http.StatusBadRequest)
		return
	}

	userID := getUserID(r)
	agentID := r.FormValue("agent_id")

	if _, err := agent.GenerateAndSaveSSH(r.Context(), h.Store, agentID, userID); err != nil {
		http.Error(w, "SSH key generation failed", 500)
		return
	}

	redir := r.FormValue("redirect_to")
	if redir == "" {
		redir = h.PanelPath + "/agents/management"
	}
	http.Redirect(w, r, redir, http.StatusSeeOther)
}

func (h *WebHandler) AgentRenameHandler(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid form", http.StatusBadRequest)
		return
	}

	userID := getUserID(r)
	agentID := strings.TrimSpace(r.FormValue("agent_id"))
	if agentID == "" {
		http.Error(w, "Agent ID is required", http.StatusBadRequest)
		return
	}

	nickname := strings.TrimSpace(r.FormValue("nickname"))
	if nickname == "" {
		http.Error(w, "Nickname cannot be empty", http.StatusBadRequest)
		return
	}
	if len(nickname) > 64 {
		http.Error(w, "Nickname too long (max 64 chars)", http.StatusBadRequest)
		return
	}

	if err := h.Store.AgentUpdate(r.Context(), agentID, userID, &nickname, nil); err != nil {
		http.Error(w, "Failed to rename agent: "+err.Error(), http.StatusInternalServerError)
		return
	}
	ctx.InvalidateAgent(agentID)

	redir := r.FormValue("redirect_to")
	if redir == "" {
		redir = h.PanelPath + "/agents/management"
	}
	http.Redirect(w, r, redir, http.StatusSeeOther)
}

func (h *WebHandler) AgentDeleteHandler(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid form", http.StatusBadRequest)
		return
	}

	userID := getUserID(r)
	agentID := r.FormValue("agent_id")

	if err := h.Store.AgentDelete(r.Context(), agentID, userID); err != nil {
		http.Error(w, "Failed to persist change", 500)
		return
	}
	h.Cache.Delete(agentID)

	redir := r.FormValue("redirect_to")
	if redir == "" {
		redir = h.PanelPath + "/agents/management"
	}
	http.Redirect(w, r, redir, http.StatusSeeOther)
}
