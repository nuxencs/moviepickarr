package server

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/rs/zerolog"
)

// The cache reads empty before any warm, populates (capped at 20) after a
// successful warm, and holds the last good list when a later warm fails. The
// warm is driven directly via an injectable fetch func, so no real sleeps or a
// live TMDB are needed.
func TestPosterWallCache_WarmAndKeepLastGood(t *testing.T) {
	t.Parallel()

	// 25 paths: the fetch returns more than the cap so we can assert the trim.
	source := make([]string, 25)
	for i := range source {
		source[i] = "/p" + string(rune('a'+i%26)) + ".jpg"
	}

	var fail bool
	fetch := func(context.Context) ([]string, error) {
		if fail {
			return nil, errors.New("tmdb down")
		}
		return source, nil
	}

	c := newPosterWallCache(fetch, 0, zerolog.Nop())

	// Empty before any warm.
	if got := c.list(); len(got) != 0 {
		t.Fatalf("before warm: got %v, want empty", got)
	}

	// A successful warm populates, capped at posterWallMax.
	c.warm(context.Background())
	got := c.list()
	if len(got) != posterWallMax {
		t.Fatalf("after warm: len = %d, want %d", len(got), posterWallMax)
	}
	if !slices.Equal(got, source[:posterWallMax]) {
		t.Fatalf("after warm: got %v, want first %d of source", got, posterWallMax)
	}

	// A failing warm keeps the last good list rather than clearing it.
	fail = true
	c.warm(context.Background())
	if after := c.list(); !slices.Equal(after, source[:posterWallMax]) {
		t.Fatalf("after failed warm: got %v, want last-good held", after)
	}
}

// list returns a copy: a caller mutating the returned slice cannot corrupt the
// cache a concurrent refresh may be reading.
func TestPosterWallCache_ListReturnsCopy(t *testing.T) {
	t.Parallel()
	fetch := func(context.Context) ([]string, error) { return []string{"/a.jpg"}, nil }
	c := newPosterWallCache(fetch, 0, zerolog.Nop())
	c.warm(context.Background())

	got := c.list()
	got[0] = "/mutated.jpg"
	if again := c.list(); again[0] != "/a.jpg" {
		t.Fatalf("list did not return a copy: cache mutated to %q", again[0])
	}
}

// Start runs the initial warm and Stop unwinds the goroutine. With refresh
// disabled (0), Start warms once and the run loop returns on its own.
func TestPosterWallCache_StartWarmsThenStops(t *testing.T) {
	t.Parallel()
	warmed := make(chan struct{}, 1)
	fetch := func(context.Context) ([]string, error) {
		select {
		case warmed <- struct{}{}:
		default:
		}
		return []string{"/a.jpg"}, nil
	}
	c := newPosterWallCache(fetch, 0, zerolog.Nop())
	c.Start(context.Background())

	select {
	case <-warmed:
	case <-time.After(2 * time.Second):
		t.Fatal("Start did not warm within 2s")
	}
	c.Stop()
	if got := c.list(); len(got) != 1 || got[0] != "/a.jpg" {
		t.Fatalf("after Start warm: got %v, want [/a.jpg]", got)
	}
}

// A nil cache (keyless boot) no-ops on Start/Stop rather than panicking. Run
// and the shutdown path call both unconditionally. The endpoint guards nil
// before ever calling list(), so list() itself need not be nil-safe.
func TestPosterWallCache_NilSafe(t *testing.T) {
	t.Parallel()
	var c *posterWallCache
	c.Start(context.Background())
	c.Stop()
}
