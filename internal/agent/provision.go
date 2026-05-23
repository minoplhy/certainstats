package agent

import (
	agentdata "certainstats/internal/agent_data"
	"certainstats/internal/agent_parser/registry"
	base "certainstats/internal/base/agent"
	ctx "certainstats/internal/context"
	api_response "certainstats/internal/response"
	"certainstats/internal/store"
	"crypto/ed25519"
	crypto_rand "crypto/rand"
	"encoding/json"
	"encoding/pem"
	"math/rand"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"golang.org/x/crypto/ssh"
)

var adjectives = []string{
	"amber", "ancient", "arctic", "autumn", "azure",
	"bold", "bright", "bronze", "calm", "cardinal",
	"cedar", "cerulean", "cobalt", "cold", "copper",
	"cosmic", "crimson", "crystal", "dark", "dawn",
	"deep", "delta", "desert", "digital", "dusk",
	"elder", "ember", "epic", "eternal", "feral",
	"fierce", "flint", "forest", "frozen", "ghost",
	"gilded", "glacial", "golden", "grand", "hollow",
	"hyper", "indigo", "inland", "iron", "jade",
	"kindred", "kinetic", "lapis", "laser", "latent",
	"lunar", "magnet", "marble", "midnight", "mighty",
	"misty", "molten", "mystic", "nebula", "nimble",
	"noble", "north", "obsidian", "ocean", "onyx",
	"opal", "orbital", "polar", "prime", "prismatic",
	"quantum", "quiet", "rapid", "rogue", "rugged",
	"runic", "rustic", "sage", "scarlet", "serene",
	"shadow", "silent", "silver", "sleek", "solar",
	"sonic", "stark", "static", "steel", "stellar",
	"still", "stone", "storm", "swift", "tidal",
	"titan", "topaz", "turbo", "twilight", "ultra",
	"vast", "velvet", "vivid", "void", "volcanic",
	"wandering", "wild", "winter", "zephyr", "zenith",
}

var nouns = []string{
	"anchor", "apex", "arc", "arrow", "atlas",
	"beacon", "blade", "blaze", "bolt", "bridge",
	"cache", "cascade", "circuit", "citadel", "cipher",
	"comet", "compass", "conduit", "core", "crest",
	"crown", "current", "delta", "drifter", "echo",
	"edge", "engine", "epoch", "falcon", "field",
	"flare", "agent", "forge", "fortress", "fractal",
	"frontier", "gate", "glacier", "grid", "harbor",
	"hawk", "horizon", "host", "hub", "hulk",
	"hunter", "index", "island", "kernel", "lance",
	"lantern", "lattice", "layer", "lens", "link",
	"lynx", "matrix", "mesa", "mesh", "mirror",
	"module", "nexus", "node", "nomad", "north",
	"nova", "orbit", "outpost", "peak", "phantom",
	"pilot", "pinnacle", "pixel", "platform", "probe",
	"pulse", "raven", "ray", "reactor", "relay",
	"rift", "rook", "router", "runner", "sentry",
	"server", "signal", "socket", "spectre", "sphere",
	"spine", "spire", "stack", "star", "station",
	"stream", "strike", "strider", "summit", "surge",
	"switch", "synapse", "tether", "tower", "tracer",
	"trail", "vault", "vector", "veil", "vertex",
	"vortex", "watch", "wire", "warden", "wing",
	"wolf", "wraith", "zenith",
}

func GenerateNickname() string {
	return adjectives[rand.Intn(len(adjectives))] + "-" + nouns[rand.Intn(len(nouns))]
}

