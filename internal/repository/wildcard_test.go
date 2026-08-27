package repository

import (
	"errors"
	"sync"
	"testing"
	"time"

	"moviepickarr/internal/domain"
)

func revealedCurrentForWildcard(t *testing.T, e *userRemoveEnv, memberID int) *domain.Movie {
	t.Helper()
	movie, err := e.movies.Add(e.ctx, "Current", "pool", memberID)
	if err != nil {
		t.Fatalf("add current candidate: %v", err)
	}
	drawnAt := time.Date(2026, 8, 25, 19, 0, 0, 0, time.UTC)
	if err := e.movies.StartDraw(e.ctx, movie.ID, drawnAt, drawnAt.Add(time.Second), "drawer"); err != nil {
		t.Fatalf("start draw: %v", err)
	}
	if err := e.movies.RevealDraw(e.ctx, movie.ID, drawnAt.Add(time.Second)); err != nil {
		t.Fatalf("reveal draw: %v", err)
	}
	return movie
}

func TestWildcardWatchPreservesCurrentDrawAndNextUp(t *testing.T) {
	e := setupUserRemoveEnv(t)
	first, err := e.users.Create(e.ctx, "Ana")
	if err != nil {
		t.Fatal(err)
	}
	second, err := e.users.Create(e.ctx, "Ben")
	if err != nil {
		t.Fatal(err)
	}
	if err := e.nextUp.Set(e.ctx, first.ID); err != nil {
		t.Fatal(err)
	}
	host := revealedCurrentForWildcard(t, e, first.ID)

	tmdbID := 550
	selectedAt := time.Date(2026, 8, 25, 20, 0, 0, 0, time.UTC)
	wildcard, err := e.movies.StartWildcard(e.ctx, second.ID, domain.WildcardSelection{
		ExpectedHostMovieID: host.ID,
		Title:               "Fight Club",
		TMDBID:              &tmdbID,
	}, selectedAt, false)
	if err != nil {
		t.Fatalf("start wildcard: %v", err)
	}
	if wildcard.HostMovieID != host.ID || wildcard.Movie.Status != "wildcard" || !wildcard.CreatedForWildcard {
		t.Fatalf("started wildcard = %+v", wildcard)
	}
	if wildcard.Movie.AddedByID != second.ID {
		t.Fatalf("new wildcard adder = %d, want %d", wildcard.Movie.AddedByID, second.ID)
	}

	var source string
	var wildcardID int64
	var revealedAt int64
	var actionVersion int
	if err := e.pool.Read.QueryRowContext(e.ctx, `
		SELECT source, wildcard_id, revealed_at, action_version
		FROM radarr_acquisitions
		WHERE movie_id = ?
	`, wildcard.Movie.ID).Scan(&source, &wildcardID, &revealedAt, &actionVersion); err != nil {
		t.Fatalf("read wildcard acquisition: %v", err)
	}
	if source != "wildcard" || wildcardID != wildcard.ID || revealedAt != selectedAt.UnixMilli() || actionVersion != 1 {
		t.Fatalf("acquisition = source %q wildcard %d revealed %d version %d", source, wildcardID, revealedAt, actionVersion)
	}

	watchedAt := selectedAt.Add(2 * time.Hour)
	watched, err := e.movies.WatchWildcard(e.ctx, wildcard.ID, watchedAt)
	if err != nil {
		t.Fatalf("watch wildcard: %v", err)
	}
	if watched.Movie.Status != "watched" || watched.Movie.WildcardOfMovieID == nil || *watched.Movie.WildcardOfMovieID != host.ID {
		t.Fatalf("watched wildcard = %+v", watched)
	}
	current, err := e.movies.GetCurrent(e.ctx)
	if err != nil || current.ID != host.ID || current.Status != "current" {
		t.Fatalf("current after wildcard = %+v err=%v", current, err)
	}
	next, err := e.nextUp.Get(e.ctx)
	if err != nil || next.ID != first.ID {
		t.Fatalf("next up after wildcard = %+v err=%v", next, err)
	}

	// A second Wildcard can follow under the same Current draw.
	stash, err := e.movies.Add(e.ctx, "Second wildcard", "stash", first.ID)
	if err != nil {
		t.Fatal(err)
	}
	secondWildcard, err := e.movies.StartWildcard(e.ctx, first.ID, domain.WildcardSelection{
		ExpectedHostMovieID: host.ID,
		ExistingMovieID:     &stash.ID,
	}, watchedAt.Add(time.Hour), false)
	if err != nil {
		t.Fatalf("start second wildcard: %v", err)
	}
	if secondWildcard.HostMovieID != host.ID || secondWildcard.ID == wildcard.ID {
		t.Fatalf("second wildcard = %+v", secondWildcard)
	}
}

