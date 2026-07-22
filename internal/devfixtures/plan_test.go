package devfixtures

import (
	"testing"
	"time"

	"moviepickarr/internal/domain"
)

// testFilms returns a distinct-id film list large enough for a full plan.
func testFilms(n int) []Film {
	films := make([]Film, n)
	for i := range films {
		films[i] = Film{TMDBID: 1000 + i, Title: "Film", Year: 2000}
	}
	return films
}

func buildTestPlan(t *testing.T) Plan {
	t.Helper()
	plan, err := BuildPlan(testFilms(600), time.Unix(1_700_000_000, 0).UTC())
	if err != nil {
		t.Fatalf("BuildPlan: %v", err)
	}
	return plan
}

func TestBuildPlanRosterShape(t *testing.T) {
	plan := buildTestPlan(t)

	if got := len(plan.Members); got != 7 {
		t.Fatalf("members = %d, want 7", got)
	}

	var admins, logins, placeholders, archived int
	for _, m := range plan.Members {
		if m.Role == domain.RoleAdmin {
			admins++
		}
		if m.Login != nil {
			logins++
		}
		if m.Archived {
			archived++
		}
		if m.Login == nil && !m.Archived {
			placeholders++
		}
	}
	if admins != 1 {
		t.Errorf("admins = %d, want 1", admins)
	}
	if logins != 5 {
		t.Errorf("members with logins = %d, want 5", logins)
	}
	if placeholders != 1 {
		t.Errorf("placeholders (active, no login) = %d, want 1", placeholders)
	}
	if archived != 1 {
		t.Errorf("archived = %d, want 1", archived)
	}
}

func TestBuildPlanArchivedAndPlaceholderHaveNoLogin(t *testing.T) {
	plan := buildTestPlan(t)
	for _, m := range plan.Members {
		if (m.Archived || m.Login == nil) && m.Role == domain.RoleAdmin {
			t.Errorf("member %q is admin but non-login; admin must be a login member", m.Name)
		}
		if m.Archived && m.Login != nil {
			t.Errorf("archived member %q has a login; archival strips credentials", m.Name)
		}
	}
}

func TestBuildPlanMovieCountsPerStatus(t *testing.T) {
	plan := buildTestPlan(t)

	counts := map[domain.MovieStatus]int{}
	for _, mv := range plan.Movies {
		counts[mv.Status]++
	}
	if counts[domain.MovieStatusWatched] != watchedCount {
		t.Errorf("watched = %d, want %d", counts[domain.MovieStatusWatched], watchedCount)
	}
	if counts[domain.MovieStatusStash] != stashPerMember*len(loginMemberIndices) {
		t.Errorf("stash = %d, want %d", counts[domain.MovieStatusStash], stashPerMember*len(loginMemberIndices))
	}
	if counts[domain.MovieStatusCurrent] != 0 {
		t.Errorf("current = %d, want 0 (opens ready-to-draw)", counts[domain.MovieStatusCurrent])
	}
	if counts[domain.MovieStatusPool] == 0 {
		t.Errorf("pool = 0, want > 0")
	}
}

func TestBuildPlanPoolRespectsPerMemberCap(t *testing.T) {
	plan := buildTestPlan(t)
	perMember := map[int]int{}
	for _, mv := range plan.Movies {
		if mv.Status == domain.MovieStatusPool {
			perMember[mv.AdderIndex]++
		}
	}
	for idx, n := range perMember {
		if n > 3 {
			t.Errorf("member index %d holds %d pooled movies, cap is 3", idx, n)
		}
	}
}

func TestBuildPlanTMDBIDsAreDistinct(t *testing.T) {
	plan := buildTestPlan(t)
	seen := map[int]bool{}
	for _, mv := range plan.Movies {
		if mv.TMDBID == 0 {
			t.Errorf("movie %q has no tmdb_id", mv.Title)
		}
		if seen[mv.TMDBID] {
			t.Fatalf("duplicate tmdb_id %d: violates the movies_tmdb_id_unique index", mv.TMDBID)
		}
		seen[mv.TMDBID] = true
	}
}

