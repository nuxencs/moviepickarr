package movie

import (
	"context"
	"database/sql"
	"errors"
	"math/rand/v2"
	"sync"
	"time"

	"moviepickarr/internal/domain"
)

// maxPoolSize is the per-user pool cap, enforced atomically by
// PromoteToPoolIfRoom so concurrent promotions cannot overshoot it.
const maxPoolSize = 3

// DefaultAutoRevealDelay is how long after a draw the reveal fires by itself.
// Clients read it only through the `revealAt` payload field.
const DefaultAutoRevealDelay = 16500 * time.Millisecond

// A fired time.AfterFunc cannot be reused, so a failed durable Reveal gets a
// bounded retry timer.
const (
	autoRevealRetryDelay = time.Second
	maxAutoRevealRetries = 3
)

// watchCurrentDrawStore watches the Current draw. A watch that reveals the draw
// also commits the next-up handoff.
type watchCurrentDrawStore interface {
	WatchCurrentDraw(
		ctx context.Context,
		watchedAt time.Time,
		revealsDraw bool,
	) (watched *domain.Movie, next *domain.User, err error)
}

// editMovieStore runs the whole edit in one writer transaction, so a failed
// edit leaves no durable fragment.
type editMovieStore interface {
	EditMovie(
		ctx context.Context,
		movieID, actorID int,
		title string,
		target domain.MovieIdentityTarget,
		watchedAt *time.Time,
	) (movie *domain.Movie, identityChanged bool, err error)
}

// drawLifecycleStore owns the durable half of Draw and Reveal. Each write
// commits before any in-memory flip or client publication.
type drawLifecycleStore interface {
	StartDraw(
		ctx context.Context,
		movieID int,
		drawnAt, revealAt time.Time,
		drawClientID string,
	) error
	RevealDrawAndAdvanceNextUp(ctx context.Context, movieID int, revealedAt time.Time) (next *domain.User, err error)
	ConcealedCurrentDraw(
		ctx context.Context,
	) (movieID int, drawnAt, revealAt time.Time, drawClientID string, found bool, err error)
}

type wildcardLifecycleStore interface {
	StartWildcard(
		ctx context.Context,
		actorID int,
		selection domain.WildcardSelection,
		selectedAt time.Time,
		poolLocked bool,
	) (*domain.Wildcard, error)
	ActiveWildcard(ctx context.Context) (*domain.Wildcard, error)
	CancelWildcard(ctx context.Context, actorID int, expectedWildcardID int64, canceledAt time.Time) (*domain.Wildcard, error)
	WatchWildcard(ctx context.Context, expectedWildcardID int64, watchedAt time.Time) (*domain.Wildcard, error)
}

type movieStore interface {
	domain.MovieRepo
	watchCurrentDrawStore
	editMovieStore
	drawLifecycleStore
	wildcardLifecycleStore
}

// ActiveDraw records the most recent draw so a reloading or late client can
// resume the reel instead of jumping to the result.
type ActiveDraw struct {
	MovieID int
	// Generation binds timer arming and stale deadline callbacks to this draw.
	Generation uint64
	DrawnAt    time.Time
	// RevealAt is the auto-reveal deadline. Clients time the countdown as
	// revealAt - serverNow, immune to client clock skew.
	RevealAt time.Time
	// DrawClientID is the client that clicked Draw; only it shows the confirm button.
	DrawClientID string
	Revealed     bool
}

// Reveal is one committed Reveal. NextUp is nil when the turn stayed put.
type Reveal struct {
	ActiveDraw
	NextUp *domain.User
}

// DrawResult is the publication snapshot of one draw, taken under one lock so
// later pool mutations cannot change it.
type DrawResult struct {
	Movie      *domain.Movie
	Candidates []*domain.Movie
	ActiveDraw ActiveDraw
}

