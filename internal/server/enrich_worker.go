package server

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"moviepickarr/internal/domain"
	"moviepickarr/internal/integration"

	"github.com/rs/zerolog"
	zlog "github.com/rs/zerolog/log"
)

// enrichConfig tunes the background enrichment worker. All fields are
// overridable via environment variables (see loadEnrichConfig).
type enrichConfig struct {
	MinInterval     time.Duration // min gap between TMDB requests
	MaxRetries      int           // retry attempts per request
	BackoffBase     time.Duration // base delay for exponential backoff
	QueueSize       int           // buffered auto-on-add queue capacity
	BatchLimit      int           // max candidates fetched per drain
	CastLimit       int           // cast rows kept per movie at ingest (0 = full cast)
	RefreshInterval time.Duration // periodic stale-scan cadence (0 disables)
	TTL             time.Duration // rows older than now-TTL are re-enriched

	// Enriched-broadcast coalescing. A drain enriches many movies back-to-back;
	// emitting one SSE event per movie makes the frontend invalidate-refetch
	// every list per movie. Instead, ids accumulate and a single
	// "movies:enriched-batch" fires once the burst goes quiet for BatchDebounce,
	// with BatchMaxWait as a ceiling so a long backfill still shows periodic
	// progress rather than one delayed dump at drain end.
	BatchDebounce time.Duration
	BatchMaxWait  time.Duration
}

func defaultEnrichConfig() enrichConfig {
	return enrichConfig{
		MinInterval:     250 * time.Millisecond, // ~4 req/s, polite to TMDB
		MaxRetries:      4,
		BackoffBase:     500 * time.Millisecond,
		QueueSize:       256,
		BatchLimit:      200,
		CastLimit:       15, // top billing only; deepening just needs a re-enrich
		RefreshInterval: time.Hour,
		TTL:             720 * time.Hour, // 30 days
		BatchDebounce:   500 * time.Millisecond,
		BatchMaxWait:    2 * time.Second,
	}
}

func loadEnrichConfig() enrichConfig {
	cfg := defaultEnrichConfig()
	if v, ok := envDurationMS("TMDB_ENRICH_MIN_INTERVAL_MS"); ok {
		cfg.MinInterval = v
	}
	if v, ok := envInt("TMDB_ENRICH_MAX_RETRIES"); ok && v >= 0 {
		cfg.MaxRetries = v
	}
	if v, ok := envDurationMS("TMDB_ENRICH_BACKOFF_MS"); ok {
		cfg.BackoffBase = v
	}
	if v, ok := envInt("TMDB_ENRICH_QUEUE_SIZE"); ok && v > 0 {
		cfg.QueueSize = v
	}
	if v, ok := envInt("TMDB_ENRICH_BATCH_LIMIT"); ok && v > 0 {
		cfg.BatchLimit = v
	}
	if v, ok := envInt("TMDB_ENRICH_CAST_LIMIT"); ok && v >= 0 {
		cfg.CastLimit = v // 0 = full cast; negatives keep the default
	}
	if v, ok := envDuration("TMDB_ENRICH_REFRESH_INTERVAL"); ok {
		cfg.RefreshInterval = v
	}
	if v, ok := envDuration("TMDB_ENRICH_TTL"); ok && v > 0 {
		cfg.TTL = v
	}
	if v, ok := envDurationMS("TMDB_ENRICH_BATCH_DEBOUNCE_MS"); ok {
		cfg.BatchDebounce = v
	}
	if v, ok := envDurationMS("TMDB_ENRICH_BATCH_MAX_WAIT_MS"); ok {
		cfg.BatchMaxWait = v
	}
	return cfg
}

func envInt(key string) (int, bool) {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return 0, false
	}
	v, err := strconv.Atoi(raw)
	if err != nil {
		zlog.Warn().Str("component", "enrich").Str("key", key).Str("value", raw).
			Msg("env value is not an integer, using default")
		return 0, false
	}
	return v, true
}

func envDurationMS(key string) (time.Duration, bool) {
	v, ok := envInt(key)
	if !ok || v < 0 {
		return 0, false
	}
	return time.Duration(v) * time.Millisecond, true
}

