package server

import (
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"moviepickarr/internal/domain"
)

func TestParseStatsWindow(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		want    statsWindow
		wantErr bool
	}{
		{name: "default", input: "", want: statsWindow30d},
		{name: "24h", input: "24h", want: statsWindow24h},
		{name: "7d", input: "7d", want: statsWindow7d},
		{name: "30d", input: "30d", want: statsWindow30d},
		{name: "90d", input: "90d", want: statsWindow90d},
		{name: "1y", input: "1y", want: statsWindow1y},
		{name: "all-time", input: "all-time", want: statsWindowAllTime},
		{name: "custom", input: "custom", want: statsWindowCustom},
		{name: "invalid", input: "2w", wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := parseStatsWindow(tc.input)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("expected %q, got %q", tc.want, got)
			}
		})
	}
}

func TestBuildStatsResponse_WindowCountsAndSelectedBreakdown(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 2, 7, 12, 0, 0, 0, time.UTC)
	location := time.UTC

	movies := []*domain.Movie{
		{AddedByName: "Alice", WatchedAt: new(now.Add(-2 * time.Hour))},
		{AddedByName: "Bob", WatchedAt: new(now.Add(-25 * time.Hour))},
		{AddedByName: "Alice", WatchedAt: new(now.AddDate(0, 0, -8))},
		{AddedByName: "Cara", WatchedAt: new(now.AddDate(0, 0, -31))},
		{AddedByName: "Bob", WatchedAt: new(now.AddDate(0, 0, -95))},
		{AddedByName: "Alice", WatchedAt: new(now.AddDate(0, 0, -200))},
		{AddedByName: "Alice", WatchedAt: new(now.AddDate(0, 0, -370))},
		{AddedByName: "Nobody", WatchedAt: nil},
		{AddedByName: "Future", WatchedAt: new(now.Add(1 * time.Hour))},
	}

	got := buildStatsResponse(movies, nil, nil, nil, statsFilters{}, statsWindow30d, nil, location, "UTC", now)

	if got.SelectedWindow != "30d" {
		t.Fatalf("expected selectedWindow 30d, got %q", got.SelectedWindow)
	}
	if got.SelectedWindowCount != 3 {
		t.Fatalf("expected selected window count 3, got %d", got.SelectedWindowCount)
	}
	if got.TotalWatched != 7 {
		t.Fatalf("expected total watched 7, got %d", got.TotalWatched)
	}

	expectedWindowCounts := map[string]int{
		"24h":      1,
		"7d":       2,
		"30d":      3,
		"90d":      4,
		"1y":       6,
		"all-time": 7,
	}
	for _, entry := range got.CountsByWindow {
		want, ok := expectedWindowCounts[entry.Window]
		if !ok {
			t.Fatalf("unexpected window %q", entry.Window)
		}
		if entry.Count != want {
			t.Fatalf("window %s expected %d got %d", entry.Window, want, entry.Count)
		}
	}

	// Every all-time picker is present every window (Cara watched 31d ago, so
	// she's 0 in the 30d window but still listed), ordered by stable all-time
	// total (Alice 4, Bob 2, Cara 1) — not the window count — so rows don't jump
	// when switching ranges. "Future" (future-dated) and "Nobody" (unwatched)
	// are excluded.
	if len(got.WatchedByUser) != 3 {
		t.Fatalf("expected 3 members, got %d (%+v)", len(got.WatchedByUser), got.WatchedByUser)
	}
	if got.WatchedByUser[0].Name != "Alice" || got.WatchedByUser[0].Count != 2 {
		t.Fatalf("unexpected member[0] %+v", got.WatchedByUser[0])
	}
	if got.WatchedByUser[1].Name != "Bob" || got.WatchedByUser[1].Count != 1 {
		t.Fatalf("unexpected member[1] %+v", got.WatchedByUser[1])
	}
	if got.WatchedByUser[2].Name != "Cara" || got.WatchedByUser[2].Count != 0 {
		t.Fatalf("unexpected member[2] %+v", got.WatchedByUser[2])
	}

	friday := findCountByName(t, got.WeekdayActivity, "Fri")
	saturday := findCountByName(t, got.WeekdayActivity, "Sat")
	if friday != 2 {
		t.Fatalf("expected Fri count 2, got %d", friday)
	}
	if saturday != 1 {
		t.Fatalf("expected Sat count 1, got %d", saturday)
	}

	if got.HourActivity[10].Count != 1 {
		t.Fatalf("expected hour 10:00 count 1, got %d", got.HourActivity[10].Count)
	}
	if got.HourActivity[11].Count != 1 {
		t.Fatalf("expected hour 11:00 count 1, got %d", got.HourActivity[11].Count)
	}
	if got.HourActivity[12].Count != 1 {
		t.Fatalf("expected hour 12:00 count 1, got %d", got.HourActivity[12].Count)
	}
}

