package ws

import (
	ctx "certainstats/internal/context"
	log "certainstats/internal/logger"
	apiresponse "certainstats/internal/response"
	"certainstats/internal/store"
	"net/http"
	"time"

	"golang.org/x/net/websocket"
)

// UIWebSocketHandler creates a handler for browser WebSocket connections
func UIWebSocketHandler(broadcaster *AgentBroadcaster, sessions store.SessionStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// 1. Get UserID from context (populated by requireAuth middleware)
		userID, ok := r.Context().Value(ctx.UserIDKey).(string)
		if !ok || userID == "" {
			apiresponse.Error(w, http.StatusUnauthorized, "Unauthorized")
			return
		}

		cookie, err := r.Cookie("session_token")
		if err != nil {
			apiresponse.Error(w, 401, "Unauthorized")
			return
		}
		if !requireBrowserProtocol(w, r, broadcaster.protobuf) {
			return
		}
		release, ok := browserSlot(w, "user:"+userID, 64)
		if !ok {
			return
		}
		defer release()

		// 2. Setup WebSocket Server
		server := websocket.Server{
			Handshake: func(config *websocket.Config, req *http.Request) error {
				origin := req.Header.Get("Origin")
				if err := checkRequestOrigin(req); err != nil {
					log.Printf("[UI-WS] Rejected connection from unauthorized origin: %s", origin)
					return err
				}
				return selectBrowserProtocol(config, broadcaster.protobuf)
			},
			Handler: func(conn *websocket.Conn) {
				defer conn.Close()
				conn.MaxPayloadBytes = 4096
				stop := watchBrowser(conn, func() bool {
					s, e := sessions.SessionGet(r.Context(), cookie.Value)
					return e == nil && time.Now().Before(s.ExpiresAt)
				})
				defer stop()

				// Subscribe to broadcaster
				broadcaster.SubscribeUser(userID, conn)
				broadcaster.SubscribeSession(cookie.Value, conn)
				defer broadcaster.UnsubscribeSession(cookie.Value, conn)
				defer broadcaster.UnsubscribeUser(userID, conn)

				log.Debugf("[UI-WS] Browser connected for User: %s", userID)

				// Keep connection alive/open until client disconnects
				// We don't expect messages FROM the UI for now, but we must read to detect disconnects
				for {
					var msg []byte
					if err := websocket.Message.Receive(conn, &msg); err != nil {
						log.Debugf("[UI-WS] Browser disconnected for User: %s", userID)
						break
					}
				}
			},
		}

		server.ServeHTTP(w, r)
	}
}
