package nextup

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"moviepickarr/internal/domain"
)

type fakeNextUpRepo struct {
	userID   int // 0 = unset
	eligible []*domain.User
}

func (r *fakeNextUpRepo) Get(_ context.Context) (*domain.User, error) {
	if r.userID == 0 {
		return nil, sql.ErrNoRows
	}
	for _, u := range r.eligible {
		if u.ID == r.userID {
			c := *u
			return &c, nil
		}
	}
	// The stored holder is no longer eligible. The real repository's join drops
	// the pointer so the service runs its SetFirstEligible repair path.
	return nil, sql.ErrNoRows
}

func (r *fakeNextUpRepo) Set(_ context.Context, userID int) error {
	r.userID = userID
	return nil
}

func (r *fakeNextUpRepo) SetFirstEligible(_ context.Context) (*domain.User, error) {
	if len(r.eligible) == 0 {
		return nil, sql.ErrNoRows
	}
	r.userID = r.eligible[0].ID
	copy := *r.eligible[0]
	return &copy, nil
}

func roster(names ...string) []*domain.User {
	users := make([]*domain.User, len(names))
	for i, n := range names {
		users[i] = &domain.User{ID: i + 1, Name: n}
	}
	return users
}

func newTestService(users []*domain.User, current int) (*Service, *fakeNextUpRepo) {
	nextUpRepo := &fakeNextUpRepo{userID: current, eligible: users}
	return NewService(nextUpRepo), nextUpRepo
}

func TestGetSeedsFirstEligibleParticipant(t *testing.T) {
	users := roster("guest", "member")
	repo := &fakeNextUpRepo{
		eligible: users[1:],
	}

	nextUp, err := NewService(repo).Get(t.Context())
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if nextUp.ID != 2 || repo.userID != 2 {
		t.Fatalf("seeded %+v with stored id %d, want eligible member 2", nextUp, repo.userID)
	}
}

func TestGetRepairsIneligibleStoredHolder(t *testing.T) {
	users := roster("guest", "member")
	repo := &fakeNextUpRepo{
		userID:   users[0].ID,
		eligible: users[1:],
	}

	nextUp, err := NewService(repo).Get(t.Context())
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if nextUp.ID != 2 || repo.userID != 2 {
		t.Fatalf("repaired to %+v with stored id %d, want eligible member 2", nextUp, repo.userID)
	}
}

func TestGetSelfSeedsFreshInstall(t *testing.T) {
	svc, repo := newTestService(roster("ana", "ben"), 0)

	nextUp, err := svc.Get(context.Background())
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if nextUp.ID != 1 {
		t.Fatalf("expected seed with first member, got %+v", nextUp)
	}
	if repo.userID != 1 {
		t.Fatalf("expected seed persisted, got %d", repo.userID)
	}
}

func TestGetEmptyRosterMeansNoRows(t *testing.T) {
	svc, _ := newTestService(roster(), 0)

	_, err := svc.Get(context.Background())
	if !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("expected sql.ErrNoRows for an empty roster, got %v", err)
	}
}
