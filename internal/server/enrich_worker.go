package server

import (
	"context"
	"errors"
	"log"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gofiber/fiber/v2"
)

// enrichConfig tunes the background enrichment worker. All fields are
// overridable via environment variables (see loadEnrichConfig).
type enrichConfig struct {
	MinInterval     time.Duration // min gap between TMDB requests
	MaxRetries      int           // retry attempts per request
	BackoffBase     time.Duration // base delay for exponential backoff
	QueueSize       int           // buffered auto-on-add queue capacity
	BatchLimit      int           // max candidates fetched per drain
	RefreshInterval time.Duration // periodic stale-scan cadence (0 disables)
	TTL             time.Duration // rows older than now-TTL are re-enriched
}

func defaultEnrichConfig() enrichConfig {
	return enrichConfig{
		MinInterval:     250 * time.Millisecond, // ~4 req/s, polite to TMDB
		MaxRetries:      4,
		BackoffBase:     500 * time.Millisecond,
		QueueSize:       256,
		BatchLimit:      200,
		RefreshInterval: time.Hour,
		TTL:             720 * time.Hour, // 30 days
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
	if v, ok := envDuration("TMDB_ENRICH_REFRESH_INTERVAL"); ok {
		cfg.RefreshInterval = v
	}
	if v, ok := envDuration("TMDB_ENRICH_TTL"); ok && v > 0 {
		cfg.TTL = v
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
		log.Printf("enrich: invalid %s=%q, using default", key, raw)
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
		log.Printf("enrich: invalid %s=%q, using default", key, raw)
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
	enricher Enricher
	broker   *eventBroker // optional SSE; nil-safe
	cfg      enrichConfig

	queue    chan int
	trigger  chan struct{}
	inflight map[int]struct{}
	mu       sync.Mutex // guards inflight only

	wg       sync.WaitGroup
	stopOnce sync.Once
	cancel   context.CancelFunc
}

func newEnrichRunner(enricher Enricher, broker *eventBroker, cfg enrichConfig) *enrichRunner {
	return &enrichRunner{
		enricher: enricher,
		broker:   broker,
		cfg:      cfg,
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
	log.Printf("enrich: worker started (rate=%s, retries=%d, batch=%d, ttl=%s, refresh=%s)",
		r.cfg.MinInterval, r.cfg.MaxRetries, r.cfg.BatchLimit, r.cfg.TTL, refresh)

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
	})
}

// Enqueue schedules a single movie for enrichment (auto-on-add). Non-blocking:
// if the queue is full the id is dropped and the next scheduled drain re-picks
// it up. Dedup avoids queuing an id that is already pending.
func (r *enrichRunner) Enqueue(movieID int) {
	r.mu.Lock()
	if _, ok := r.inflight[movieID]; ok {
		r.mu.Unlock()
		return
	}
	r.inflight[movieID] = struct{}{}
	r.mu.Unlock()

	select {
	case r.queue <- movieID:
	default:
		r.clearInflight(movieID)
		log.Printf("enrich: queue full, dropped movie %d (will retry on next scan)", movieID)
	}
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
			log.Printf("enrich: needs-enrichment query failed: %v", err)
		}
		return
	}
	if len(candidates) == 0 {
		return
	}

	log.Printf("enrich: draining %d movie(s)", len(candidates))
	var enriched, skipped, failed int
	for _, c := range candidates {
		switch r.process(ctx, c.MovieID) {
		case outcomeEnriched:
			enriched++
		case outcomeSkipped:
			skipped++
		case outcomeFailed:
			failed++
		case outcomeCanceled:
			log.Printf("enrich: drain interrupted — %d/%d done (%d enriched, %d skipped, %d failed)",
				enriched+skipped+failed, len(candidates), enriched, skipped, failed)
			return
		}
	}
	log.Printf("enrich: drain complete — %d enriched, %d skipped, %d failed", enriched, skipped, failed)
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
		log.Printf("enrich: movie %d enriched → tmdb %d (%d genres)", movieID, res.TMDBID, res.Genres)
		r.broadcastEnriched(movieID)
		return outcomeEnriched
	case errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded):
		return outcomeCanceled
	case errors.Is(err, ErrEnrichNoIMDbID) || errors.Is(err, ErrEnrichNotFound):
		log.Printf("enrich: skip movie %d: %v", movieID, err)
		return outcomeSkipped
	default:
		log.Printf("enrich: movie %d failed: %v", movieID, err)
		return outcomeFailed
	}
}

func (r *enrichRunner) broadcastEnriched(movieID int) {
	if r.broker == nil {
		return
	}
	r.broker.Broadcast(event{Type: "movie:enriched", Data: fiber.Map{"id": movieID}})
}
