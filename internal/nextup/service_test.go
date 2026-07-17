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
func (r *fakeUserRepo) Delete(context.Context, int) error { panic("unexpected call") }

type fakePool struct{ pooled int }

func (p *fakePool) CountByStatus(_ context.Context, status string) (int, error) {
	if status != "pool" {
		return 0, nil
	}
	return p.pooled, nil
}

func roster(names ...string) *fakeUserRepo {
	users := make([]*domain.User, len(names))
	for i, n := range names {
		users[i] = &domain.User{ID: i + 1, Name: n}
	}
	return &fakeUserRepo{list: users}
}

func newTestService(users *fakeUserRepo, pooled int, current int) (*Service, *fakeNextUpRepo) {
	nextUpRepo := &fakeNextUpRepo{userID: current, users: users}
	return NewService(nextUpRepo, users, &fakePool{pooled: pooled}), nextUpRepo
}

func TestAdvanceRotatesInRosterOrder(t *testing.T) {
	svc, repo := newTestService(roster("ana", "ben", "cai"), 2, 1)

	next, changed, err := svc.Advance(context.Background())
	if err != nil {
		t.Fatalf("advance: %v", err)
	}
	if !changed || next.ID != 2 {
		t.Fatalf("expected turn to pass to member 2, got changed=%v next=%+v", changed, next)
	}
	if repo.userID != 2 {
		t.Fatalf("expected repo to store member 2, got %d", repo.userID)
	}
}

func TestAdvanceWrapsAroundTheRoster(t *testing.T) {
	svc, repo := newTestService(roster("ana", "ben", "cai"), 1, 3)

	next, changed, err := svc.Advance(context.Background())
	if err != nil {
		t.Fatalf("advance: %v", err)
	}
	if !changed || next.ID != 1 {
		t.Fatalf("expected wrap to member 1, got changed=%v next=%+v", changed, next)
	}
	if repo.userID != 1 {
		t.Fatalf("expected repo to store member 1, got %d", repo.userID)
	}
}

func TestAdvanceNoOpWithSingleMember(t *testing.T) {
	svc, repo := newTestService(roster("ana"), 5, 1)

	_, changed, err := svc.Advance(context.Background())
	if err != nil {
		t.Fatalf("advance: %v", err)
	}
	if changed {
		t.Fatal("a single member keeps the turn")
	}
	if repo.userID != 1 {
		t.Fatalf("turn moved to %d", repo.userID)
	}
}

func TestAdvanceNoOpWithEmptyPool(t *testing.T) {
	svc, repo := newTestService(roster("ana", "ben"), 0, 1)

	_, changed, err := svc.Advance(context.Background())
	if err != nil {
		t.Fatalf("advance: %v", err)
	}
	if changed {
		t.Fatal("the turn must not move while the pool is empty")
	}
	if repo.userID != 1 {
		t.Fatalf("turn moved to %d", repo.userID)
	}
}

func TestAdvanceSeedsThenRotatesOnFreshInstall(t *testing.T) {
	// No next up stored yet: Get seeds the first member, Advance then passes
	// the turn onward — matching the old handler's init-then-rotate behaviour.
	svc, repo := newTestService(roster("ana", "ben"), 1, 0)

	next, changed, err := svc.Advance(context.Background())
	if err != nil {
		t.Fatalf("advance: %v", err)
	}
	if !changed || next.ID != 2 {
		t.Fatalf("expected seed to ana then turn to ben, got changed=%v next=%+v", changed, next)
	}
	if repo.userID != 2 {
		t.Fatalf("expected repo to store member 2, got %d", repo.userID)
	}
}

func TestAdvanceHandsTurnToFirstWhenNextUpLeftRoster(t *testing.T) {
	// The stored next up (id 9) is no longer on the roster.
	svc, repo := newTestService(roster("ana", "ben"), 1, 9)

	next, changed, err := svc.Advance(context.Background())
	if err != nil {
		t.Fatalf("advance: %v", err)
	}
	if !changed || next.ID != 1 {
		t.Fatalf("expected turn to fall back to member 1, got changed=%v next=%+v", changed, next)
	}
	if repo.userID != 1 {
		t.Fatalf("expected repo to store member 1, got %d", repo.userID)
	}
}

func TestGetSelfSeedsFreshInstall(t *testing.T) {
	svc, repo := newTestService(roster("ana", "ben"), 0, 0)

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
	svc, _ := newTestService(roster(), 0, 0)

	_, err := svc.Get(context.Background())
	if !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("expected sql.ErrNoRows for an empty roster, got %v", err)
	}
}
