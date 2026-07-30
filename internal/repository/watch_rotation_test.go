package repository

import (
	"errors"
	"testing"
	"time"

	"moviepickarr/internal/domain"
)

func TestWatchCurrentAndAdvanceNextUp_RotatesValidHolder(t *testing.T) {
	tests := []struct {
		name       string
		holder     int
		wantHolder int
	}{
		{name: "advances", holder: 0, wantHolder: 1},
		{name: "wraps", holder: 2, wantHolder: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := setupUserRemoveEnv(t)
			members := make([]*domain.User, 0, 3)
			for _, name := range []string{"Ana", "Ben", "Cai"} {
				member, err := e.users.Create(e.ctx, name)
				if err != nil {
					t.Fatalf("create member %q: %v", name, err)
				}
				members = append(members, member)
			}
			if err := e.nextUp.Set(e.ctx, members[tt.holder].ID); err != nil {
				t.Fatalf("set next up: %v", err)
			}
			if _, err := e.movies.Add(e.ctx, "Heat", "current", members[0].ID); err != nil {
				t.Fatalf("add current movie: %v", err)
			}
			if _, err := e.movies.Add(e.ctx, "Thief", "pool", members[0].ID); err != nil {
				t.Fatalf("add pooled movie: %v", err)
			}

			_, next, changed, err := e.movies.WatchCurrentAndAdvanceNextUp(e.ctx, time.Now().UTC())
			if err != nil {
				t.Fatalf("watch and rotate: %v", err)
			}
			if !changed || next == nil || next.ID != members[tt.wantHolder].ID {
				t.Fatalf(
					"handoff = changed=%v next=%+v, want member %d",
					changed,
					next,
					members[tt.wantHolder].ID,
				)
			}
		})
	}
}

func TestWatchCurrentAndAdvanceNextUp_SeedsThenRotatesFreshInstall(t *testing.T) {
	e := setupUserRemoveEnv(t)
	first, err := e.users.Create(e.ctx, "Ana")
	if err != nil {
		t.Fatalf("create first member: %v", err)
	}
	second, err := e.users.Create(e.ctx, "Ben")
	if err != nil {
		t.Fatalf("create second member: %v", err)
	}
	current, err := e.movies.Add(e.ctx, "Heat", "current", first.ID)
	if err != nil {
		t.Fatalf("add current movie: %v", err)
	}
	if _, err := e.movies.Add(e.ctx, "Thief", "pool", first.ID); err != nil {
		t.Fatalf("add pooled movie: %v", err)
	}

	watchedAt := time.Date(2026, 7, 30, 20, 0, 0, 0, time.UTC)
	watched, next, changed, err := e.movies.WatchCurrentAndAdvanceNextUp(e.ctx, watchedAt)
	if err != nil {
		t.Fatalf("watch and rotate: %v", err)
	}
	if watched.ID != current.ID || watched.Status != "watched" || watched.WatchedAt == nil {
		t.Fatalf("watched movie = %+v, want movie %d watched", watched, current.ID)
	}
	if !changed || next == nil || next.ID != second.ID {
		t.Fatalf("handoff = changed=%v next=%+v, want member %d", changed, next, second.ID)
	}

	stored, err := e.nextUp.Get(e.ctx)
	if err != nil {
		t.Fatalf("get stored next up: %v", err)
	}
	if stored.ID != second.ID {
		t.Fatalf("stored next up = %d, want %d", stored.ID, second.ID)
	}
}

