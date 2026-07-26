package movie

import (
	"context"
	"database/sql"
	"errors"
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

// ActiveDraw records the most recent random draw so a reloading client — or one
// that joined late / dropped the SSE event — can resume the draw-reveal spin
// instead of jumping straight to the result. Held in memory only: a server
// restart mid-draw forgets it, and clients then show the result directly
// (the ceremony is lost, the state stays correct).
type ActiveDraw struct {
	MovieID int
	DrawnAt time.Time
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

// DrawConfig wires the server-owned auto-reveal into the Service. The zero
// value works for callers that don't care about the reveal (unit tests):
// the default delay applies and a nil OnRevealed just isn't notified.
type DrawConfig struct {
	// AutoRevealDelay overrides DefaultAutoRevealDelay when > 0.
	AutoRevealDelay time.Duration
	// StartTimer schedules fn once after d and returns a stop func. Nil uses
	// time.AfterFunc; tests inject their own to drive the deadline by hand.
	StartTimer func(d time.Duration, fn func()) (stop func())
	// OnRevealed observes every reveal flip (manual confirm or auto-reveal)
	// exactly once per draw. The server wires it to the movie:revealed
	// broadcast so every client closes its reel off one frame.
	OnRevealed func(ActiveDraw)
}

// Service owns the movie lifecycle: stash/pool moves, watched history, and the
// whole draw lifecycle: the in-memory active draw, the auto-reveal deadline
// and its timer, and the reveal-once flip behind the cross-client reveal.
type Service struct {
	movieRepo domain.MovieRepo
	drawCfg   DrawConfig

	mu             sync.Mutex
	activeDraw     *ActiveDraw
	stopAutoReveal func()
	// drawGen counts draws so an auto-reveal timer can confine itself to the
	// draw it was armed for: a watch + fresh draw bumps it, so a stale timer
	// that already fired reveals nothing instead of the replacement draw.
	drawGen uint64
}

func NewService(movieRepo domain.MovieRepo, drawCfg DrawConfig) *Service {
	if drawCfg.AutoRevealDelay <= 0 {
		drawCfg.AutoRevealDelay = DefaultAutoRevealDelay
	}
	if drawCfg.StartTimer == nil {
		drawCfg.StartTimer = func(d time.Duration, fn func()) func() {
			t := time.AfterFunc(d, fn)
			return func() { t.Stop() }
		}
	}
	return &Service{
		movieRepo: movieRepo,
		drawCfg:   drawCfg,
	}
}

// Close drops any pending auto-reveal timer; used on server shutdown.
func (s *Service) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cancelAutoRevealLocked()
}

// armAutoRevealLocked (re)arms the auto-reveal for the active draw. There is
// only ever one active draw, so a prior pending timer is stopped first. The
// timer is bound to gen (the draw's generation) so that when it fires it only
// reveals that draw, never one that replaced it. Callers hold s.mu.
func (s *Service) armAutoRevealLocked(gen uint64) {
	s.cancelAutoRevealLocked()
	s.stopAutoReveal = s.drawCfg.StartTimer(s.drawCfg.AutoRevealDelay, func() {
		// Guarded by gen: a late manual confirm, a watch, or a watch-then-redraw
		// all leave this deadline a harmless no-op.
		s.revealActive(gen, true)
	})
}

// cancelAutoRevealLocked stops a pending auto-reveal: a manual confirm won
// the race, the draw was watched, or the server is shutting down. Callers
// hold s.mu.
func (s *Service) cancelAutoRevealLocked() {
	if s.stopAutoReveal != nil {
		s.stopAutoReveal()
		s.stopAutoReveal = nil
	}
}

func (s *Service) AddToStash(ctx context.Context, title string, userID int) (*domain.Movie, error) {
	return s.movieRepo.Add(ctx, title, "stash", userID)
}

// MoveToPool promotes a stashed movie into its owner's pool. It is idempotent
// (already-pooled is a no-op) and reports whether a real transition happened.
func (s *Service) MoveToPool(ctx context.Context, id int) (bool, error) {
	limit, err := s.poolLimit(ctx, id)
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
	// no-op; a still-stashed movie means the pool cap was hit; any other status
	// is an illegal source for a promotion.
	movie, err := s.movieRepo.FindByID(ctx, id)
	if err != nil {
		return false, err
	}
	switch movie.Status {
	case "pool":
		return false, nil
	case "stash":
		return false, domain.ErrPoolLimitReached
	default:
		return false, domain.ErrInvalidState
	}
}

// poolLimit is the per-user pool cap as it applies to promoting movie id. A
// held draw (see withHeldDraw) still occupies a tile in its adder's pool, so it
// has to keep costing them a slot: counting only the rows with status "pool"
// would hand that member a free fourth movie for the length of every draw.
func (s *Service) poolLimit(ctx context.Context, id int) (int, error) {
	held, ok := s.heldDraw()
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
	if held, ok := s.heldDraw(); ok {
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

func (s *Service) SetExternalIDs(ctx context.Context, id int, tmdbID *int, imdbID *string) error {
	return s.movieRepo.SetExternalIDs(ctx, id, tmdbID, imdbID)
}

// Delete removes a stash or pool row. poolLocked is the pool lock, read by the
// caller (the move handler reads it the same way): the ordering of the two
// refusals belongs here, next to the draw the service owns.
func (s *Service) Delete(ctx context.Context, id int, poolLocked bool) error {
	movie, err := s.movieRepo.FindByID(ctx, id)
	if err != nil {
		return err
	}

	// Same reasoning as MoveToStash: while a draw is unrevealed, every pool tile
	// answers alike (the held winner included, which is why this runs before the
	// status check below — it is "current", not "pool"). Stashes stay deletable.
	if held, ok := s.heldDraw(); ok && (movie.Status == "pool" || movie.ID == held.MovieID) {
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

func (s *Service) Update(ctx context.Context, id int, title string, watchedAt *time.Time) (*domain.Movie, error) {
	movie, err := s.movieRepo.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}

	if movie.Status != string(domain.MovieStatusWatched) && watchedAt != nil {
		return nil, domain.ErrInvalidInput
	}

	if err = s.movieRepo.UpdateTitle(ctx, id, title); err != nil {
		return nil, err
	}

	if watchedAt != nil {
		if err = s.movieRepo.UpdateWatchedAt(ctx, id, watchedAt.UTC()); err != nil {
			return nil, err
		}
	}

	return s.movieRepo.FindByID(ctx, id)
}

func (s *Service) Get(ctx context.Context, id int) (*domain.Movie, error) {
	return s.movieRepo.FindByID(ctx, id)
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
	if movie.Status != "current" || (userID != 0 && movie.AddedByID != userID) {
		return pooled, nil
	}
	// A draw that landed between the listing query and the read above leaves the
	// row in both: hand it out once.
	for _, m := range pooled {
		if m.ID == movie.ID {
			return pooled, nil
		}
	}

	shown := *movie
	shown.Status = "pool"
	at := len(pooled)
	for i, m := range pooled {
		if shown.Title < m.Title {
			at = i
			break
		}
	}
	out := make([]*domain.Movie, 0, len(pooled)+1)
	out = append(out, pooled[:at]...)
	out = append(out, &shown)
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
func (s *Service) DrawRandom(ctx context.Context, clientID string) (*domain.Movie, error) {
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

	movie, err := s.movieRepo.GetRandomPooled(ctx)
	if err != nil {
		return nil, err
	}

	if err = s.movieRepo.UpdateStatus(ctx, movie.ID, "current"); err != nil {
		return nil, err
	}

	drawnAt := time.Now().UTC()
	s.mu.Lock()
	s.drawGen++
	s.activeDraw = &ActiveDraw{
		MovieID:      movie.ID,
		DrawnAt:      drawnAt,
		RevealAt:     drawnAt.Add(s.drawCfg.AutoRevealDelay),
		DrawClientID: clientID,
	}
	s.armAutoRevealLocked(s.drawGen)
	s.mu.Unlock()

	return movie, nil
}

func (s *Service) MarkCurrentAsWatched(ctx context.Context) (*domain.Movie, error) {
	current, err := s.movieRepo.GetCurrent(ctx)
	if err != nil {
		return nil, err
	}

	if current == nil {
		return nil, domain.ErrNoCurrentDraw
	}

	watchedAt := time.Now().UTC()
	if err = s.movieRepo.MarkAsWatched(ctx, current.ID, watchedAt); err != nil {
		return nil, err
	}

	// The draw is done: clear the active spin (and its pending auto-reveal,
	// so a stale timer can't fire against the next draw) so a reload now shows
	// the result directly rather than replaying the reel.
	s.mu.Lock()
	s.activeDraw = nil
	s.cancelAutoRevealLocked()
	s.mu.Unlock()

	current.Status = "watched"
	current.WatchedAt = &watchedAt

	return current, nil
}

// ActiveDraw reports the in-flight draw (movie id + when it was drawn) that
// drives the cross-client draw-reveal spin, or ok=false when none is active
// (no current draw, or the current draw was already marked watched). It is
// in-memory only, consistent with the in-process event broker: a server
// restart drops it, which just means a reload won't replay the spin.
func (s *Service) ActiveDraw() (ActiveDraw, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.activeDraw == nil {
		return ActiveDraw{}, false
	}
	return *s.activeDraw, true
}

// RevealCurrentDraw marks the active draw as revealed: the drawer confirmed,
// or the auto-reveal deadline fired. The flip happens at most once per draw:
// the winning call cancels the pending timer and notifies OnRevealed exactly
// once; a duplicate confirm is a silent no-op. It reports the draw plus
// whether this call flipped it; ok=false when there's no active draw or it
// was already revealed.
func (s *Service) RevealCurrentDraw() (ActiveDraw, bool) {
	return s.revealActive(0, false)
}

// revealActive flips the active draw to revealed, cancels its pending timer,
// and notifies OnRevealed exactly once. When requireGen is set it only
// proceeds while the active draw is still generation gen (the draw the
// auto-reveal timer was armed for) so a stale deadline can't reveal a
// replacement. Manual confirms pass requireGen=false: they target whatever
// draw is current.
func (s *Service) revealActive(gen uint64, requireGen bool) (ActiveDraw, bool) {
	s.mu.Lock()
	if s.activeDraw == nil || s.activeDraw.Revealed || (requireGen && s.drawGen != gen) {
		s.mu.Unlock()
		return ActiveDraw{}, false
	}
	s.activeDraw.Revealed = true
	ap := *s.activeDraw
	s.cancelAutoRevealLocked()
	// Notify outside the lock: OnRevealed re-enters broker/handler code.
	s.mu.Unlock()
	if s.drawCfg.OnRevealed != nil {
		s.drawCfg.OnRevealed(ap)
	}
	return ap, true
}
