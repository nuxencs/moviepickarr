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

// maxPoolSize is the per-user cap on pooled movies. It is enforced atomically
// at the repository layer (see PromoteToPoolIfRoom) so concurrent promotions
// cannot overshoot it.
const maxPoolSize = 3

// DefaultAutoRevealDelay is how long after a draw the reveal fires by itself
// when no client confirms. The server owns this deadline outright: it rides
// every draw payload as `revealAt`, and clients derive their confirm
// countdown from it: there is no client-side copy to keep in sync.
const DefaultAutoRevealDelay = 16500 * time.Millisecond

// A fired time.AfterFunc cannot be reused. When its durable Reveal write fails,
// schedule a small bounded retry instead of leaving autoRevealArmed true for a
// timer that has already completed. The admin error hook still receives every
// failure, and a manual confirm can succeed between retries.
const (
	autoRevealRetryDelay = time.Second
	maxAutoRevealRetries = 3
)

// watchCurrentAndAdvanceNextUpStore is the consumer-side port for the one
// lifecycle transition whose durable state crosses movies and next_up.
type watchCurrentAndAdvanceNextUpStore interface {
	WatchCurrentAndAdvanceNextUp(
		ctx context.Context,
		watchedAt time.Time,
	) (watched *domain.Movie, next *domain.User, changed bool, err error)
}

// editMovieStore is the transaction-bound edit command. Ownership, watched
// state, movie fields, enrichment staleness, and the response read share one
// writer transaction so a failed edit has no durable fragment.
type editMovieStore interface {
	EditMovie(
		ctx context.Context,
		movieID, actorID int,
		title string,
		target domain.MovieIdentityTarget,
		watchedAt *time.Time,
	) (movie *domain.Movie, identityChanged bool, err error)
}

// drawLifecycleStore owns the durable half of Draw and Reveal. StartDraw commits
// the movie's pool -> current transition and its concealed Pending acquisition
// together. RevealDraw persists the visibility boundary before any in-memory
// flip or client publication. ConcealedCurrentDraw restores a draw whose process
// was restarted before Reveal.
type drawLifecycleStore interface {
	StartDraw(
		ctx context.Context,
		movieID int,
		drawnAt, revealAt time.Time,
		drawClientID string,
	) error
	RevealDraw(ctx context.Context, movieID int, revealedAt time.Time) error
	ConcealedCurrentDraw(
		ctx context.Context,
	) (movieID int, drawnAt, revealAt time.Time, drawClientID string, found bool, err error)
}

type movieStore interface {
	domain.MovieRepo
	watchCurrentAndAdvanceNextUpStore
	editMovieStore
	drawLifecycleStore
}

// ActiveDraw records the most recent random draw so a reloading client, one
// that joined late, or one that dropped the SSE event can resume the draw-reveal
// spin instead of jumping straight to the result. A concealed draw is restored
// from its durable Acquisition after a server restart.
type ActiveDraw struct {
	MovieID int
	// Generation binds post-publication timer arming and stale deadline
	// callbacks to this exact process-local draw.
	Generation uint64
	DrawnAt    time.Time
	// RevealAt is the server's auto-reveal deadline: the instant the reveal
	// fires by itself if no client confirms first. Clients time the confirm
	// countdown off it (revealAt − serverNow, immune to client clock skew).
	RevealAt time.Time
	// DrawClientID is the client that clicked Draw. Only that client shows the
	// reel's confirm button; everyone else's reel closes when the draw is revealed.
	DrawClientID string
	// Revealed flips true once the draw has been confirmed (drawer pressed the
	// button or its countdown filled). A reload then shows the result directly
	// instead of re-opening the reel.
	Revealed bool
}

// DrawResult is the complete publication snapshot of one successful draw.
// Candidates is the exact pool that was eligible before the winner became
// current. ActiveDraw is copied from the same lock boundary, so callers never
// have to reconstruct either value after later pool mutations can proceed.
type DrawResult struct {
	Movie      *domain.Movie
	Candidates []*domain.Movie
	ActiveDraw ActiveDraw
}