func TestWatchCurrentAndAdvanceNextUp_HandsArchivedTurnToFirstActiveMember(t *testing.T) {
	e := setupUserRemoveEnv(t)
	departing, err := e.users.Create(e.ctx, "Departing")
	if err != nil {
		t.Fatalf("create departing member: %v", err)
	}
	firstActive, err := e.users.Create(e.ctx, "First active")
	if err != nil {
		t.Fatalf("create first active member: %v", err)
	}
	if _, err := e.users.Create(e.ctx, "Second active"); err != nil {
		t.Fatalf("create second active member: %v", err)
	}
	if err := e.nextUp.Set(e.ctx, departing.ID); err != nil {
		t.Fatalf("set departing member next up: %v", err)
	}
	if _, err := e.movies.Add(e.ctx, "Heat", "current", departing.ID); err != nil {
		t.Fatalf("add current movie: %v", err)
	}
	if _, err := e.movies.Add(e.ctx, "Thief", "pool", firstActive.ID); err != nil {
		t.Fatalf("add pooled movie: %v", err)
	}
	if outcome, err := e.users.Remove(e.ctx, departing.ID); err != nil || outcome != domain.OutcomeArchived {
		t.Fatalf("archive departing member: outcome=%q err=%v", outcome, err)
	}

	_, next, changed, err := e.movies.WatchCurrentAndAdvanceNextUp(e.ctx, time.Now().UTC())
	if err != nil {
		t.Fatalf("watch and rotate: %v", err)
	}
	if !changed || next == nil || next.ID != firstActive.ID {
		t.Fatalf("handoff = changed=%v next=%+v, want first active member %d", changed, next, firstActive.ID)
	}
}

func TestWatchCurrentAndAdvanceNextUp_KeepsTurnWhenPoolIsEmpty(t *testing.T) {
	e := setupUserRemoveEnv(t)
	first, err := e.users.Create(e.ctx, "Ana")
	if err != nil {
		t.Fatalf("create first member: %v", err)
	}
	if _, err := e.users.Create(e.ctx, "Ben"); err != nil {
		t.Fatalf("create second member: %v", err)
	}
	if err := e.nextUp.Set(e.ctx, first.ID); err != nil {
		t.Fatalf("set next up: %v", err)
	}
	if _, err := e.movies.Add(e.ctx, "Heat", "current", first.ID); err != nil {
		t.Fatalf("add current movie: %v", err)
	}

	_, next, changed, err := e.movies.WatchCurrentAndAdvanceNextUp(e.ctx, time.Now().UTC())
	if err != nil {
		t.Fatalf("watch without handoff: %v", err)
	}
	if changed || next != nil {
		t.Fatalf("handoff = changed=%v next=%+v, want no change", changed, next)
	}

	stored, err := e.nextUp.Get(e.ctx)
	if err != nil {
		t.Fatalf("get stored next up: %v", err)
	}
	if stored.ID != first.ID {
		t.Fatalf("stored next up = %d, want %d", stored.ID, first.ID)
	}
}

func TestWatchCurrentAndAdvanceNextUp_KeepsTurnWithOneMember(t *testing.T) {
	e := setupUserRemoveEnv(t)
	only, err := e.users.Create(e.ctx, "Only")
	if err != nil {
		t.Fatalf("create member: %v", err)
	}
	if err := e.nextUp.Set(e.ctx, only.ID); err != nil {
		t.Fatalf("set next up: %v", err)
	}
	if _, err := e.movies.Add(e.ctx, "Heat", "current", only.ID); err != nil {
		t.Fatalf("add current movie: %v", err)
	}
	if _, err := e.movies.Add(e.ctx, "Thief", "pool", only.ID); err != nil {
		t.Fatalf("add pooled movie: %v", err)
	}

	_, next, changed, err := e.movies.WatchCurrentAndAdvanceNextUp(e.ctx, time.Now().UTC())
	if err != nil {
		t.Fatalf("watch without handoff: %v", err)
	}
	if changed || next != nil {
		t.Fatalf("handoff = changed=%v next=%+v, want no change", changed, next)
	}
}

func TestWatchCurrentAndAdvanceNextUp_RequiresCurrentMovie(t *testing.T) {
	e := setupUserRemoveEnv(t)

	_, _, _, err := e.movies.WatchCurrentAndAdvanceNextUp(e.ctx, time.Now().UTC())
	if !errors.Is(err, domain.ErrNoCurrentDraw) {
		t.Fatalf("watch without current movie: got %v, want ErrNoCurrentDraw", err)
	}
}