func ProvisionAgentHandler(agent store.AgentStore, parserRegistry *registry.Registry) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req base.ProvisionRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil && r.ContentLength > 0 {
			api_response.Error(w, http.StatusBadRequest, "Invalid JSON")
			return
		}

		// Default to beszel if not provided
		if req.AgentType == "" {
			req.AgentType = "beszel"
		}

		// Validate against Registry
		if !parserRegistry.IsSupported(req.AgentType) {
			api_response.Error(w, http.StatusBadRequest, "Unsupported agent type: "+req.AgentType)
			return
		}

		userID := r.Context().Value(ctx.UserIDKey).(string)
		agentID := agentdata.GenerateAgentID()
		token := agentdata.GenerateDeviceToken(req.AgentType)

		nickname := req.Nickname
		if nickname == "" {
			nickname = GenerateNickname()
		}

		err := agent.AgentProvision(r.Context(), agentID, userID, token, nickname, req.AgentType)
		if err != nil {
			api_response.Error(w, http.StatusInternalServerError, "Failed to provision agent")
			return
		}

		var publicKey string
		if req.AgentType == "beszel" {
			// Pre-generate unique SSH key pair for Beszel compatibility
			_, priv, err := ed25519.GenerateKey(crypto_rand.Reader)
			if err == nil {
				privPEM, err := ssh.MarshalPrivateKey(priv, "")
				if err == nil {
					keyBytes := pem.EncodeToMemory(privPEM)
					signer, _ := ssh.ParsePrivateKey(keyBytes)

					// Persist to beszel_ssh table immediately
					pubKey := signer.PublicKey()
					pubBytes := ssh.MarshalAuthorizedKey(pubKey)
					publicKey = strings.TrimSpace(string(pubBytes))

					_ = agent.BeszelSSHSave(r.Context(), store.BeszelSSH{
						AgentID:    agentID,
						PublicKey:  publicKey,
						PrivateKey: string(keyBytes),
					}, userID)
				}
			}
		}

		// Generate setup instructions
		panelPath, _ := r.Context().Value(ctx.PanelPathKey).(string)
		messages := getSetupInstructions(req.AgentType, token, r.Host, panelPath, publicKey)

		api_response.JSON(w, http.StatusOK, base.ProvisionResponse{
			AgentID:   agentID,
			Nickname:  nickname,
			AgentType: req.AgentType,
			Messages:  messages,
			Message:   "Agent provisioned successfully.",
		})
	}
}

func InstallAgentHandler(agent store.AgentStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := r.Context().Value(ctx.UserIDKey).(string)
		agentID := chi.URLParam(r, "id")
		if agentID == "" {
			api_response.Error(w, http.StatusBadRequest, "Missing agent id")
			return
		}

		// 1. Fetch management data to get token and public key
		mgtAgents, err := agent.AgentListManagement(r.Context(), userID)
		if err != nil {
			api_response.Error(w, http.StatusInternalServerError, "Database error")
			return
		}

		var mgtTarget *store.AgentManagement
		for _, a := range mgtAgents {
			if a.AgentID == agentID {
				mgtTarget = &a
				break
			}
		}

		if mgtTarget == nil {
			api_response.Error(w, http.StatusNotFound, "Agent management data not found")
			return
		}

		// Generate setup instructions using existing token and SSH key
		panelPath, _ := r.Context().Value(ctx.PanelPathKey).(string)
		messages := getSetupInstructions(mgtTarget.AgentType, mgtTarget.Token, r.Host, panelPath, mgtTarget.BeszelPublicKey)

		api_response.JSON(w, http.StatusOK, base.ProvisionResponse{
			AgentID:   mgtTarget.AgentID,
			Nickname:  mgtTarget.Nickname,
			AgentType: mgtTarget.AgentType,
			Messages:  messages,
			Message:   "Installation instructions generated.",
		})
	}
}

func UninstallAgentHandler(agent store.AgentStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := r.Context().Value(ctx.UserIDKey).(string)
		agentID := chi.URLParam(r, "id")
		if agentID == "" {
			api_response.Error(w, http.StatusBadRequest, "Missing agent id")
			return
		}

		// 1. Fetch management data to get token
		mgtAgents, err := agent.AgentListManagement(r.Context(), userID)
		if err != nil {
			api_response.Error(w, http.StatusInternalServerError, "Database error")
			return
		}

		var mgtTarget *store.AgentManagement
		for _, a := range mgtAgents {
			if a.AgentID == agentID {
				mgtTarget = &a
				break
			}
		}

		if mgtTarget == nil {
			api_response.Error(w, http.StatusNotFound, "Agent management data not found")
			return
		}

		// Generate uninstall instructions using existing token
		messages := getUninstallInstructions(mgtTarget.AgentType, mgtTarget.Token)

		api_response.JSON(w, http.StatusOK, base.ProvisionResponse{
			AgentID:   mgtTarget.AgentID,
			Nickname:  mgtTarget.Nickname,
			AgentType: mgtTarget.AgentType,
			Messages:  messages,
			Message:   "Uninstall instructions generated.",
		})
	}
}
