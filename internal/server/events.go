package server

import "sync"

type event struct {
	Type string `json:"type"`
	Data any    `json:"data,omitempty"`
}

type eventBroker struct {
	clients map[chan event]bool
	mu      sync.RWMutex
}

func newEventBroker() *eventBroker {
	return &eventBroker{clients: make(map[chan event]bool)}
}

func (b *eventBroker) Subscribe() chan event {
	b.mu.Lock()
	defer b.mu.Unlock()

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

func (b *eventBroker) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()

	for client := range b.clients {
		close(client)
		delete(b.clients, client)
	}
}