// DrawConfig wires the auto-reveal into the Service. The zero value is valid.
type DrawConfig struct {
	// AutoRevealDelay overrides DefaultAutoRevealDelay when > 0.
	AutoRevealDelay time.Duration
	// RandomIndex returns an index in [0, n). Nil uses the process random source.
	RandomIndex func(n int) int
	// StartTimer runs fn once after d. Nil uses time.AfterFunc.
	StartTimer func(d time.Duration, fn func()) (stop func())
	// OnRevealed runs exactly once per draw, after its next-up handoff commits.
	// The server wires it to the movie:revealed and settings:next-up-changed
	// broadcasts.
	OnRevealed func(Reveal)
	// OnRevealError observes a failed durable Reveal; the draw stays unrevealed.
	OnRevealError func(error)
}

// Service owns the movie lifecycle, including the in-memory active draw and
// its auto-reveal timer.
type Service struct {
	movieRepo movieStore
	drawCfg   DrawConfig

	mu                sync.Mutex
	activeDraw        *ActiveDraw
	stopAutoReveal    func()
	autoRevealArmed   bool
	autoRevealRetries int
	closed            bool
	// drawGen keeps a stale auto-reveal timer from revealing a replacement draw.
	drawGen uint64
}

func NewService(movieRepo movieStore, drawCfg DrawConfig) *Service {
	service, err := NewServiceChecked(movieRepo, drawCfg)
	if err != nil {
		panic(err)
	}
	return service
}

// NewServiceChecked restores the concealed draw before returning. A repository
// failure is fatal because serving without that draw could expose its winner.
func NewServiceChecked(movieRepo movieStore, drawCfg DrawConfig) (*Service, error) {
	if drawCfg.AutoRevealDelay <= 0 {
		drawCfg.AutoRevealDelay = DefaultAutoRevealDelay
	}
	if drawCfg.RandomIndex == nil {
		drawCfg.RandomIndex = rand.IntN
	}
	if drawCfg.StartTimer == nil {
		drawCfg.StartTimer = func(d time.Duration, fn func()) func() {
			t := time.AfterFunc(d, fn)
			return func() { t.Stop() }
		}
	}
	service := &Service{
		movieRepo: movieRepo,
		drawCfg:   drawCfg,
	}
	if err := service.resumeConcealedDraw(context.Background()); err != nil {
		return nil, err
	}
	return service, nil
}

// resumeConcealedDraw restores a draw that was not revealed before a restart.
// The persisted deadline stays authoritative.
func (s *Service) resumeConcealedDraw(ctx context.Context) error {
	movieID, drawnAt, revealAt, clientID, found, err := s.movieRepo.ConcealedCurrentDraw(ctx)
	if err != nil {
		s.notifyRevealError(err)
		return err
	}
	if !found {
		return nil
	}

	s.mu.Lock()
	s.drawGen++
	s.activeDraw = &ActiveDraw{
		MovieID:      movieID,
		Generation:   s.drawGen,
		DrawnAt:      drawnAt,
		RevealAt:     revealAt,
		DrawClientID: clientID,
	}
	s.armAutoRevealLocked(s.drawGen)
	s.mu.Unlock()
	return nil
}

func (s *Service) notifyRevealError(err error) {
	if err != nil && s.drawCfg.OnRevealError != nil {
		s.drawCfg.OnRevealError(err)
	}
}

// Close permanently stops auto-reveal scheduling on server shutdown.
func (s *Service) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	s.cancelAutoRevealLocked()
}

// armAutoRevealLocked replaces any pending timer with one bound to draw gen.
// Callers hold s.mu.
func (s *Service) armAutoRevealLocked(gen uint64) {
	s.cancelAutoRevealLocked()
	delay := max(time.Until(s.activeDraw.RevealAt), 0)
	s.scheduleAutoRevealLocked(gen, delay)
}

