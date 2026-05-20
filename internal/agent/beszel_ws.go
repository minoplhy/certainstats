package agent

import (
	"context"
	"crypto/rand"
	log "certainstats/internal/logger"
	"net/http"

	beszelparser "certainstats/internal/agent_parser/Beszel"
	agentparser "certainstats/internal/agent_parser"
	"certainstats/internal/metrics"
	"certainstats/internal/store"
	"certainstats/internal/ws"

	"github.com/prometheus/prometheus/tsdb"
	"golang.org/x/crypto/ssh"
	"golang.org/x/net/websocket"
)

// BeszelWSHandler handles persistent WebSocket connections from Beszel agents.
// These connections are polled for stats at a configurable interval (default 60s).
func BeszelWSHandler(db store.AgentStore, tdb *tsdb.DB, wsManager *ws.Manager, cache *metrics.RealtimeCache) http.HandlerFunc {
	acr := &ws.AgentConnectRequest{}
	parser := &beszelparser.BeszelStats{}

	return func(w http.ResponseWriter, r *http.Request) {
		token, err := parser.ParseToken(r.Header)
		if err != nil {
			log.Printf("[WS] Header validation failed: %v", err)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		version := r.Header.Get("X-Beszel")

		// 1. Resolve Agent Identity (Validate Token)
		identity, err := db.AgentGetByToken(r.Context(), token)
		if err != nil {
			log.Printf("[WS] Unauthorized agent connection attempt: %s", token)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		acr.Upgrade(w, r, token, version, func(conn *websocket.Conn, token string, version string) {
			ctx := context.Background()
			log.Debugf("[WS] Agent connected: %s (version: %s)", token, version)

			hub := ws.NewHub()
			hub.SetConn(conn)
			defer hub.Close()

			// Register agent in central manager
			wsManager.Register(token, hub)
			defer wsManager.Unregister(token)

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

			signature := sig.Blob

			authID := uint32(0)
			authReq := ws.HubRequest[ws.FingerprintRequest]{
				Action: ws.CheckFingerprint,
				Data: ws.FingerprintRequest{
					Signature:   signature,
					NeedSysInfo: true,
				},
				Id: &authID,
			}
			if err := hub.Send(authReq); err != nil {
				log.Printf("[WS] Failed to send auth challenge to %s: %v", token, err)
				return
			}

			// 3. Request initial data (with full details)
			id := uint32(1)
			hub.Send(ws.HubRequest[ws.DataRequestOptions]{
				Action: ws.GetData,
				Data:   ws.DataRequestOptions{CacheTimeMs: 60000, IncludeDetails: true},
				Id:     &id,
			})

			// 4. Message Receiving Loop
			for {
				var resp ws.AgentResponse
				if err := hub.Receive(&resp); err != nil {
					log.Printf("[WS] Connection lost for %s: %v", token, err)
					break
				}

				payload := resp.GetPayload()
				if len(payload) > 0 {
					// Use the standardized Beszel parser
					parsed, err := parser.Parse(payload)
					if err == nil {
						// Trigger detail update asynchronously to prevent database locks from blocking the websocket message loop
						go func(agentID, userID string, info *agentparser.ParsedMetadata) {
							ctxBg := context.Background()
							var dbErr error
							if info != nil {
								dbErr = db.AgentUpsertDetails(ctxBg, store.Agent{
									AgentID:      agentID,
									UserID:       userID,
									Uptime:       info.Uptime,
									LinuxVersion: info.LinuxVersion,
									CpuModel:     info.CpuModel,
									CpuCores:     info.CpuCores,
									RamSize:      info.RamSize,
									DiskSize:     info.DiskSize,
									SwapSize:     info.SwapSize,
								})
							} else {
								dbErr = db.AgentUpdateHeartbeat(ctxBg, agentID, userID)
							}
							if dbErr != nil {
								log.Printf("[WS] background state update error for %s: %v", agentID, dbErr)
							}
						}(identity.AgentID, identity.UserID, parsed.AgentInfo)

						// Normalize IO metrics to delta bytes (in-place) before TSDB + cache
						if len(parsed.Metrics) > 0 {
							normalizeIOMetrics(identity.AgentID, parsed.Metrics)

							var batchRX, batchTX float64
							diskDeltas := make(map[string]*store.DiskDelta)
							for _, m := range parsed.Metrics {
								batchRX += m.RXBytes
								batchTX += m.TXBytes
								for _, d := range m.Disks {
									if d.Path == "" {
										continue
									}
									if existing, ok := diskDeltas[d.Path]; ok {
										existing.ReadBytes += d.ReadBytes
										existing.WriteBytes += d.WriteBytes
									} else {
										diskDeltas[d.Path] = &store.DiskDelta{
											Path:       d.Path,
											ReadBytes:  d.ReadBytes,
											WriteBytes: d.WriteBytes,
										}
									}
								}
							}

							var disks []store.DiskDelta
							for _, dd := range diskDeltas {
								disks = append(disks, *dd)
							}

							_ = db.AgentIncrementTraffic(ctx, identity.AgentID, identity.UserID, uint64(batchRX), uint64(batchTX), disks)
						}

						// Write to TSDB
						if len(parsed.Metrics) > 0 {
							WriteStatsToTSDB(ctx, tdb, identity, parsed.Metrics)
						}

						// Update Realtime Cache
						if cache != nil {
							cache.Update(identity.AgentID, parsed)
						}

						log.Debugf("[WS] Persisted data for %s (Host: %s)", token, parsed.AgentInfo.CpuModel)
					} else if len(resp.Fingerprint) > 0 {
						log.Debugf("[WS] Auth confirmed by agent %s", token)
					}
				}

				if resp.Error != "" {
					log.Printf("[WS] Error from agent %s: %s", token, resp.Error)
				}
			}
		})
	}
}
