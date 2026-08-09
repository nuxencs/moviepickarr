package devfixtures

import (
	"fmt"
	"strings"
	"time"

	"moviepickarr/internal/domain"
)

// devPassword is the shared plaintext every seeded login gets. Dev-only and
// intentionally weak-but-legal: 11 chars clears the min-8 local-login rule.
// Documented in docs/DEVELOPMENT.md so contributors know how to log in.
const devPassword = "devpassword"

// Roster and movie-volume knobs. Kept as named constants so the tests and the
// run summary read the same numbers the builder does.
const (
	stashPerMember = 15
	watchedCount   = 120
)

// loginMemberIndices are the members that get a real local login: index 0 is
// the admin, 1-4 are plain members. Indices 5 (placeholder) and 6 (archived)
// deliberately have none.
var loginMemberIndices = []int{0, 1, 2, 3, 4}

// poolPerLoginMember is how many pooled movies each login member holds, varied
// on purpose and always within the per-member cap of 3.
var poolPerLoginMember = []int{3, 2, 3, 1, 2}

// nextUpIndex is the seeded turn holder: an active login member (not the
// placeholder or archived member, who cannot take a turn).
const nextUpIndex = 1

// Login is the local credential a seeded member logs in with.
type Login struct {
	Username string
	Password string
}

// Member is one roster member in the plan. Login is nil for the placeholder
// and the archived member. Archived marks the removed-but-attributed member.
type Member struct {
	Name     string
	Role     string
	Archived bool
	Login    *Login
}

// Movie is one seeded movie, attributed to a member by AdderIndex (its position
// in Plan.Members). WatchedAt is non-nil exactly when Status is
// MovieStatusWatched, mirroring the DB's status<->watched_at CHECK.
type Movie struct {
	Title      string
	TMDBID     int
	Status     domain.MovieStatus
	AdderIndex int
	AddedAt    time.Time
	WatchedAt  *time.Time
}

// Plan is the whole deterministic developer world: who is on the roster, every
// movie and its state, the turn holder, and the pool lock. Apply writes it to a
// DB in one transaction.
type Plan struct {
	Members     []Member
	Movies      []Movie
	NextUpIndex int
	PoolLocked  bool
}

// roster is the fixed cast. Names are single words so usernames (lowercased
// name) and the "possessive of a name" copy paths both get exercised.
func roster() []Member {
	login := func(name string) *Login {
		return &Login{Username: strings.ToLower(name), Password: devPassword}
	}
	return []Member{
		{Name: "Ada", Role: domain.RoleAdmin, Login: login("Ada")},
		{Name: "Ben", Role: domain.RoleMember, Login: login("Ben")},
		{Name: "Cleo", Role: domain.RoleMember, Login: login("Cleo")},
		{Name: "Dev", Role: domain.RoleMember, Login: login("Dev")},
		{Name: "Erin", Role: domain.RoleMember, Login: login("Erin")},
		{Name: "Finn", Role: domain.RoleMember},                 // placeholder: no login
		{Name: "Gwen", Role: domain.RoleMember, Archived: true}, // archived: no login
	}
}

// watchedAdderPattern is cycled over the watched movies to attribute them. It
// covers every member index 0-6 so the placeholder (5) and archived member (6)
// both author watched history, and weights the login members more heavily so
// the leaderboard is uneven (and therefore worth reading). Length 12 divides
// 120 evenly, keeping the cycle deterministic and balanced.
var watchedAdderPattern = []int{0, 1, 2, 3, 4, 6, 0, 1, 2, 3, 5, 6}