// scheduleAutoRevealLocked installs one timer without changing the retry
// counter. Callers hold s.mu and have already retired any previous timer.
func (s *Service) scheduleAutoRevealLocked(gen uint64, delay time.Duration) {
	s.stopAutoReveal = s.drawCfg.StartTimer(delay, func() {
		_, _, _ = s.revealActive(context.Background(), gen, true)
	})
	s.autoRevealArmed = true
}

// retryAutoRevealLocked retires the failed one-shot timer and schedules a
// bounded retry while draw gen is still active. Once the budget is spent,
// autoRevealArmed stays false so state never points at a dead timer.
func (s *Service) retryAutoRevealLocked(gen uint64) {
	s.stopAutoReveal = nil
	s.autoRevealArmed = false
	if s.activeDraw == nil ||
		s.activeDraw.Revealed ||
		s.activeDraw.Generation != gen ||
		s.drawGen != gen ||
		s.closed ||
		s.autoRevealRetries >= maxAutoRevealRetries {
		return
	}
	s.autoRevealRetries++
	s.scheduleAutoRevealLocked(gen, autoRevealRetryDelay)
}

// cancelAutoRevealLocked stops a pending auto-reveal. Callers hold s.mu.
func (s *Service) cancelAutoRevealLocked() {
	if s.stopAutoReveal != nil {
		s.stopAutoReveal()
		s.stopAutoReveal = nil
	}
	s.autoRevealArmed = false
	s.autoRevealRetries = 0
}

func (s *Service) AddToStash(
	ctx context.Context,
	title string,
	userID int,
	tmdbID *int,
	imdbID *string,
) (*domain.Movie, error) {
	return s.movieRepo.AddToStash(ctx, title, userID, tmdbID, imdbID)
}

// MoveToPool promotes a stashed movie into its owner's pool. It is idempotent
// and reports whether a real transition happened.
func (s *Service) MoveToPool(ctx context.Context, id int) (bool, error) {
	// Hold the lock so a draw cannot land between the limit and the promotion
	// and let the held winner push the member over the cap.
	s.mu.Lock()
	defer s.mu.Unlock()

	limit, err := s.poolLimitLocked(ctx, id)
	if err != nil {
		return false, err
	}

	n, err := s.movieRepo.PromoteToPoolIfRoom(ctx, id, limit)
	if err != nil {
		return false, err
	}
	if n == 1 {
		return true, nil
	}

	// No row transitioned. The held winner counts as already pooled, since every
	// client-facing read still shows it there.
	movie, err := s.movieRepo.FindByID(ctx, id)
	if err != nil {
		return false, err
	}
	switch movie.Status {
	case "pool":
		return false, nil
	case "stash":
		return false, domain.ErrPoolLimitReached
	case "current":
		if held, ok := s.heldDrawLocked(); ok && held.MovieID == movie.ID {
			return false, nil
		}
		return false, domain.ErrInvalidState
	default:
		return false, domain.ErrInvalidState
	}
}

// poolLimitLocked is the pool cap for promoting movie id. A held draw still
// costs its adder a slot, or they would get a free fourth movie during every
// draw. Callers hold s.mu through the promotion.
func (s *Service) poolLimitLocked(ctx context.Context, id int) (int, error) {
	held, ok := s.heldDrawLocked()
	if !ok {
		return maxPoolSize, nil
	}

	heldMovie, err := s.movieRepo.FindByID(ctx, held.MovieID)
	if err != nil {
		if isNotFound(err) {
			return maxPoolSize, nil
		}
		return 0, err
	}
	if heldMovie.Status != "current" {
		return maxPoolSize, nil
	}

	target, err := s.movieRepo.FindByID(ctx, id)
	if err != nil {
		return 0, err
	}
	if target.AddedByID != heldMovie.AddedByID {
		return maxPoolSize, nil
	}
	return maxPoolSize - 1, nil
}

