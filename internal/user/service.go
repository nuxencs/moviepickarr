package user

import (
	"context"
	"database/sql"
	"errors"

	"moviepickarr/internal/domain"
)

// Service owns member management. Creation also seeds the next-up rotation on
// a fresh roster (see Create).
type Service struct {
	userRepo   domain.UserRepo
	nextUpRepo domain.NextUpRepo
}

func NewService(userRepo domain.UserRepo, nextUpRepo domain.NextUpRepo) *Service {
	return &Service{
		userRepo:   userRepo,
		nextUpRepo: nextUpRepo,
	}
}

// Create adds a member to the roster. The first member ever created becomes
// next up immediately, so the rotation has a starting point before the first
// draw.
func (s *Service) Create(ctx context.Context, name string) (*domain.User, error) {
	user, err := s.userRepo.Create(ctx, name)
	if err != nil {
		return nil, err
	}

	_, err = s.nextUpRepo.Get(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			if setErr := s.nextUpRepo.Set(ctx, user.ID); setErr != nil {
				return nil, setErr
			}
		} else {
			return nil, err
		}
	}

	return user, nil
}

func (s *Service) Delete(ctx context.Context, id int) error {
	return s.userRepo.Delete(ctx, id)
}

func (s *Service) Get(ctx context.Context, id int) (*domain.User, error) {
	return s.userRepo.FindByID(ctx, id)
}

func (s *Service) List(ctx context.Context) ([]*domain.User, error) {
	users, err := s.userRepo.List(ctx)
	if err != nil {
		return nil, err
	}

	return users, nil
}
