package server

import (
	"context"
	"errors"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

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
		zlog.Warn().Str("key", key).Str("value", raw).Msg("enrich: invalid env value, using default")
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
		zlog.Warn().Str("key", key).Str("value", raw).Msg("enrich: invalid env value, using default")
		return 0, false
	}
	return d, true
}

// rateLimiter enforces a minimum interval between calls. Safe for concurrent
// use; for the single worker it simply paces successive requests.
type rateLimiter struct {
	mu       sync.Mutex
	interval time.Duration
	next     time.Time
}

func newRateLimiter(interval time.Duration) *rateLimiter {
	return &rateLimiter{interval: interval}
}

func (r *rateLimiter) wait(ctx context.Context) error {
	if r == nil || r.interval <= 0 {
		return ctx.Err()
	}

	r.mu.Lock()
	now := time.Now()
	if r.next.Before(now) {
		r.next = now
	}
	wait := r.next.Sub(now)
	r.next = r.next.Add(r.interval)
	r.mu.Unlock()

	if wait <= 0 {
		return ctx.Err()
	}

	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
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

	queue    chan int
	trigger  chan struct{}
	inflight map[int]struct{}
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

func newEnrichRunner(enricher Enricher, broker *eventBroker, cfg enrichConfig, log zerolog.Logger) *enrichRunner {
	return &enrichRunner{
		enricher: enricher,
		broker:   broker,
		cfg:      cfg,
		log:      log,
		queue:    make(chan int, cfg.QueueSize),
		trigger:  make(chan struct{}, 1),
		inflight: make(map[int]struct{}),
	}
}

func (r *enrichRunner) Start(ctx context.Context) {
	stopCtx, cancel := context.WithCancel(ctx)
	r.cancel = cancel

	refresh := r.cfg.RefreshInterval.String()
	if r.cfg.RefreshInterval <= 0 {
		refresh = "disabled"
	}
	cast := strconv.Itoa(r.cfg.CastLimit)
	if r.cfg.CastLimit <= 0 {
		cast = "unlimited"
	}
	// rate/ttl are logged as strings (e.g. "250ms", "720h0m0s") to match the
	// already-stringified cast/refresh — this line is a human-facing config echo,
	// not a metric, so readable units beat zerolog's raw-millisecond numbers.
	r.log.Info().
		Str("rate", r.cfg.MinInterval.String()).
		Int("retries", r.cfg.MaxRetries).
		Int("batch", r.cfg.BatchLimit).
		Str("cast", cast).
		Str("ttl", r.cfg.TTL.String()).
		Str("refresh", refresh).
		Msg("enrich worker started")

	r.wg.Add(1)
	go r.consume(stopCtx)

	if r.cfg.RefreshInterval > 0 {
		r.wg.Add(1)
		go r.scheduleLoop(stopCtx)
	}

	// Kick the initial backfill of un-enriched (and any already-stale) rows.
	r.triggerDrain()
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
	if !r.tryClaim(movieID) {
		return
	}

	select {
	case r.queue <- movieID:
	default:
		r.clearInflight(movieID)
		r.log.Warn().Int("movieID", movieID).Msg("enrich queue full, dropped movie (will retry on next scan)")
	}
}

// tryClaim marks a movie in-flight, reporting false if it was already claimed
// (queued via Enqueue or being drained). It's the single dedup gate shared by
// the auto-on-add queue and the backfill drain, so a just-added movie that is
// both queued and a drain candidate is enriched once, not twice. process()
// always releases the claim via clearInflight in its defer.
func (r *enrichRunner) tryClaim(movieID int) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.inflight[movieID]; ok {
		return false
	}
	r.inflight[movieID] = struct{}{}
	return true
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
			r.log.Error().Err(err).Msg("enrich needs-enrichment query failed")
		}
		return
	}
	if len(candidates) == 0 {
		return
	}

	r.log.Info().Int("count", len(candidates)).Msg("enrich draining")
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
				Msg("enrich drain interrupted")
			return
		}
	}
	r.flushBatch() // emit the tail of this drain's batch immediately
	r.log.Info().
		Int("enriched", enriched).
		Int("skipped", skipped).
		Int("failed", failed).
		Msg("enrich drain complete")
}

type enrichOutcome int

const (
	outcomeEnriched enrichOutcome = iota
	outcomeSkipped
	outcomeFailed
	outcomeCanceled
)

func (r *enrichRunner) process(ctx context.Context, movieID int) enrichOutcome {
	defer r.clearInflight(movieID)

	res, err := r.enricher.EnrichOne(ctx, movieID)
	switch {
	case err == nil:
		r.log.Debug().
			Int("movieID", movieID).
			Int("tmdbID", res.TMDBID).
			Int("genres", res.Genres).
			Int("credits", res.Credits).
			Msg("enrich movie enriched")
		r.recordEnriched(movieID)
		return outcomeEnriched
	case errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded):
		return outcomeCanceled
	case errors.Is(err, ErrEnrichNoIMDbID) || errors.Is(err, ErrEnrichNotFound):
		r.log.Debug().Int("movieID", movieID).Err(err).Msg("enrich skip movie")
		return outcomeSkipped
	default:
		r.log.Warn().Int("movieID", movieID).Err(err).Msg("enrich movie failed")
		return outcomeFailed
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
	r.log.Debug().Int("count", n).Str("event", "movies:enriched-batch").Msg("enrich flushed batch")
	r.broker.Broadcast(event{Type: "movies:enriched-batch"})
}
