package main

import (
	"certainstats/internal/ws"
	"fmt"
	"log"
	"net/url"
	"os"
	"os/signal"
	"strings"

	"github.com/fxamacker/cbor/v2"
	"golang.org/x/net/websocket"
)

func main() {
	token := os.Getenv("TOKEN")
	hubURL := os.Getenv("PANEL_PATH")

	if token == "" || hubURL == "" {
		log.Fatal("Error: TOKEN and PANEL_PATH environment variables must be set")
	}

	// Clean up URL
	hubURL = strings.TrimSuffix(hubURL, "/")
	u, err := url.Parse(hubURL)
	if err != nil {
		log.Fatalf("Invalid HUB_URL: %v", err)
	}

	// Determine WebSocket Scheme
	scheme := "ws"
	if u.Scheme == "https" {
		scheme = "wss"
	}

	wsURL := fmt.Sprintf("%s://%s%s/api/beszel/agent-connect", scheme, u.Host, u.Path)
	origin := hubURL

	fmt.Printf("🕵️  Beszel Agent Spy started\n")
	fmt.Printf("🔗 Connecting to: %s\n", wsURL)
	fmt.Printf("🔑 Token: %s...%s\n", token[:4], token[len(token)-4:])
	fmt.Println("--------------------------------------------------")

	config, err := websocket.NewConfig(wsURL, origin)
	if err != nil {
		log.Fatal(err)
	}
	config.Header.Set("X-Token", token)

	conn, err := websocket.DialConfig(config)
	if err != nil {
		log.Fatalf("Connection failed: %v", err)
	}
	defer conn.Close()

	fmt.Println("✅ Connection Established. Waiting for Hub requests...")

	// Handle Interrupts
	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt)

	go func() {
		for {
			var raw []byte
			err := websocket.Message.Receive(conn, &raw)
			if err != nil {
				log.Printf("❌ Read Error: %v", err)
				return
			}

			var req ws.HubRequest[ws.DataRequestOptions]
			if err := cbor.Unmarshal(raw, &req); err != nil {
				fmt.Printf("❓ Received Non-CBOR or Unknown Data: %d bytes\n", len(raw))
				continue
			}

			actionStr := "Unknown"
			switch req.Action {
			case ws.GetData:
				actionStr = "GET_DATA"
			case ws.CheckFingerprint:
				actionStr = "CHECK_FINGERPRINT"
			}

			fmt.Printf("\n📥 [RECV] Action: %s\n", actionStr)
			if req.Id != nil {
				fmt.Printf("   ID: %d\n", *req.Id)
			}
			fmt.Printf("   IncludeDetails: %v\n", req.Data.IncludeDetails)
			fmt.Printf("   CacheTimeMs:    %d\n", req.Data.CacheTimeMs)
		}
	}()

	<-interrupt
	fmt.Println("\n👋 Spy stopped.")
}
