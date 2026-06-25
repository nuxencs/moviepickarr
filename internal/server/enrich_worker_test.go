package server

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/rs/zerolog"
)

// countBatchEvents drains a broker client for "movies:enriched-batch" frames
// until the channel goes quiet for `within`.
func countBatchEvents(client chan event, within time.Duration) int {
	count := 0
	for {
		select {
		case e, ok := <-client:
			if !ok {
				return count
			}
			if e.Type == "movies:enriched-batch" {
				count++
			}
		case <-time.After(within):
			return count
		}
	}
}

func newBatchRunner(broker *eventBroker, debounce, maxWait time.Duration) (*enrichRunner, *atomic.Int32) {
	cfg := defaultEnrichConfig()
	cfg.BatchDebounce = debounce
	cfg.BatchMaxWait = maxWait
	r := newEnrichRunner(nil, broker, cfg, zerolog.Nop())

	var statsInvalidations atomic.Int32
	r.onEnriched = func() { statsInvalidations.Add(1) }
	return r, &statsInvalidations
}

// A drain enriching many movies back-to-back must collapse to ONE batch event
// and ONE stats-cache invalidation — the heart of the coalescing fix.
func TestEnrichRunner_DrainCoalescesToSingleBroadcast(t *testing.T) {
	broker := newEventBroker()
	client := broker.Subscribe()
	defer broker.Unsubscribe(client)

	// Long debounce so only the explicit drain-tail flush fires, not the timer.
	r, stats := newBatchRunner(broker, 5*time.Second, 5*time.Second)

	const n = 50
	for i := range n {
		r.recordEnriched(i)
	}
	r.flushBatch() // mirrors the flush at drain completion

	if got := countBatchEvents(client, 50*time.Millisecond); got != 1 {
		t.Fatalf("expected 1 movies:enriched-batch for %d enrichments, got %d", n, got)
	}
	if got := stats.Load(); got != 1 {
		t.Fatalf("expected stats cache invalidated once per batch, got %d", got)
	}
}

// The debounce timer alone (no explicit flush, as on a sparse single enqueue)
// must emit exactly one batch once the burst goes quiet.
func TestEnrichRunner_DebounceTimerEmitsOnce(t *testing.T) {
	broker := newEventBroker()
	client := broker.Subscribe()
	defer broker.Unsubscribe(client)

	r, stats := newBatchRunner(broker, 20*time.Millisecond, 500*time.Millisecond)

	for i := range 3 {
		r.recordEnriched(i)
	}
	// Don't flush explicitly — let the debounce timer fire.

	if got := countBatchEvents(client, 200*time.Millisecond); got != 1 {
		t.Fatalf("expected 1 debounced movies:enriched-batch, got %d", got)
	}
	if got := stats.Load(); got != 1 {
		t.Fatalf("expected stats cache invalidated once, got %d", got)
	}
}

// flushBatch with nothing buffered must not broadcast or invalidate.
func TestEnrichRunner_EmptyFlushIsNoop(t *testing.T) {
	broker := newEventBroker()
	client := broker.Subscribe()
	defer broker.Unsubscribe(client)

	r, stats := newBatchRunner(broker, time.Second, time.Second)
	r.flushBatch()

	if got := countBatchEvents(client, 30*time.Millisecond); got != 0 {
		t.Fatalf("expected no events from an empty flush, got %d", got)
	}
	if got := stats.Load(); got != 0 {
		t.Fatalf("expected no stats invalidation from an empty flush, got %d", got)
	}
}

// Two separated bursts must each emit their own batch.
func TestEnrichRunner_SeparateBurstsEachFlush(t *testing.T) {
	broker := newEventBroker()
	client := broker.Subscribe()
	defer broker.Unsubscribe(client)

	r, stats := newBatchRunner(broker, 5*time.Second, 5*time.Second)

	r.recordEnriched(1)
	r.recordEnriched(2)
	r.flushBatch()

	r.recordEnriched(3)
	r.flushBatch()

	if got := countBatchEvents(client, 50*time.Millisecond); got != 2 {
		t.Fatalf("expected 2 batch events for 2 bursts, got %d", got)
	}
	if got := stats.Load(); got != 2 {
		t.Fatalf("expected 2 stats invalidations for 2 bursts, got %d", got)
	}
}
