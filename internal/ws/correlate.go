package ws

import (
	"context"
	"fmt"
	"time"

	"github.com/fxamacker/cbor/v2"
)

// requestExpiry bounds how long a request without a deadline stays tracked.
const requestExpiry = 90 * time.Second

type pendingRequest struct {
	action   uint8
	response chan AgentResponse
	expires  time.Time
}

// Request is used by adapters. The socket reader delivers its response through Route.
func (h *Hub) Request(ctx context.Context, action uint8, data any) ([]byte, error) {
	id, response := h.track(ctx, action)
	defer h.forget(id)
	if err := h.Send(HubRequest[any]{Action: action, Data: data, Id: &id}); err != nil {
		return nil, err
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case resp, ok := <-response:
		if !ok {
			return nil, fmt.Errorf("agent disconnected")
		}
		if resp.Error != "" {
			return nil, fmt.Errorf("remote agent operation failed")
		}
		return resp.GetPayload(), nil
	}
}

// track registers a request ID, expiring stale entries. The request expires
// at the context deadline, or after requestExpiry without one.
func (h *Hub) track(ctx context.Context, action uint8) (uint32, chan AgentResponse) {
	h.pendingMu.Lock()
	defer h.pendingMu.Unlock()
	if h.pending == nil {
		h.pending = map[uint32]pendingRequest{}
	}

	now := time.Now()
	for id, p := range h.pending {
		if now.After(p.expires) {
			delete(h.pending, id)
			close(p.response)
		}
	}

	h.nextID++
	if h.nextID == 0 {
		h.nextID++
	}
	expires := now.Add(requestExpiry)
	if deadline, ok := ctx.Deadline(); ok {
		expires = deadline
	}
	response := make(chan AgentResponse, 1)
	h.pending[h.nextID] = pendingRequest{action: action, response: response, expires: expires}
	return h.nextID, response
}

func (h *Hub) forget(id uint32) {
	h.pendingMu.Lock()
	delete(h.pending, id)
	h.pendingMu.Unlock()
}

// SendTracked sends a request whose response is delivered by Route, not awaited.
func (h *Hub) SendTracked(action uint8, data any) error {
	id, _ := h.track(context.Background(), action)
	err := h.Send(HubRequest[any]{Action: action, Data: data, Id: &id})
	if err != nil {
		h.forget(id)
	}
	return err
}

// Route delivers a response to its waiting request. It returns the payload
// only for tracked, unexpired GetData responses, for telemetry ingestion.
func (h *Hub) Route(resp AgentResponse) (cbor.RawMessage, bool) {
	if resp.Id == nil {
		return nil, false
	}
	h.pendingMu.Lock()
	p, ok := h.pending[*resp.Id]
	if ok {
		delete(h.pending, *resp.Id)
	}
	if ok && time.Now().Before(p.expires) {
		p.response <- resp
		close(p.response)
	}
	h.pendingMu.Unlock()

	if !ok || time.Now().After(p.expires) || resp.Error != "" || p.action != GetData {
		return nil, false
	}
	return resp.GetPayload(), true
}

func (h *Hub) cancelPending() {
	h.pendingMu.Lock()
	for id, p := range h.pending {
		delete(h.pending, id)
		close(p.response)
	}
	h.pendingMu.Unlock()
}
