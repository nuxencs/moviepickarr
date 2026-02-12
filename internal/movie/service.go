package movie

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"time"

	"moviepickarr/internal/domain"
)

type Service interface {
	AddToPool(ctx context.Context, title, link string, userID int) (*domain.Movie, error)
	AddToStash(ctx context.Context, title, link string, userID int) (*domain.Movie, error)
	MoveToPool(ctx context.Context, id int) error
	MoveToStash(ctx context.Context, id int) error
	Delete(ctx context.Context, id int) error
	Update(ctx context.Context, id int, title, link string, watchedAt *time.Time) (*domain.Movie, error)
	Get(ctx context.Context, id int) (*domain.Movie, error)
	List(ctx context.Context) ([]*domain.Movie, error)
	Pooled(ctx context.Context) ([]*domain.Movie, error)
	Watched(ctx context.Context) ([]*domain.Movie, error)
	Current(ctx context.Context) (*domain.Movie, error)
	PooledByUserID(ctx context.Context, userID int) ([]*domain.Movie, error)
	StashedByUserID(ctx context.Context, userID int) ([]*domain.Movie, error)
	PickRandom(ctx context.Context) (*domain.Movie, error)
	MarkCurrentAsWatched(ctx context.Context) (*domain.Movie, error)
}

type service struct {
	movieRepo    domain.MovieRepo
	settingsRepo domain.SettingsRepo
}

func NewService(movieRepo domain.MovieRepo, settingsRepo domain.SettingsRepo) Service {
	return &service{
		movieRepo:    movieRepo,
		settingsRepo: settingsRepo,
	}
}

func (s *service) AddToPool(ctx context.Context, title, link string, userID int) (*domain.Movie, error) {
	pooled, err := s.movieRepo.CountByUserIDAndStatus(ctx, userID, "pool")
	if err != nil {
		return nil, err
	}

	if pooled >= 3 {
		return nil, domain.ErrPoolLimitReached
	}

	poolLocked, err := s.settingsRepo.FindByKey(ctx, "pool_locked")
	if err != nil {
		return nil, err
	}

	parseBool, err := strconv.ParseBool(poolLocked)
	if err != nil {
		return nil, err
	}

	if parseBool {
		return nil, domain.ErrPoolLocked
	}

	return s.movieRepo.Add(ctx, title, link, "pool", userID)
}

func (s *service) AddToStash(ctx context.Context, title, link string, userID int) (*domain.Movie, error) {
	return s.movieRepo.Add(ctx, title, link, "stash", userID)
}

func (s *service) MoveToPool(ctx context.Context, id int) error {
	movie, err := s.movieRepo.FindByID(ctx, id)
	if err != nil {
		return err
	}

	if movie.Status != "stash" {
		return domain.ErrInvalidState
	}

	if err = s.movieRepo.UpdateStatus(ctx, id, "pool"); err != nil {
		return err
	}

	return nil
}

func (s *service) MoveToStash(ctx context.Context, id int) error {
	movie, err := s.movieRepo.FindByID(ctx, id)
	if err != nil {
		return err
	}

	if movie.Status != "pool" {
		return domain.ErrInvalidState
	}

	if err = s.movieRepo.UpdateStatus(ctx, id, "stash"); err != nil {
		return err
	}

	return nil
}

func (s *service) Delete(ctx context.Context, id int) error {
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

func (s *service) Update(ctx context.Context, id int, title, link string, watchedAt *time.Time) (*domain.Movie, error) {
	movie, err := s.movieRepo.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}

	if movie.Status != string(domain.MovieStatusWatched) && watchedAt != nil {
		return nil, domain.ErrInvalidInput
	}

	if err = s.movieRepo.UpdateTitleAndLink(ctx, id, title, link); err != nil {
		return nil, err
	}

	if watchedAt != nil {
		if err = s.movieRepo.UpdateWatchedAt(ctx, id, watchedAt.UTC()); err != nil {
			return nil, err
		}
	}

	return s.movieRepo.FindByID(ctx, id)
}

func (s *service) Get(ctx context.Context, id int) (*domain.Movie, error) {
	return s.movieRepo.FindByID(ctx, id)
}

func (s *service) List(ctx context.Context) ([]*domain.Movie, error) {
	return s.movieRepo.List(ctx)
}

func (s *service) Pooled(ctx context.Context) ([]*domain.Movie, error) {
	return s.movieRepo.FindByStatus(ctx, "pool")
}

func (s *service) Watched(ctx context.Context) ([]*domain.Movie, error) {
	return s.movieRepo.FindByStatus(ctx, "watched")
}

func (s *service) Current(ctx context.Context) (*domain.Movie, error) {
	return s.movieRepo.GetCurrent(ctx)
}

func (s *service) PooledByUserID(ctx context.Context, userID int) ([]*domain.Movie, error) {
	movies, err := s.movieRepo.FindByUserIDAndStatus(ctx, userID, "pool")
	if err != nil {
		return nil, err
	}

	return movies, nil
}

func (s *service) StashedByUserID(ctx context.Context, userID int) ([]*domain.Movie, error) {
	movies, err := s.movieRepo.FindByUserIDAndStatus(ctx, userID, "stash")
	if err != nil {
		return nil, err
	}

	return movies, nil
}

func (s *service) PickRandom(ctx context.Context) (*domain.Movie, error) {
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
		return nil, domain.ErrCurrentMovieExist
	}

	movie, err := s.movieRepo.GetRandomPooled(ctx)
	if err != nil {
		return nil, err
	}

	if err = s.movieRepo.UpdateStatus(ctx, movie.ID, "current"); err != nil {
		return nil, err
	}

	return movie, nil
}

func (s *service) MarkCurrentAsWatched(ctx context.Context) (*domain.Movie, error) {
	current, err := s.movieRepo.GetCurrent(ctx)
	if err != nil {
		return nil, err
	}

	if current == nil {
		return nil, domain.ErrNoCurrentMovie
	}

	watchedAt := time.Now().UTC()
	if err = s.movieRepo.MarkAsWatched(ctx, current.ID, watchedAt); err != nil {
		return nil, err
	}

	current.Status = "watched"
	current.WatchedAt = &watchedAt

	return current, nil
}