// MoveToStash demotes a pooled movie back to the stash. Idempotent; reports
// whether a real transition happened. During an unrevealed draw every pool tile
// refuses alike, so a failed demotion cannot reveal which movie was drawn.
func (s *Service) MoveToStash(ctx context.Context, id int) (bool, error) {
	// Hold the lock through the status flip so a draw cannot select from a pool
	// this demotion is about to change.
	s.mu.Lock()
	defer s.mu.Unlock()

	if held, ok := s.heldDrawLocked(); ok {
		movie, err := s.movieRepo.FindByID(ctx, id)
		if err != nil {
			return false, err
		}
		if movie.Status == "pool" || movie.ID == held.MovieID {
			return false, domain.ErrDrawInProgress
		}
	}

	n, err := s.movieRepo.UpdateStatusIf(ctx, id, "stash", "pool")
	if err != nil {
		return false, err
	}
	if n == 1 {
		return true, nil
	}

	movie, err := s.movieRepo.FindByID(ctx, id)
	if err != nil {
		return false, err
	}
	if movie.Status == "stash" {
		return false, nil
	}
	return false, domain.ErrInvalidState
}

// Delete removes a stash or pool row. The caller reads poolLocked; the order of
// the two refusals lives here, next to the draw.
func (s *Service) Delete(ctx context.Context, id int, poolLocked bool) error {
	// A stale "pool" read must never authorize deleting a winner DrawRandom
	// has already persisted as current.
	s.mu.Lock()
	defer s.mu.Unlock()

	movie, err := s.movieRepo.FindByID(ctx, id)
	if err != nil {
		return err
	}

	// As in MoveToStash. Runs before the status check because the held winner
	// is "current", not "pool".
	if held, ok := s.heldDrawLocked(); ok && (movie.Status == "pool" || movie.ID == held.MovieID) {
		return domain.ErrDrawInProgress
	}

	// Stash adds are not lock-checked, so stash deletes are not either.
	if poolLocked && movie.Status == "pool" {
		return domain.ErrPoolLocked
	}

	if movie.Status != "pool" && movie.Status != "stash" {
		return domain.ErrInvalidState
	}

	if err = s.movieRepo.Delete(ctx, id); err != nil {
		return err
	}

	return nil
}

func (s *Service) Edit(
	ctx context.Context,
	movieID, actorID int,
	title string,
	target domain.MovieIdentityTarget,
	watchedAt *time.Time,
) (*domain.Movie, bool, error) {
	return s.movieRepo.EditMovie(ctx, movieID, actorID, title, target, watchedAt)
}

func (s *Service) Get(ctx context.Context, id int) (*domain.Movie, error) {
	return s.movieRepo.FindByID(ctx, id)
}

// GetForDisplay returns one movie as clients may see it: a held winner reads as
// pooled until reveal. Commands use Get, which returns the persisted state.
func (s *Service) GetForDisplay(ctx context.Context, id int) (*domain.Movie, error) {
	movie, err := s.movieRepo.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}

	held, ok := s.heldDraw()
	if !ok {
		return movie, nil
	}
	shown, _ := asHeldPoolMovie(movie, held)
	return shown, nil
}

func (s *Service) List(ctx context.Context) ([]*domain.Movie, error) {
	return s.movieRepo.List(ctx)
}

// Pooled is the pool as clients may see it, including the held draw (see
// withHeldDraw).
func (s *Service) Pooled(ctx context.Context) ([]*domain.Movie, error) {
	movies, err := s.movieRepo.FindByStatus(ctx, "pool")
	if err != nil {
		return nil, err
	}

	return s.withHeldDraw(ctx, movies, 0)
}

// heldDraw returns the active draw while it is still unrevealed.
func (s *Service) heldDraw() (ActiveDraw, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.heldDrawLocked()
}

func (s *Service) heldDrawLocked() (ActiveDraw, bool) {
	if s.activeDraw == nil || s.activeDraw.Revealed {
		return ActiveDraw{}, false
	}
	return *s.activeDraw, true
}