func TestCancelWildcardRestoresMovieAndClosesAcquisitionRequirement(t *testing.T) {
	e := setupUserRemoveEnv(t)
	member, err := e.users.Create(e.ctx, "Ana")
	if err != nil {
		t.Fatal(err)
	}
	host := revealedCurrentForWildcard(t, e, member.ID)
	pooled, err := e.movies.Add(e.ctx, "Alternate", "pool", member.ID)
	if err != nil {
		t.Fatal(err)
	}
	wildcard, err := e.movies.StartWildcard(e.ctx, member.ID, domain.WildcardSelection{
		ExpectedHostMovieID: host.ID,
		ExistingMovieID:     &pooled.ID,
	}, time.Now().UTC(), false)
	if err != nil {
		t.Fatalf("start wildcard: %v", err)
	}

	canceled, err := e.movies.CancelWildcard(e.ctx, member.ID, wildcard.ID, time.Now().UTC())
	if err != nil {
		t.Fatalf("cancel wildcard: %v", err)
	}
	if canceled.Status != domain.WildcardStatusCanceled || canceled.Movie.Status != "pool" {
		t.Fatalf("canceled wildcard = %+v", canceled)
	}
	var acquisitionID int64
	if err := e.pool.Read.QueryRowContext(e.ctx,
		"SELECT id FROM radarr_acquisitions WHERE wildcard_id = ?", wildcard.ID,
	).Scan(&acquisitionID); err != nil {
		t.Fatalf("resolve canceled acquisition: %v", err)
	}
	acquisition, err := NewSqliteRadarrRepository(e.pool).GetVisibleAcquisition(e.ctx, acquisitionID)
	if err != nil {
		t.Fatalf("read canceled acquisition: %v", err)
	}
	if acquisition.Status != "abandoned" || acquisition.CanceledAt == nil || acquisition.AbandonmentReason != "Wildcard canceled" {
		t.Fatalf("canceled acquisition = %+v", acquisition)
	}
	attention, err := NewSqliteRadarrRepository(e.pool).AttentionCount(e.ctx)
	if err != nil || attention != 1 {
		// The host Current draw still needs its preset. The canceled Wildcard does not.
		t.Fatalf("attention = %d err=%v, want host draw only", attention, err)
	}
}