// watchedBuckets place watched_at timestamps across the stats windows so every
// preset (24h … all-time) returns a different, non-empty leaderboard. Each
// bucket spreads its movies evenly between minAge and maxAge, measured back
// from "now". Counts sum to watchedCount.
var watchedBuckets = []struct {
	count          int
	minAge, maxAge time.Duration
}{
	{4, 1 * time.Hour, 23 * time.Hour},                   // last 24h
	{10, 25 * time.Hour, 7 * 24 * time.Hour},             // 1-7 days
	{22, 8 * 24 * time.Hour, 30 * 24 * time.Hour},        // 1-4 weeks
	{28, 31 * 24 * time.Hour, 90 * 24 * time.Hour},       // 1-3 months
	{28, 91 * 24 * time.Hour, 365 * 24 * time.Hour},      // 3-12 months
	{28, 366 * 24 * time.Hour, 4 * 365 * 24 * time.Hour}, // 1-4 years
}

// BuildPlan composes the developer world deterministically from movies and a
// reference time. The only thing that varies run-to-run is the absolute
// timestamps (anchored to now); the shape (roster, counts, per-window
// distribution, attribution) is identical every time.
func BuildPlan(catalog []MovieIdentity, now time.Time) (Plan, error) {
	members := roster()

	poolTotal := 0
	for _, n := range poolPerLoginMember {
		poolTotal += n
	}
	stashTotal := stashPerMember * len(loginMemberIndices)
	need := poolTotal + stashTotal + watchedCount
	if len(catalog) < need {
		return Plan{}, fmt.Errorf("dev-fixtures needs at least %d distinct movies, dataset has %d", need, len(catalog))
	}

	movies := make([]Movie, 0, need)
	next := 0 // cursor into the catalog; each movie consumes a distinct one (unique tmdb_id)
	take := func() MovieIdentity {
		f := catalog[next]
		next++
		return f
	}

	// Pool: recent, unwatched, per login member within the cap.
	for i, memberIdx := range loginMemberIndices {
		for k := 0; k < poolPerLoginMember[i]; k++ {
			f := take()
			movies = append(movies, Movie{
				Title:      f.Title,
				TMDBID:     f.TMDBID,
				Status:     domain.MovieStatusPool,
				AdderIndex: memberIdx,
				AddedAt:    now.Add(-time.Duration(k+1) * 24 * time.Hour),
			})
		}
	}

	// Stash: each login member's private backlog.
	for _, memberIdx := range loginMemberIndices {
		for k := range stashPerMember {
			f := take()
			movies = append(movies, Movie{
				Title:      f.Title,
				TMDBID:     f.TMDBID,
				Status:     domain.MovieStatusStash,
				AdderIndex: memberIdx,
				AddedAt:    now.Add(-time.Duration((k%60)+1) * 24 * time.Hour),
			})
		}
	}

	// Watched: spread over the stats windows, attributed across everyone.
	watchedIdx := 0
	for _, b := range watchedBuckets {
		for k := 0; k < b.count; k++ {
			f := take()
			age := bucketAge(b.minAge, b.maxAge, k, b.count)
			watchedAt := now.Add(-age)
			// Added a little before it was watched, deterministic and always
			// on-or-before watched_at.
			addedAt := watchedAt.Add(-time.Duration((watchedIdx%14)+2) * 24 * time.Hour)
			movies = append(movies, Movie{
				Title:      f.Title,
				TMDBID:     f.TMDBID,
				Status:     domain.MovieStatusWatched,
				AdderIndex: watchedAdderPattern[watchedIdx%len(watchedAdderPattern)],
				AddedAt:    addedAt,
				WatchedAt:  &watchedAt,
			})
			watchedIdx++
		}
	}

	return Plan{
		Members:     members,
		Movies:      movies,
		NextUpIndex: nextUpIndex,
		PoolLocked:  false,
	}, nil
}

// bucketAge spreads the k-th of n movies evenly across [minAge, maxAge]. The
// (k+0.5)/n midpoint keeps every movie strictly inside the window (no movie
// lands exactly on a boundary that a window preset might exclude).
func bucketAge(minAge, maxAge time.Duration, k, n int) time.Duration {
	span := maxAge - minAge
	return minAge + time.Duration((float64(k)+0.5)/float64(n)*float64(span))
}
