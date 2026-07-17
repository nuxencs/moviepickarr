package nextup

import (
	"context"

	"moviepickarr/internal/domain"
)

type Service interface {
	Get(ctx context.Context) (*domain.User, error)
	Set(ctx context.Context, userID int) error
}

type service struct {
	nextUpRepo domain.NextUpRepo
	userRepo   domain.UserRepo
}

func NewService(nextUpRepo domain.NextUpRepo, userRepo domain.UserRepo) Service {
	return &service{
		nextUpRepo: nextUpRepo,
		userRepo:   userRepo,
	}
}

func (s *service) Get(ctx context.Context) (*domain.User, error) {
	nextUp, err := s.nextUpRepo.Get(ctx)
	if err != nil {
		return nil, err
	}

	return nextUp, nil
}

func (s *service) Set(ctx context.Context, userID int) error {
	return s.nextUpRepo.Set(ctx, userID)
}