func envDuration(key string) (time.Duration, bool) {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return 0, false
	}
	d, err := time.ParseDuration(raw)
	if err != nil || d < 0 {
		zlog.Warn().Str("component", "enrich").Str("key", key).Str("value", raw).
			Msg("env value is not a non-negative duration, using default")
		return 0, false
	}
	return d, true
}

// rateLimiter enforces a minimum interval between calls. Safe for concurrent
// use; for the single worker it simply paces successive requests.
const rateLimiterMaxPending = 32

type tmdbRequestQueueFullError struct {
	Limit int
}

func (e *tmdbRequestQueueFullError) Error() string {
	return fmt.Sprintf("tmdb request queue is full (%d pending)", e.Limit)
}

type rateLimiter struct {
	interval time.Duration
	next     time.Time
	turn     chan struct{}
	pending  chan struct{}
}

func newRateLimiter(interval time.Duration) *rateLimiter {
	turn := make(chan struct{}, 1)
	turn <- struct{}{}
	return &rateLimiter{
		interval: interval,
		turn:     turn,
		pending:  make(chan struct{}, rateLimiterMaxPending),
	}
}

func (r *rateLimiter) wait(ctx context.Context) error {
	return r.waitWithAdmission(ctx, true)
}

// waitReserved admits bounded internal workers even when interactive callers
// fill the public queue. Internal concurrency is fixed by the worker and run
// controller, so waiting here cannot create an unbounded goroutine set.
func (r *rateLimiter) waitReserved(ctx context.Context) error {
	return r.waitWithAdmission(ctx, false)
}

