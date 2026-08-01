package server

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"moviepickarr/internal/domain"

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
	client, _ := broker.Subscribe()
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
	client, _ := broker.Subscribe()
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
	client, _ := broker.Subscribe()
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
	client, _ := broker.Subscribe()
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

type blockingRerunEnricher struct {
	calls         atomic.Int32
	firstStarted  chan struct{}
	releaseFirst  chan struct{}
	secondStarted chan struct{}
	releaseSecond chan struct{}
	firstErr      error
}

func (e *blockingRerunEnricher) EnrichOne(ctx context.Context, movieID int) (enrichResult, error) {
	call := e.calls.Add(1)
	if call == 1 && e.firstStarted != nil {
		close(e.firstStarted)
		select {
		case <-ctx.Done():
			return enrichResult{}, ctx.Err()
		case <-e.releaseFirst:
		}
	}
	if call == 1 && e.firstErr != nil {
		return enrichResult{}, e.firstErr
	}
	if call == 2 && e.secondStarted != nil {
		close(e.secondStarted)
		select {
		case <-ctx.Done():
			return enrichResult{}, ctx.Err()
		case <-e.releaseSecond:
		}
	}
	return enrichResult{TMDBID: movieID}, nil
}

func (*blockingRerunEnricher) NeedsEnrichment(
	context.Context,
	time.Time,
	int,
) ([]domain.EnrichmentCandidate, error) {
	return nil, nil
}

func TestEnrichRunner_EnqueueDuringProcessingCoalescesOneRerun(t *testing.T) {
	broker := newEventBroker()
	client, _ := broker.Subscribe()
	defer broker.Unsubscribe(client)

	r, stats := newBatchRunner(broker, 5*time.Second, 5*time.Second)
	enricher := &blockingRerunEnricher{
		firstStarted: make(chan struct{}),
		releaseFirst: make(chan struct{}),
		firstErr:     fmt.Errorf("stale response: %w", ErrEnrichSuperseded),
	}
	r.enricher = enricher

	const movieID = 7
	r.Enqueue(movieID)
	queuedID := <-r.queue
	done := make(chan enrichOutcome, 1)
	go func() {
		done <- r.process(context.Background(), queuedID)
	}()

	<-enricher.firstStarted
	r.Enqueue(movieID)
	r.Enqueue(movieID)
	close(enricher.releaseFirst)

	if got := <-done; got != outcomeEnriched {
		t.Fatalf("final outcome = %d, want enriched", got)
	}
	if got := enricher.calls.Load(); got != 2 {
		t.Fatalf("enrichment calls = %d, want one stale attempt plus one coalesced rerun", got)
	}

	r.flushBatch()
	select {
	case got := <-client:
		if got.Type != "movies:enriched-batch" {
			t.Fatalf("event = %q, want movies:enriched-batch", got.Type)
		}
	default:
		t.Fatal("successful rerun did not publish movies:enriched-batch")
	}
	select {
	case got := <-client:
		t.Fatalf("unexpected extra event %q", got.Type)
	default:
	}
	if got := stats.Load(); got != 1 {
		t.Fatalf("stats invalidations = %d, want only the successful rerun", got)
	}
}

func TestEnrichRunner_DuplicateEnqueueWhileQueuedDoesNotRerun(t *testing.T) {
	r, _ := newBatchRunner(nil, 5*time.Second, 5*time.Second)
	enricher := &blockingRerunEnricher{}
	r.enricher = enricher

	const movieID = 8
	r.Enqueue(movieID)
	r.Enqueue(movieID)

	if got := r.process(context.Background(), <-r.queue); got != outcomeEnriched {
		t.Fatalf("outcome = %d, want enriched", got)
	}
	if got := enricher.calls.Load(); got != 1 {
		t.Fatalf("enrichment calls = %d, want one queued attempt", got)
	}
	select {
	case duplicate := <-r.queue:
		t.Fatalf("duplicate queued movie %d", duplicate)
	default:
	}
	r.flushBatch()
}

