package ws

import (
	"golang.org/x/net/websocket"
	"net/http"
	"sync"
	"time"
)

var browserConnections = make(chan struct{}, 1024)
var connectionMu sync.Mutex
var connectionCounts = make(map[string]int)

func browserSlot(w http.ResponseWriter, key string, limit int) (func(), bool) {
	connectionMu.Lock()
	defer connectionMu.Unlock()
	if connectionCounts[key] >= limit {
		w.Header().Set("Retry-After", "2")
		http.Error(w, "Connection limit exceeded", 429)
		return nil, false
	}
	select {
	case browserConnections <- struct{}{}:
	default:
		w.Header().Set("Retry-After", "2")
		http.Error(w, "Connection capacity unavailable", 503)
		return nil, false
	}
	connectionCounts[key]++
	return func() {
		connectionMu.Lock()
		defer connectionMu.Unlock()
		connectionCounts[key]--
		if connectionCounts[key] == 0 {
			delete(connectionCounts, key)
		}
		<-browserConnections
	}, true
}
func watchBrowser(conn *websocket.Conn, valid func() bool) func() {
	done := make(chan struct{})
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				if !valid() {
					conn.Close()
					return
				}
			}
		}
	}()
	return func() { close(done) }
}