func TestWildcardRequiresRevealAndRespectsPoolLock(t *testing.T) {
	e := setupUserRemoveEnv(t)
	member, err := e.users.Create(e.ctx, "Ana")
	if err != nil {
		t.Fatal(err)
	}
	host, err := e.movies.Add(e.ctx, "Current", "pool", member.ID)
	if err != nil {
		t.Fatal(err)
	}
	drawnAt := time.Now().UTC()
	if err := e.movies.StartDraw(e.ctx, host.ID, drawnAt, drawnAt.Add(time.Minute), "drawer"); err != nil {
		t.Fatal(err)
	}
	pooled, err := e.movies.Add(e.ctx, "Alternate", "pool", member.ID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = e.movies.StartWildcard(e.ctx, member.ID, domain.WildcardSelection{ExpectedHostMovieID: host.ID, ExistingMovieID: &pooled.ID}, time.Now().UTC(), false)
	if !errors.Is(err, domain.ErrDrawNotRevealed) {
		t.Fatalf("start before Reveal error = %v", err)
	}
	if err := e.movies.RevealDraw(e.ctx, host.ID, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	_, err = e.movies.StartWildcard(e.ctx, member.ID, domain.WildcardSelection{ExpectedHostMovieID: host.ID, ExistingMovieID: &pooled.ID}, time.Now().UTC(), true)
	if !errors.Is(err, domain.ErrPoolLocked) {
		t.Fatalf("start from locked Pool error = %v", err)
	}
	stored, err := e.movies.FindByID(e.ctx, pooled.ID)
	if err != nil || stored.Status != "pool" {
		t.Fatalf("locked pooled movie = %+v err=%v", stored, err)
	}
}

func TestCurrentWatchWaitsForActiveWildcard(t *testing.T) {
	e := setupUserRemoveEnv(t)
	member, err := e.users.Create(e.ctx, "Ana")
	if err != nil {
		t.Fatal(err)
	}
	host := revealedCurrentForWildcard(t, e, member.ID)
	stash, err := e.movies.Add(e.ctx, "Alternate", "stash", member.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.movies.StartWildcard(e.ctx, member.ID, domain.WildcardSelection{ExpectedHostMovieID: host.ID, ExistingMovieID: &stash.ID}, time.Now().UTC(), false); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := e.movies.WatchCurrentAndAdvanceNextUp(e.ctx, time.Now().UTC()); !errors.Is(err, domain.ErrActiveWildcard) {
		t.Fatalf("watch Current with Active wildcard error = %v", err)
	}
	current, err := e.movies.GetCurrent(e.ctx)
	if err != nil || current.ID != host.ID {
		t.Fatalf("current after refused watch = %+v err=%v", current, err)
	}
}

func TestWildcardCommandsRejectStaleDrawAndWildcardIDs(t *testing.T) {
	e := setupUserRemoveEnv(t)
	member, err := e.users.Create(e.ctx, "Ana")
	if err != nil {
		t.Fatal(err)
	}
	host := revealedCurrentForWildcard(t, e, member.ID)
	stash, err := e.movies.Add(e.ctx, "Alternate", "stash", member.ID)
	if err != nil {
		t.Fatal(err)
	}

	_, err = e.movies.StartWildcard(e.ctx, member.ID, domain.WildcardSelection{
		ExpectedHostMovieID: host.ID + 1,
		ExistingMovieID:     &stash.ID,
	}, time.Now().UTC(), false)
	if !errors.Is(err, domain.ErrCurrentDrawChanged) {
		t.Fatalf("stale host selection error = %v", err)
	}

	wildcard, err := e.movies.StartWildcard(e.ctx, member.ID, domain.WildcardSelection{
		ExpectedHostMovieID: host.ID,
		ExistingMovieID:     &stash.ID,
	}, time.Now().UTC(), false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.movies.WatchWildcard(e.ctx, wildcard.ID+1, time.Now().UTC()); !errors.Is(err, domain.ErrWildcardChanged) {
		t.Fatalf("stale watch error = %v", err)
	}
	if _, err := e.movies.CancelWildcard(e.ctx, member.ID, wildcard.ID+1, time.Now().UTC()); !errors.Is(err, domain.ErrWildcardChanged) {
		t.Fatalf("stale cancel error = %v", err)
	}
	active, err := e.movies.ActiveWildcard(e.ctx)
	if err != nil || active.ID != wildcard.ID {
		t.Fatalf("active wildcard after stale commands = %+v err=%v", active, err)
	}
}

func TestConcurrentWildcardSelectionsCreateOneActiveWildcard(t *testing.T) {
	e := setupUserRemoveEnv(t)
	member, err := e.users.Create(e.ctx, "Ana")
	if err != nil {
		t.Fatal(err)
	}
	host := revealedCurrentForWildcard(t, e, member.ID)
	first, _ := e.movies.Add(e.ctx, "First", "stash", member.ID)
	second, _ := e.movies.Add(e.ctx, "Second", "stash", member.ID)

	ids := []int{first.ID, second.ID}
	errs := make(chan error, len(ids))
	var wg sync.WaitGroup
	for i := range ids {
		wg.Add(1)
		go func(movieID int) {
			defer wg.Done()
			_, err := e.movies.StartWildcard(e.ctx, member.ID, domain.WildcardSelection{ExpectedHostMovieID: host.ID, ExistingMovieID: &movieID}, time.Now().UTC(), false)
			errs <- err
		}(ids[i])
	}
	wg.Wait()
	close(errs)
	succeeded := 0
	conflicted := 0
	for err := range errs {
		switch {
		case err == nil:
			succeeded++
		case errors.Is(err, domain.ErrActiveWildcard):
			conflicted++
		default:
			t.Fatalf("unexpected selection error: %v", err)
		}
	}
	if succeeded != 1 || conflicted != 1 {
		t.Fatalf("selections = %d success, %d conflict", succeeded, conflicted)
	}
}