func TestBuildStatsResponse_TimezoneAffectsWeekdayAndHour(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 2, 7, 12, 0, 0, 0, time.UTC)
	watchedAt := time.Date(2026, 2, 7, 1, 0, 0, 0, time.UTC)
	movies := []*domain.Movie{
		{AddedByName: "Alice", WatchedAt: &watchedAt},
	}

	utcStats := buildStatsResponse(movies, nil, nil, nil, statsFilters{}, statsWindow24h, nil, time.UTC, "UTC", now)
	pst, err := time.LoadLocation("America/Los_Angeles")
	if err != nil {
		t.Fatalf("load location: %v", err)
	}
	pstStats := buildStatsResponse(movies, nil, nil, nil, statsFilters{}, statsWindow24h, nil, pst, "America/Los_Angeles", now)

	if findCountByName(t, utcStats.WeekdayActivity, "Sat") != 1 {
		t.Fatalf("expected UTC Saturday count 1")
	}
	if findCountByName(t, pstStats.WeekdayActivity, "Fri") != 1 {
		t.Fatalf("expected PST Friday count 1")
	}

	if utcStats.HourActivity[1].Count != 1 {
		t.Fatalf("expected UTC hour 01:00 count 1, got %d", utcStats.HourActivity[1].Count)
	}
	if pstStats.HourActivity[17].Count != 1 {
		t.Fatalf("expected PST hour 17:00 count 1, got %d", pstStats.HourActivity[17].Count)
	}
}

func TestParseCustomDateRange(t *testing.T) {
	t.Parallel()

	location, err := time.LoadLocation("America/Los_Angeles")
	if err != nil {
		t.Fatalf("load location: %v", err)
	}

	custom, err := parseCustomDateRange("2026-01-10", "2026-01-12", location, statsWindowCustom)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if custom == nil {
		t.Fatalf("expected custom range")
	}
	if custom.StartDate != "2026-01-10" || custom.EndDate != "2026-01-12" {
		t.Fatalf("unexpected custom dates: %+v", custom)
	}

	startLocal := custom.StartUTC.In(location).Format("2006-01-02 15:04")
	endLocal := custom.EndUTC.In(location).Format("2006-01-02 15:04")
	if startLocal != "2026-01-10 00:00" {
		t.Fatalf("unexpected start local %s", startLocal)
	}
	if endLocal != "2026-01-13 00:00" {
		t.Fatalf("unexpected end local %s", endLocal)
	}
}

func TestParseCustomDateRange_Validation(t *testing.T) {
	t.Parallel()

	location := time.UTC

	testCases := []struct {
		name    string
		start   string
		end     string
		window  statsWindow
		wantErr bool
	}{
		{name: "non-custom ignores dates", window: statsWindow30d, start: "", end: "", wantErr: false},
		{name: "missing start", window: statsWindowCustom, start: "", end: "2026-01-12", wantErr: true},
		{name: "missing end", window: statsWindowCustom, start: "2026-01-10", end: "", wantErr: true},
		{name: "invalid start", window: statsWindowCustom, start: "2026/01/10", end: "2026-01-12", wantErr: true},
		{name: "invalid end", window: statsWindowCustom, start: "2026-01-10", end: "2026/01/12", wantErr: true},
		{name: "reversed", window: statsWindowCustom, start: "2026-01-13", end: "2026-01-12", wantErr: true},
		{name: "same day", window: statsWindowCustom, start: "2026-01-12", end: "2026-01-12", wantErr: false},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			custom, err := parseCustomDateRange(tc.start, tc.end, location, tc.window)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tc.window == statsWindowCustom && custom == nil {
				t.Fatalf("expected custom range for custom window")
			}
			if tc.window != statsWindowCustom && custom != nil {
				t.Fatalf("expected nil custom range for non-custom window")
			}
		})
	}
}

func TestBuildStatsResponse_CustomRange(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 2, 7, 12, 0, 0, 0, time.UTC)
	location := time.UTC
	custom := &customDateRange{
		StartUTC:  time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC),
		EndUTC:    time.Date(2026, 2, 4, 0, 0, 0, 0, time.UTC),
		StartDate: "2026-02-01",
		EndDate:   "2026-02-03",
	}

	movies := []*domain.Movie{
		{AddedByName: "Alice", WatchedAt: new(time.Date(2026, 2, 1, 10, 0, 0, 0, time.UTC))},
		{AddedByName: "Bob", WatchedAt: new(time.Date(2026, 2, 3, 23, 59, 59, 0, time.UTC))},
		{AddedByName: "Cara", WatchedAt: new(time.Date(2026, 2, 4, 0, 0, 0, 0, time.UTC))},
	}

	got := buildStatsResponse(movies, nil, nil, nil, statsFilters{}, statsWindowCustom, custom, location, "UTC", now)
	if got.SelectedWindow != "custom" {
		t.Fatalf("expected selectedWindow custom, got %q", got.SelectedWindow)
	}
	if got.SelectedWindowCount != 2 {
		t.Fatalf("expected selected window count 2, got %d", got.SelectedWindowCount)
	}
	if got.CustomRangeStart != "2026-02-01" || got.CustomRangeEnd != "2026-02-03" {
		t.Fatalf("unexpected custom range fields: %s to %s", got.CustomRangeStart, got.CustomRangeEnd)
	}
}

