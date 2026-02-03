package user

import (
	"context"
	"database/sql"
	"errors"
	"moviepickarr/internal/domain"
)

type Service interface {
	Create(ctx context.Context, name string) (*domain.User, error)
	Delete(ctx context.Context, id int) error
	Get(ctx context.Context, id int) (*domain.User, error)
	List(ctx context.Context) ([]*domain.User, error)
}

type service struct {
	userRepo       domain.UserRepo
	nextPickerRepo domain.NextPickerRepo
}

func NewService(userRepo domain.UserRepo, nextPickerRepo domain.NextPickerRepo) Service {
	return &service{
		userRepo:       userRepo,
		nextPickerRepo: nextPickerRepo,
	}
}

func (s *service) Create(ctx context.Context, name string) (*domain.User, error) {
	user, err := s.userRepo.Create(ctx, name)
	if err != nil {
		return nil, err
	}

	_, err = s.nextPickerRepo.Get(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			if setErr := s.nextPickerRepo.Set(ctx, user.ID); setErr != nil {
				return nil, setErr
			}
		} else {
			return nil, err
		}
	}

	return user, nil
}

func (s *service) Delete(ctx context.Context, id int) error {
	return s.userRepo.Delete(ctx, id)
}

func (s *service) Get(ctx context.Context, id int) (*domain.User, error) {
	return s.userRepo.FindByID(ctx, id)
}

func (s *service) List(ctx context.Context) ([]*domain.User, error) {
	users, err := s.userRepo.List(ctx)
	if err != nil {
		return nil, err
	}

	return users, nil
}
