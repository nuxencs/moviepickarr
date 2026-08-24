package server

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"moviepickarr/internal/domain"
	"moviepickarr/internal/integration"

	"github.com/rs/zerolog"
)

func TestRateLimiter_CancelledWaitDoesNotDelayNextCaller(t *testing.T) {
	const interval = 400 * time.Millisecond
	limiter := newRateLimiter(interval)
	if err := limiter.wait(context.Background()); err != nil {
		t.Fatalf("first wait: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	waiting := make(chan error, 1)
	go func() { waiting <- limiter.wait(ctx) }()
	time.Sleep(25 * time.Millisecond)
	cancel()
	if err := <-waiting; !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled wait error = %v, want context canceled", err)
	}

	started := time.Now()
	if err := limiter.wait(context.Background()); err != nil {
		t.Fatalf("next wait: %v", err)
	}
	elapsed := time.Since(started)
	if elapsed > 600*time.Millisecond {
		t.Fatalf("next wait took %s, want at most the remainder of one 400ms interval", elapsed)
	}
}

func TestRateLimiter_PacesConcurrentCallers(t *testing.T) {
	const (
		callers  = 4
		interval = 30 * time.Millisecond
	)
	limiter := newRateLimiter(interval)
	start := make(chan struct{})
	type result struct {
		at  time.Time
		err error
	}
	completed := make(chan result, callers)
	for range callers {
		go func() {
			<-start
			err := limiter.wait(context.Background())
			completed <- result{at: time.Now(), err: err}
		}()
	}
	close(start)

	times := make([]time.Time, 0, callers)
	for range callers {
		result := <-completed
		if result.err != nil {
			t.Fatalf("wait: %v", result.err)
		}
		times = append(times, result.at)
	}
	slices.SortFunc(times, func(a, b time.Time) int { return a.Compare(b) })
	for index := 1; index < len(times); index++ {
		if gap := times[index].Sub(times[index-1]); gap < interval/2 {
			t.Fatalf("completion gap %d = %s, want paced calls", index, gap)
		}
	}
}

func TestRateLimiter_RejectsBurstBeyondPendingLimit(t *testing.T) {
	limiter := newRateLimiter(time.Hour)
	if err := limiter.wait(context.Background()); err != nil {
		t.Fatalf("prime limiter: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	const callers = rateLimiterMaxPending + 1
	start := make(chan struct{})
	results := make(chan error, callers)
	for range callers {
		go func() {
			<-start
			results <- limiter.wait(ctx)
		}()
	}
	close(start)

	var first error
	select {
	case first = <-results:
	case <-time.After(time.Second):
		cancel()
		t.Fatal("burst did not reject an excess waiter")
	}
	if _, ok := errors.AsType[*tmdbRequestQueueFullError](first); !ok {
		cancel()
		t.Fatalf("burst error = %v, want typed queue-full error", first)
	}

	cancel()
	for received := 1; received < callers; received++ {
		select {
		case err := <-results:
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("admitted waiter error = %v, want context canceled", err)
			}
		case <-time.After(time.Second):
			t.Fatal("admitted waiters did not leave after cancellation")
		}
	}
}

func TestRateLimiter_ReservedCallerWaitsInsteadOfRejectingFullQueue(t *testing.T) {
	limiter := newRateLimiter(time.Hour)
	if err := limiter.wait(context.Background()); err != nil {
		t.Fatalf("prime limiter: %v", err)
	}

	searchCtx := t.Context()
	for range rateLimiterMaxPending {
		go func() { _ = limiter.wait(searchCtx) }()
	}
	deadline := time.Now().Add(time.Second)
	for len(limiter.pending) != cap(limiter.pending) && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if len(limiter.pending) != cap(limiter.pending) {
		t.Fatalf("pending = %d, want full queue %d", len(limiter.pending), cap(limiter.pending))
	}

	reservedCtx, cancelReserved := context.WithCancel(context.Background())
	reserved := make(chan error, 1)
	go func() { reserved <- limiter.waitReserved(reservedCtx) }()
	select {
	case err := <-reserved:
		t.Fatalf("reserved caller returned early with %v", err)
	case <-time.After(25 * time.Millisecond):
	}
	cancelReserved()
	if err := <-reserved; !errors.Is(err, context.Canceled) {
		t.Fatalf("reserved caller error = %v, want context canceled", err)
	}
}

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

func TestEnrichRunner_StartupLogOmitsInactiveDrainConfiguration(t *testing.T) {
	t.Parallel()
	var logs bytes.Buffer
	cfg := defaultEnrichConfig()
	cfg.RefreshInterval = 0
	r := newEnrichRunner(nil, nil, cfg, zerolog.New(&logs))
	r.initialDrain = false

	r.Start(context.Background())
	r.Stop()

	logged := logs.String()
	for _, inactive := range []string{`"rate":`, `"retries":`, `"batch":`, `"cast":`, `"ttl":`, `"refresh":`} {
		if strings.Contains(logged, inactive) {
			t.Fatalf("startup log includes inactive field %s: %s", inactive, logged)
		}
	}
	for _, active := range []string{`"queue_capacity":256`, `"batch_debounce":"500ms"`, `"batch_max_wait":"2s"`} {
		if !strings.Contains(logged, active) {
			t.Fatalf("startup log omits active field %s: %s", active, logged)
		}
	}
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

type timeoutThenSuccessEnricher struct {
	calls atomic.Int32
}

func (e *timeoutThenSuccessEnricher) EnrichOne(_ context.Context, movieID int) (enrichResult, error) {
	if e.calls.Add(1) == 1 {
		return enrichResult{}, fmt.Errorf("tmdb request: %w", context.DeadlineExceeded)
	}
	return enrichResult{TMDBID: movieID}, nil
}

func (*timeoutThenSuccessEnricher) NeedsEnrichment(
	context.Context,
	time.Time,
	int,
) ([]domain.EnrichmentCandidate, error) {
	return []domain.EnrichmentCandidate{{MovieID: 1}, {MovieID: 2}}, nil
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

type triggerRecordingEnricher struct {
	triggers []integration.RunTrigger
}

func (e *triggerRecordingEnricher) EnrichOne(context.Context, int) (enrichResult, error) {
	return enrichResult{}, errors.New("trigger was not forwarded")
}

func (e *triggerRecordingEnricher) EnrichOneWithTrigger(
	_ context.Context,
	movieID int,
	trigger integration.RunTrigger,
) (enrichResult, error) {
	e.triggers = append(e.triggers, trigger)
	return enrichResult{TMDBID: movieID}, nil
}

func (*triggerRecordingEnricher) NeedsEnrichment(
	context.Context,
	time.Time,
	int,
) ([]domain.EnrichmentCandidate, error) {
	return nil, nil
}

func TestEnrichRunner_ForwardsMovieUpdateTrigger(t *testing.T) {
	t.Parallel()
	enricher := &triggerRecordingEnricher{}
	r := newEnrichRunner(enricher, nil, defaultEnrichConfig(), zerolog.Nop())

	// The edit can arrive before an earlier add has left the queue. The worker
	// enriches the latest identity, so the ledger must retain the edit trigger.
	r.Enqueue(17)
	r.EnqueueWithTrigger(17, integration.RunTriggerMovieUpdated)
	if got := r.process(context.Background(), <-r.queue); got != outcomeEnriched {
		t.Fatalf("outcome = %d, want enriched", got)
	}
	if !slices.Equal(enricher.triggers, []integration.RunTrigger{integration.RunTriggerMovieUpdated}) {
		t.Fatalf("triggers = %v, want movie update", enricher.triggers)
	}
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

func TestEnrichRunner_DrainContinuesAfterRequestTimeout(t *testing.T) {
	var logs bytes.Buffer
	enricher := &timeoutThenSuccessEnricher{}
	r := newEnrichRunner(enricher, nil, defaultEnrichConfig(), zerolog.New(&logs))

	r.drain(context.Background())

	if got := enricher.calls.Load(); got != 2 {
		t.Fatalf("enrichment calls = %d, want timeout followed by next candidate", got)
	}
	logged := logs.String()
	if !strings.Contains(logged, `"message":"movie enrichment failed"`) {
		t.Fatalf("request timeout was not logged as an enrichment failure: %s", logged)
	}
	if strings.Contains(logged, "drain interrupted by shutdown") {
		t.Fatalf("request timeout was logged as shutdown: %s", logged)
	}
}
