package agent

import (
	"certainstats/internal/agentmeta"
	log "certainstats/internal/logger"
	nm "certainstats/internal/networkmonitor"
	"certainstats/internal/networkservice"
	apiresponse "certainstats/internal/response"
	"context"
	"crypto/rand"
	"errors"
	"net/http"
	"time"

	beszelparser "certainstats/internal/agent_parser/Beszel"
	"certainstats/internal/metrics"
	"certainstats/internal/store"
	"certainstats/internal/ws"

	"github.com/fxamacker/cbor/v2"
	"github.com/prometheus/prometheus/tsdb"
	"golang.org/x/crypto/ssh"
	"golang.org/x/net/websocket"
)

// authTimeout bounds the CheckFingerprint handshake.
const authTimeout = 10 * time.Second

// BeszelWSHandler handles persistent WebSocket connections from Beszel agents.
// These connections are polled for stats at a configurable interval (default 60s).
// Network monitor results arrive in the same stats payload and go to network.
func BeszelWSHandler(db store.AgentStore, tdb *tsdb.DB, wsManager *ws.Manager, cache *metrics.RealtimeCache, network *networkservice.Service) http.HandlerFunc {
	acr := &ws.AgentConnectRequest{}
	parser := &beszelparser.BeszelStats{}

	return func(w http.ResponseWriter, r *http.Request) {
		token, err := parser.ParseToken(r.Header)
		if err != nil {
			log.Printf("[WS] Header validation failed: %v", err)
			apiresponse.Error(w, http.StatusUnauthorized, "Unauthorized")
			return
		}
		version := r.Header.Get("X-Beszel")

		// 1. Resolve Agent Identity (Validate Token)
		identity, err := db.AgentGetByToken(r.Context(), token)
		if err != nil || (identity != nil && identity.AgentType != "" && identity.AgentType != "beszel") {
			log.Printf("[WS] Unauthorized agent connection attempt (invalid credentials)")
			apiresponse.Error(w, http.StatusUnauthorized, "Unauthorized")
			return
		}

		acr.Upgrade(w, r, token, version, func(conn *websocket.Conn, token string, version string) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			log.Debugf("[WS] Agent connected: %s (version: %s)", identity.AgentID, version)

			hub := ws.NewHub()
			// The epoch scopes duplicate suppression of network results to this connection.
			hub.ConnectionEpoch = nm.ID()
			hub.SetConn(conn)
			defer hub.Close()

			telemetry := make(chan cbor.RawMessage, 8)
			// Reader is the only socket receiver. Request responses are correlated by ID.
			go func() {
				defer cancel()
				defer hub.Close()
				for {
					var resp ws.AgentResponse
					if err := hub.Receive(&resp); err != nil {
						log.Debugf("[WS] Connection closed for %s: %v", identity.AgentID, err)
						return
					}
					if payload, ok := hub.Route(resp); ok {
						select {
						case telemetry <- payload:
						case <-ctx.Done():
							return
						}
					}
				}
			}()

			// 2. Auth Handshake (CheckFingerprint)
			// Load or Generate per-agent SSH key for "1 key, 1 machine" policy
			sshKey, err := db.BeszelSSHGet(ctx, identity.AgentID, identity.UserID)
			if err != nil {
				log.Printf("[WS] Database error fetching SSH key for %s: %v", identity.AgentID, err)
			}

			var signer ssh.Signer
			if sshKey != nil {
				signer, err = ssh.ParsePrivateKey([]byte(sshKey.PrivateKey))
				if err != nil {
					log.Printf("[WS] Failed to parse existing SSH key for %s: %v", identity.AgentID, err)
				}
			}

			if signer == nil {
				log.Printf("[WS] Connection rejected: No valid SSH key for agent %s. Please re-provision or generate key manually.", identity.AgentID)
				return
			}

			sig, err := signer.Sign(rand.Reader, []byte(token))
			if err != nil {
				log.Printf("[WS] Failed to sign token for %s: %v", identity.AgentID, err)
				return
			}

			if err = authenticate(ctx, hub, sig.Blob); err != nil {
				log.Printf("[WS] Authentication failed for %s: %v", identity.AgentID, err)
				return
			}

			// The handshake version header is the first runtime observation.
			runtime := agentmeta.Runtime{AgentVersion: agentmeta.String(version), VersionSource: "beszel_header"}
			if err = db.AgentUpdateRuntime(ctx, identity.AgentID, identity.UserID, runtime); err != nil {
				log.Printf("[WS] Runtime update failed for %s: %v", identity.AgentID, err)
				return
			}

			wsManager.Register(token, hub)
			defer wsManager.UnregisterHub(token, hub)

			// A reconnect always resends the full monitor set.
			go syncNetwork(ctx, network, identity, hub, true)

			request := ws.DataRequestOptions{CacheTimeMs: ws.StatsCacheTimeMs, IncludeDetails: true}
			if err = hub.SendTracked(ws.GetData, request); err != nil {
				log.Printf("[WS] Initial stats request failed for %s: %v", identity.AgentID, err)
				return
			}
			for {
				select {
				case <-ctx.Done():
					return
				case payload := <-telemetry:
					parsed, err := parser.Parse(payload)
					if err != nil {
						log.Printf("[WS] Invalid data for %s: %v", identity.AgentID, err)
						continue
					}
					networkResults := parsed.NetworkResults
					parsed.NetworkResults = nil
					if err = Ingest(ctx, db, tdb, cache, identity, parsed); err != nil {
						log.Printf("[WS] Ingestion failed for %s: %v", identity.AgentID, err)
						return
					}
					if err = network.Accept(ctx, identity.UserID, identity.AgentID, hub.ConnectionEpoch, networkResults); err != nil {
						log.Printf("[Network] Ingestion failed for %s: %v", identity.AgentID, err)
					}
					// Retry configuration that is still pending after an earlier failure.
					go syncNetwork(ctx, network, identity, hub, false)
				}
			}
		})
	}
}

// authenticate completes the CheckFingerprint handshake with the signed token.
func authenticate(ctx context.Context, hub *ws.Hub, signature []byte) error {
	authCtx, cancel := context.WithTimeout(ctx, authTimeout)
	defer cancel()
	raw, err := hub.Request(authCtx, ws.CheckFingerprint, ws.FingerprintRequest{Signature: signature, NeedSysInfo: true})
	if err != nil {
		return err
	}
	var response struct {
		Fingerprint string `cbor:"0,keyasint"`
	}
	if err = cbor.Unmarshal(raw, &response); err != nil {
		return err
	}
	if response.Fingerprint == "" {
		return errors.New("empty fingerprint")
	}
	return nil
}

func syncNetwork(ctx context.Context, network *networkservice.Service, identity *store.AgentIdentity, hub *ws.Hub, force bool) {
	if err := network.Sync(ctx, identity.UserID, identity.AgentID, hub, force); err != nil && ctx.Err() == nil {
		log.Printf("[Network] Configuration sync failed for %s: %v", identity.AgentID, err)
	}
}
