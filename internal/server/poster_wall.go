package server

import (
	"context"
	"sync"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/rs/zerolog"
)

const (
	// posterWallMax caps how many poster paths the wall holds. One /discover page
	// is 20 results, and the login panel never renders more than a handful.
	posterWallMax = 20
	// posterWallRefreshInterval re-warms the wall on a long cadence: the popular
	// list barely moves day to day, so a weekly refresh keeps it from going stale
	// over a long-running deploy without hammering TMDB.
	posterWallRefreshInterval = 7 * 24 * time.Hour
)

// posterFetch fetches the current popular poster paths. It is an injectable seam
// so the cache's warm/refresh behavior can be driven deterministically in tests
// without a live TMDB or real sleeps.
type posterFetch func(ctx context.Context) ([]string, error)

// posterWallCache holds the poster paths the public /auth/poster-wall endpoint
// serves. It is warmed once on startup and refreshed on a cadence in a background
// goroutine, so no request ever blocks on TMDB. A failed warm or refresh keeps
// the last good list rather than clearing it, so a transient TMDB outage never
// blanks the panel. Reads return the current slice (empty until the first warm
// lands).
type posterWallCache struct {
	fetch   posterFetch
	refresh time.Duration
	log     zerolog.Logger

	mu      sync.RWMutex
	current []string

	cancel   context.CancelFunc
	trigger  chan struct{}
	wg       sync.WaitGroup
	stopOnce sync.Once
}

func newPosterWallCache(fetch posterFetch, refresh time.Duration, log zerolog.Logger) *posterWallCache {
	return &posterWallCache{fetch: fetch, refresh: refresh, log: log, trigger: make(chan struct{}, 1)}
}

func (c *posterWallCache) Refresh() {
	if c == nil {
		return
	}
	select {
	case c.trigger <- struct{}{}:
	default:
	}
}

// list returns a copy of the cached poster paths, never nil, so the endpoint
// serializes a clean JSON [] while the cache is still unwarmed. The copy keeps a
// caller from mutating the slice a concurrent refresh may be swapping under.
func (c *posterWallCache) list() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]string, len(c.current))
	copy(out, c.current)
	return out
}

// Start warms the cache once and then refreshes it on the cadence, all in one
// background goroutine so boot is never blocked by the TMDB round trip. Stop
// cancels it. A nil cache (no TMDB key) is a no-op, mirroring the enrichRunner
// nil guard.
func (c *posterWallCache) Start(ctx context.Context) {
	if c == nil {
		return
	}
	runCtx, cancel := context.WithCancel(ctx)
	c.cancel = cancel

	c.wg.Add(1)
	go c.run(runCtx)
}

// Stop cancels the background goroutine and waits for it to unwind. Safe to call
// on a nil cache or one that never started.
func (c *posterWallCache) Stop() {
	if c == nil {
		return
	}
	c.stopOnce.Do(func() {
		if c.cancel != nil {
			c.cancel()
		}
		c.wg.Wait()
	})
}

func (c *posterWallCache) run(ctx context.Context) {
	defer c.wg.Done()

	c.warm(ctx)
	if c.refresh <= 0 {
		return
	}

	ticker := time.NewTicker(c.refresh)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-c.trigger:
			c.warm(ctx)
		case <-ticker.C:
			c.warm(ctx)
		}
	}
}

// warm runs one fetch and, on success, swaps in up to posterWallMax paths. On a
// failed fetch it logs and returns, leaving the last good list in place.
func (c *posterWallCache) warm(ctx context.Context) {
	paths, err := c.fetch(ctx)
	if err != nil {
		// A cancelled context is an orderly shutdown, not a fault worth an error line.
		if ctx.Err() == nil {
			c.log.Warn().Err(err).Msg("poster wall warm failed, keeping last good list")
		}
		return
	}

	if len(paths) > posterWallMax {
		paths = paths[:posterWallMax]
	}

	c.mu.Lock()
	c.current = paths
	c.mu.Unlock()
	c.log.Debug().Int("count", len(paths)).Msg("poster wall warmed")
}

// handlePosterWall serves the public, pre-session poster wall: a bare JSON
// []string of poster paths in popularity order. It carries no secrets (poster
// paths are public artwork) and serves [] whenever the cache is unwarmed or no
// TMDB key is set (posterWall stays nil), so the client always has a clean empty
// signal to fall back to its gradient tiles.
func (h *handler) handlePosterWall(c *fiber.Ctx) error {
	if h.posterWall == nil {
		return c.Status(fiber.StatusOK).JSON([]string{})
	}
	return c.Status(fiber.StatusOK).JSON(h.posterWall.list())
}
