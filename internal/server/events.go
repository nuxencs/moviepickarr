package server

import (
	"sync"

	"github.com/google/uuid"
)

type event struct {
	// Seq is a broker-global monotonic sequence number assigned at broadcast
	// time. The client uses it purely for gap detection: a per-client jump in
	// seq means a frame was dropped (a full buffer, or a window while the socket
	// was down), which it heals with one resync. It is NOT a replay cursor — the
	// broker keeps no history.
	Seq  uint64 `json:"seq"`
	Type string `json:"type"`
	Data any    `json:"data,omitempty"`
}

type eventBroker struct {
	clients map[chan event]bool
	// seq is the last assigned broadcast sequence (see event.Seq). epoch is a
	// boot-unique id handed to clients in the connected handshake so they can
	// detect a server restart (epoch changed) and resync.
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

// Subscribe registers a new client channel and returns it alongside the current
// head sequence, or (nil, 0) once the broker has been closed (server shutting
// down) so a late-arriving SSE stream returns immediately instead of blocking on
// a channel that will never be fed/closed. The head seq is captured under the
// same lock as registration, so any event broadcast after Subscribe carries a
// seq strictly greater than the returned head — letting the client align its
// gap-detection cursor from the connected handshake without racing an in-flight
// broadcast into a spurious gap.
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

// HeadSeq returns the most recently assigned broadcast sequence. The heartbeat
// frame carries it so an idle client whose cursor trails the head knows it
// missed an event and resyncs.
func (b *eventBroker) HeadSeq() uint64 {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.seq
}

// Epoch is the broker's boot-unique id. It is set once at construction and never
// mutated, so it is safe to read without the lock.
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

// Broadcast assigns the next monotonic sequence number and fans the event out to
// every subscriber. It takes the write lock (not a read lock) so the seq
// assignment and the enqueue are atomic: each client receives events in strict
// seq order on its channel, making a per-client seq gap an unambiguous
// dropped-frame signal rather than a reorder artefact. A full client buffer
// still drops (non-blocking) — the skipped seq is exactly what the client's
// gap-detector / heartbeat catches and heals via resync.
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