// DrawConfig wires the server-owned auto-reveal into the Service. The zero
// value works for callers that don't care about the reveal (unit tests):
// the default delay applies and a nil OnRevealed just isn't notified.
type DrawConfig struct {
	// AutoRevealDelay overrides DefaultAutoRevealDelay when > 0.
	AutoRevealDelay time.Duration
	// RandomIndex chooses one candidate index in [0, n). Nil uses the process
	// random source. Tests inject it to make selection deterministic.
	RandomIndex func(n int) int
	// StartTimer schedules fn asynchronously once after d and returns a stop
	// func. Nil uses time.AfterFunc; tests inject their own to drive the
	// deadline by hand.
	StartTimer func(d time.Duration, fn func()) (stop func())
	// OnRevealed observes every reveal flip (manual confirm, auto-reveal, or an
	// early watch) exactly once per draw. The server wires it to the
	// movie:revealed broadcast so every client closes its reel off one frame.
	OnRevealed func(ActiveDraw)
	// OnRevealError observes a failed durable Reveal. The active draw stays
	// unrevealed and no OnRevealed callback runs. Nil is allowed.
	OnRevealError func(error)
}

// Service owns the movie lifecycle: stash/pool moves, watched history, and the
// whole draw lifecycle: the in-memory active draw, the auto-reveal deadline
// and its timer, and the reveal-once flip behind the cross-client reveal.
type Service struct {
	movieRepo movieStore
	drawCfg   DrawConfig

	mu                sync.Mutex
	activeDraw        *ActiveDraw
	stopAutoReveal    func()
	autoRevealArmed   bool
	autoRevealRetries int
	closed            bool
	// drawGen counts draws so an auto-reveal timer can confine itself to the
	// draw it was armed for: a watch + fresh draw bumps it, so a stale timer
	// that already fired reveals nothing instead of the replacement draw.
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

// resumeConcealedDraw restores the one durable draw that did not cross Reveal
// before a restart. The persisted deadline remains authoritative. An elapsed
// deadline schedules immediately; a future deadline keeps the Held draw hidden
// until its remaining time passes.
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

// Close permanently stops auto-reveal scheduling and drops any pending timer;
// used on server shutdown.
func (s *Service) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	s.cancelAutoRevealLocked()
}

// armAutoRevealLocked (re)arms the auto-reveal for the active draw. There is
// only ever one active draw, so a prior pending timer is stopped first. The
// timer is bound to gen (the draw's generation) so that when it fires it only
// reveals that draw, never one that replaced it. Callers hold s.mu.
func (s *Service) armAutoRevealLocked(gen uint64) {
	s.cancelAutoRevealLocked()
	delay := max(time.Until(s.activeDraw.RevealAt), 0)
	s.scheduleAutoRevealLocked(gen, delay)
}

// scheduleAutoRevealLocked installs one timer without changing the retry
// counter. Callers hold s.mu and have already retired any previous timer.
func (s *Service) scheduleAutoRevealLocked(gen uint64, delay time.Duration) {
	s.stopAutoReveal = s.drawCfg.StartTimer(delay, func() {
		// Guarded by gen: a late manual confirm, a watch, or a watch-then-redraw
		// all leave this deadline a harmless no-op.
		_, _, _ = s.revealActive(context.Background(), gen, true)
	})
	s.autoRevealArmed = true
}

// retryAutoRevealLocked retires the one-shot timer that just failed and, while
// this exact draw remains active, schedules a bounded retry. Once the retry
// budget is exhausted autoRevealArmed stays false, so state never claims that a
// dead timer is still responsible for the Reveal.
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

