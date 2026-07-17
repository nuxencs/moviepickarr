package movie

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"moviepickarr/internal/domain"
)

type testMovieRepo struct {
	movies             map[int]*domain.Movie
	updateTitleHit     int
	updateWatchedAtHit int
}

func (r *testMovieRepo) FindByID(_ context.Context, id int) (*domain.Movie, error) {
	movie, ok := r.movies[id]
	if !ok {
		return nil, sql.ErrNoRows
	}

	copyMovie := *movie
	return &copyMovie, nil
}

func (r *testMovieRepo) UpdateTitle(_ context.Context, id int, title string) error {
	movie, ok := r.movies[id]
	if !ok {
		return sql.ErrNoRows
	}
	movie.Title = title
	r.updateTitleHit++
	return nil
}

func (r *testMovieRepo) UpdateWatchedAt(_ context.Context, id int, watchedAt time.Time) error {
	movie, ok := r.movies[id]
	if !ok {
		return sql.ErrNoRows
	}
	movie.WatchedAt = &watchedAt
	r.updateWatchedAtHit++
	return nil
}

func (r *testMovieRepo) List(context.Context) ([]*domain.Movie, error) { panic("unexpected call") }
func (r *testMovieRepo) FindByUserID(context.Context, int) ([]*domain.Movie, error) {
	panic("unexpected call")
}

func (r *testMovieRepo) FindByStatus(_ context.Context, status string) ([]*domain.Movie, error) {
	var out []*domain.Movie
	for _, m := range r.movies {
		if m.Status == status {
			c := *m
			out = append(out, &c)
		}
	}
	return out, nil
}

func (r *testMovieRepo) FindByUserIDAndStatus(context.Context, int, string) ([]*domain.Movie, error) {
	panic("unexpected call")
}
func (r *testMovieRepo) CountByStatus(context.Context, string) (int, error) { panic("unexpected call") }
func (r *testMovieRepo) CountByUserIDAndStatus(context.Context, int, string) (int, error) {
	panic("unexpected call")
}

func (r *testMovieRepo) Add(context.Context, string, string, int) (*domain.Movie, error) {
	panic("unexpected call")
}

func (r *testMovieRepo) SetExternalIDs(context.Context, int, *int, *string) error {
	panic("unexpected call")
}

func (r *testMovieRepo) UpdateStatus(_ context.Context, id int, status string) error {
	m, ok := r.movies[id]
	if !ok {
		return sql.ErrNoRows
	}
	m.Status = status
	return nil
}

func (r *testMovieRepo) UpdateStatusIf(context.Context, int, string, string) (int64, error) {
	panic("unexpected call")
}

func (r *testMovieRepo) PromoteToPoolIfRoom(context.Context, int, int) (int64, error) {
	panic("unexpected call")
}

func (r *testMovieRepo) MarkAsWatched(_ context.Context, id int, watchedAt time.Time) error {
	m, ok := r.movies[id]
	if !ok {
		return sql.ErrNoRows
	}
	m.Status = "watched"
	m.WatchedAt = &watchedAt
	return nil
}

// GetRandomPooled returns a deterministic pool movie (lowest id) so the tests
// can assert on the drawn id without flaking on map iteration order.
func (r *testMovieRepo) GetRandomPooled(context.Context) (*domain.Movie, error) {
	best := -1
	for id, m := range r.movies {
		if m.Status == "pool" && (best == -1 || id < best) {
			best = id
		}
	}
	if best == -1 {
		return nil, sql.ErrNoRows
	}
	c := *r.movies[best]
	return &c, nil
}

func (r *testMovieRepo) GetCurrent(context.Context) (*domain.Movie, error) {
	for _, m := range r.movies {
		if m.Status == "current" {
			c := *m
			return &c, nil
		}
	}
	return nil, sql.ErrNoRows
}
func (r *testMovieRepo) Delete(context.Context, int) error { panic("unexpected call") }

