package server

import "sync"

type event struct {
	Type string `json:"type"`
	Data any    `json:"data,omitempty"`
}

type eventBroker struct {
	clients map[chan event]bool
	closed  bool
	mu      sync.RWMutex
}

func newEventBroker() *eventBroker {
	return &eventBroker{clients: make(map[chan event]bool)}
}

// Subscribe registers a new client channel, or returns nil once the broker has
// been closed (server shutting down) so a late-arriving SSE stream returns
// immediately instead of blocking on a channel that will never be fed/closed.
func (b *eventBroker) Subscribe() chan event {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.closed {
		return nil
	}
	client := make(chan event, 10)
	b.clients[client] = true
	return client
}

func (b *eventBroker) Unsubscribe(client chan event) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if _, ok := b.clients[client]; ok {
		delete(b.clients, client)
		close(client)
	}
}

func (b *eventBroker) Broadcast(e event) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	for client := range b.clients {
		select {
		case client <- e:
		default:
		}
	}
}

// Close unwinds every subscribed stream and marks the broker closed so no new
// stream can subscribe. Idempotent: a second call (the deferred shutdown path)
// is a no-op.
func (b *eventBroker) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.closed {
		return
	}
	b.closed = true
	for client := range b.clients {
		close(client)
		delete(b.clients, client)
	}
}