func findCountByName(t *testing.T, counts []statsNamedCount, name string) int {
	t.Helper()

	for _, count := range counts {
		if count.Name == name {
			return count.Count
		}
	}
	t.Fatalf("name %q not found", name)
	return 0
}

func TestBuildStatsCacheKey(t *testing.T) {
	t.Parallel()

	custom := &customDateRange{
		StartDate: "2026-01-01",
		EndDate:   "2026-01-31",
	}

	got := buildStatsCacheKey(statsWindowCustom, "Europe/Berlin", custom, statsFilters{})
	want := "custom|Europe/Berlin|2026-01-01|2026-01-31||||0|0"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}

	gotPreset := buildStatsCacheKey(statsWindow30d, "UTC", nil, statsFilters{})
	wantPreset := "30d|UTC||||||0|0"
	if gotPreset != wantPreset {
		t.Fatalf("expected %q, got %q", wantPreset, gotPreset)
	}

	// Filters are part of the key — same window, different subsets must never
	// collide — and the genre is lowercased to match the case-insensitive filter.
	gotFiltered := buildStatsCacheKey(statsWindow30d, "UTC", nil, statsFilters{
		Genre: "Action", ActorIDs: []int{530, 6384}, CrewIDs: []int{9340}, ReleaseYear: 1999,
	})
	wantFiltered := "30d|UTC|||action|530,6384|9340|1999|0"
	if gotFiltered != wantFiltered {
		t.Fatalf("expected %q, got %q", wantFiltered, gotFiltered)
	}
	if gotFiltered == gotPreset {
		t.Fatalf("filtered key must differ from the unfiltered key")
	}

	// A decade filter occupies its own key segment, so it can never collide with
	// the equivalent exact-year selection.
	gotDecade := buildStatsCacheKey(statsWindow30d, "UTC", nil, statsFilters{ReleaseDecade: 1990})
	wantDecade := "30d|UTC||||||0|1990"
	if gotDecade != wantDecade {
		t.Fatalf("expected %q, got %q", wantDecade, gotDecade)
	}
	if gotDecade == buildStatsCacheKey(statsWindow30d, "UTC", nil, statsFilters{ReleaseYear: 1990}) {
		t.Fatalf("decade key must differ from the same-numbered year key")
	}

	// The id lists are canonicalized at parse time, so the same selection in a
	// different request order must land on the same cache entry.
	first, err := parseStatsFilters("", "6384,530", "", "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	second, err := parseStatsFilters("", "530,6384", "", "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if buildStatsCacheKey(statsWindow30d, "UTC", nil, first) != buildStatsCacheKey(statsWindow30d, "UTC", nil, second) {
		t.Fatalf("expected order-insensitive cache keys, got %q vs %q",
			buildStatsCacheKey(statsWindow30d, "UTC", nil, first), buildStatsCacheKey(statsWindow30d, "UTC", nil, second))
	}
}

func TestStatsCacheSetGetInvalidate(t *testing.T) {
	t.Parallel()

	h := &handler{
		statsCache:    make(map[string]statsCacheEntry),
		statsCacheTTL: time.Minute,
	}
	now := time.Date(2026, 2, 7, 12, 0, 0, 0, time.UTC)
	key := "30d|UTC||"
	expected := statsResponse{
		SelectedWindow:      "30d",
		SelectedWindowCount: 7,
		Timezone:            "UTC",
	}

	h.setCachedStats(key, expected, now)

	got, ok := h.getCachedStats(key, now.Add(30*time.Second))
	if !ok {
		t.Fatalf("expected cache hit")
	}
	if got.SelectedWindow != expected.SelectedWindow || got.SelectedWindowCount != expected.SelectedWindowCount {
		t.Fatalf("unexpected cached payload: %+v", got)
	}

	h.invalidateStatsCache()

	if _, ok := h.getCachedStats(key, now.Add(30*time.Second)); ok {
		t.Fatalf("expected cache miss after invalidation")
	}
}

func TestStatsCacheExpiresEntries(t *testing.T) {
	t.Parallel()

	h := &handler{
		statsCache:    make(map[string]statsCacheEntry),
		statsCacheTTL: time.Minute,
	}
	now := time.Date(2026, 2, 7, 12, 0, 0, 0, time.UTC)
	key := "7d|UTC||"
	h.setCachedStats(key, statsResponse{SelectedWindow: "7d"}, now)

	if _, ok := h.getCachedStats(key, now.Add(61*time.Second)); ok {
		t.Fatalf("expected expired cache entry")
	}
}

