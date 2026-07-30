package nextup

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"moviepickarr/internal/domain"
)

type fakeNextUpRepo struct {
	userID int // 0 = unset
	users  *fakeUserRepo
}

func (r *fakeNextUpRepo) Get(_ context.Context) (*domain.User, error) {
	if r.userID == 0 {
		return nil, sql.ErrNoRows
	}
	for _, u := range r.users.list {
		if u.ID == r.userID {
			c := *u
			return &c, nil
		}
	}
	// The stored next up left the roster; the singleton row still points at it.
	return &domain.User{ID: r.userID, Name: "gone"}, nil
}

func (r *fakeNextUpRepo) Set(_ context.Context, userID int) error {
	r.userID = userID
	return nil
}

type fakeUserRepo struct {
	list []*domain.User
}

func (r *fakeUserRepo) List(_ context.Context) ([]*domain.User, error) { return r.list, nil }
func (r *fakeUserRepo) FindByID(context.Context, int) (*domain.User, error) {
	panic("unexpected call")
}

func (r *fakeUserRepo) Create(context.Context, string) (*domain.User, error) {
	panic("unexpected call")
}

func (r *fakeUserRepo) Remove(context.Context, int) (domain.RemoveOutcome, error) {
	panic("unexpected call")
}
func (r *fakeUserRepo) Restore(context.Context, int) error { panic("unexpected call") }

func roster(names ...string) *fakeUserRepo {
	users := make([]*domain.User, len(names))
	for i, n := range names {
		users[i] = &domain.User{ID: i + 1, Name: n}
	}
	return &fakeUserRepo{list: users}
}

func newTestService(users *fakeUserRepo, current int) (*Service, *fakeNextUpRepo) {
	nextUpRepo := &fakeNextUpRepo{userID: current, users: users}
	return NewService(nextUpRepo, users), nextUpRepo
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