func TestActiveDrawLifecycle(t *testing.T) {
	t.Parallel()

	repo := &testMovieRepo{
		movies: map[int]*domain.Movie{
			1: {ID: 1, Title: "One", Status: string(domain.MovieStatusPool)},
			2: {ID: 2, Title: "Two", Status: string(domain.MovieStatusPool)},
		},
	}
	svc := NewService(repo, DrawConfig{})
	ctx := context.Background()

	if _, ok := svc.ActiveDraw(); ok {
		t.Fatal("expected no active draw before any draw")
	}

	drawn, err := svc.DrawRandom(ctx, "client-abc")
	if err != nil {
		t.Fatalf("DrawRandom: unexpected error: %v", err)
	}

	ap, ok := svc.ActiveDraw()
	if !ok {
		t.Fatal("expected an active draw after DrawRandom")
	}
	if ap.MovieID != drawn.ID {
		t.Fatalf("active draw movie = %d, want %d", ap.MovieID, drawn.ID)
	}
	if ap.DrawnAt.IsZero() {
		t.Fatal("expected a non-zero drawnAt")
	}
	if ap.DrawClientID != "client-abc" {
		t.Fatalf("draw client id = %q, want %q", ap.DrawClientID, "client-abc")
	}
	if ap.Revealed {
		t.Fatal("expected a fresh draw to be unrevealed")
	}

	// First reveal flips it and reports the draw; a second is a no-op so the
	// handler broadcasts movie:revealed exactly once.
	revealed, flipped := svc.RevealCurrentDraw()
	if !flipped {
		t.Fatal("expected the first reveal to flip the draw")
	}
	if revealed.MovieID != drawn.ID {
		t.Fatalf("revealed movie = %d, want %d", revealed.MovieID, drawn.ID)
	}
	if _, flippedAgain := svc.RevealCurrentDraw(); flippedAgain {
		t.Fatal("expected a second reveal to be a no-op")
	}
	if ap, ok := svc.ActiveDraw(); !ok || !ap.Revealed {
		t.Fatal("expected the active draw to remain, now marked revealed")
	}

	if _, err := svc.MarkCurrentAsWatched(ctx); err != nil {
		t.Fatalf("MarkCurrentAsWatched: unexpected error: %v", err)
	}

	if _, ok := svc.ActiveDraw(); ok {
		t.Fatal("expected the active draw to be cleared after marking watched")
	}
}

// fakeTimer captures the auto-reveal scheduling so tests drive the deadline
// by hand: fire() runs the scheduled fn, stops counts cancellations.
type fakeTimer struct {
	fn      func()
	starts  int
	stops   int
	lastDur time.Duration
}

func (ft *fakeTimer) start(d time.Duration, fn func()) func() {
	ft.fn = fn
	ft.starts++
	ft.lastDur = d
	return func() { ft.stops++ }
}

func (ft *fakeTimer) fire() {
	if ft.fn != nil {
		ft.fn()
	}
}

func poolRepo() *testMovieRepo {
	return &testMovieRepo{
		movies: map[int]*domain.Movie{
			1: {ID: 1, Title: "One", Status: string(domain.MovieStatusPool)},
			2: {ID: 2, Title: "Two", Status: string(domain.MovieStatusPool)},
		},
	}
}

func TestDrawArmsAutoRevealAndStampsDeadline(t *testing.T) {
	t.Parallel()

	ft := &fakeTimer{}
	var revealedDraws []ActiveDraw
	svc := NewService(poolRepo(), DrawConfig{
		AutoRevealDelay: 5 * time.Second,
		StartTimer:      ft.start,
		OnRevealed:      func(ap ActiveDraw) { revealedDraws = append(revealedDraws, ap) },
	})

	if _, err := svc.DrawRandom(context.Background(), "c1"); err != nil {
		t.Fatalf("DrawRandom: %v", err)
	}
	if ft.starts != 1 || ft.lastDur != 5*time.Second {
		t.Fatalf("expected one 5s timer armed, got starts=%d dur=%v", ft.starts, ft.lastDur)
	}

	ap, ok := svc.ActiveDraw()
	if !ok {
		t.Fatal("expected an active draw")
	}
	if got := ap.RevealAt.Sub(ap.DrawnAt); got != 5*time.Second {
		t.Fatalf("revealAt - drawnAt = %v, want 5s", got)
	}

	// Deadline fires: the reveal flips and notifies exactly once; a late
	// duplicate fire stays silent.
	ft.fire()
	ft.fire()
	if len(revealedDraws) != 1 {
		t.Fatalf("expected exactly one OnRevealed, got %d", len(revealedDraws))
	}
	if ap, ok := svc.ActiveDraw(); !ok || !ap.Revealed {
		t.Fatal("expected the draw to be revealed after the deadline")
	}
}