func (r *rateLimiter) waitWithAdmission(ctx context.Context, rejectWhenFull bool) error {
	if r == nil || r.interval <= 0 {
		return ctx.Err()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if rejectWhenFull {
		select {
		case r.pending <- struct{}{}:
		default:
			return &tmdbRequestQueueFullError{Limit: cap(r.pending)}
		}
	} else {
		select {
		case r.pending <- struct{}{}:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	defer func() { <-r.pending }()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-r.turn:
	}
	defer func() { r.turn <- struct{}{} }()

	now := time.Now()
	wait := r.next.Sub(now)
	if wait > 0 {
		timer := time.NewTimer(wait)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	r.next = time.Now().Add(r.interval)
	return nil
}

// enrichRunner owns the single background goroutine that enriches movies.
// Auto-on-add ids arrive on queue; backfill and the periodic refresh are
// triggered as a "drain" that pulls candidates and processes them inline —
// so a large backlog can never overflow the bounded queue.
type enrichRunner struct {
	enricher   Enricher
	broker     *eventBroker // optional SSE; nil-safe
	onEnriched func()       // optional post-enrich hook (stats-cache invalidation); nil-safe
	cfg        enrichConfig
	log        zerolog.Logger
	// Library-wide work is owned by tmdbRunController in production. Legacy
	// worker tests and direct construction keep the initial drain enabled.
	initialDrain bool

	queue    chan int
	trigger  chan struct{}
	inflight map[int]enrichClaim
	mu       sync.Mutex // guards inflight only

	// batch coalesces freshly-enriched ids into one SSE broadcast. batchMu guards
	// these because the debounce timer fires on its own goroutine; flushBatch does
	// the broker/onEnriched work outside the lock.
	batchMu       sync.Mutex
	pending       []int
	flushTimer    *time.Timer
	batchDeadline time.Time

	wg       sync.WaitGroup
	stopOnce sync.Once
	cancel   context.CancelFunc
}

type enrichClaim struct {
	processing   bool
	trigger      integration.RunTrigger
	rerun        bool
	rerunTrigger integration.RunTrigger
}

type triggerAwareEnricher interface {
	EnrichOneWithTrigger(
		ctx context.Context,
		movieID int,
		trigger integration.RunTrigger,
	) (enrichResult, error)
}

func newEnrichRunner(enricher Enricher, broker *eventBroker, cfg enrichConfig, log zerolog.Logger) *enrichRunner {
	return &enrichRunner{
		enricher:     enricher,
		broker:       broker,
		cfg:          cfg,
		log:          log,
		queue:        make(chan int, cfg.QueueSize),
		trigger:      make(chan struct{}, 1),
		inflight:     make(map[int]enrichClaim),
		initialDrain: true,
	}
}

func (r *enrichRunner) Start(ctx context.Context) {
	stopCtx, cancel := context.WithCancel(ctx)
	r.cancel = cancel

	startup := r.log.Info().
		Int("queue_capacity", cap(r.queue)).
		Str("batch_debounce", r.cfg.BatchDebounce.String()).
		Str("batch_max_wait", r.cfg.BatchMaxWait.String())
	if r.initialDrain || r.cfg.RefreshInterval > 0 {
		refresh := r.cfg.RefreshInterval.String()
		if r.cfg.RefreshInterval <= 0 {
			refresh = "disabled"
		}
		cast := strconv.Itoa(r.cfg.CastLimit)
		if r.cfg.CastLimit <= 0 {
			cast = "unlimited"
		}
		startup.
			Str("rate", r.cfg.MinInterval.String()).
			Int("retries", r.cfg.MaxRetries).
			Int("batch", r.cfg.BatchLimit).
			Str("cast", cast).
			Str("ttl", r.cfg.TTL.String()).
			Str("refresh", refresh)
	}
	startup.Msg("worker started")

	r.wg.Add(1)
	go r.consume(stopCtx)

	if r.cfg.RefreshInterval > 0 {
		r.wg.Add(1)
		go r.scheduleLoop(stopCtx)
	}

	if r.initialDrain {
		// Kick the initial backfill of un-enriched (and any already-stale) rows.
		r.triggerDrain()
	}
}

func (r *enrichRunner) Stop() {
	r.stopOnce.Do(func() {
		if r.cancel != nil {
			r.cancel()
		}
		r.wg.Wait()
		r.flushBatch() // emit any final partial batch and stop the debounce timer
	})
}

// Enqueue schedules a single movie for enrichment (auto-on-add). Non-blocking:
// if the queue is full the id is dropped and the next scheduled drain re-selects
// it. Dedup avoids queuing an id that is already pending.
func (r *enrichRunner) Enqueue(movieID int) {
	r.EnqueueWithTrigger(movieID, integration.RunTriggerMovieAdded)
}

// EnqueueWithTrigger schedules single-movie enrichment while preserving the
// event that caused it for the integration run ledger.
func (r *enrichRunner) EnqueueWithTrigger(movieID int, trigger integration.RunTrigger) {
	if !r.tryEnqueue(movieID, trigger) {
		return
	}

	select {
	case r.queue <- movieID:
	default:
		r.clearInflight(movieID)
		r.log.Warn().Int("movie_id", movieID).Msg("queue full, dropped newly added movie; the next scan re-selects it")
	}
}

// tryEnqueue claims a new queued movie. A duplicate for a queued movie is
// already covered by its future read. A duplicate for a processing movie means
// committed state changed after that read, so remember one coalesced rerun.
func (r *enrichRunner) tryEnqueue(movieID int, trigger integration.RunTrigger) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	claim, ok := r.inflight[movieID]
	if ok {
		if claim.processing {
			claim.rerun = true
			claim.rerunTrigger = trigger
		} else {
			claim.trigger = trigger
		}
		r.inflight[movieID] = claim
		return false
	}
	r.inflight[movieID] = enrichClaim{trigger: trigger}
	return true
}

// tryClaim marks a drain candidate in-flight, reporting false if it was already
// queued or processing. A drain collision is deduplication, not evidence of a
// newer edit, so it does not request a rerun.
func (r *enrichRunner) tryClaim(movieID int) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.inflight[movieID]; ok {
		return false
	}
	r.inflight[movieID] = enrichClaim{}
	return true
}

func (r *enrichRunner) markProcessing(movieID int) integration.RunTrigger {
	r.mu.Lock()
	claim := r.inflight[movieID]
	claim.processing = true
	r.inflight[movieID] = claim
	r.mu.Unlock()
	return claim.trigger
}

// finishAttempt atomically either consumes one requested rerun while retaining
// the claim, or releases the claim. Enqueue therefore lands on exactly one side
// of the boundary: it marks this process dirty or creates a fresh queued claim.
type attemptFinish int

const (
	attemptComplete attemptFinish = iota
	attemptRerunInline
	attemptRerunQueued
)

func (r *enrichRunner) finishAttempt(
	movieID int,
	allowRerun bool,
	inlineRerunAvailable bool,
) attemptFinish {
	r.mu.Lock()
	defer r.mu.Unlock()

	claim, ok := r.inflight[movieID]
	if !ok {
		return attemptComplete
	}
	if allowRerun && claim.rerun {
		claim.processing = false
		claim.rerun = false
		claim.trigger = claim.rerunTrigger
		claim.rerunTrigger = ""
		r.inflight[movieID] = claim
		if inlineRerunAvailable {
			return attemptRerunInline
		}
		return attemptRerunQueued
	}
	delete(r.inflight, movieID)
	return attemptComplete
}

func (r *enrichRunner) clearInflight(movieID int) {
	r.mu.Lock()
	delete(r.inflight, movieID)
	r.mu.Unlock()
}

func (r *enrichRunner) triggerDrain() {
	select {
	case r.trigger <- struct{}{}:
	default: // a drain is already pending; coalesce
	}
}

func (r *enrichRunner) consume(ctx context.Context) {
	defer r.wg.Done()
	for {
		select {
		case <-ctx.Done():
			return
		case <-r.trigger:
			r.drain(ctx)
		case id := <-r.queue:
			r.process(ctx, id)
		}
	}
}

func (r *enrichRunner) scheduleLoop(ctx context.Context) {
	defer r.wg.Done()
	ticker := time.NewTicker(r.cfg.RefreshInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.triggerDrain()
		}
	}
}

