package nextpicker

import (
	"context"

	"moviepickarr/internal/domain"
)

type Service interface {
	Get(ctx context.Context) (*domain.User, error)
	Set(ctx context.Context, userID int) error
}

type service struct {
	nextPickerRepo domain.NextPickerRepo
	userRepo       domain.UserRepo
}

func NewService(nextPickerRepo domain.NextPickerRepo, userRepo domain.UserRepo) Service {
	return &service{
		nextPickerRepo: nextPickerRepo,
		userRepo:       userRepo,
	}
}

func (s *service) Get(ctx context.Context) (*domain.User, error) {
	nextPicker, err := s.nextPickerRepo.Get(ctx)
	if err != nil {
		return nil, err
	}

	return nextPicker, nil
}

func (s *service) Set(ctx context.Context, userID int) error {
	return s.nextPickerRepo.Set(ctx, userID)
}