// isNotFound accepts the sqlite repo's domain.ErrNotFound and a bare sql.ErrNoRows.
func isNotFound(err error) bool {
	return errors.Is(err, domain.ErrNotFound) || errors.Is(err, sql.ErrNoRows)
}

// asHeldPoolMovie returns a pooled copy of the held winner and reports whether
// it projected. The repository record stays untouched.
func asHeldPoolMovie(movie *domain.Movie, held ActiveDraw) (*domain.Movie, bool) {
	if movie.ID != held.MovieID || movie.Status != "current" {
		return movie, false
	}
	shown := *movie
	shown.Status = "pool"
	return &shown, true
}

func cloneMovieSnapshot(movie *domain.Movie) *domain.Movie {
	if movie == nil {
		return nil
	}

	cloned := *movie
	if movie.AddedAt != nil {
		addedAt := *movie.AddedAt
		cloned.AddedAt = &addedAt
	}
	if movie.WatchedAt != nil {
		watchedAt := *movie.WatchedAt
		cloned.WatchedAt = &watchedAt
	}
	if movie.TMDBID != nil {
		tmdbID := *movie.TMDBID
		cloned.TMDBID = &tmdbID
	}
	if movie.IMDbID != nil {
		imdbID := *movie.IMDbID
		cloned.IMDbID = &imdbID
	}
	if movie.WildcardOfMovieID != nil {
		hostMovieID := *movie.WildcardOfMovieID
		cloned.WildcardOfMovieID = &hostMovieID
	}
	return &cloned
}

func (s *Service) SelectWildcard(
	ctx context.Context,
	actorID int,
	selection domain.WildcardSelection,
	poolLocked bool,
) (*domain.Wildcard, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.movieRepo.StartWildcard(ctx, actorID, selection, time.Now().UTC(), poolLocked)
}

func (s *Service) ActiveWildcard(ctx context.Context) (*domain.Wildcard, error) {
	return s.movieRepo.ActiveWildcard(ctx)
}

func (s *Service) CancelActiveWildcard(ctx context.Context, actorID int, expectedWildcardID int64) (*domain.Wildcard, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.movieRepo.CancelWildcard(ctx, actorID, expectedWildcardID, time.Now().UTC())
}

func (s *Service) MarkActiveWildcardWatched(ctx context.Context, expectedWildcardID int64) (*domain.Wildcard, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.movieRepo.WatchWildcard(ctx, expectedWildcardID, time.Now().UTC())
}

// withHeldDraw puts a drawn-but-unrevealed movie back into a pool listing, so
// a reload mid-spin cannot spot the winner by its missing tile. Only the copy
// reads as pooled; the row stays "current". userID 0 means the whole pool. The
// insert keeps the repo's title order.
func (s *Service) withHeldDraw(ctx context.Context, pooled []*domain.Movie, userID int) ([]*domain.Movie, error) {
	held, ok := s.heldDraw()
	if !ok {
		return pooled, nil
	}

	movie, err := s.movieRepo.FindByID(ctx, held.MovieID)
	if err != nil {
		// Movie deleted mid-draw: the listing is still correct without it.
		if isNotFound(err) {
			return pooled, nil
		}
		return nil, err
	}
	if userID != 0 && movie.AddedByID != userID {
		return pooled, nil
	}
	shown, ok := asHeldPoolMovie(movie, held)
	if !ok {
		return pooled, nil
	}
	// A draw between the listing query and the read above puts the row in both.
	for _, m := range pooled {
		if m.ID == movie.ID {
			return pooled, nil
		}
	}

	at := len(pooled)
	for i, m := range pooled {
		if shown.Title < m.Title {
			at = i
			break
		}
	}
	out := make([]*domain.Movie, 0, len(pooled)+1)
	out = append(out, pooled[:at]...)
	out = append(out, shown)
	out = append(out, pooled[at:]...)
	return out, nil
}

