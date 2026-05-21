package ws

import (
	ctx "certainstats/internal/context"
	log "certainstats/internal/logger"
	apiresponse "certainstats/internal/response"
	"errors"
	"net/http"
	"os"
	"strings"

	"golang.org/x/net/websocket"
)

// UIWebSocketHandler creates a handler for browser WebSocket connections
func UIWebSocketHandler(broadcaster *AgentBroadcaster) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// 1. Get UserID from context (populated by requireAuth middleware)
		userID, ok := r.Context().Value(ctx.UserIDKey).(string)
		if !ok || userID == "" {
			apiresponse.Error(w, http.StatusUnauthorized, "Unauthorized")
			return
		}

		// 2. Setup WebSocket Server
		server := websocket.Server{
			Handshake: func(config *websocket.Config, req *http.Request) error {
				origin := req.Header.Get("Origin")
				allowed := os.Getenv("ALLOWED_ORIGINS")

				// If no allowed origins specified, allow all (dev mode)
				if allowed == "" {
					return nil
				}

				// Check if origin matches any of the allowed patterns
				origins := strings.Split(allowed, ",")
				for _, o := range origins {
					if strings.TrimSpace(o) == origin {
						return nil
					}
				}

				log.Printf("[UI-WS] Rejected connection from unauthorized origin: %s", origin)
				return errors.New("unauthorized origin")
			},
			Handler: func(conn *websocket.Conn) {
				defer conn.Close()

				// Subscribe to broadcaster
				broadcaster.SubscribeUser(userID, conn)
				defer broadcaster.UnsubscribeUser(userID, conn)

				log.Debugf("[UI-WS] Browser connected for User: %s", userID)

				// Keep connection alive/open until client disconnects
				// We don't expect messages FROM the UI for now, but we must read to detect disconnects
				for {
					var msg string
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
