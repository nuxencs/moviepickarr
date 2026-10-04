package nextup

import (
	"context"
	"database/sql"
	"errors"

	"moviepickarr/internal/domain"
)

// Service owns next-up reads. The turn handoff commits with the Reveal in the
// movie store because it spans both durable records.
type Service struct {
	nextUpRepo domain.NextUpRepo
}

func NewService(nextUpRepo domain.NextUpRepo) *Service {
	return &Service{
		nextUpRepo: nextUpRepo,
	}
}

// Get returns the member whose turn it is. A fresh install has no next up
// yet, so Get seeds it with the first eligible Turn participant before
// answering. sql.ErrNoRows means no active Turn participant exists.
func (s *Service) Get(ctx context.Context) (*domain.User, error) {
	nextUp, err := s.nextUpRepo.Get(ctx)
	if err == nil {
		return nextUp, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}

	return s.nextUpRepo.SetFirstEligible(ctx)
}

// Skip is the admin's explicit turn handoff from holderID to the next Turn
// participant. The repository owns the stale-holder and unrevealed-draw guards.
func (s *Service) Skip(ctx context.Context, holderID int) (*domain.User, error) {
	return s.nextUpRepo.Skip(ctx, holderID)
}
