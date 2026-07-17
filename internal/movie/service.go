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

// ActiveDraw records the most recent random draw so a reloading client — or one
// that joined late / dropped the SSE event — can resume the draw-reveal spin
// instead of jumping straight to the result. Held in memory only.
type ActiveDraw struct {
	MovieID int
	DrawnAt time.Time
	// DrawClientID is the client that clicked Draw. Only that client shows the
	// reel's confirm button; everyone else's reel closes when the draw is revealed.
	DrawClientID string
	// Revealed flips true once the draw has been confirmed (drawer pressed the
	// button or its countdown filled). A reload then shows the result directly
	// instead of re-opening the reel.
	Revealed bool
}

// Service owns the movie lifecycle: stash/pool moves, watched history, the
// draw, and the in-memory active-draw state behind the cross-client reveal.
type Service struct {
	movieRepo domain.MovieRepo

	mu         sync.Mutex
	activeDraw *ActiveDraw
}

func NewService(movieRepo domain.MovieRepo) *Service {
	return &Service{
		movieRepo: movieRepo,
	}
}

func (s *Service) AddToStash(ctx context.Context, title string, userID int) (*domain.Movie, error) {
	return s.movieRepo.Add(ctx, title, "stash", userID)
}

// MoveToPool promotes a stashed movie into its owner's pool. It is idempotent
// (already-pooled is a no-op) and reports whether a real transition happened.
func (s *Service) MoveToPool(ctx context.Context, id int) (bool, error) {
	n, err := s.movieRepo.PromoteToPoolIfRoom(ctx, id, maxPoolSize)
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

// MoveToStash demotes a pooled movie back to the stash. Idempotent; reports
// whether a real transition happened.
func (s *Service) MoveToStash(ctx context.Context, id int) (bool, error) {
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

func (s *Service) Delete(ctx context.Context, id int) error {
	movie, err := s.movieRepo.FindByID(ctx, id)
	if err != nil {
		return err
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

func (s *Service) Pooled(ctx context.Context) ([]*domain.Movie, error) {
	return s.movieRepo.FindByStatus(ctx, "pool")
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

func (s *Service) PooledByUserID(ctx context.Context, userID int) ([]*domain.Movie, error) {
	movies, err := s.movieRepo.FindByUserIDAndStatus(ctx, userID, "pool")
	if err != nil {
		return nil, err
	}

	return movies, nil
}

func (s *Service) StashedByUserID(ctx context.Context, userID int) ([]*domain.Movie, error) {
	movies, err := s.movieRepo.FindByUserIDAndStatus(ctx, userID, "stash")
	if err != nil {
		return nil, err
	}

	return movies, nil
}

// DrawRandom selects a random pooled movie as the current draw. clientID is
// the opaque id of the client that initiated the draw (see ActiveDraw) — it
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

	s.mu.Lock()
	s.activeDraw = &ActiveDraw{MovieID: movie.ID, DrawnAt: time.Now().UTC(), DrawClientID: clientID}
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

	// The draw is done — clear the active spin so a reload now shows the result
	// directly rather than replaying the reel.
	s.mu.Lock()
	s.activeDraw = nil
	s.mu.Unlock()

	current.Status = "watched"
	current.WatchedAt = &watchedAt

	return current, nil
}

// ActiveDraw reports the in-flight draw (movie id + when it was drawn) that
// drives the cross-client draw-reveal spin, or ok=false when none is active
// (no current movie, or the current movie was already marked watched). It is
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

// RevealCurrentDraw marks the active draw as revealed (the drawer confirmed,
// or the reel's countdown elapsed). It reports the draw plus whether this call
// is the one that flipped it — so the caller broadcasts movie:revealed exactly
// once, and a duplicate confirm is a silent no-op. ok=false when there's no
// active draw or it was already revealed.
func (s *Service) RevealCurrentDraw() (ActiveDraw, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.activeDraw == nil || s.activeDraw.Revealed {
		return ActiveDraw{}, false
	}
	s.activeDraw.Revealed = true
	return *s.activeDraw, true
}
