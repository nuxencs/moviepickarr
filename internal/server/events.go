package server

import (
	"sync"

	"github.com/google/uuid"
)

type event struct {
	// Seq is for client gap detection only (a jump triggers a resync), not a
	// replay cursor: the broker keeps no history.
	Seq  uint64 `json:"seq"`
	Type string `json:"type"`
	Data any    `json:"data,omitzero"`
}

type eventBroker struct {
	clients map[chan event]bool
	// seq is the last assigned Seq. epoch is boot-unique, so clients detect a restart.
	seq    uint64
	epoch  string
	closed bool
	mu     sync.RWMutex
}

func newEventBroker() *eventBroker {
	return &eventBroker{
		clients: make(map[chan event]bool),
		epoch:   uuid.NewString(),
	}
}

// Subscribe registers a client channel and returns the head seq, read under the
// same lock so no in-flight broadcast looks like a gap. It returns (nil, 0)
// once closed, so a late stream does not block forever.
func (b *eventBroker) Subscribe() (chan event, uint64) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.closed {
		return nil, 0
	}
	client := make(chan event, 10)
	b.clients[client] = true
	return client, b.seq
}

// HeadSeq returns the last assigned seq; heartbeats carry it so idle clients
// detect a missed event.
func (b *eventBroker) HeadSeq() uint64 {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.seq
}

// Epoch is the broker's boot-unique id. Immutable, so it needs no lock.
func (b *eventBroker) Epoch() string {
	return b.epoch
}

func (b *eventBroker) Unsubscribe(client chan event) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if _, ok := b.clients[client]; ok {
		delete(b.clients, client)
		close(client)
	}
}

// Broadcast assigns the next seq and fans the event out. The write lock keeps
// each client's seq in order, so a gap always means a dropped frame (a full
// buffer drops without blocking).
func (b *eventBroker) Broadcast(e event) {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.seq++
	e.Seq = b.seq

	for client := range b.clients {
		select {
		case client <- e:
		default:
		}
	}
}

// Close ends every stream and refuses new subscribers. Idempotent.
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