func TestStatsCacheBoundedSize(t *testing.T) {
	t.Parallel()

	h := &handler{
		statsCache:    make(map[string]statsCacheEntry),
		statsCacheTTL: time.Minute,
	}
	now := time.Date(2026, 2, 7, 12, 0, 0, 0, time.UTC)
	for i := range statsCacheMaxEntries {
		h.setCachedStats(fmt.Sprintf("30d|UTC|||junk%d|0|0", i), statsResponse{}, now)
	}
	if len(h.statsCache) != statsCacheMaxEntries {
		t.Fatalf("expected %d entries, got %d", statsCacheMaxEntries, len(h.statsCache))
	}

	// The insert that would exceed the cap resets the map instead of growing it.
	h.setCachedStats("one-over", statsResponse{}, now)
	if len(h.statsCache) != 1 {
		t.Fatalf("expected the cache to reset to 1 entry at the cap, got %d", len(h.statsCache))
	}
	if _, ok := h.getCachedStats("one-over", now); !ok {
		t.Fatalf("expected the newest entry to survive the reset")
	}
}

// statsEnrichedFixture builds three watched movies inside every window: The
// Matrix and Speed are enriched (sharing Keanu Reeves as cast), Mystery is not
// enriched at all — so it must vanish whenever any filter is active.
func statsEnrichedFixture(now time.Time) ([]*domain.Movie, metaByID, creditsByID) {
	keanuProfile := "/kr.jpg"
	movies := []*domain.Movie{
		{ID: 1, Title: "The Matrix", AddedByName: "Alice", WatchedAt: new(now.Add(-2 * time.Hour))},
		{ID: 2, Title: "Speed", AddedByName: "Bob", WatchedAt: new(now.Add(-3 * time.Hour))},
		{ID: 3, Title: "Mystery", AddedByName: "Cara", WatchedAt: new(now.Add(-4 * time.Hour))},
	}
	meta := metaByID{
		1: {MovieID: 1, Genres: []string{"Action", "Science Fiction"}, ReleaseDate: "1999-03-30", Runtime: 136, VoteAverage: 8.2},
		2: {MovieID: 2, Genres: []string{"Action"}, ReleaseDate: "1994-06-10", Runtime: 0, VoteAverage: 0}, // zero runtime/rating skip the averages
	}
	credits := creditsByID{
		1: {
			{MovieID: 1, Person: domain.Person{ID: 6384, Name: "Keanu Reeves", ProfilePath: &keanuProfile}, Kind: domain.CreditKindCast, Character: "Neo", CastOrder: 0},
			{MovieID: 1, Person: domain.Person{ID: 530, Name: "Carrie-Anne Moss"}, Kind: domain.CreditKindCast, Character: "Trinity", CastOrder: 1},
			{MovieID: 1, Person: domain.Person{ID: 9340, Name: "Lana Wachowski"}, Kind: domain.CreditKindCrew, Job: "Director", Department: "Directing"},
			{MovieID: 1, Person: domain.Person{ID: 9340, Name: "Lana Wachowski"}, Kind: domain.CreditKindCrew, Job: "Writer", Department: "Writing"},
		},
		2: {
			{MovieID: 2, Person: domain.Person{ID: 6384, Name: "Keanu Reeves", ProfilePath: &keanuProfile}, Kind: domain.CreditKindCast, Character: "Jack Traven", CastOrder: 0},
			{MovieID: 2, Person: domain.Person{ID: 56, Name: "Jan de Bont"}, Kind: domain.CreditKindCrew, Job: "Director", Department: "Directing"},
			// Writer-only crew — proves crewIds match any whitelisted job, not
			// just directors.
			{MovieID: 2, Person: domain.Person{ID: 7707, Name: "Graham Yost"}, Kind: domain.CreditKindCrew, Job: "Writer", Department: "Writing"},
		},
	}
	return movies, meta, credits
}

