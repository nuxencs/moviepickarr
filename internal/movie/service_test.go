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

	// Rotation-on-watch (Model B) at the movie layer: the drawn movie is the
	// runner's pick and stays "current" across the reveal — it becomes "watched"
	// only when watched. The next-up rotation (advanced by the server on watch)
	// therefore holds across draw → reveal and passes only here.
	if got := repo.movies[drawn.ID].Status; got != "current" {
		t.Fatalf("after reveal: movie status = %q, want current (not yet watched)", got)
	}

	if _, err := svc.MarkCurrentAsWatched(ctx); err != nil {
		t.Fatalf("MarkCurrentAsWatched: unexpected error: %v", err)
	}

	if got := repo.movies[drawn.ID].Status; got != "watched" {
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
	fireA := ft.fn

	// A is watched (clearing the draw), then a fresh draw B takes the slot.
	if _, err := svc.MarkCurrentAsWatched(ctx); err != nil {
		t.Fatalf("MarkCurrentAsWatched: %v", err)
	}
	if len(revealed) != 1 || revealed[0].MovieID != drawA.ID {
		t.Fatalf("watch should reveal draw A once, got %+v", revealed)
	}
	drawB, err := svc.DrawRandom(ctx, "c2")
	if err != nil {
		t.Fatalf("DrawRandom B: %v", err)
	}
	if drawB.ID == drawA.ID {
		t.Fatalf("test setup: expected B (%d) to differ from A (%d)", drawB.ID, drawA.ID)
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
	svc := NewService(repo, DrawConfig{})
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

// The whole point of the pool view: an unrevealed draw stays in the pool, in
// its title position, so no client can tell which movie was drawn before the
// reel lands. GetRandomPooled picks the lowest id, so "Alpha" is the winner.
func TestPooledHoldsTheDrawnMovieUntilRevealed(t *testing.T) {
	t.Parallel()

	repo := titleRepo()
	svc := NewService(repo, DrawConfig{})
	ctx := context.Background()

	drawn, err := svc.DrawRandom(ctx, "client-abc")
	if err != nil {
		t.Fatalf("DrawRandom: unexpected error: %v", err)
	}
	if drawn.Title != "Alpha" {
		t.Fatalf("drawn = %q, want Alpha", drawn.Title)
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
	svc := NewService(repo, DrawConfig{})
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
	svc := NewService(repo, DrawConfig{})
	ctx := context.Background()

	if _, err := svc.DrawRandom(ctx, ""); err != nil {
		t.Fatalf("DrawRandom: unexpected error: %v", err)
	}
	if _, err := svc.MarkCurrentAsWatched(ctx); err != nil {
		t.Fatalf("MarkCurrentAsWatched: unexpected error: %v", err)
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
	svc := NewService(repo, DrawConfig{})
	ctx := context.Background()

	drawn, err := svc.DrawRandom(ctx, "")
	if err != nil {
		t.Fatalf("DrawRandom: unexpected error: %v", err)
	}
	delete(repo.movies, drawn.ID)

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