func (s *Service) Stashed(ctx context.Context) ([]*domain.Movie, error) {
	return s.movieRepo.FindByStatus(ctx, "stash")
}

func (s *Service) Watched(ctx context.Context) ([]*domain.Movie, error) {
	return s.movieRepo.FindByStatus(ctx, "watched")
}

func (s *Service) Current(ctx context.Context) (*domain.Movie, error) {
	return s.movieRepo.GetCurrent(ctx)
}

// PooledByUserID is Pooled for one member; the held draw shows only in its
// adder's pool.
func (s *Service) PooledByUserID(ctx context.Context, userID int) ([]*domain.Movie, error) {
	movies, err := s.movieRepo.FindByUserIDAndStatus(ctx, userID, "pool")
	if err != nil {
		return nil, err
	}

	return s.withHeldDraw(ctx, movies, userID)
}

func (s *Service) StashedByUserID(ctx context.Context, userID int) ([]*domain.Movie, error) {
	movies, err := s.movieRepo.FindByUserIDAndStatus(ctx, userID, "stash")
	if err != nil {
		return nil, err
	}

	return movies, nil
}

// DrawRandom selects a random pooled movie as the current draw. clientID is the
// drawer (see ActiveDraw); "" means no drawer.
func (s *Service) DrawRandom(ctx context.Context, clientID string) (*DrawResult, error) {
	// Concurrent reads block on heldDraw, so they see the status flip and the
	// in-memory hold together or neither.
	s.mu.Lock()
	defer s.mu.Unlock()

	pooled, err := s.movieRepo.FindByStatus(ctx, "pool")
	if err != nil {
		return nil, err
	}

	if len(pooled) == 0 {
		return nil, domain.ErrNotFound
	}

	current, err := s.movieRepo.GetCurrent(ctx)
	if !errors.Is(err, sql.ErrNoRows) && err != nil {
		return nil, err
	}

	if current != nil {
		return nil, domain.ErrCurrentDrawExists
	}

	// Clone, as repo fakes share pointers and a later edit must not mutate the
	// published reel.
	candidates := make([]*domain.Movie, len(pooled))
	for i, candidate := range pooled {
		candidates[i] = cloneMovieSnapshot(candidate)
	}

	selectedIndex := s.drawCfg.RandomIndex(len(candidates))
	if selectedIndex < 0 || selectedIndex >= len(candidates) {
		return nil, domain.ErrInvalidState
	}
	selected := cloneMovieSnapshot(candidates[selectedIndex])

	drawnAt := time.Now().UTC()
	revealAt := drawnAt.Add(s.drawCfg.AutoRevealDelay)
	if err = s.movieRepo.StartDraw(ctx, selected.ID, drawnAt, revealAt, clientID); err != nil {
		return nil, err
	}

	s.drawGen++
	activeDraw := ActiveDraw{
		MovieID:      selected.ID,
		Generation:   s.drawGen,
		DrawnAt:      drawnAt,
		RevealAt:     revealAt,
		DrawClientID: clientID,
	}
	s.activeDraw = &activeDraw

	return &DrawResult{
		Movie:      selected,
		Candidates: candidates,
		ActiveDraw: activeDraw,
	}, nil
}

// StartAutoReveal arms the deadline after movie:drawn is published, so
// movie:revealed can never go out first. The deadline stays anchored to
// DrawnAt. Stale, duplicate, and post-Close calls are no-ops.
func (s *Service) StartAutoReveal(movieID int, generation uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.activeDraw == nil ||
		s.activeDraw.MovieID != movieID ||
		s.activeDraw.Generation != generation ||
		s.drawGen != generation ||
		s.activeDraw.Revealed ||
		s.closed ||
		s.autoRevealArmed {
		return
	}
	s.armAutoRevealLocked(generation)
}

