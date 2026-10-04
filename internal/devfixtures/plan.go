package devfixtures

import (
	"fmt"
	"strings"
	"time"

	"moviepickarr/internal/domain"
)

// devPassword is every seeded login's password; keep in step with docs/DEVELOPMENT.md.
const devPassword = "devpassword"

const (
	stashPerMember = 15
	watchedCount   = 120
)

// loginMemberIndices: 0 is the admin; 5 (placeholder) and 6 (archived) have no login.
var loginMemberIndices = []int{0, 1, 2, 3, 4}

// poolPerLoginMember stays within the per-member pool cap of 3.
var poolPerLoginMember = []int{3, 2, 3, 1, 2}

const nextUpIndex = 1

// Login is the local credential a seeded member logs in with.
type Login struct {
	Username string
	Password string
}

// Member is one roster member in the plan. Login is nil for the placeholder
// and the archived member.
type Member struct {
	Name     string
	Role     domain.Role
	Archived bool
	Login    *Login
}

// Movie is one seeded movie. AdderIndex indexes Plan.Members. WatchedAt is set
// only for watched movies, as the DB CHECK requires.
type Movie struct {
	Title      string
	TMDBID     int
	Status     domain.MovieStatus
	AdderIndex int
	AddedAt    time.Time
	WatchedAt  *time.Time
}

// Plan is the deterministic developer world that Apply writes in one transaction.
type Plan struct {
	Members     []Member
	Movies      []Movie
	NextUpIndex int
	PoolLocked  bool
}

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

// watchedAdderPattern attributes watched movies across every member, uneven so
// the leaderboard is worth reading. Its length divides watchedCount.
var watchedAdderPattern = []int{0, 1, 2, 3, 4, 6, 0, 1, 2, 3, 5, 6}

// watchedBuckets spread watched_at so every stats preset returns a different,
// non-empty leaderboard. Counts sum to watchedCount.
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

// BuildPlan composes the developer world. Only the timestamps, anchored to now,
// vary between runs.
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

	watchedIdx := 0
	for _, b := range watchedBuckets {
		for k := 0; k < b.count; k++ {
			f := take()
			age := bucketAge(b.minAge, b.maxAge, k, b.count)
			watchedAt := now.Add(-age)
			// Always on-or-before watched_at.
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
// midpoint keeps each movie off a window boundary that a preset might exclude.
func bucketAge(minAge, maxAge time.Duration, k, n int) time.Duration {
	span := maxAge - minAge
	return minAge + time.Duration((float64(k)+0.5)/float64(n)*float64(span))
}
