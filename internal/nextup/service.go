package nextup

import (
	"context"
	"database/sql"
	"errors"

	"moviepickarr/internal/domain"
)

// Service owns next-up reads. The atomic watched movie plus turn handoff lives
// in the movie store because it spans both durable records.
type Service struct {
	nextUpRepo domain.NextUpRepo
	userRepo   domain.UserRepo
}

func NewService(nextUpRepo domain.NextUpRepo, userRepo domain.UserRepo) *Service {
	return &Service{
		nextUpRepo: nextUpRepo,
		userRepo:   userRepo,
	}
}

// Get returns the member whose turn it is. A fresh install has no next up
// yet, so Get seeds it with the first roster member before answering;
// sql.ErrNoRows therefore means the roster itself is empty.
func (s *Service) Get(ctx context.Context) (*domain.User, error) {
	nextUp, err := s.nextUpRepo.Get(ctx)
	if err == nil {
		return nextUp, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}

	users, err := s.userRepo.List(ctx)
	if err != nil {
		return nil, err
	}
	if len(users) == 0 {
		return nil, sql.ErrNoRows
	}
	if err := s.nextUpRepo.Set(ctx, users[0].ID); err != nil {
		return nil, err
	}

	return s.nextUpRepo.Get(ctx)
}