// drain pulls every movie that needs enrichment (never-enriched OR stale) and
// processes them synchronously. One query per drain (capped at BatchLimit);
// failed rows simply reappear on the next scheduled drain — no re-query loop,
// so a movie that can't be matched never spins the worker.
func (r *enrichRunner) drain(ctx context.Context) {
	staleBefore := time.Now().Add(-r.cfg.TTL)
	candidates, err := r.enricher.NeedsEnrichment(ctx, staleBefore, r.cfg.BatchLimit)
	if err != nil {
		if ctx.Err() == nil {
			r.log.Error().Err(err).Msg("needs-enrichment query failed")
		}
		return
	}
	if len(candidates) == 0 {
		return
	}

	r.log.Info().Int("count", len(candidates)).Msg("drain started")
	var enriched, skipped, failed int
	for _, c := range candidates {
		// Skip a candidate already claimed by the auto-on-add queue — consume
		// will process it once. Claiming here also blocks a concurrent Enqueue
		// of the same id from double-processing while this drain runs it.
		if !r.tryClaim(c.MovieID) {
			skipped++
			continue
		}
		switch r.process(ctx, c.MovieID) {
		case outcomeEnriched:
			enriched++
		case outcomeSkipped:
			skipped++
		case outcomeFailed:
			failed++
		case outcomeCanceled:
			r.log.Warn().
				Int("done", enriched+skipped+failed).
				Int("total", len(candidates)).
				Int("enriched", enriched).
				Int("skipped", skipped).
				Int("failed", failed).
				Msg("drain interrupted by shutdown")
			return
		}
	}
	r.flushBatch() // emit the tail of this drain's batch immediately
	r.log.Info().
		Int("enriched", enriched).
		Int("skipped", skipped).
		Int("failed", failed).
		Msg("drain complete")
}

type enrichOutcome int

const (
	outcomeEnriched enrichOutcome = iota
	outcomeSkipped
	outcomeFailed
	outcomeCanceled
)

func (r *enrichRunner) process(ctx context.Context, movieID int) enrichOutcome {
	inlineRerunAvailable := true
	for {
		trigger := r.markProcessing(movieID)
		res, outcome, err := r.processAttempt(ctx, movieID, trigger)
		switch r.finishAttempt(movieID, outcome != outcomeCanceled, inlineRerunAvailable) {
		case attemptRerunInline:
			inlineRerunAvailable = false
			continue
		case attemptRerunQueued:
			select {
			case r.queue <- movieID:
			default:
				r.clearInflight(movieID)
				r.log.Warn().
					Int("movie_id", movieID).
					Msg("queue full, dropped re-dirtied movie; the next scan re-selects it")
			}
			return outcomeSkipped
		case attemptComplete:
			r.publishAttempt(movieID, res, err, outcome)
			return outcome
		}
	}
}

