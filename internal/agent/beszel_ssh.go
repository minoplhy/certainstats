package agent

import (
	ctx "certainstats/internal/context"
	api_response "certainstats/internal/response"
	"certainstats/internal/store"
	"certainstats/internal/ws"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"golang.org/x/crypto/ssh"
)

func ResetAgentSSHKeyHandler(agent store.AgentStore, wsManager *ws.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := r.Context().Value(ctx.UserIDKey).(string)
		agentID := chi.URLParam(r, "id")
		if agentID == "" {
			api_response.Error(w, http.StatusBadRequest, "Missing agent_id")
			return
		}

		// 1. Verify agent type and ownership
		agents, err := agent.AgentList(r.Context(), userID)
		if err != nil {
			api_response.Error(w, http.StatusInternalServerError, "Database error")
			return
		}

		var targetAgent *store.Agent
		for _, a := range agents {
			if a.AgentID == agentID {
				targetAgent = &a
				break
			}
		}

		if targetAgent == nil {
			api_response.Error(w, http.StatusNotFound, "Agent not found or unauthorized")
			return
		}

		if targetAgent.AgentType != "beszel" {
			api_response.Error(w, http.StatusBadRequest, "SSH key management is only supported for Beszel agents")
			return
		}

		// 2. Generate new unique SSH key pair
		_, priv, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			api_response.Error(w, http.StatusInternalServerError, "Key generation failed")
			return
		}

		privPEM, err := ssh.MarshalPrivateKey(priv, "")
		if err != nil {
			api_response.Error(w, http.StatusInternalServerError, "Key encoding failed")
			return
		}
		keyBytes := pem.EncodeToMemory(privPEM)
		signer, _ := ssh.ParsePrivateKey(keyBytes)
		pubKey := signer.PublicKey()
		pubBytes := ssh.MarshalAuthorizedKey(pubKey)
		publicKey := strings.TrimSpace(string(pubBytes))

		// 3. Persist to beszel_ssh table
		err = agent.BeszelSSHSave(r.Context(), store.BeszelSSH{
			AgentID:    agentID,
			PublicKey:  publicKey,
			PrivateKey: string(keyBytes),
		}, userID)
		if err != nil {
			api_response.Error(w, http.StatusInternalServerError, "Failed to save new SSH key")
			return
		}

		api_response.JSON(w, http.StatusOK, map[string]string{
			"status":     "success",
			"message":    "SSH key reset successfully",
			"public_key": publicKey,
		})
	}
}

func GetAgentSSHKeyHandler(agent store.AgentStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := r.Context().Value(ctx.UserIDKey).(string)
		agentID := chi.URLParam(r, "id")
		if agentID == "" {
			api_response.Error(w, http.StatusBadRequest, "Missing agent_id")
			return
		}

		sshKey, err := agent.BeszelSSHGet(r.Context(), agentID, userID)
		if err != nil {
			api_response.Error(w, http.StatusInternalServerError, "Database error")
			return
		}

		if sshKey == nil {
			api_response.Error(w, http.StatusNotFound, "SSH key not found for this agent")
			return
		}

		api_response.JSON(w, http.StatusOK, map[string]string{
			"agent_id":   agentID,
			"public_key": sshKey.PublicKey,
		})
	}
}
