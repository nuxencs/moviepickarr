package user

import (
	"context"
	"database/sql"
	"errors"

	"moviepickarr/internal/domain"
)

// Repo composes the roster methods here to keep them off the narrower UserRepo.
type Repo interface {
	domain.UserRepo
	domain.RosterRepo
}

// Service owns member management.
type Service struct {
	userRepo   Repo
	nextUpRepo domain.NextUpRepo
}

func NewService(userRepo Repo, nextUpRepo domain.NextUpRepo) *Service {
	return &Service{
		userRepo:   userRepo,
		nextUpRepo: nextUpRepo,
	}
}

// Create adds a member. The first member becomes next up, so the rotation has a
// starting point.
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

// Remove deletes a member who authored no movies and archives one who did, so
// watch-history attribution survives. The repo refuses the last active admin.
func (s *Service) Remove(ctx context.Context, id int) (domain.RemoveOutcome, error) {
	return s.userRepo.Remove(ctx, id)
}

// Restore reactivates an archived member. The caller re-issues a claim invite,
// since archiving stripped their credentials.
func (s *Service) Restore(ctx context.Context, id int) error {
	return s.userRepo.Restore(ctx, id)
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

// Roster returns every member, active and archived, with derived login state.
func (s *Service) Roster(ctx context.Context) ([]*domain.RosterMember, error) {
	return s.userRepo.Roster(ctx)
}

// SetRole changes an active member's role, moving Next up atomically when
// needed. Sessions stay valid because authorization reads the live role.
func (s *Service) SetRole(ctx context.Context, change domain.RoleChange) (domain.RoleChangeResult, error) {
	if change.MemberID <= 0 || !change.Role.Valid() {
		return domain.RoleChangeResult{}, domain.ErrInvalidInput
	}
	return s.userRepo.SetRole(ctx, change)
}
