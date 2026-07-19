package nextup

import (
	"context"
	"database/sql"
	"errors"

	"moviepickarr/internal/domain"
)

// poolCounter is the one slice of the movie repository the rotation rule
// needs: next up only advances while the pool still has movies. Narrowed
// consumer-side so tests fake one method, not the whole MovieRepo.
type poolCounter interface {
	CountByStatus(ctx context.Context, status string) (int, error)
}

// Service owns the next-up rotation: whose turn it is to run movie night, and
// when the turn passes. The turn is enforced (only the next-up member or an
// admin can draw, reveal, or mark watched) and rotates on watch; every rule
// about who holds it and when it passes lives here.
type Service struct {
	nextUpRepo domain.NextUpRepo
	userRepo   domain.UserRepo
	pool       poolCounter
}

func NewService(nextUpRepo domain.NextUpRepo, userRepo domain.UserRepo, pool poolCounter) *Service {
	return &Service{
		nextUpRepo: nextUpRepo,
		userRepo:   userRepo,
		pool:       pool,
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

func (s *Service) Set(ctx context.Context, userID int) error {
	return s.nextUpRepo.Set(ctx, userID)
}

// Advance passes the turn to the next roster member, in roster order. The server
// calls it when a movie is watched (rotation-on-watch, Model B), not on draw, so
// the runner keeps the turn across the whole draw → reveal → watch cycle. The
// turn only moves while the pool still has movies left and more than one member
// exists; otherwise it reports changed=false and the current member keeps the
// turn. A next up who has left the roster hands the turn to the first member.
func (s *Service) Advance(ctx context.Context) (next *domain.User, changed bool, err error) {
	users, err := s.userRepo.List(ctx)
	if err != nil {
		return nil, false, err
	}
	if len(users) <= 1 {
		return nil, false, nil
	}

	pooled, err := s.pool.CountByStatus(ctx, "pool")
	if err != nil {
		return nil, false, err
	}
	if pooled == 0 {
		return nil, false, nil
	}

	current, err := s.Get(ctx)
	if err != nil {
		// Get self-seeds, so no rows here means an empty roster, unreachable
		// behind the len(users) guard, but harmless to treat as a no-op.
		if errors.Is(err, sql.ErrNoRows) {
			return nil, false, nil
		}
		return nil, false, err
	}

	currentIndex := -1
	for i := range users {
		if users[i].ID == current.ID {
			currentIndex = i
			break
		}
	}

	nextIndex := 0
	if currentIndex >= 0 {
		nextIndex = (currentIndex + 1) % len(users)
	}

	if err := s.nextUpRepo.Set(ctx, users[nextIndex].ID); err != nil {
		return nil, false, err
	}

	return users[nextIndex], true, nil
}
