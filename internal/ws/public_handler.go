package ws

import (
	log "certainstats/internal/logger"
	"certainstats/internal/store"
	"errors"
	"net/http"
	"os"
	"strings"

	"github.com/go-chi/chi/v5"
	"golang.org/x/net/websocket"
)

// PublicWebSocketHandler creates a handler for public dashboard viewers
func PublicWebSocketHandler(agents store.AgentStore, broadcaster *AgentBroadcaster) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		dashID := chi.URLParam(r, "id")
		if dashID == "" {
			http.Error(w, "Missing dashboard id", http.StatusBadRequest)
			return
		}

		// 1. Verify dashboard exists (optional but good for validation)
		// We can just trust the ID for now or verify if it exists in the store
		// For pulse consistency, we just need the ID to subscribe.

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

				log.Printf("[Public-WS] Rejected connection from unauthorized origin: %s", origin)
				return errors.New("unauthorized origin")
			},
			Handler: func(conn *websocket.Conn) {
				defer conn.Close()

				// Subscribe to dashboard updates
				broadcaster.SubscribeDash(dashID, conn)
				defer broadcaster.UnsubscribeDash(dashID, conn)

				log.Debugf("[Public-WS] Viewer connected for Dashboard: %s", dashID)

				// Keep connection open
				for {
					var msg string
					if err := websocket.Message.Receive(conn, &msg); err != nil {
						log.Debugf("[Public-WS] Viewer disconnected for Dashboard: %s", dashID)
						break
					}
				}
			},
		}

		server.ServeHTTP(w, r)
	}
}
