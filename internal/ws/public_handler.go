package ws

import (
	log "certainstats/internal/logger"
	apiresponse "certainstats/internal/response"
	"certainstats/internal/store"
	"database/sql"
	"errors"
	"net/http"
	"os"
	"strings"

	"github.com/go-chi/chi/v5"
	"golang.org/x/net/websocket"
)

// PublicWebSocketHandler creates a handler for public dashboard viewers
func PublicWebSocketHandler(dashboard store.DashboardStore, broadcaster *AgentBroadcaster) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		dashID := chi.URLParam(r, "id")
		if dashID == "" {
			apiresponse.Error(w, http.StatusNotFound, "Missing dashboard id")
			return
		}

		// 1. Verify dashboard exists
		// 	  This isn't meant to implement initially, but after some thoughts,
		//    It is not ~~safe~~ to let invalid dashboard freely subscribe into WebSocket.
		//	  While this might not meant anything, as we utilize announce-only policy on Websocket,
		//    there is no any meant of 2-Way Communication. But still, leaving TCP open for invalid request
		//    is also not optimal.
		_, err := dashboard.DashboardGetByID(r.Context(), dashID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				apiresponse.Error(w, http.StatusNotFound, "Dashboard not found")
				return
			}
			apiresponse.Error(w, http.StatusInternalServerError, "Database error")
			return
		}

		// 2. Setup WebSocket Server
		server := websocket.Server{
			Handshake: func(config *websocket.Config, req *http.Request) error {
				origin := req.Header.Get("Origin")
				allowed := os.Getenv("ALLOWED_ORIGINS")

				// If no allowed origins specified, allow all
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
