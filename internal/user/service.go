package user

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"moviepickarr/internal/domain"
)

// Repo is what the member service needs from persistence: the movie-board
// UserRepo plus the admin roster read and role write. One SqliteUserRepository
// satisfies all of it; composing the interfaces here keeps the roster/role
// methods off the narrower UserRepo the rest of the app depends on.
type Repo interface {
	domain.UserRepo
	domain.RosterRepo
}

// Service owns member management. Creation also seeds the next-up rotation on
// a fresh roster (see Create).
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

// Remove deletes or archives a member as one admin action, chosen by whether
// they authored movies: zero authored movies hard-deletes the row, one or more
// archives it so the group's watch-history attribution survives. It returns
// which path ran so the caller can report delete-vs-archive.
func (s *Service) Remove(ctx context.Context, id int) (domain.RemoveOutcome, error) {
	return s.userRepo.Remove(ctx, id)
}

// Restore reactivates an archived member (clears archived_at). Archiving stripped
// their credentials, so the caller re-issues a claim invite to let them log back
// in; this only reopens the membership.
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

// Roster returns the admin roster: every member, active and archived, with the
// presence-derived login state the admin surface renders.
func (s *Service) Roster(ctx context.Context) ([]*domain.RosterMember, error) {
	return s.userRepo.Roster(ctx)
}

// SetRole changes an active member's role after validating it against the
// app-owned enum. The repo enforces the last-admin guard; role is read live per
// request, so the change reflects on the member's next call without touching
// their sessions.
func (s *Service) SetRole(ctx context.Context, id int, role string) error {
	if role != domain.RoleMember && role != domain.RoleAdmin {
		return fmt.Errorf("%w: role must be %q or %q", domain.ErrInvalidInput, domain.RoleMember, domain.RoleAdmin)
	}
	return s.userRepo.SetRole(ctx, id, role)
}
