package web

import (
	"certainstats/internal/agent"
	agentdata "certainstats/internal/agent_data"
	"certainstats/internal/store"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/pem"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"golang.org/x/crypto/ssh"
)

func (h *WebHandler) AgentsListHandler(w http.ResponseWriter, r *http.Request) {
	userID := getUserID(r)
	agents, err := h.Store.AgentList(r.Context(), userID)
	if err != nil {
		http.Error(w, "Failed to load agents", http.StatusInternalServerError)
		return
	}

	pd := h.newPageData(r, "Agent Hub", "agents", map[string]any{"Agents": agents})
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

	pd := h.newPageData(r, "Fleet Management", "management", map[string]any{"Agents": agents})
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
		pub, priv, err := ed25519.GenerateKey(rand.Reader)
		if err == nil {
			privPEM, err := ssh.MarshalPrivateKey(priv, "")
			if err == nil {
				sshPub, err := ssh.NewPublicKey(pub)
				if err == nil {
					privBlock := pem.EncodeToMemory(privPEM)
					pubBytes := ssh.MarshalAuthorizedKey(sshPub)
					_ = h.Store.BeszelSSHSave(r.Context(), store.BeszelSSH{
						AgentID:    agentID,
						PublicKey:  strings.TrimSpace(string(pubBytes)),
						PrivateKey: string(privBlock),
					}, userID)
				}
			}
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
	rand.Read(tokBytes)
	newToken := hex.EncodeToString(tokBytes)

	_ = h.Store.AgentResetToken(r.Context(), agentID, userID, newToken)

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

	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err == nil {
		privPEM, err := ssh.MarshalPrivateKey(priv, "")
		if err == nil {
			sshPub, err := ssh.NewPublicKey(pub)
			if err == nil {
				privBlock := pem.EncodeToMemory(privPEM)
				pubAuthorizedKey := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(sshPub)))
				_ = h.Store.BeszelSSHSave(r.Context(), store.BeszelSSH{
					AgentID:    agentID,
					PublicKey:  pubAuthorizedKey,
					PrivateKey: string(privBlock),
				}, userID)
			}
		}
	}

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

	_ = h.Store.AgentDelete(r.Context(), agentID, userID)
	h.Cache.Delete(agentID)

	redir := r.FormValue("redirect_to")
	if redir == "" {
		redir = h.PanelPath + "/agents/management"
	}
	http.Redirect(w, r, redir, http.StatusSeeOther)
}