func TestBuildPlanWatchedStatusCoupling(t *testing.T) {
	plan := buildTestPlan(t)
	for _, mv := range plan.Movies {
		watched := mv.Status == domain.MovieStatusWatched
		hasTime := mv.WatchedAt != nil
		if watched != hasTime {
			t.Errorf("movie %q status=%s watchedAt=%v: violates the status<->watched_at CHECK", mv.Title, mv.Status, mv.WatchedAt)
		}
		if watched && !mv.WatchedAt.After(mv.AddedAt) && !mv.WatchedAt.Equal(mv.AddedAt) {
			t.Errorf("movie %q watched_at %v is before added_at %v", mv.Title, mv.WatchedAt, mv.AddedAt)
		}
	}
}

func TestBuildPlanWatchedHistorySpansWindows(t *testing.T) {
	now := time.Unix(1_700_000_000, 0).UTC()
	plan, err := BuildPlan(testFilms(600), now)
	if err != nil {
		t.Fatalf("BuildPlan: %v", err)
	}

	var last24h, lastWeek, lastYear, older int
	for _, mv := range plan.Movies {
		if mv.WatchedAt == nil {
			continue
		}
		age := now.Sub(*mv.WatchedAt)
		switch {
		case age <= 24*time.Hour:
			last24h++
		case age <= 7*24*time.Hour:
			lastWeek++
		case age <= 365*24*time.Hour:
			lastYear++
		default:
			older++
		}
	}
	if last24h == 0 {
		t.Error("no watched movies in the last 24h window")
	}
	if lastWeek == 0 {
		t.Error("no watched movies in the 24h-7d window")
	}
	if lastYear == 0 {
		t.Error("no watched movies in the 7d-1y window")
	}
	if older == 0 {
		t.Error("no watched movies older than 1y")
	}
}

func TestBuildPlanArchivedAndPlaceholderAuthorWatched(t *testing.T) {
	plan := buildTestPlan(t)

	authored := map[int]int{}
	for _, mv := range plan.Movies {
		if mv.Status == domain.MovieStatusWatched {
			authored[mv.AdderIndex]++
		}
	}
	for i, m := range plan.Members {
		if (m.Archived || m.Login == nil) && authored[i] == 0 {
			t.Errorf("member %q (archived/placeholder) authored no watched movies; attribution stays unexercised", m.Name)
		}
	}
}

func TestBuildPlanNextUpIsActiveLoginMember(t *testing.T) {
	plan := buildTestPlan(t)
	if plan.NextUpIndex < 0 || plan.NextUpIndex >= len(plan.Members) {
		t.Fatalf("NextUpIndex %d out of range", plan.NextUpIndex)
	}
	m := plan.Members[plan.NextUpIndex]
	if m.Archived || m.Login == nil {
		t.Errorf("next-up member %q must be an active login member", m.Name)
	}
}

func TestBuildPlanIsDeterministic(t *testing.T) {
	now := time.Unix(1_700_000_000, 0).UTC()
	a, err := BuildPlan(testFilms(600), now)
	if err != nil {
		t.Fatalf("BuildPlan: %v", err)
	}
	b, err := BuildPlan(testFilms(600), now)
	if err != nil {
		t.Fatalf("BuildPlan: %v", err)
	}
	if len(a.Movies) != len(b.Movies) {
		t.Fatalf("movie counts differ: %d vs %d", len(a.Movies), len(b.Movies))
	}
	for i := range a.Movies {
		// Compare by value: WatchedAt is a pointer, so a raw struct compare
		// would trip on identity rather than the timestamp it points at.
		if !sameMovie(a.Movies[i], b.Movies[i]) {
			t.Fatalf("movie %d differs between runs: %+v vs %+v", i, a.Movies[i], b.Movies[i])
		}
	}
}

func sameMovie(x, y Movie) bool {
	if x.Title != y.Title || x.TMDBID != y.TMDBID || x.Status != y.Status ||
		x.AdderIndex != y.AdderIndex || !x.AddedAt.Equal(y.AddedAt) {
		return false
	}
	switch {
	case x.WatchedAt == nil && y.WatchedAt == nil:
		return true
	case x.WatchedAt == nil || y.WatchedAt == nil:
		return false
	default:
		return x.WatchedAt.Equal(*y.WatchedAt)
	}
}

func TestBuildPlanRejectsTooFewFilms(t *testing.T) {
	if _, err := BuildPlan(testFilms(10), time.Unix(1_700_000_000, 0).UTC()); err == nil {
		t.Fatal("expected an error when the dataset is too small, got nil")
	}
}