func TestManualRevealCancelsAutoRevealAndNotifiesOnce(t *testing.T) {
	t.Parallel()

	ft := &fakeTimer{}
	notified := 0
	svc := NewService(poolRepo(), DrawConfig{
		StartTimer: ft.start,
		OnRevealed: func(ActiveDraw) { notified++ },
	})

	if _, err := svc.DrawRandom(context.Background(), "c1"); err != nil {
		t.Fatalf("DrawRandom: %v", err)
	}

	if _, flipped := svc.RevealCurrentDraw(); !flipped {
		t.Fatal("expected the manual reveal to flip the draw")
	}
	if ft.stops == 0 {
		t.Fatal("expected the manual reveal to cancel the pending auto-reveal")
	}

	// The (already-stopped, but racing) deadline fires anyway: no double notify.
	ft.fire()
	if notified != 1 {
		t.Fatalf("expected exactly one OnRevealed, got %d", notified)
	}
}

func TestWatchClearsDrawAndCancelsAutoReveal(t *testing.T) {
	t.Parallel()

	ft := &fakeTimer{}
	notified := 0
	svc := NewService(poolRepo(), DrawConfig{
		StartTimer: ft.start,
		OnRevealed: func(ActiveDraw) { notified++ },
	})
	ctx := context.Background()

	if _, err := svc.DrawRandom(ctx, "c1"); err != nil {
		t.Fatalf("DrawRandom: %v", err)
	}
	if _, err := svc.MarkCurrentAsWatched(ctx); err != nil {
		t.Fatalf("MarkCurrentAsWatched: %v", err)
	}
	if ft.stops == 0 {
		t.Fatal("expected the watch to cancel the pending auto-reveal")
	}

	// A stale deadline firing after the watch must not reveal anything.
	ft.fire()
	if notified != 0 {
		t.Fatalf("expected no OnRevealed after the draw was cleared, got %d", notified)
	}
}

func TestUpdateRejectsWatchedAtForNonWatchedMovie(t *testing.T) {
	t.Parallel()

	repo := &testMovieRepo{
		movies: map[int]*domain.Movie{
			42: {
				ID:     42,
				Title:  "Before",
				Status: string(domain.MovieStatusPool),
			},
		},
	}
	svc := NewService(repo, DrawConfig{})
	watchedAt := time.Date(2026, 2, 8, 10, 30, 0, 0, time.UTC)

	_, err := svc.Update(context.Background(), 42, "After", &watchedAt)
	if !errors.Is(err, domain.ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput, got %v", err)
	}

	if repo.updateTitleHit != 0 {
		t.Fatalf("expected title update not to run, got %d calls", repo.updateTitleHit)
	}
	if repo.updateWatchedAtHit != 0 {
		t.Fatalf("expected watchedAt update not to run, got %d calls", repo.updateWatchedAtHit)
	}
}

func TestUpdateWatchedMovieAllowsWatchedAt(t *testing.T) {
	t.Parallel()

	repo := &testMovieRepo{
		movies: map[int]*domain.Movie{
			7: {
				ID:     7,
				Title:  "Before",
				Status: string(domain.MovieStatusWatched),
			},
		},
	}
	svc := NewService(repo, DrawConfig{})
	watchedAt := time.Date(2026, 2, 8, 18, 45, 0, 0, time.UTC)

	updated, err := svc.Update(context.Background(), 7, "After", &watchedAt)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if updated.Title != "After" {
		t.Fatalf("expected title updated, got %q", updated.Title)
	}
	if updated.WatchedAt == nil || !updated.WatchedAt.Equal(watchedAt) {
		t.Fatalf("expected watchedAt %v, got %v", watchedAt, updated.WatchedAt)
	}

	if repo.updateTitleHit != 1 {
		t.Fatalf("expected title update once, got %d", repo.updateTitleHit)
	}
	if repo.updateWatchedAtHit != 1 {
		t.Fatalf("expected watchedAt update once, got %d", repo.updateWatchedAtHit)
	}
}