func TestBuildStatsResponse_EnrichedAggregates(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 2, 7, 12, 0, 0, 0, time.UTC)
	movies, meta, credits := statsEnrichedFixture(now)

	got := buildStatsResponse(movies, meta, credits, nil, statsFilters{}, statsWindow30d, nil, time.UTC, "UTC", now)

	// Without filters the unenriched movie still counts toward the totals…
	if got.SelectedWindowCount != 3 || got.TotalWatched != 3 {
		t.Fatalf("expected 3/3 movies, got %d/%d", got.SelectedWindowCount, got.TotalWatched)
	}

	// …but only enriched movies contribute to the enrichment-derived tallies.
	wantGenres := []statsNamedCount{{Name: "Action", Count: 2}, {Name: "Science Fiction", Count: 1}}
	if !reflect.DeepEqual(got.TopGenres, wantGenres) {
		t.Fatalf("topGenres mismatch: %+v", got.TopGenres)
	}

	wantActors := []statsPersonCount{
		{PersonID: 6384, Name: "Keanu Reeves", ProfilePath: "/kr.jpg", Count: 2},
		{PersonID: 530, Name: "Carrie-Anne Moss", Count: 1},
	}
	if !reflect.DeepEqual(got.TopActors, wantActors) {
		t.Fatalf("topActors mismatch: %+v", got.TopActors)
	}

	// Lana's Writer row must not double-tally her as director; equal counts
	// order by name ascending.
	wantDirectors := []statsPersonCount{
		{PersonID: 56, Name: "Jan de Bont", Count: 1},
		{PersonID: 9340, Name: "Lana Wachowski", Count: 1},
	}
	if !reflect.DeepEqual(got.TopDirectors, wantDirectors) {
		t.Fatalf("topDirectors mismatch: %+v", got.TopDirectors)
	}

	wantYears := []statsYearCount{{Year: 1994, Count: 1}, {Year: 1999, Count: 1}}
	if !reflect.DeepEqual(got.ReleaseYears, wantYears) {
		t.Fatalf("releaseYears mismatch: %+v", got.ReleaseYears)
	}

	// Speed's zero runtime/rating stay out of the denominators.
	wantRuntime := statsRuntime{TotalMinutes: 136, AverageMinutes: 136, LongestMinutes: 136, LongestTitle: "The Matrix"}
	if got.Runtime != wantRuntime {
		t.Fatalf("runtime mismatch: %+v", got.Runtime)
	}
	if got.AverageRating != 8.2 {
		t.Fatalf("expected averageRating 8.2, got %v", got.AverageRating)
	}

	if !reflect.DeepEqual(got.Filters, statsFiltersEcho{}) {
		t.Fatalf("expected empty filters echo, got %+v", got.Filters)
	}
}