func (r *enrichRunner) processAttempt(
	ctx context.Context,
	movieID int,
	trigger integration.RunTrigger,
) (enrichResult, enrichOutcome, error) {
	var (
		res enrichResult
		err error
	)
	if triggered, ok := r.enricher.(triggerAwareEnricher); ok && trigger != "" {
		res, err = triggered.EnrichOneWithTrigger(ctx, movieID, trigger)
	} else {
		res, err = r.enricher.EnrichOne(ctx, movieID)
	}
	switch {
	case err == nil:
		return res, outcomeEnriched, nil
	case ctx.Err() != nil:
		return enrichResult{}, outcomeCanceled, err
	case errors.Is(err, ErrEnrichNoIMDbID) ||
		errors.Is(err, ErrEnrichNotFound) ||
		errors.Is(err, ErrEnrichSuperseded) ||
		errors.Is(err, domain.ErrNotFound):
		return enrichResult{}, outcomeSkipped, err
	default:
		return enrichResult{}, outcomeFailed, err
	}
}

func (r *enrichRunner) publishAttempt(
	movieID int,
	res enrichResult,
	err error,
	outcome enrichOutcome,
) {
	switch outcome {
	case outcomeEnriched:
		r.log.Debug().
			Int("movie_id", movieID).
			Int("tmdb_id", res.TMDBID).
			Int("genres", res.Genres).
			Int("credits", res.Credits).
			Msg("movie enriched")
		r.recordEnriched(movieID)
	case outcomeSkipped:
		r.log.Debug().Int("movie_id", movieID).Err(err).Msg("movie enrichment skipped")
	case outcomeFailed:
		r.log.Warn().Int("movie_id", movieID).Err(err).Msg("movie enrichment failed")
	}
}

func (r *enrichRunner) notifyEnriched() {
	if r.onEnriched == nil {
		return
	}
	r.onEnriched()
}

// recordEnriched buffers a freshly-enriched movie id and (re)arms the debounce
// timer. Enrichments within BatchDebounce of each other coalesce into one flush;
// BatchMaxWait caps how long the first id in a batch waits, so a long backfill
// still emits periodic progress instead of one delayed dump at drain end.
func (r *enrichRunner) recordEnriched(movieID int) {
	r.batchMu.Lock()
	defer r.batchMu.Unlock()

	r.pending = append(r.pending, movieID)
	if r.flushTimer == nil {
		r.batchDeadline = time.Now().Add(r.cfg.BatchMaxWait)
		r.flushTimer = time.AfterFunc(r.cfg.BatchDebounce, r.flushBatch)
		return
	}

	d := r.cfg.BatchDebounce
	if rem := time.Until(r.batchDeadline); rem < d {
		d = rem
	}
	if d < 0 {
		d = 0
	}
	r.flushTimer.Reset(d)
}

// flushBatch emits one coalesced enrichment signal for the buffered burst: it
// invalidates the stats cache once (onEnriched) and broadcasts a single
// "movies:enriched-batch" SSE event. A no-op when nothing is pending, so it is
// safe to call from the drain tail, the debounce timer, and Stop(). The broker
// and onEnriched work runs outside the lock.
func (r *enrichRunner) flushBatch() {
	r.batchMu.Lock()
	if r.flushTimer != nil {
		r.flushTimer.Stop()
		r.flushTimer = nil
	}
	n := len(r.pending)
	r.pending = nil
	r.batchMu.Unlock()

	if n == 0 {
		return
	}
	r.notifyEnriched()
	r.broadcastEnrichedBatch(n)
}

func (r *enrichRunner) broadcastEnrichedBatch(n int) {
	if r.broker == nil {
		return
	}
	r.log.Debug().Int("count", n).Str("event", "movies:enriched-batch").Msg("flushed enriched batch")
	r.broker.Broadcast(event{Type: "movies:enriched-batch"})
}
