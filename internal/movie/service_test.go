package movie

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"sort"
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

func (r *testMovieRepo) EditMovie(
	_ context.Context,
	id, actorID int,
	title string,
	target domain.MovieIdentityTarget,
	watchedAt *time.Time,
) (*domain.Movie, bool, error) {
	movie, ok := r.movies[id]
	if !ok {
		return nil, false, sql.ErrNoRows
	}
	if movie.AddedByID != actorID {
		return nil, false, domain.ErrForbidden
	}
	if watchedAt != nil && movie.Status != string(domain.MovieStatusWatched) {
		return nil, false, domain.ErrInvalidInput
	}

	currentIMDb := ""
	if movie.IMDbID != nil {
		currentIMDb = *movie.IMDbID
	}
	matchingTMDB := target.TMDBID != nil && movie.TMDBID != nil && *target.TMDBID == *movie.TMDBID
	matchingIMDb := target.IMDbID != nil && currentIMDb != "" && *target.IMDbID == currentIMDb
	identityChanged := !matchingTMDB && !matchingIMDb

	movie.Title = title
	r.updateTitleHit++
	if watchedAt != nil {
		at := watchedAt.UTC()
		movie.WatchedAt = &at
		r.updateWatchedAtHit++
	}
	if identityChanged {
		if target.TMDBID == nil {
			movie.TMDBID = nil
		} else {
			id := *target.TMDBID
			movie.TMDBID = &id
		}
		if target.IMDbID == nil {
			movie.IMDbID = nil
		} else {
			id := *target.IMDbID
			movie.IMDbID = &id
		}
	}

	copyMovie := *movie
	return &copyMovie, identityChanged, nil
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

func (r *testMovieRepo) AddToStash(context.Context, string, int, *int, *string) (*domain.Movie, error) {
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

func (r *testMovieRepo) WatchCurrentAndAdvanceNextUp(
	_ context.Context,
	watchedAt time.Time,
) (*domain.Movie, *domain.User, bool, error) {
	for _, movie := range r.movies {
		if movie.Status != "current" {
			continue
		}
		movie.Status = "watched"
		movie.WatchedAt = &watchedAt
		watched := *movie
		return &watched, nil, false, nil
	}
	return nil, nil, false, domain.ErrNoCurrentDraw
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

func (r *testMovieRepo) Delete(_ context.Context, id int) error {
	if _, ok := r.movies[id]; !ok {
		return sql.ErrNoRows
	}
	delete(r.movies, id)
	return nil
}

// TestDeleteUnderPoolLock covers the lock's promise: once the pool is locked
// its composition is fixed, so the adder can't shrink the candidate set out
// from under the draw it was locked in for. The stash sits outside the lock and
// stays deletable, matching the add path, which has no lock check either.
func TestDeleteUnderPoolLock(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	newSvc := func() (*testMovieRepo, *Service) {
		repo := &testMovieRepo{
			movies: map[int]*domain.Movie{
				1: {ID: 1, Title: "Pooled", Status: string(domain.MovieStatusPool)},
				2: {ID: 2, Title: "Stashed", Status: string(domain.MovieStatusStash)},
			},
		}
		return repo, NewService(repo, DrawConfig{})
	}

	repo, svc := newSvc()
	if err := svc.Delete(ctx, 1, true); !errors.Is(err, domain.ErrPoolLocked) {
		t.Fatalf("delete pooled movie while locked = %v, want ErrPoolLocked", err)
	}
	if _, ok := repo.movies[1]; !ok {
		t.Fatal("expected the pooled movie to survive the refusal")
	}

	if err := svc.Delete(ctx, 2, true); err != nil {
		t.Fatalf("delete stashed movie while locked: %v", err)
	}
	if _, ok := repo.movies[2]; ok {
		t.Fatal("expected the stashed movie to be gone")
	}

	// Unlocked, the pool row goes as it always did.
	repo, svc = newSvc()
	if err := svc.Delete(ctx, 1, false); err != nil {
		t.Fatalf("delete pooled movie while unlocked: %v", err)
	}
	if _, ok := repo.movies[1]; ok {
		t.Fatal("expected the pooled movie to be gone")
	}

	// Both rules apply at once: the unrevealed draw answers first, so nothing
	// about the lock distinguishes a mid-draw tile from any other.
	_, svc = newSvc()
	if _, err := svc.DrawRandom(ctx, "client-abc"); err != nil {
		t.Fatalf("DrawRandom: %v", err)
	}
	if err := svc.Delete(ctx, 1, true); !errors.Is(err, domain.ErrDrawInProgress) {
		t.Fatalf("delete the held winner while locked = %v, want ErrDrawInProgress", err)
	}
}

func TestActiveDrawLifecycle(t *testing.T) {
	t.Parallel()

	repo := &testMovieRepo{
		movies: map[int]*domain.Movie{
			1: {ID: 1, Title: "One", Status: string(domain.MovieStatusPool)},
			2: {ID: 2, Title: "Two", Status: string(domain.MovieStatusPool)},
		},
	}
	svc := NewService(repo, firstCandidateDrawConfig())
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
	if ap.MovieID != drawn.Movie.ID {
		t.Fatalf("active draw movie = %d, want %d", ap.MovieID, drawn.Movie.ID)
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
	if revealed.MovieID != drawn.Movie.ID {
		t.Fatalf("revealed movie = %d, want %d", revealed.MovieID, drawn.Movie.ID)
	}
	if _, flippedAgain := svc.RevealCurrentDraw(); flippedAgain {
		t.Fatal("expected a second reveal to be a no-op")
	}
	if ap, ok := svc.ActiveDraw(); !ok || !ap.Revealed {
		t.Fatal("expected the active draw to remain, now marked revealed")
	}

	// Rotation-on-watch (Model B) at the movie layer: the drawn movie is the
	// runner's pick and stays "current" across the reveal — it becomes "watched"
	// only when watched. The next-up rotation in the atomic watch store therefore
	// holds across draw → reveal and passes only here.
	if got := repo.movies[drawn.Movie.ID].Status; got != "current" {
		t.Fatalf("after reveal: movie status = %q, want current (not yet watched)", got)
	}

	if _, _, _, err := svc.MarkCurrentAsWatchedAndAdvanceNextUp(ctx); err != nil {
		t.Fatalf("MarkCurrentAsWatchedAndAdvanceNextUp: unexpected error: %v", err)
	}

	if got := repo.movies[drawn.Movie.ID].Status; got != "watched" {
		t.Fatalf("after watch: movie status = %q, want watched", got)
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

func TestPublishedDrawArmsAutoRevealAndStampsDeadline(t *testing.T) {
	t.Parallel()

	ft := &fakeTimer{}
	var revealedDraws []ActiveDraw
	svc := NewService(poolRepo(), DrawConfig{
		AutoRevealDelay: 5 * time.Second,
		StartTimer:      ft.start,
		OnRevealed:      func(ap ActiveDraw) { revealedDraws = append(revealedDraws, ap) },
	})

	drawn, err := svc.DrawRandom(context.Background(), "c1")
	if err != nil {
		t.Fatalf("DrawRandom: %v", err)
	}
	if ft.starts != 0 {
		t.Fatalf("timer armed before draw publication: starts=%d", ft.starts)
	}
	ap, ok := svc.ActiveDraw()
	if !ok {
		t.Fatal("expected an active draw")
	}
	svc.StartAutoReveal(drawn.Movie.ID+1000, ap.Generation)
	svc.StartAutoReveal(drawn.Movie.ID, ap.Generation+1)
	if ft.starts != 0 {
		t.Fatalf("stale publication token armed a timer: starts=%d", ft.starts)
	}
	svc.StartAutoReveal(drawn.Movie.ID, ap.Generation)
	svc.StartAutoReveal(drawn.Movie.ID, ap.Generation)
	if ft.starts != 1 || ft.lastDur > 5*time.Second {
		t.Fatalf("expected one timer within the 5s deadline, got starts=%d dur=%v", ft.starts, ft.lastDur)
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

func TestStartAutoRevealUsesRemainingDeadline(t *testing.T) {
	t.Parallel()

	ft := &fakeTimer{}
	svc := NewService(poolRepo(), DrawConfig{
		AutoRevealDelay: 5 * time.Second,
		StartTimer:      ft.start,
	})
	drawn, err := svc.DrawRandom(context.Background(), "c1")
	if err != nil {
		t.Fatalf("DrawRandom: %v", err)
	}
	ap, ok := svc.ActiveDraw()
	if !ok {
		t.Fatal("expected an active draw")
	}

	// Simulate payload construction running past the advertised deadline. The
	// post-publication timer fires immediately; it does not grant a fresh 5s.
	svc.mu.Lock()
	svc.activeDraw.RevealAt = time.Now().Add(-time.Second)
	svc.mu.Unlock()
	svc.StartAutoReveal(drawn.Movie.ID, ap.Generation)

	if ft.starts != 1 || ft.lastDur != 0 {
		t.Fatalf("expired draw timer starts=%d dur=%v, want one immediate timer", ft.starts, ft.lastDur)
	}
}

func TestCloseStopsDelayedAndTriggeredAutoReveal(t *testing.T) {
	t.Parallel()

	t.Run("arm after close", func(t *testing.T) {
		ft := &fakeTimer{}
		svc := NewService(poolRepo(), DrawConfig{StartTimer: ft.start})
		drawn, err := svc.DrawRandom(context.Background(), "c1")
		if err != nil {
			t.Fatalf("DrawRandom: %v", err)
		}
		ap, ok := svc.ActiveDraw()
		if !ok {
			t.Fatal("expected an active draw")
		}

		svc.Close()
		svc.StartAutoReveal(drawn.Movie.ID, ap.Generation)
		if ft.starts != 0 {
			t.Fatalf("post-close publication armed %d timers, want 0", ft.starts)
		}
	})

	t.Run("triggered callback after close", func(t *testing.T) {
		ft := &fakeTimer{}
		notified := 0
		svc := NewService(poolRepo(), DrawConfig{
			StartTimer: ft.start,
			OnRevealed: func(ActiveDraw) {
				notified++
			},
		})
		drawn, err := svc.DrawRandom(context.Background(), "c1")
		if err != nil {
			t.Fatalf("DrawRandom: %v", err)
		}
		ap, ok := svc.ActiveDraw()
		if !ok {
			t.Fatal("expected an active draw")
		}
		svc.StartAutoReveal(drawn.Movie.ID, ap.Generation)
		triggered := ft.fn

		svc.Close()
		triggered()
		if notified != 0 {
			t.Fatalf("post-close deadline sent %d notifications, want 0", notified)
		}
		if ap, ok := svc.ActiveDraw(); !ok || ap.Revealed {
			t.Fatalf("post-close deadline changed active draw to %+v, ok=%v", ap, ok)
		}
	})
}

func TestManualRevealCancelsAutoRevealAndNotifiesOnce(t *testing.T) {
	t.Parallel()

	ft := &fakeTimer{}
	notified := 0
	svc := NewService(poolRepo(), DrawConfig{
		StartTimer: ft.start,
		OnRevealed: func(ActiveDraw) { notified++ },
	})

	drawn, err := svc.DrawRandom(context.Background(), "c1")
	if err != nil {
		t.Fatalf("DrawRandom: %v", err)
	}
	ap, ok := svc.ActiveDraw()
	if !ok {
		t.Fatal("expected an active draw")
	}
	svc.StartAutoReveal(drawn.Movie.ID, ap.Generation)

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

	drawn, err := svc.DrawRandom(ctx, "c1")
	if err != nil {
		t.Fatalf("DrawRandom: %v", err)
	}
	ap, ok := svc.ActiveDraw()
	if !ok {
		t.Fatal("expected an active draw")
	}
	svc.StartAutoReveal(drawn.Movie.ID, ap.Generation)
	if _, _, _, err := svc.MarkCurrentAsWatchedAndAdvanceNextUp(ctx); err != nil {
		t.Fatalf("MarkCurrentAsWatchedAndAdvanceNextUp: %v", err)
	}
	if ft.stops == 0 {
		t.Fatal("expected the watch to cancel the pending auto-reveal")
	}
	if notified != 1 {
		t.Fatalf("expected watch to reveal the draw for other clients, got %d notifications", notified)
	}

	// A stale deadline firing after the watch must not reveal a second time.
	ft.fire()
	if notified != 1 {
		t.Fatalf("expected exactly one OnRevealed after watch, got %d", notified)
	}
}

// A draw's auto-reveal timer belongs to THAT draw. time.AfterFunc can't
// un-fire a callback that already triggered, so a stale deadline that runs
// after its draw was watched and replaced must not reveal the replacement.
func TestStaleAutoRevealDoesNotRevealReplacementDraw(t *testing.T) {
	t.Parallel()

	ft := &fakeTimer{}
	var revealed []ActiveDraw
	svc := NewService(poolRepo(), DrawConfig{
		StartTimer: ft.start,
		OnRevealed: func(ap ActiveDraw) { revealed = append(revealed, ap) },
	})
	ctx := context.Background()

	// Draw A and grab the exact callback armed for it, before a later draw can
	// overwrite the fake timer's captured fn.
	drawA, err := svc.DrawRandom(ctx, "c1")
	if err != nil {
		t.Fatalf("DrawRandom A: %v", err)
	}
	activeA, ok := svc.ActiveDraw()
	if !ok {
		t.Fatal("expected active draw A")
	}
	svc.StartAutoReveal(drawA.Movie.ID, activeA.Generation)
	fireA := ft.fn

	// A is watched (clearing the draw), then a fresh draw B takes the slot.
	if _, _, _, err := svc.MarkCurrentAsWatchedAndAdvanceNextUp(ctx); err != nil {
		t.Fatalf("MarkCurrentAsWatchedAndAdvanceNextUp: %v", err)
	}
	if len(revealed) != 1 || revealed[0].MovieID != drawA.Movie.ID {
		t.Fatalf("watch should reveal draw A once, got %+v", revealed)
	}
	drawB, err := svc.DrawRandom(ctx, "c2")
	if err != nil {
		t.Fatalf("DrawRandom B: %v", err)
	}
	if drawB.Movie.ID == drawA.Movie.ID {
		t.Fatalf("test setup: expected B (%d) to differ from A (%d)", drawB.Movie.ID, drawA.Movie.ID)
	}
	activeB, ok := svc.ActiveDraw()
	if !ok {
		t.Fatal("expected active draw B")
	}
	svc.StartAutoReveal(drawA.Movie.ID, activeA.Generation)
	if ft.starts != 1 {
		t.Fatalf("stale draw A publication rearmed a timer: starts=%d", ft.starts)
	}
	svc.StartAutoReveal(drawB.Movie.ID, activeB.Generation)
	svc.StartAutoReveal(drawB.Movie.ID, activeB.Generation)
	if ft.starts != 2 {
		t.Fatalf("draw B timer starts=%d, want one new timer", ft.starts)
	}

	// A's deadline was cancelled by the watch, but simulate its already-triggered
	// callback running now, after B is active. It must not touch B.
	fireA()

	if len(revealed) != 1 {
		t.Fatalf("stale auto-reveal added %d notifications; must not reveal the replacement draw", len(revealed)-1)
	}
	if ap, ok := svc.ActiveDraw(); !ok || ap.Revealed {
		t.Fatal("draw B must remain unrevealed after draw A's stale timer fired")
	}
}

func TestEditRejectsWatchedAtForNonWatchedMovie(t *testing.T) {
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

	imdbID := "tt0000042"
	_, _, err := svc.Edit(
		context.Background(),
		42,
		0,
		"After",
		domain.MovieIdentityTarget{IMDbID: &imdbID},
		&watchedAt,
	)
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

func TestEditWatchedMovieAllowsWatchedAt(t *testing.T) {
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

	imdbID := "tt0000007"
	updated, _, err := svc.Edit(
		context.Background(),
		7,
		0,
		"After",
		domain.MovieIdentityTarget{IMDbID: &imdbID},
		&watchedAt,
	)
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

// titlePoolRepo holds three pooled movies whose titles sort A < B < C, plus a
// second owner, so the pool-view tests can assert both placement and per-member
// scoping. FindByUserIDAndStatus is implemented here (the shared repo panics on
// it) because the per-member pool view goes through it.
type titlePoolRepo struct {
	testMovieRepo
}

func (r *titlePoolRepo) FindByUserIDAndStatus(_ context.Context, userID int, status string) ([]*domain.Movie, error) {
	var out []*domain.Movie
	for _, m := range r.movies {
		if m.AddedByID == userID && m.Status == status {
			c := *m
			out = append(out, &c)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Title < out[j].Title })
	return out, nil
}

// FindByID mirrors the sqlite repo, which wraps a miss as domain.ErrNotFound
// rather than returning sql.ErrNoRows bare.
func (r *titlePoolRepo) FindByID(_ context.Context, id int) (*domain.Movie, error) {
	movie, ok := r.movies[id]
	if !ok {
		return nil, fmt.Errorf("%w: movie id %d", domain.ErrNotFound, id)
	}
	c := *movie
	return &c, nil
}

func (r *titlePoolRepo) FindByStatus(ctx context.Context, status string) ([]*domain.Movie, error) {
	out, err := r.testMovieRepo.FindByStatus(ctx, status)
	if err != nil {
		return nil, err
	}
	// The real repo sorts the pool by title; the pool view inserts the held
	// draw into that order, so the fake has to sort too.
	sort.Slice(out, func(i, j int) bool { return out[i].Title < out[j].Title })
	return out, nil
}

func titleRepo() *titlePoolRepo {
	return &titlePoolRepo{testMovieRepo{
		movies: map[int]*domain.Movie{
			1: {ID: 1, Title: "Alpha", Status: string(domain.MovieStatusPool), AddedByID: 10},
			2: {ID: 2, Title: "Bravo", Status: string(domain.MovieStatusPool), AddedByID: 10},
			3: {ID: 3, Title: "Charlie", Status: string(domain.MovieStatusPool), AddedByID: 20},
		},
	}}
}

func firstCandidateDrawConfig() DrawConfig {
	return DrawConfig{RandomIndex: func(int) int { return 0 }}
}

// movableTitlePoolRepo adds the conditional promotion write used by
// MoveToPool. The shared title repo leaves writes loud by default so a pool
// view test cannot start mutating state accidentally.
type movableTitlePoolRepo struct {
	titlePoolRepo
}

func (r *movableTitlePoolRepo) PromoteToPoolIfRoom(
	_ context.Context,
	id int,
	maxPool int,
) (int64, error) {
	target, ok := r.movies[id]
	if !ok || target.Status != "stash" {
		return 0, nil
	}

	pooled := 0
	for _, movie := range r.movies {
		if movie.AddedByID == target.AddedByID && movie.Status == "pool" {
			pooled++
		}
	}
	if pooled >= maxPool {
		return 0, nil
	}

	target.Status = "pool"
	return 1, nil
}

// A promotion that starts after DrawRandom publishes is legal. It belongs to
// the next draw, not the reel whose candidate set was already selected.
func TestDrawRandomSnapshotsCandidatesBeforeLaterPromotion(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	repo := &movableTitlePoolRepo{titlePoolRepo: *titleRepo()}
	repo.movies[4] = &domain.Movie{
		ID:        4,
		Title:     "Delta",
		Status:    string(domain.MovieStatusStash),
		AddedByID: 10,
	}
	svc := NewService(repo, firstCandidateDrawConfig())

	draw, err := svc.DrawRandom(ctx, "client-abc")
	if err != nil {
		t.Fatalf("DrawRandom: %v", err)
	}
	changed, err := svc.MoveToPool(ctx, 4)
	if err != nil {
		t.Fatalf("promote after draw: %v", err)
	}
	if !changed {
		t.Fatal("promotion after draw was not applied")
	}

	if got := titles(draw.Candidates); !slices.Equal(got, []string{"Alpha", "Bravo", "Charlie"}) {
		t.Fatalf("draw candidates after promotion = %v, want pre-draw pool", got)
	}
	winnerInSnapshot := false
	for _, candidate := range draw.Candidates {
		if candidate.ID == draw.Movie.ID {
			winnerInSnapshot = true
		}
		if candidate.ID == 4 {
			t.Fatal("post-draw promotion entered the reel candidates")
		}
	}
	if !winnerInSnapshot {
		t.Fatalf("winner %d missing from candidate snapshot", draw.Movie.ID)
	}

	visible, err := svc.Pooled(ctx)
	if err != nil {
		t.Fatalf("load current pool view: %v", err)
	}
	if !slices.Contains(titles(visible), "Delta") {
		t.Fatal("test setup: promoted movie is not visible in the later pool view")
	}
}

// The repository is allowed to reuse movie pointers internally. DrawRandom
// must still publish detached selected/candidate values from the same pre-draw
// snapshot.
func TestDrawRandomDetachesSelectedMovieAndCandidates(t *testing.T) {
	t.Parallel()

	repo := titleRepo()
	addedAt := time.Date(2026, time.July, 30, 20, 0, 0, 0, time.UTC)
	watchedAt := addedAt.Add(time.Hour)
	wantAddedAt := addedAt
	wantWatchedAt := watchedAt
	tmdbID := 100
	imdbID := "tt0000100"
	repo.movies[1].AddedAt = &addedAt
	repo.movies[1].WatchedAt = &watchedAt
	repo.movies[1].TMDBID = &tmdbID
	repo.movies[1].IMDbID = &imdbID

	draw, err := NewService(repo, DrawConfig{
		RandomIndex: func(int) int { return 0 },
	}).DrawRandom(context.Background(), "")
	if err != nil {
		t.Fatalf("DrawRandom: %v", err)
	}
	if draw.Movie.Status != string(domain.MovieStatusPool) {
		t.Fatalf("selected status = %q, want pre-draw pool", draw.Movie.Status)
	}

	draw.Movie.Title = "Changed selected"
	if repo.movies[1].Title != "Alpha" {
		t.Fatalf("selected movie mutated repository title to %q", repo.movies[1].Title)
	}
	if draw.Candidates[0].Title != "Alpha" {
		t.Fatalf("selected movie mutated candidate title to %q", draw.Candidates[0].Title)
	}

	draw.Candidates[0].Title = "Changed candidate"
	if draw.Movie.Title != "Changed selected" {
		t.Fatalf("candidate mutated selected title to %q", draw.Movie.Title)
	}
	if repo.movies[1].Title != "Alpha" {
		t.Fatalf("candidate mutated repository title to %q", repo.movies[1].Title)
	}

	*draw.Movie.AddedAt = wantAddedAt.Add(2 * time.Hour)
	*draw.Movie.TMDBID = 200
	if got := *draw.Candidates[0].AddedAt; !got.Equal(wantAddedAt) {
		t.Fatalf("selected movie mutated candidate addedAt to %v", got)
	}
	if got := *repo.movies[1].TMDBID; got != 100 {
		t.Fatalf("selected movie mutated repository TMDB id to %d", got)
	}

	*draw.Candidates[0].WatchedAt = wantWatchedAt.Add(2 * time.Hour)
	*draw.Candidates[0].IMDbID = "tt0000200"
	if got := *draw.Movie.WatchedAt; !got.Equal(wantWatchedAt) {
		t.Fatalf("candidate mutated selected watchedAt to %v", got)
	}
	if got := *repo.movies[1].IMDbID; got != "tt0000100" {
		t.Fatalf("candidate mutated repository IMDb id to %q", got)
	}
}

func TestDrawRandomUsesConfiguredRandomIndex(t *testing.T) {
	t.Parallel()

	repo := titleRepo()
	calledWith := 0
	svc := NewService(repo, DrawConfig{
		RandomIndex: func(n int) int {
			calledWith = n
			return 1
		},
	})

	draw, err := svc.DrawRandom(context.Background(), "")
	if err != nil {
		t.Fatalf("DrawRandom: %v", err)
	}
	if calledWith != 3 {
		t.Fatalf("RandomIndex called with %d candidates, want 3", calledWith)
	}
	if draw.Movie.ID != 2 || draw.Movie.Title != "Bravo" {
		t.Fatalf("selected movie = %d/%q, want 2/Bravo", draw.Movie.ID, draw.Movie.Title)
	}
	if got := repo.movies[2].Status; got != "current" {
		t.Fatalf("selected status = %q, want current", got)
	}
	if got := repo.movies[1].Status; got != "pool" {
		t.Fatalf("unselected status = %q, want pool", got)
	}
}

type countingStatusUpdateRepo struct {
	titlePoolRepo
	updateCalls int
}

func (r *countingStatusUpdateRepo) UpdateStatus(ctx context.Context, id int, status string) error {
	r.updateCalls++
	return r.titlePoolRepo.UpdateStatus(ctx, id, status)
}

func TestDrawRandomRejectsInvalidRandomIndexBeforeMutation(t *testing.T) {
	t.Parallel()

	for name, index := range map[string]int{
		"negative": -1,
		"past end": 3,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			repo := &countingStatusUpdateRepo{titlePoolRepo: *titleRepo()}
			svc := NewService(repo, DrawConfig{
				RandomIndex: func(int) int { return index },
			})

			draw, err := svc.DrawRandom(context.Background(), "")
			if draw != nil {
				t.Fatalf("draw = %+v, want nil", draw)
			}
			if !errors.Is(err, domain.ErrInvalidState) {
				t.Fatalf("DrawRandom error = %v, want ErrInvalidState", err)
			}
			if repo.updateCalls != 0 {
				t.Fatalf("status updates = %d, want 0", repo.updateCalls)
			}
			if _, ok := svc.ActiveDraw(); ok {
				t.Fatal("invalid random index published an active draw")
			}
			for id, movie := range repo.movies {
				if movie.Status != "pool" {
					t.Fatalf("movie %d status = %q, want pool", id, movie.Status)
				}
			}
		})
	}
}

func TestDrawRandomDoesNotSelectWhenCurrentDrawExists(t *testing.T) {
	t.Parallel()

	repo := titleRepo()
	repo.movies[4] = &domain.Movie{
		ID:        4,
		Title:     "Current",
		Status:    string(domain.MovieStatusCurrent),
		AddedByID: 10,
	}
	randomIndexCalled := false
	svc := NewService(repo, DrawConfig{
		RandomIndex: func(int) int {
			randomIndexCalled = true
			return 0
		},
	})

	draw, err := svc.DrawRandom(context.Background(), "")
	if draw != nil {
		t.Fatalf("draw = %+v, want nil", draw)
	}
	if !errors.Is(err, domain.ErrCurrentDrawExists) {
		t.Fatalf("DrawRandom error = %v, want ErrCurrentDrawExists", err)
	}
	if randomIndexCalled {
		t.Fatal("RandomIndex called despite an existing current draw")
	}
	if _, ok := svc.ActiveDraw(); ok {
		t.Fatal("existing current draw published a new ActiveDraw")
	}
}

// pausingDrawRepo exposes the instant after the winner is persisted as current
// but before UpdateStatus returns to the service.
type pausingDrawRepo struct {
	titlePoolRepo
	statusUpdated chan struct{}
	resume        chan struct{}
}

func (r *pausingDrawRepo) UpdateStatus(ctx context.Context, id int, status string) error {
	if err := r.titlePoolRepo.UpdateStatus(ctx, id, status); err != nil {
		return err
	}
	close(r.statusUpdated)
	<-r.resume
	return nil
}

// repoCallPause stops a repository mutation after the service has made its
// draw-state decision but before the persisted change. That exposes the reverse
// side of the publication race covered by pausingDrawRepo.
type repoCallPause struct {
	reached chan struct{}
	resume  chan struct{}
}

func newRepoCallPause() repoCallPause {
	return repoCallPause{
		reached: make(chan struct{}),
		resume:  make(chan struct{}),
	}
}

func (p *repoCallPause) wait() {
	close(p.reached)
	<-p.resume
}

type pausingDeleteRepo struct {
	titlePoolRepo
	pause repoCallPause
}

func (r *pausingDeleteRepo) Delete(ctx context.Context, id int) error {
	r.pause.wait()
	return r.titlePoolRepo.Delete(ctx, id)
}

type pausingDemoteRepo struct {
	titlePoolRepo
	pause repoCallPause
}

func (r *pausingDemoteRepo) UpdateStatusIf(_ context.Context, id int, to, from string) (int64, error) {
	r.pause.wait()
	movie, ok := r.movies[id]
	if !ok || movie.Status != from {
		return 0, nil
	}
	movie.Status = to
	return 1, nil
}

type pausingPromoteRepo struct {
	titlePoolRepo
	pause repoCallPause
}

func (r *pausingPromoteRepo) PromoteToPoolIfRoom(
	_ context.Context,
	id int,
	maxPool int,
) (int64, error) {
	r.pause.wait()

	target, ok := r.movies[id]
	if !ok || target.Status != "stash" {
		return 0, nil
	}

	pooled := 0
	for _, movie := range r.movies {
		if movie.AddedByID == target.AddedByID && movie.Status == "pool" {
			pooled++
		}
	}
	if pooled >= maxPool {
		return 0, nil
	}

	target.Status = "pool"
	return 1, nil
}

type drawCallResult struct {
	movie *domain.Movie
	err   error
}

func startDraw(ctx context.Context, svc *Service) <-chan drawCallResult {
	done := make(chan drawCallResult, 1)
	go func() {
		draw, err := svc.DrawRandom(ctx, "")
		var selected *domain.Movie
		if draw != nil {
			selected = draw.Movie
		}
		done <- drawCallResult{movie: selected, err: err}
	}()
	return done
}

func earlyDraw(done <-chan drawCallResult) (drawCallResult, bool) {
	select {
	case result := <-done:
		return result, true
	case <-time.After(50 * time.Millisecond):
		return drawCallResult{}, false
	}
}

func titles(movies []*domain.Movie) []string {
	out := make([]string, 0, len(movies))
	for _, m := range movies {
		out = append(out, m.Title)
	}
	return out
}

func TestDrawPublicationHidesWinnerAtomically(t *testing.T) {
	repo := &pausingDrawRepo{
		titlePoolRepo: *titleRepo(),
		statusUpdated: make(chan struct{}),
		resume:        make(chan struct{}),
	}
	t.Cleanup(func() { close(repo.resume) })
	svc := NewService(repo, firstCandidateDrawConfig())
	ctx := context.Background()

	drawDone := make(chan error, 1)
	go func() {
		_, err := svc.DrawRandom(ctx, "")
		drawDone <- err
	}()
	<-repo.statusUpdated

	escaped := make(chan string, 3)
	detail := make(chan *domain.Movie, 1)
	go func() {
		movie, _ := svc.GetForDisplay(ctx, 1)
		detail <- movie
		escaped <- "detail"
	}()
	pool := make(chan []*domain.Movie, 1)
	go func() {
		movies, _ := svc.Pooled(ctx)
		pool <- movies
		escaped <- "pool"
	}()
	gate := make(chan bool, 1)
	go func() {
		gate <- svc.DrawInProgress()
		escaped <- "gate"
	}()

	// The persisted winner is already current. None of the client-facing reads
	// may pass the draw publication boundary and observe that half-state.
	select {
	case name := <-escaped:
		t.Fatalf("%s read escaped before the held draw was published", name)
	case <-time.After(50 * time.Millisecond):
	}

	repo.resume <- struct{}{}
	if err := <-drawDone; err != nil {
		t.Fatalf("DrawRandom: %v", err)
	}
	if got := <-detail; got.Status != "pool" {
		t.Fatalf("detail status = %q, want pool", got.Status)
	}
	if got := titles(<-pool); !slices.Contains(got, "Alpha") {
		t.Fatalf("pool = %v, want held winner Alpha", got)
	}
	if inProgress := <-gate; !inProgress {
		t.Fatal("draw gate opened before publication")
	}
}

func TestDeleteSerializesBeforeDrawPublication(t *testing.T) {
	repo := &pausingDeleteRepo{
		titlePoolRepo: *titleRepo(),
		pause:         newRepoCallPause(),
	}
	t.Cleanup(func() { close(repo.pause.resume) })
	svc := NewService(repo, firstCandidateDrawConfig())
	ctx := context.Background()

	deleteDone := make(chan error, 1)
	go func() {
		deleteDone <- svc.Delete(ctx, 1, false)
	}()
	<-repo.pause.reached

	drawDone := startDraw(ctx, svc)
	draw, escaped := earlyDraw(drawDone)

	repo.pause.resume <- struct{}{}
	if err := <-deleteDone; err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if !escaped {
		draw = <-drawDone
	}

	if escaped {
		t.Error("draw completed while a pre-draw delete was waiting to persist")
	}
	if draw.err != nil {
		t.Fatalf("DrawRandom: %v", draw.err)
	}
	if draw.movie.ID != 2 {
		t.Fatalf("drawn movie = %d, want 2 after movie 1 was deleted", draw.movie.ID)
	}
	if _, ok := repo.movies[1]; ok {
		t.Fatal("deleted movie 1 still exists")
	}
	if got := repo.movies[draw.movie.ID].Status; got != "current" {
		t.Fatalf("drawn movie status = %q, want current", got)
	}
}

func TestMoveToStashSerializesBeforeDrawPublication(t *testing.T) {
	repo := &pausingDemoteRepo{
		titlePoolRepo: *titleRepo(),
		pause:         newRepoCallPause(),
	}
	t.Cleanup(func() { close(repo.pause.resume) })
	svc := NewService(repo, firstCandidateDrawConfig())
	ctx := context.Background()

	type moveResult struct {
		changed bool
		err     error
	}
	moveDone := make(chan moveResult, 1)
	go func() {
		changed, err := svc.MoveToStash(ctx, 2)
		moveDone <- moveResult{changed: changed, err: err}
	}()
	<-repo.pause.reached

	drawDone := startDraw(ctx, svc)
	draw, escaped := earlyDraw(drawDone)

	repo.pause.resume <- struct{}{}
	move := <-moveDone
	if !escaped {
		draw = <-drawDone
	}

	if escaped {
		t.Error("draw completed while a pre-draw demotion was waiting to persist")
	}
	if move.err != nil {
		t.Fatalf("MoveToStash: %v", move.err)
	}
	if !move.changed {
		t.Fatal("MoveToStash reported no transition")
	}
	if draw.err != nil {
		t.Fatalf("DrawRandom: %v", draw.err)
	}
	if draw.movie.ID != 1 {
		t.Fatalf("drawn movie = %d, want 1", draw.movie.ID)
	}
	if got := repo.movies[2].Status; got != "stash" {
		t.Fatalf("demoted movie status = %q, want stash", got)
	}
}

func TestMoveToPoolKeepsHeldWinnerInCapAcrossDrawPublication(t *testing.T) {
	repo := &pausingPromoteRepo{
		titlePoolRepo: titlePoolRepo{testMovieRepo{movies: map[int]*domain.Movie{
			1: {ID: 1, Title: "Alpha", Status: "pool", AddedByID: 10},
			2: {ID: 2, Title: "Bravo", Status: "pool", AddedByID: 10},
			3: {ID: 3, Title: "Charlie", Status: "pool", AddedByID: 10},
			4: {ID: 4, Title: "Delta", Status: "stash", AddedByID: 10},
		}}},
		pause: newRepoCallPause(),
	}
	t.Cleanup(func() { close(repo.pause.resume) })
	svc := NewService(repo, DrawConfig{})
	ctx := context.Background()

	type moveResult struct {
		changed bool
		err     error
	}
	moveDone := make(chan moveResult, 1)
	go func() {
		changed, err := svc.MoveToPool(ctx, 4)
		moveDone <- moveResult{changed: changed, err: err}
	}()
	<-repo.pause.reached

	drawDone := startDraw(ctx, svc)
	draw, escaped := earlyDraw(drawDone)

	repo.pause.resume <- struct{}{}
	move := <-moveDone
	if !escaped {
		draw = <-drawDone
	}

	if escaped {
		t.Error("draw completed while a pre-draw promotion was evaluating the pool cap")
	}
	if move.changed {
		t.Error("MoveToPool transitioned a fourth movie into a full pool")
	}
	if !errors.Is(move.err, domain.ErrPoolLimitReached) {
		t.Errorf("MoveToPool error = %v, want ErrPoolLimitReached", move.err)
	}
	if draw.err != nil {
		t.Fatalf("DrawRandom: %v", draw.err)
	}

	pooled, err := svc.PooledByUserID(ctx, 10)
	if err != nil {
		t.Fatalf("PooledByUserID: %v", err)
	}
	if got := len(pooled); got != maxPoolSize {
		t.Fatalf("effective pool size = %d, want %d", got, maxPoolSize)
	}
	if got := repo.movies[4].Status; got != "stash" {
		t.Fatalf("promotion target status = %q, want stash", got)
	}
}

func TestMoveToPoolTreatsOnlyHeldWinnerAsAlreadyPooled(t *testing.T) {
	t.Parallel()

	repo := &movableTitlePoolRepo{titlePoolRepo: *titleRepo()}
	svc := NewService(repo, firstCandidateDrawConfig())
	t.Cleanup(svc.Close)
	ctx := context.Background()

	drawn, err := svc.DrawRandom(ctx, "")
	if err != nil {
		t.Fatalf("DrawRandom: %v", err)
	}
	if drawn.Movie.ID != 1 {
		t.Fatalf("drawn movie = %d, want 1 so movie 2 stays a bystander", drawn.Movie.ID)
	}

	// Both tiles are projected into the same pool. Reasserting that directional
	// target must therefore be the same idempotent no-op for the persisted
	// current winner as it is for an ordinary pool row.
	for name, movieID := range map[string]int{
		"held winner":      drawn.Movie.ID,
		"pooled bystander": 2,
	} {
		t.Run(name, func(t *testing.T) {
			changed, err := svc.MoveToPool(ctx, movieID)
			if err != nil {
				t.Fatalf("MoveToPool: %v", err)
			}
			if changed {
				t.Fatal("MoveToPool reported a transition")
			}
		})
	}
	if got := repo.movies[drawn.Movie.ID].Status; got != "current" {
		t.Fatalf("held winner status = %q, want current", got)
	}

	// The exception is the active, unrevealed hold, not current status alone.
	// Once revealed, the same persisted state is an invalid promotion source.
	if _, flipped := svc.RevealCurrentDraw(); !flipped {
		t.Fatal("expected reveal to flip the draw")
	}
	changed, err := svc.MoveToPool(ctx, drawn.Movie.ID)
	if changed {
		t.Fatal("revealed current movie transitioned to the pool")
	}
	if !errors.Is(err, domain.ErrInvalidState) {
		t.Fatalf("revealed current MoveToPool error = %v, want ErrInvalidState", err)
	}
}

// The whole point of the pool view: an unrevealed draw stays in the pool, in
// its title position, so no client can tell which movie was drawn before the
// reel lands. The injected index picks "Alpha" as the deterministic winner.
func TestPooledHoldsTheDrawnMovieUntilRevealed(t *testing.T) {
	t.Parallel()

	repo := titleRepo()
	svc := NewService(repo, firstCandidateDrawConfig())
	ctx := context.Background()

	drawn, err := svc.DrawRandom(ctx, "client-abc")
	if err != nil {
		t.Fatalf("DrawRandom: unexpected error: %v", err)
	}
	if drawn.Movie.Title != "Alpha" {
		t.Fatalf("drawn = %q, want Alpha", drawn.Movie.Title)
	}

	pooled, err := svc.Pooled(ctx)
	if err != nil {
		t.Fatalf("Pooled: unexpected error: %v", err)
	}
	if want := []string{"Alpha", "Bravo", "Charlie"}; !slices.Equal(titles(pooled), want) {
		t.Fatalf("pool during the draw = %v, want %v (the winner must not go missing)", titles(pooled), want)
	}

	if _, flipped := svc.RevealCurrentDraw(); !flipped {
		t.Fatal("expected the reveal to flip the draw")
	}

	pooled, err = svc.Pooled(ctx)
	if err != nil {
		t.Fatalf("Pooled after reveal: unexpected error: %v", err)
	}
	if want := []string{"Bravo", "Charlie"}; !slices.Equal(titles(pooled), want) {
		t.Fatalf("pool after the reveal = %v, want %v", titles(pooled), want)
	}
}

// The board renders each member's own pool, so the hold has to reach that read
// too — and only for the member who actually owns the drawn movie.
func TestPooledByUserIDHoldsTheDrawnMovieForItsOwner(t *testing.T) {
	t.Parallel()

	repo := titleRepo()
	svc := NewService(repo, firstCandidateDrawConfig())
	ctx := context.Background()

	if _, err := svc.DrawRandom(ctx, ""); err != nil {
		t.Fatalf("DrawRandom: unexpected error: %v", err)
	}

	own, err := svc.PooledByUserID(ctx, 10)
	if err != nil {
		t.Fatalf("PooledByUserID: unexpected error: %v", err)
	}
	if want := []string{"Alpha", "Bravo"}; !slices.Equal(titles(own), want) {
		t.Fatalf("owner pool during the draw = %v, want %v", titles(own), want)
	}

	other, err := svc.PooledByUserID(ctx, 20)
	if err != nil {
		t.Fatalf("PooledByUserID: unexpected error: %v", err)
	}
	if want := []string{"Charlie"}; !slices.Equal(titles(other), want) {
		t.Fatalf("other member's pool = %v, want %v (the held draw isn't theirs)", titles(other), want)
	}

	if _, flipped := svc.RevealCurrentDraw(); !flipped {
		t.Fatal("expected the reveal to flip the draw")
	}

	own, err = svc.PooledByUserID(ctx, 10)
	if err != nil {
		t.Fatalf("PooledByUserID after reveal: unexpected error: %v", err)
	}
	if want := []string{"Bravo"}; !slices.Equal(titles(own), want) {
		t.Fatalf("owner pool after the reveal = %v, want %v", titles(own), want)
	}
}

// A watch clears the active draw, and the watched movie must not reappear in
// the pool through the hold.
func TestPooledDropsTheDrawnMovieOnceWatched(t *testing.T) {
	t.Parallel()

	repo := titleRepo()
	svc := NewService(repo, firstCandidateDrawConfig())
	ctx := context.Background()

	if _, err := svc.DrawRandom(ctx, ""); err != nil {
		t.Fatalf("DrawRandom: unexpected error: %v", err)
	}
	if _, _, _, err := svc.MarkCurrentAsWatchedAndAdvanceNextUp(ctx); err != nil {
		t.Fatalf("MarkCurrentAsWatchedAndAdvanceNextUp: unexpected error: %v", err)
	}

	pooled, err := svc.Pooled(ctx)
	if err != nil {
		t.Fatalf("Pooled: unexpected error: %v", err)
	}
	if want := []string{"Bravo", "Charlie"}; !slices.Equal(titles(pooled), want) {
		t.Fatalf("pool after the watch = %v, want %v", titles(pooled), want)
	}
}

// A movie deleted out from under an in-flight draw must not fail every pool
// read: the listing is correct without it.
func TestPooledSurvivesAHeldDrawWhoseMovieIsGone(t *testing.T) {
	t.Parallel()

	repo := titleRepo()
	svc := NewService(repo, firstCandidateDrawConfig())
	ctx := context.Background()

	drawn, err := svc.DrawRandom(ctx, "")
	if err != nil {
		t.Fatalf("DrawRandom: unexpected error: %v", err)
	}
	delete(repo.movies, drawn.Movie.ID)

	pooled, err := svc.Pooled(ctx)
	if err != nil {
		t.Fatalf("Pooled: unexpected error: %v", err)
	}
	if want := []string{"Bravo", "Charlie"}; !slices.Equal(titles(pooled), want) {
		t.Fatalf("pool = %v, want %v", titles(pooled), want)
	}
}

// staleListingRepo answers the pool listing from before the draw while FindByID
// already reports the winner as current: the window between the two queries.
type staleListingRepo struct {
	titlePoolRepo
}

func (r *staleListingRepo) FindByStatus(_ context.Context, status string) ([]*domain.Movie, error) {
	var out []*domain.Movie
	for _, m := range r.movies {
		if m.Status == status || (status == "pool" && m.Status == "current") {
			c := *m
			c.Status = "pool"
			out = append(out, &c)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Title < out[j].Title })
	return out, nil
}

// A draw landing between the listing query and the held-draw read leaves the
// movie in both; it must still appear once.
func TestPooledHandsOutTheHeldDrawOnlyOnce(t *testing.T) {
	t.Parallel()

	repo := &staleListingRepo{*titleRepo()}
	svc := NewService(repo, DrawConfig{})
	ctx := context.Background()

	if _, err := svc.DrawRandom(ctx, ""); err != nil {
		t.Fatalf("DrawRandom: unexpected error: %v", err)
	}

	pooled, err := svc.Pooled(ctx)
	if err != nil {
		t.Fatalf("Pooled: unexpected error: %v", err)
	}
	if want := []string{"Alpha", "Bravo", "Charlie"}; !slices.Equal(titles(pooled), want) {
		t.Fatalf("pool = %v, want %v (no duplicate tile)", titles(pooled), want)
	}
}