func TestBuildStatsResponse_Filters(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 2, 7, 12, 0, 0, 0, time.UTC)
	movies, meta, credits := statsEnrichedFixture(now)
	// The member roster: Dana has never picked anything; everyone still keeps
	// a leaderboard row under every filter (zero when nothing matches).
	members := []string{"Alice", "Bob", "Cara", "Dana"}

	cases := []struct {
		name       string
		filters    statsFilters
		wantTotal  int
		wantUsers  []string
		wantActors []statsFilterPerson
		wantCrew   []statsFilterPerson
		wantGenre  string // canonical echo when it differs from the request casing
	}{
		{
			name:      "genre is case-insensitive",
			filters:   statsFilters{Genre: "science fiction"},
			wantTotal: 1,
			wantUsers: []string{"Alice", "Bob", "Cara", "Dana"},
			wantGenre: "Science Fiction",
		},
		{
			// U+0130 "İ" ToLower-folds to "i" but has no simple case folding;
			// matching must use the same fold as the cache key so colliding
			// keys can't carry different match sets.
			name:      "genre folding matches the cache key fold",
			filters:   statsFilters{Genre: "scİence fiction"},
			wantTotal: 1,
			wantUsers: []string{"Alice", "Bob", "Cara", "Dana"},
			wantGenre: "Science Fiction",
		},
		{
			name:       "actorIds match cast",
			filters:    statsFilters{ActorIDs: []int{6384}},
			wantTotal:  2,
			wantUsers:  []string{"Alice", "Bob", "Cara", "Dana"},
			wantActors: []statsFilterPerson{{PersonID: 6384, Name: "Keanu Reeves"}},
		},
		{
			name:       "actorIds ignore crew credits",
			filters:    statsFilters{ActorIDs: []int{9340}}, // Lana is crew-only
			wantTotal:  0,
			wantUsers:  []string{"Alice", "Bob", "Cara", "Dana"},
			wantActors: []statsFilterPerson{{PersonID: 9340, Name: "Lana Wachowski"}},
		},
		{
			name:      "crewIds match directors",
			filters:   statsFilters{CrewIDs: []int{9340}},
			wantTotal: 1,
			wantUsers: []string{"Alice", "Bob", "Cara", "Dana"},
			wantCrew:  []statsFilterPerson{{PersonID: 9340, Name: "Lana Wachowski"}},
		},
		{
			name:      "crewIds match any whitelisted job",
			filters:   statsFilters{CrewIDs: []int{7707}}, // Graham Yost, Writer-only
			wantTotal: 1,
			wantUsers: []string{"Bob", "Alice", "Cara", "Dana"},
			wantCrew:  []statsFilterPerson{{PersonID: 7707, Name: "Graham Yost"}},
		},
		{
			name:      "crewIds ignore cast credits",
			filters:   statsFilters{CrewIDs: []int{6384}}, // Keanu is cast-only
			wantTotal: 0,
			wantUsers: []string{"Alice", "Bob", "Cara", "Dana"},
			wantCrew:  []statsFilterPerson{{PersonID: 6384, Name: "Keanu Reeves"}},
		},
		{
			name:      "actorIds are any-of within the list",
			filters:   statsFilters{ActorIDs: []int{530, 6384}}, // Trinity OR Neo
			wantTotal: 2,
			wantUsers: []string{"Alice", "Bob", "Cara", "Dana"},
			wantActors: []statsFilterPerson{
				{PersonID: 530, Name: "Carrie-Anne Moss"},
				{PersonID: 6384, Name: "Keanu Reeves"},
			},
		},
		{
			name:       "actor and crew groups intersect",
			filters:    statsFilters{ActorIDs: []int{6384}, CrewIDs: []int{9340}}, // Keanu AND Lana → Matrix only
			wantTotal:  1,
			wantUsers:  []string{"Alice", "Bob", "Cara", "Dana"},
			wantActors: []statsFilterPerson{{PersonID: 6384, Name: "Keanu Reeves"}},
			wantCrew:   []statsFilterPerson{{PersonID: 9340, Name: "Lana Wachowski"}},
		},
		{
			name:      "releaseYear",
			filters:   statsFilters{ReleaseYear: 1994},
			wantTotal: 1,
			wantUsers: []string{"Bob", "Alice", "Cara", "Dana"},
		},
		{
			name:      "releaseDecade spans the whole decade",
			filters:   statsFilters{ReleaseDecade: 1990}, // The Matrix (1999) + Speed (1994)
			wantTotal: 2,
			wantUsers: []string{"Alice", "Bob", "Cara", "Dana"},
		},
		{
			name:       "combined filters intersect",
			filters:    statsFilters{Genre: "Action", ActorIDs: []int{6384}, ReleaseYear: 1999},
			wantTotal:  1,
			wantUsers:  []string{"Alice", "Bob", "Cara", "Dana"},
			wantActors: []statsFilterPerson{{PersonID: 6384, Name: "Keanu Reeves"}},
		},
		{
			name:      "zero-match combination",
			filters:   statsFilters{Genre: "Action", ReleaseYear: 2003},
			wantTotal: 0,
			wantUsers: []string{"Alice", "Bob", "Cara", "Dana"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := buildStatsResponse(movies, meta, credits, members, tc.filters, statsWindow30d, nil, time.UTC, "UTC", now)

			// EVERY aggregate is computed over the filtered subset — the
			// all-time total and per-window counts included. The unenriched
			// "Mystery" fails every active filter, so it never counts — but
			// Cara still keeps her zero leaderboard row via the roster.
			if got.SelectedWindowCount != tc.wantTotal || got.TotalWatched != tc.wantTotal {
				t.Fatalf("expected %d movies, got selected=%d total=%d", tc.wantTotal, got.SelectedWindowCount, got.TotalWatched)
			}
			names := make([]string, 0, len(got.WatchedByUser))
			for _, row := range got.WatchedByUser {
				names = append(names, row.Name)
			}
			if !reflect.DeepEqual(names, tc.wantUsers) {
				t.Fatalf("watchedByUser mismatch: got %v want %v", names, tc.wantUsers)
			}

			// The echo carries the active filters, with each person resolved
			// to a display name from the credit rows and the genre resolved
			// to its stored canonical casing.
			wantGenre := tc.wantGenre
			if wantGenre == "" {
				wantGenre = tc.filters.Genre
			}
			if got.Filters.Genre != wantGenre || got.Filters.ReleaseYear != tc.filters.ReleaseYear ||
				got.Filters.ReleaseDecade != tc.filters.ReleaseDecade {
				t.Fatalf("filters echo mismatch: %+v", got.Filters)
			}
			if !reflect.DeepEqual(got.Filters.Actors, tc.wantActors) {
				t.Fatalf("expected actors echo %+v, got %+v", tc.wantActors, got.Filters.Actors)
			}
			if !reflect.DeepEqual(got.Filters.Crew, tc.wantCrew) {
				t.Fatalf("expected crew echo %+v, got %+v", tc.wantCrew, got.Filters.Crew)
			}

			if tc.wantTotal == 0 {
				// Zero-match: empty aggregates, zeroed KPIs — never NaN.
				if len(got.TopGenres) != 0 || len(got.TopActors) != 0 || len(got.TopDirectors) != 0 || len(got.ReleaseYears) != 0 {
					t.Fatalf("expected empty aggregates, got %+v", got)
				}
				if got.Runtime != (statsRuntime{}) || got.AverageRating != 0 {
					t.Fatalf("expected zero runtime/rating, got %+v / %v", got.Runtime, got.AverageRating)
				}
			}
		})
	}
}

func TestBuildStatsResponse_MembersAlwaysListed(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 2, 7, 12, 0, 0, 0, time.UTC)
	movies, meta, credits := statsEnrichedFixture(now)

	// Dana is on the roster but has never picked; Cara's only movie is
	// unenriched and fails the actor filter. Both keep zero rows — members
	// never vanish from the leaderboard, whatever the window or filters.
	// Blank roster names are skipped rather than rendered as empty rows.
	members := []string{"Alice", "Bob", "Cara", "Dana", " "}
	got := buildStatsResponse(movies, meta, credits, members, statsFilters{ActorIDs: []int{6384}}, statsWindow30d, nil, time.UTC, "UTC", now)

	want := []statsNamedCount{
		{Name: "Alice", Count: 1},
		{Name: "Bob", Count: 1},
		{Name: "Cara", Count: 0},
		{Name: "Dana", Count: 0},
	}
	if !reflect.DeepEqual(got.WatchedByUser, want) {
		t.Fatalf("watchedByUser mismatch: got %+v want %+v", got.WatchedByUser, want)
	}
}