// cancelAutoRevealLocked stops a pending auto-reveal: a manual confirm won
// the race, the draw was watched, or the server is shutting down. Callers
// hold s.mu.
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
// (already-pooled is a no-op) and reports whether a real transition happened.
func (s *Service) MoveToPool(ctx context.Context, id int) (bool, error) {
	// The held-draw snapshot and the promotion must sit on one side of draw
	// publication. Otherwise a draw can remove one DB pool row after this method
	// derives maxPoolSize, letting the held winner plus the promoted rows exceed
	// the member's cap.
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

	// No row transitioned. Disambiguate against committed state: a movie already
	// in the pool (e.g. a duplicate click that lost the race) is an idempotent
	// no-op; so is the active held winner, which every client-facing read still
	// projects into that pool. A still-stashed movie means the pool cap was hit;
	// any other status is an illegal source for a promotion.
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

// poolLimitLocked is the per-user pool cap as it applies to promoting movie id.
// A held draw (see withHeldDraw) still occupies a tile in its adder's pool, so
// it has to keep costing them a slot: counting only the rows with status "pool"
// would hand that member a free fourth movie for the length of every draw.
// Callers hold s.mu through the resulting promotion.
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
// whether a real transition happened.
//
// While a draw is unrevealed the whole pool is frozen: the held winner sits in
// the pool as a normal tile but is really "current", so demoting it would fail
// where every other tile succeeds — and that difference tells whoever tries
// which movie was drawn, before the reel lands. One answer for every tile keeps
// the draw secret, and the pool the reel is spinning over stays put.
func (s *Service) MoveToStash(ctx context.Context, id int) (bool, error) {
	// Serialize the draw-state decision through the status transition. A draw
	// that wins this lock freezes every pool tile; a demotion that wins changes
	// the candidate set before the draw selects from it.
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

	// No row transitioned: already-stashed is an idempotent no-op, anything else
	// (watched/current/missing) is an illegal source for a demotion.
	movie, err := s.movieRepo.FindByID(ctx, id)
	if err != nil {
		return false, err
	}
	if movie.Status == "stash" {
		return false, nil
	}
	return false, domain.ErrInvalidState
}

// Delete removes a stash or pool row. poolLocked is the pool lock, read by the
// caller (the move handler reads it the same way): the ordering of the two
// refusals belongs here, next to the draw the service owns.
func (s *Service) Delete(ctx context.Context, id int, poolLocked bool) error {
	// Keep the lifecycle read and delete on one side of draw publication. In
	// particular, a stale "pool" read must never authorize deleting a winner
	// after DrawRandom has persisted it as current.
	s.mu.Lock()
	defer s.mu.Unlock()

	movie, err := s.movieRepo.FindByID(ctx, id)
	if err != nil {
		return err
	}

	// Same reasoning as MoveToStash: while a draw is unrevealed, every pool tile
	// answers alike (the held winner included, which is why this runs before the
	// status check below — it is "current", not "pool"). Stashes stay deletable.
	if held, ok := s.heldDrawLocked(); ok && (movie.Status == "pool" || movie.ID == held.MovieID) {
		return domain.ErrDrawInProgress
	}

	// A locked pool has a fixed set of candidates, and deleting a pooled movie
	// shrinks it just as surely as demoting one does, so the lock refuses both.
	// The stash sits outside it: adds aren't lock-checked, so deletes aren't
	// either.
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

// GetForDisplay returns one movie as clients may see it. A held winner still
// reads as pooled until reveal, matching every pool listing and preventing the
// detail endpoint from identifying it while the reel is in flight.
//
// Command handlers use Get instead: authorization and mutations need the
// persisted lifecycle state, not this display projection.
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

// Pooled is the pool as clients may see it, which is not the same as the rows
// with status "pool": a drawn-but-unrevealed movie is held in it (see
// withHeldDraw). Everything that renders a pool reads through here.
func (s *Service) Pooled(ctx context.Context) ([]*domain.Movie, error) {
	movies, err := s.movieRepo.FindByStatus(ctx, "pool")
	if err != nil {
		return nil, err
	}

	return s.withHeldDraw(ctx, movies, 0)
}

// heldDraw returns the active draw while it is still unrevealed: the window in
// which the pool must not give the winner away. Everything that has to behave
// differently during the ceremony asks here.
func (s *Service) heldDraw() (ActiveDraw, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.heldDrawLocked()
}

// heldDrawLocked is heldDraw for callers that already own s.mu.
func (s *Service) heldDrawLocked() (ActiveDraw, bool) {
	if s.activeDraw == nil || s.activeDraw.Revealed {
		return ActiveDraw{}, false
	}
	return *s.activeDraw, true
}

// isNotFound covers both shapes a repo may report a missing row with: the sqlite
// repo wraps the miss as domain.ErrNotFound, others return sql.ErrNoRows bare.
func isNotFound(err error) bool {
	return errors.Is(err, domain.ErrNotFound) || errors.Is(err, sql.ErrNoRows)
}

// asHeldPoolMovie projects the active unrevealed winner into the pool without
// mutating the repository-owned record. The bool reports whether projection
// happened.
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
	return &cloned
}

// withHeldDraw puts a drawn-but-unrevealed movie back into a pool listing.
//
// DrawRandom flips the winner to "current" the moment it draws, so every pool
// read loses that tile immediately — while the reel is still spinning. Clients
// that were already on the page don't notice (their cached pool isn't refreshed
// until the reel lands), but a reload mid-spin, or any client opening the board
// during the draw, fetches the post-draw pool and the missing tile gives the
// winner away before the reveal. Holding the movie in the view until the draw
// is revealed makes the pool read the same for everyone, whatever their cache
// state, and the reveal-time refresh drops it.
//
// The row keeps its DB status ("current", so it can't be drawn again), only the
// copy handed out reads as pooled; callers bucket on Status. userID scopes the
// hold to one member's pool (0 = the whole pool). The movie is inserted in
// title order, matching the repo's sort, so the tile doesn't jump to the end.
func (s *Service) withHeldDraw(ctx context.Context, pooled []*domain.Movie, userID int) ([]*domain.Movie, error) {
	held, ok := s.heldDraw()
	if !ok {
		return pooled, nil
	}

	movie, err := s.movieRepo.FindByID(ctx, held.MovieID)
	if err != nil {
		// The draw is in memory and the row is not: a deleted movie mid-draw.
		// The listing is still correct without it, so don't fail the read.
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
	// A draw that landed between the listing query and the read above leaves the
	// row in both: hand it out once.
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

// PooledByUserID is one member's slice of the same view: the held draw shows up
// only in its own adder's pool.
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

// DrawRandom selects a random pooled movie as the current draw. clientID is
// the opaque id of the client that initiated the draw (see ActiveDraw). It
// gates who sees the reel's confirm button; "" is acceptable (no drawer).
func (s *Service) DrawRandom(ctx context.Context, clientID string) (*DrawResult, error) {
	// Keep the persisted status flip and the in-memory hold one publication.
	// Client-facing reads may query the repository concurrently, but they block
	// on heldDraw before returning and therefore see either side in full.
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

	// Detach the candidate snapshot before UpdateStatus. Repository fakes may
	// expose shared movie pointers, and a later promotion or edit must not mutate
	// the already-published reel through an alias.
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

// StartAutoReveal arms the deadline for the active draw after its movie:drawn
// event has been published. Deferring the timer until that boundary prevents a
// short deadline or slow payload build from broadcasting movie:revealed first.
// The deadline stays anchored to DrawnAt; publication time is subtracted rather
// than granting a fresh delay. Stale, duplicate, and post-Close calls are no-ops.
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

// MarkCurrentAsWatchedAndAdvanceNextUp persists the watched movie and next-up
// handoff atomically. The active draw stays untouched until that transaction
// commits, so a failed handoff remains retryable and emits no reveal.
func (s *Service) MarkCurrentAsWatchedAndAdvanceNextUp(
	ctx context.Context,
) (watched *domain.Movie, next *domain.User, changed bool, err error) {
	// Hold the draw mutex across the durable transition and its in-memory
	// counterpart. The timer may wait here, but can never reveal a transaction
	// that later rolls back.
	s.mu.Lock()
	watched, next, changed, err = s.movieRepo.WatchCurrentAndAdvanceNextUp(ctx, time.Now().UTC())
	if err != nil {
		s.mu.Unlock()
		return nil, nil, false, err
	}

	revealed := s.finishWatchLocked()
	s.mu.Unlock()

	s.notifyWatchReveal(revealed)
	return watched, next, changed, nil
}

// finishWatchLocked applies the process-local half of a committed watch.
// Callers hold s.mu and notify OnRevealed only after releasing it.
func (s *Service) finishWatchLocked() *ActiveDraw {
	// An early watch is also a reveal for clients still running the reel. Mark
	// it before clearing so this path and the timer/manual paths notify once.
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

func (s *Service) notifyWatchReveal(revealed *ActiveDraw) {
	if revealed != nil && s.drawCfg.OnRevealed != nil {
		s.drawCfg.OnRevealed(*revealed)
	}
}

// ActiveDraw reports the in-flight draw (movie id + when it was drawn) that
// drives the cross-client draw-reveal spin, or ok=false when none is active
// (no current draw, or the current draw was already marked watched). A server
// restart restores a concealed draw from its durable Acquisition.
func (s *Service) ActiveDraw() (ActiveDraw, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.activeDraw == nil {
		return ActiveDraw{}, false
	}
	return *s.activeDraw, true
}

// DrawInProgress reports whether the pool is held for an unrevealed draw.
// Unlike the client reel phase, this is the server-owned mutation gate: it is
// true even when reduced motion or a lone candidate skips the animation.
func (s *Service) DrawInProgress() bool {
	_, ok := s.heldDraw()
	return ok
}

// RevealCurrentDraw marks the active draw as revealed: the drawer confirmed,
// or the auto-reveal deadline fired. The flip happens at most once per draw:
// the winning call cancels the pending timer and notifies OnRevealed exactly
// once; a duplicate confirm is a silent no-op. It reports the draw plus
// whether this call flipped it; ok=false when there's no active draw or it
// was already revealed.
func (s *Service) RevealCurrentDraw() (ActiveDraw, bool) {
	ap, flipped, _ := s.RevealCurrentDrawContext(context.Background())
	return ap, flipped
}

// RevealCurrentDrawContext is RevealCurrentDraw with a caller-owned context and
// a durable error result. Kept beside the compatibility wrapper so handlers can
// adopt explicit error reporting without changing the reveal-once semantics.
func (s *Service) RevealCurrentDrawContext(ctx context.Context) (ActiveDraw, bool, error) {
	return s.revealActive(ctx, 0, false)
}

// revealActive flips the active draw to revealed, cancels its pending timer,
// and notifies OnRevealed exactly once. When requireGen is set it only
// proceeds while the active draw is still generation gen (the draw the
// auto-reveal timer was armed for) so a stale deadline can't reveal a
// replacement. Manual confirms pass requireGen=false: they target whatever
// draw is current.
func (s *Service) revealActive(ctx context.Context, gen uint64, requireGen bool) (ActiveDraw, bool, error) {
	s.mu.Lock()
	if s.activeDraw == nil ||
		s.activeDraw.Revealed ||
		(requireGen && (s.drawGen != gen || s.closed)) {
		s.mu.Unlock()
		return ActiveDraw{}, false, nil
	}

	// The durable boundary comes first. If it fails, the draw stays held and
	// clients receive no Reveal publication. A failed auto-reveal retires its
	// spent one-shot timer and schedules a bounded retry.
	if err := s.movieRepo.RevealDraw(ctx, s.activeDraw.MovieID, time.Now().UTC()); err != nil {
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
		s.drawCfg.OnRevealed(ap)
	}
	return ap, true, nil
}
