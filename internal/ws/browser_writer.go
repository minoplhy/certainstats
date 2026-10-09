package ws

import (
	"golang.org/x/net/websocket"
	"time"
)

type browserWriter struct {
	queue chan []byte
	done  chan struct{}
}

// One bounded mailbox per bounded browser connection keeps slow readers out of
// the telemetry/alert routine. A queued older pulse is replaced by the latest.
func (b *AgentBroadcaster) startWriter(conn *websocket.Conn) {
	if b.writers[conn] != nil {
		return
	}
	writer := &browserWriter{queue: make(chan []byte, 1), done: make(chan struct{})}
	b.writers[conn] = writer
	go func() {
		for {
			select {
			case <-writer.done:
				return
			case payload := <-writer.queue:
				_ = conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
				if err := websocket.Message.Send(conn, string(payload)); err != nil {
					conn.Close()
					return
				}
			}
		}
	}()
}
func (b *AgentBroadcaster) stopWriter(conn *websocket.Conn) {
	if writer := b.writers[conn]; writer != nil {
		delete(b.writers, conn)
		close(writer.done)
	}
}