func TestBuildStatsResponse_TopPeopleCapAndOrdering(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 2, 7, 12, 0, 0, 0, time.UTC)

	// Movie 1 bills 13 actors; movie 2 re-bills only "Actor 13". The cap is
	// 12: Actor 13 leads with count 2, ties order by name, "Actor 12" drops.
	castOf := func(movieID int, ids ...int) []domain.MovieCredit {
		out := make([]domain.MovieCredit, 0, len(ids))
		for order, id := range ids {
			out = append(out, domain.MovieCredit{
				MovieID:   movieID,
				Person:    domain.Person{ID: id, Name: fmt.Sprintf("Actor %02d", id)},
				Kind:      domain.CreditKindCast,
				CastOrder: order,
			})
		}
		return out
	}
	movies := []*domain.Movie{
		{ID: 1, Title: "Ensemble", AddedByName: "Alice", WatchedAt: new(now.Add(-2 * time.Hour))},
		{ID: 2, Title: "Sequel", AddedByName: "Alice", WatchedAt: new(now.Add(-3 * time.Hour))},
	}
	credits := creditsByID{
		1: castOf(1, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13),
		2: castOf(2, 13),
	}

	got := buildStatsResponse(movies, nil, credits, nil, statsFilters{}, statsWindow30d, nil, time.UTC, "UTC", now)

	if len(got.TopActors) != statsTopPeopleLimit {
		t.Fatalf("expected %d actors, got %d", statsTopPeopleLimit, len(got.TopActors))
	}
	if got.TopActors[0].Name != "Actor 13" || got.TopActors[0].Count != 2 {
		t.Fatalf("expected Actor 13 (count 2) first, got %+v", got.TopActors[0])
	}
	if got.TopActors[1].Name != "Actor 01" || got.TopActors[len(got.TopActors)-1].Name != "Actor 11" {
		t.Fatalf("tie-break by name mismatch: %+v", got.TopActors)
	}
	for _, actor := range got.TopActors {
		if actor.Name == "Actor 12" {
			t.Fatalf("Actor 12 should be cut by the cap: %+v", got.TopActors)
		}
	}
}

func TestBuildPersonCounts_SameNameTieBreak(t *testing.T) {
	t.Parallel()

	// Two distinct people sharing a display name and a count: the id breaks
	// the tie so the order — and who survives the cap — never flips between
	// rebuilds (map iteration is random, the sort is unstable).
	counts := map[int]*statsPersonCount{
		77: {PersonID: 77, Name: "John Smith", Count: 1},
		12: {PersonID: 12, Name: "John Smith", Count: 1},
		50: {PersonID: 50, Name: "Aaron Ash", Count: 1},
	}

	got := buildPersonCounts(counts, 2)
	if len(got) != 2 {
		t.Fatalf("expected the cap of 2, got %d", len(got))
	}
	if got[0].PersonID != 50 || got[1].PersonID != 12 {
		t.Fatalf("expected ids [50 12] (name asc, then id asc), got [%d %d]", got[0].PersonID, got[1].PersonID)
	}
}

func TestBuildFiltersEcho_CanonicalGenreCasing(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 2, 7, 12, 0, 0, 0, time.UTC)
	movies, meta, credits := statsEnrichedFixture(now)

	// Matching is case-insensitive and the cache key folds case, so the echo
	// must resolve to the stored canonical casing — otherwise a cache hit
	// would serve another requester's casing.
	payload := buildStatsResponse(movies, meta, credits, nil, statsFilters{Genre: "aCtIoN"}, statsWindowAllTime, nil, time.UTC, "UTC", now)
	if payload.Filters.Genre != "Action" {
		t.Fatalf("expected the stored canonical casing %q, got %q", "Action", payload.Filters.Genre)
	}
}

func TestMovieMatchesStatsFilters_Unenriched(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		filters statsFilters
		want    bool
	}{
		{name: "no filters pass", filters: statsFilters{}, want: true},
		{name: "genre fails", filters: statsFilters{Genre: "Action"}, want: false},
		{name: "actors fail", filters: statsFilters{ActorIDs: []int{1}}, want: false},
		{name: "crew fails", filters: statsFilters{CrewIDs: []int{1}}, want: false},
		{name: "year fails", filters: statsFilters{ReleaseYear: 1999}, want: false},
		{name: "decade fails", filters: statsFilters{ReleaseDecade: 1990}, want: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			// nil metadata + nil credits = never enriched.
			if got := movieMatchesStatsFilters(nil, nil, tc.filters); got != tc.want {
				t.Fatalf("expected %v, got %v", tc.want, got)
			}
		})
	}
}