// MarkCurrentAsWatched persists the watched movie. Watching an unrevealed draw
// is also its Reveal and commits the next-up handoff in the same transaction.
func (s *Service) MarkCurrentAsWatched(ctx context.Context) (*domain.Movie, error) {
	// Held across the transaction so the timer can never reveal a draw whose
	// watch later rolls back.
	s.mu.Lock()
	revealsDraw := s.activeDraw != nil && !s.activeDraw.Revealed
	watched, next, err := s.movieRepo.WatchCurrentDraw(ctx, time.Now().UTC(), revealsDraw)
	if err != nil {
		s.mu.Unlock()
		return nil, err
	}

	revealed := s.finishWatchLocked()
	s.mu.Unlock()

	s.notifyWatchReveal(revealed, next)
	return watched, nil
}

// finishWatchLocked applies the in-memory half of a committed watch. Callers
// hold s.mu and notify OnRevealed only after releasing it.
func (s *Service) finishWatchLocked() *ActiveDraw {
	// Mark revealed before clearing so the timer and manual paths do not notify again.
	var revealed *ActiveDraw
	if s.activeDraw != nil && !s.activeDraw.Revealed {
		s.activeDraw.Revealed = true
		ap := *s.activeDraw
		revealed = &ap
	}
	s.activeDraw = nil
	s.cancelAutoRevealLocked()
	return revealed
}

func (s *Service) notifyWatchReveal(revealed *ActiveDraw, next *domain.User) {
	if revealed != nil && s.drawCfg.OnRevealed != nil {
		s.drawCfg.OnRevealed(Reveal{ActiveDraw: *revealed, NextUp: next})
	}
}

// ActiveDraw reports the in-flight draw, or ok=false when none is active.
func (s *Service) ActiveDraw() (ActiveDraw, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.activeDraw == nil {
		return ActiveDraw{}, false
	}
	return *s.activeDraw, true
}

// DrawInProgress reports whether the pool is held for an unrevealed draw. It is
// true even when the client skips the reel animation.
func (s *Service) DrawInProgress() bool {
	_, ok := s.heldDraw()
	return ok
}

// RevealCurrentDraw reveals the active draw at most once and reports whether
// this call flipped it. A duplicate confirm is a silent no-op.
func (s *Service) RevealCurrentDraw() (ActiveDraw, bool) {
	ap, flipped, _ := s.RevealCurrentDrawContext(context.Background())
	return ap, flipped
}

// RevealCurrentDrawContext is RevealCurrentDraw with a context and the durable
// error.
func (s *Service) RevealCurrentDrawContext(ctx context.Context) (ActiveDraw, bool, error) {
	return s.revealActive(ctx, 0, false)
}

// revealActive reveals the active draw and notifies OnRevealed exactly once.
// With requireGen it reveals only draw gen, so a stale timer cannot reveal a
// replacement; manual confirms target whatever draw is current.
func (s *Service) revealActive(ctx context.Context, gen uint64, requireGen bool) (ActiveDraw, bool, error) {
	s.mu.Lock()
	if s.activeDraw == nil ||
		s.activeDraw.Revealed ||
		(requireGen && (s.drawGen != gen || s.closed)) {
		s.mu.Unlock()
		return ActiveDraw{}, false, nil
	}

	// Durable write first: on failure the draw stays held and nothing is published.
	next, err := s.movieRepo.RevealDrawAndAdvanceNextUp(ctx, s.activeDraw.MovieID, time.Now().UTC())
	if err != nil {
		if requireGen {
			s.retryAutoRevealLocked(gen)
		}
		s.mu.Unlock()
		s.notifyRevealError(err)
		return ActiveDraw{}, false, err
	}
	s.activeDraw.Revealed = true
	ap := *s.activeDraw
	s.cancelAutoRevealLocked()
	// Notify outside the lock: OnRevealed re-enters broker/handler code.
	s.mu.Unlock()
	if s.drawCfg.OnRevealed != nil {
		s.drawCfg.OnRevealed(Reveal{ActiveDraw: ap, NextUp: next})
	}
	return ap, true, nil
}