func TestEnrichRunner_RerunDefersSuccessPublicationUntilLatestAttempt(t *testing.T) {
	broker := newEventBroker()
	client, _ := broker.Subscribe()
	defer broker.Unsubscribe(client)

	const debounce = 5 * time.Second
	r, stats := newBatchRunner(broker, debounce, time.Second)
	enricher := &blockingRerunEnricher{
		firstStarted:  make(chan struct{}),
		releaseFirst:  make(chan struct{}),
		secondStarted: make(chan struct{}),
		releaseSecond: make(chan struct{}),
	}
	r.enricher = enricher

	const movieID = 9
	r.Enqueue(movieID)
	done := make(chan enrichOutcome, 1)
	go func() {
		done <- r.process(context.Background(), <-r.queue)
	}()

	<-enricher.firstStarted
	r.Enqueue(movieID)
	close(enricher.releaseFirst)
	<-enricher.secondStarted

	// The first attempt succeeded, but a committed edit requested a newer pass.
	// It must not enter the batch while that newer pass is still unresolved.
	r.batchMu.Lock()
	pending := len(r.pending)
	r.batchMu.Unlock()
	if pending != 0 {
		t.Fatalf("stale attempt entered batch before rerun completed: %d pending", pending)
	}
	select {
	case got := <-client:
		t.Fatalf("stale attempt published event %q before rerun completed", got.Type)
	default:
	}
	if got := stats.Load(); got != 0 {
		t.Fatalf("stale attempt invalidated stats %d times before rerun completed", got)
	}

	close(enricher.releaseSecond)
	if got := <-done; got != outcomeEnriched {
		t.Fatalf("final outcome = %d, want enriched", got)
	}
	r.flushBatch()
	select {
	case got := <-client:
		if got.Type != "movies:enriched-batch" {
			t.Fatalf("event = %q, want movies:enriched-batch", got.Type)
		}
	default:
		t.Fatal("latest attempt did not publish movies:enriched-batch")
	}
	if got := stats.Load(); got != 1 {
		t.Fatalf("stats invalidations = %d, want one for the latest attempt", got)
	}
}

func TestEnrichRunner_SecondDirtyAttemptYieldsToQueuedMovie(t *testing.T) {
	r, _ := newBatchRunner(nil, 5*time.Second, 5*time.Second)
	enricher := &blockingRerunEnricher{
		firstStarted:  make(chan struct{}),
		releaseFirst:  make(chan struct{}),
		secondStarted: make(chan struct{}),
		releaseSecond: make(chan struct{}),
	}
	r.enricher = enricher

	const hotMovieID = 11
	const waitingMovieID = 12
	r.Enqueue(hotMovieID)
	done := make(chan enrichOutcome, 1)
	go func() {
		done <- r.process(context.Background(), <-r.queue)
	}()

	<-enricher.firstStarted
	r.Enqueue(hotMovieID)
	r.Enqueue(waitingMovieID)
	close(enricher.releaseFirst)
	<-enricher.secondStarted
	r.Enqueue(hotMovieID)
	close(enricher.releaseSecond)

	if got := <-done; got != outcomeSkipped {
		t.Fatalf("dirty second attempt outcome = %d, want skipped after requeue", got)
	}
	if got := <-r.queue; got != waitingMovieID {
		t.Fatalf("next queued movie = %d, want waiting movie %d", got, waitingMovieID)
	}
	if got := r.process(context.Background(), waitingMovieID); got != outcomeEnriched {
		t.Fatalf("waiting movie outcome = %d, want enriched", got)
	}
	if got := <-r.queue; got != hotMovieID {
		t.Fatalf("requeued movie = %d, want dirty movie %d", got, hotMovieID)
	}
	if got := r.process(context.Background(), hotMovieID); got != outcomeEnriched {
		t.Fatalf("requeued movie outcome = %d, want enriched", got)
	}
	if got := enricher.calls.Load(); got != 4 {
		t.Fatalf("enrichment calls = %d, want hot twice, waiting once, then hot once", got)
	}
	r.flushBatch()
}

func TestEnrichRunner_MissingMovieIsBenignSkip(t *testing.T) {
	broker := newEventBroker()
	client, _ := broker.Subscribe()
	defer broker.Unsubscribe(client)

	r, stats := newBatchRunner(broker, 5*time.Second, 5*time.Second)
	r.enricher = &blockingRerunEnricher{
		firstErr: fmt.Errorf("reload movie: %w", domain.ErrNotFound),
	}

	const movieID = 10
	r.Enqueue(movieID)
	if got := r.process(context.Background(), <-r.queue); got != outcomeSkipped {
		t.Fatalf("outcome = %d, want skipped", got)
	}
	r.flushBatch()
	select {
	case got := <-client:
		t.Fatalf("missing movie published event %q", got.Type)
	default:
	}
	if got := stats.Load(); got != 0 {
		t.Fatalf("missing movie invalidated stats %d times", got)
	}
}