func TestParseStatsFilters(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		genre   string
		actors  string
		crew    string
		year    string
		decade  string
		want    statsFilters
		wantErr bool
	}{
		{name: "all empty", want: statsFilters{}},
		{name: "genre trimmed", genre: "  Action  ", want: statsFilters{Genre: "Action"}},
		{name: "valid actorIds", actors: "6384", want: statsFilters{ActorIDs: []int{6384}}},
		{name: "valid crewIds", crew: "9340,56", want: statsFilters{CrewIDs: []int{56, 9340}}},
		{name: "valid releaseYear", year: "1999", want: statsFilters{ReleaseYear: 1999}},
		{
			name: "all combined", genre: "Drama", actors: "1,2", crew: "3", year: "2001",
			want: statsFilters{Genre: "Drama", ActorIDs: []int{1, 2}, CrewIDs: []int{3}, ReleaseYear: 2001},
		},
		{name: "year lower bound", year: "1870", want: statsFilters{ReleaseYear: 1870}},
		{name: "year upper bound", year: "2100", want: statsFilters{ReleaseYear: 2100}},
		{name: "valid decade", decade: "1990", want: statsFilters{ReleaseDecade: 1990}},
		{name: "decade lower bound", decade: "1870", want: statsFilters{ReleaseDecade: 1870}},
		{name: "actorIds zero", actors: "0", wantErr: true},
		{name: "actorIds negative", actors: "-3", wantErr: true},
		{name: "actorIds garbage", actors: "keanu", wantErr: true},
		{name: "crewIds garbage", crew: "1,lana", wantErr: true},
		{name: "year below sanity range", year: "1869", wantErr: true},
		{name: "year above sanity range", year: "2101", wantErr: true},
		{name: "year garbage", year: "nineteen99", wantErr: true},
		{name: "decade not a multiple of ten", decade: "1995", wantErr: true},
		{name: "decade below sanity range", decade: "1860", wantErr: true},
		{name: "decade above sanity range", decade: "2110", wantErr: true},
		{name: "decade garbage", decade: "90s", wantErr: true},
		{name: "year and decade are mutually exclusive", year: "1994", decade: "1990", wantErr: true},
		{name: "genre at length cap", genre: strings.Repeat("x", statsMaxGenreLength), want: statsFilters{Genre: strings.Repeat("x", statsMaxGenreLength)}},
		{name: "genre too long", genre: strings.Repeat("x", statsMaxGenreLength+1), wantErr: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := parseStatsFilters(tc.genre, tc.actors, tc.crew, tc.year, tc.decade)
			if tc.wantErr {
				if !errors.Is(err, domain.ErrInvalidInput) {
					t.Fatalf("expected ErrInvalidInput, got %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("expected %+v, got %+v", tc.want, got)
			}
		})
	}
}

func TestParseIDList(t *testing.T) {
	t.Parallel()

	overCap := make([]string, statsMaxPeopleFilterIDs+1)
	for i := range overCap {
		overCap[i] = strconv.Itoa(i + 1)
	}

	cases := []struct {
		name    string
		input   string
		want    []int
		wantErr bool
	}{
		// Empty input must yield nil — not an empty slice — so the filters
		// echo omits the field entirely.
		{name: "empty", input: "", want: nil},
		{name: "blank", input: "   ", want: nil},
		{name: "single", input: "6384", want: []int{6384}},
		{name: "multiple sorted", input: "530,6384", want: []int{530, 6384}},
		{name: "canonicalizes order", input: "6384,530", want: []int{530, 6384}},
		{name: "dedupes", input: "6384,530,6384", want: []int{530, 6384}},
		{name: "tolerates whitespace", input: " 6384 , 530 ", want: []int{530, 6384}},
		{name: "at cap", input: strings.Join(overCap[:statsMaxPeopleFilterIDs], ","), want: nil},
		{name: "over cap", input: strings.Join(overCap, ","), wantErr: true},
		{name: "zero", input: "0", wantErr: true},
		{name: "negative", input: "-3", wantErr: true},
		{name: "garbage", input: "keanu", wantErr: true},
		{name: "trailing comma", input: "6384,", wantErr: true},
		{name: "empty segment", input: "6384,,530", wantErr: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := parseIDList("actorIds", tc.input)
			if tc.wantErr {
				if !errors.Is(err, domain.ErrInvalidInput) {
					t.Fatalf("expected ErrInvalidInput, got %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tc.name == "at cap" {
				if len(got) != statsMaxPeopleFilterIDs {
					t.Fatalf("expected %d ids at the cap, got %d", statsMaxPeopleFilterIDs, len(got))
				}
				return
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("expected %v, got %v", tc.want, got)
			}
		})
	}
}
