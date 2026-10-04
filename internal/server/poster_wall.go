package server

import (
	"context"
	"sync"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/rs/zerolog"
)

const (
	// One /discover page.
	posterWallMax = 20
	// The popular list barely moves day to day.
	posterWallRefreshInterval = 7 * 24 * time.Hour
)

// posterFetch fetches the current popular poster paths; a seam for tests.
type posterFetch func(ctx context.Context) ([]string, error)

// posterWallCache holds the poster paths for /auth/poster-wall, refreshed in
// the background so no request blocks on TMDB. A failed refresh keeps the last
// good list.
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

// list returns a copy of the cached paths, never nil, so the endpoint
// serializes [] before the first warm.
func (c *posterWallCache) list() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]string, len(c.current))
	copy(out, c.current)
	return out
}

// Start warms and then refreshes the cache in a background goroutine. A nil
// cache (no TMDB key) is a no-op.
func (c *posterWallCache) Start(ctx context.Context) {
	if c == nil {
		return
	}
	runCtx, cancel := context.WithCancel(ctx)
	c.cancel = cancel

	c.wg.Add(1)
	go c.run(runCtx)
}

// Stop cancels the background goroutine and waits for it. Safe on a nil or
// unstarted cache.
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

// warm runs one fetch and swaps in up to posterWallMax paths on success.
func (c *posterWallCache) warm(ctx context.Context) {
	paths, err := c.fetch(ctx)
	if err != nil {
		// A cancelled context is a shutdown, not a fault.
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

// handlePosterWall serves poster paths in popularity order, or [] when the
// cache is cold or no TMDB key is set.
func (h *handler) handlePosterWall(c *fiber.Ctx) error {
	if h.posterWall == nil {
		return c.Status(fiber.StatusOK).JSON([]string{})
	}
	return c.Status(fiber.StatusOK).JSON(h.posterWall.list())
}
