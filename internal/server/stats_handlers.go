package server

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"moviepickarr/internal/domain"

	"github.com/gofiber/fiber/v2"
)

type statsWindow string

const (
	statsDateFormat                = "2006-01-02"
	statsWindow24h     statsWindow = "24h"
	statsWindow7d      statsWindow = "7d"
	statsWindow30d     statsWindow = "30d"
	statsWindow90d     statsWindow = "90d"
	statsWindow1y      statsWindow = "1y"
	statsWindowAllTime statsWindow = "all-time"
	statsWindowCustom  statsWindow = "custom"

	// Sanity bounds for the releaseYear filter (film history through a
	// comfortable future margin).
	statsMinReleaseYear = 1870
	statsMaxReleaseYear = 2100

	// statsTopPeopleLimit caps the topDirectors/topActors lists.
	statsTopPeopleLimit = 12

	// statsMaxGenreLength bounds the genre filter; TMDB genre names are short,
	// so anything longer is junk that would only bloat the cache key space.
	statsMaxGenreLength = 64

	// statsMaxPeopleFilterIDs bounds the actorIds/crewIds lists; real
	// drill-downs select a handful of people, so anything longer is junk that
	// would only bloat the cache key space.
	statsMaxPeopleFilterIDs = 25

	// statsCacheMaxEntries caps the response cache. Real usage needs a handful
	// of window/filter combos, but the key space is request-controlled, so a
	// cap keeps junk filter spam from growing the map unboundedly between
	// invalidations.
	statsCacheMaxEntries = 256
)

var statsWindowOrder = []statsWindow{
	statsWindow24h,
	statsWindow7d,
	statsWindow30d,
	statsWindow90d,
	statsWindow1y,
	statsWindowAllTime,
}

type statsCacheEntry struct {
	response  statsResponse
	expiresAt time.Time
}

func (h *handler) handleGetStats(c *fiber.Ctx) error {
	selectedWindow, err := parseStatsWindow(c.Query("window"))
	if err != nil {
		return writeError(c, err)
	}

	location, timezone, err := resolveStatsLocation(c.Query("tz"))
	if err != nil {
		return writeError(c, err)
	}

	customRange, err := parseCustomDateRange(c.Query("start"), c.Query("end"), location, selectedWindow)
	if err != nil {
		return writeError(c, err)
	}

	filters, err := parseStatsFilters(c.Query("genre"), c.Query("actorIds"), c.Query("crewIds"), c.Query("releaseYear"), c.Query("decade"), c.Query("addedByIds"))
	if err != nil {
		return writeError(c, err)
	}

	now := time.Now().UTC()
	cacheKey := buildStatsCacheKey(selectedWindow, timezone, customRange, filters)
	if cached, ok := h.getCachedStats(cacheKey, now); ok {
		return c.Status(fiber.StatusOK).JSON(cached)
	}

	ctx := c.UserContext()
	watched, err := h.movieService.Watched(ctx)
	if err != nil {
		return writeError(c, err)
	}

	// Every member gets a leaderboard row even with zero matching movies, so
	// the list needs the member roster, not just the watched history.
	users, err := h.userService.List(ctx)
	if err != nil {
		return writeError(c, err)
	}
	members := make([]string, 0, len(users))
	for i := range users {
		members = append(members, users[i].Name)
	}

	// Stats aggregate enriched data, so a metadata/credits load failure fails
	// the request — wrong stats are worse than a 500 (unlike the render-path
	// metaFor/creditsFor, which degrade gracefully).
	ids := make([]int, len(watched))
	for i := range watched {
		ids[i] = watched[i].ID
	}
	meta, err := h.movieMetadata.GetMetadataByMovieIDs(ctx, ids)
	if err != nil {
		return writeError(c, err)
	}
	credits, err := h.movieCredits.GetCreditsByMovieIDs(ctx, ids)
	if err != nil {
		return writeError(c, err)
	}

	payload := buildStatsResponse(watched, meta, credits, members, filters, selectedWindow, customRange, location, timezone, now)
	h.setCachedStats(cacheKey, payload, now)

	return c.Status(fiber.StatusOK).JSON(payload)
}

func buildStatsCacheKey(selectedWindow statsWindow, timezone string, customRange *customDateRange, filters statsFilters) string {
	start := ""
	end := ""
	if customRange != nil {
		start = customRange.StartDate
		end = customRange.EndDate
	}

	// Genre matching is case-insensitive, so the key lowercases it — "Action"
	// and "action" hit the same entry. The people lists are sorted and deduped
	// at parse time, so equivalent selections serialize to the same segment.
	return fmt.Sprintf("%s|%s|%s|%s|%s|%s|%s|%d|%d|%s",
		selectedWindow, timezone, start, end,
		strings.ToLower(filters.Genre), joinIDs(filters.ActorIDs), joinIDs(filters.CrewIDs),
		filters.ReleaseYear, filters.ReleaseDecade, joinIDs(filters.AddedByIDs))
}

func (h *handler) getCachedStats(key string, now time.Time) (statsResponse, bool) {
	h.statsCacheMu.RLock()
	entry, ok := h.statsCache[key]
	h.statsCacheMu.RUnlock()
	if !ok {
		return statsResponse{}, false
	}
	if now.After(entry.expiresAt) {
		h.statsCacheMu.Lock()
		delete(h.statsCache, key)
		h.statsCacheMu.Unlock()
		return statsResponse{}, false
	}

	return entry.response, true
}

func (h *handler) setCachedStats(key string, response statsResponse, now time.Time) {
	h.statsCacheMu.Lock()
	defer h.statsCacheMu.Unlock()

	if h.statsCache == nil {
		h.statsCache = make(map[string]statsCacheEntry)
	}
	if len(h.statsCache) >= statsCacheMaxEntries {
		clear(h.statsCache)
	}
	h.statsCache[key] = statsCacheEntry{
		response:  response,
		expiresAt: now.Add(h.statsCacheTTL),
	}
}

func (h *handler) invalidateStatsCache() {
	h.statsCacheMu.Lock()
	defer h.statsCacheMu.Unlock()

	clear(h.statsCache)
}

func parseStatsWindow(raw string) (statsWindow, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "":
		return statsWindow30d, nil
	case string(statsWindow24h):
		return statsWindow24h, nil
	case string(statsWindow7d):
		return statsWindow7d, nil
	case string(statsWindow30d):
		return statsWindow30d, nil
	case string(statsWindow90d):
		return statsWindow90d, nil
	case string(statsWindow1y):
		return statsWindow1y, nil
	case string(statsWindowAllTime):
		return statsWindowAllTime, nil
	case string(statsWindowCustom):
		return statsWindowCustom, nil
	default:
		return "", fmt.Errorf("%w: invalid window %q", domain.ErrInvalidInput, raw)
	}
}

func resolveStatsLocation(raw string) (*time.Location, string, error) {
	// Clone: the raw value is fiber's zero-copy view of the request buffer,
	// but the timezone echo outlives the handler inside the stats cache.
	timezone := strings.Clone(strings.TrimSpace(raw))
	if timezone == "" {
		return time.UTC, "UTC", nil
	}

	location, err := time.LoadLocation(timezone)
	if err != nil {
		return nil, "", fmt.Errorf("%w: invalid timezone %q", domain.ErrInvalidInput, timezone)
	}

	return location, timezone, nil
}

// statsFilters narrows the stats computation to a subset of the watched
// library. Zero values mean "no filter". The people lists are any-of within a
// list and AND-ed across lists (and with genre/year).
type statsFilters struct {
	Genre         string // case-insensitive genre name
	ActorIDs      []int  // TMDB person ids, sorted+deduped; matched against cast credits
	CrewIDs       []int  // TMDB person ids, sorted+deduped; matched against crew credits
	ReleaseYear   int    // exact release year; mutually exclusive with ReleaseDecade
	ReleaseDecade int    // decade floor (1990 ⇒ [1990, 1999]); mutually exclusive with ReleaseYear
	AddedByIDs    []int  // user ids of the movie's adder/picker, sorted+deduped (any-of)
}

func parseStatsFilters(genreRaw, actorsRaw, crewRaw, yearRaw, decadeRaw, addedByRaw string) (statsFilters, error) {
	// Clone: the raw value is fiber's zero-copy view of the request buffer,
	// but the genre echo outlives the handler inside the stats cache.
	filters := statsFilters{Genre: strings.Clone(strings.TrimSpace(genreRaw))}
	if len(filters.Genre) > statsMaxGenreLength {
		return statsFilters{}, fmt.Errorf("%w: genre exceeds %d characters", domain.ErrInvalidInput, statsMaxGenreLength)
	}

	var err error
	if filters.ActorIDs, err = parseIDList("actorIds", actorsRaw); err != nil {
		return statsFilters{}, err
	}
	if filters.CrewIDs, err = parseIDList("crewIds", crewRaw); err != nil {
		return statsFilters{}, err
	}
	if filters.AddedByIDs, err = parseIDList("addedByIds", addedByRaw); err != nil {
		return statsFilters{}, err
	}

	yearRaw = strings.TrimSpace(yearRaw)
	if yearRaw != "" {
		v, err := strconv.Atoi(yearRaw)
		if err != nil || v < statsMinReleaseYear || v > statsMaxReleaseYear {
			return statsFilters{}, fmt.Errorf("%w: invalid releaseYear %q (expected %d-%d)",
				domain.ErrInvalidInput, yearRaw, statsMinReleaseYear, statsMaxReleaseYear)
		}
		filters.ReleaseYear = v
	}

	// A decade selection ("1990s") is the alternative to an exact year — the UI
	// offers one or the other, so reject both at once rather than guess.
	decadeRaw = strings.TrimSpace(decadeRaw)
	if decadeRaw != "" {
		if filters.ReleaseYear != 0 {
			return statsFilters{}, fmt.Errorf("%w: releaseYear and decade are mutually exclusive", domain.ErrInvalidInput)
		}
		v, err := strconv.Atoi(decadeRaw)
		if err != nil || v%10 != 0 || v < statsMinReleaseYear || v > statsMaxReleaseYear {
			return statsFilters{}, fmt.Errorf("%w: invalid decade %q (expected a multiple of 10 in %d-%d)",
				domain.ErrInvalidInput, decadeRaw, statsMinReleaseYear, statsMaxReleaseYear)
		}
		filters.ReleaseDecade = v
	}

	return filters, nil
}

// parseIDList parses a comma-separated list of positive TMDB person ids into
// the canonical sorted-and-deduped form — "6384,530" and "530,6384,530" both
// yield [530 6384], so equivalent selections share one cache key. Empty input
// returns nil (never an empty slice) so the filters echo can omit the field.
func parseIDList(param, raw string) ([]int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}

	parts := strings.Split(raw, ",")
	if len(parts) > statsMaxPeopleFilterIDs {
		return nil, fmt.Errorf("%w: %s exceeds %d ids", domain.ErrInvalidInput, param, statsMaxPeopleFilterIDs)
	}
	ids := make([]int, 0, len(parts))
	for _, part := range parts {
		v, err := strconv.Atoi(strings.TrimSpace(part))
		if err != nil || v <= 0 {
			return nil, fmt.Errorf("%w: invalid %s %q (expected comma-separated positive integers)", domain.ErrInvalidInput, param, raw)
		}
		ids = append(ids, v)
	}

	slices.Sort(ids)
	return slices.Compact(ids), nil
}

// joinIDs serializes a canonical id list for the cache key; empty → "".
func joinIDs(ids []int) string {
	if len(ids) == 0 {
		return ""
	}
	parts := make([]string, len(ids))
	for i, id := range ids {
		parts[i] = strconv.Itoa(id)
	}
	return strings.Join(parts, ",")
}

// movieMatchesStatsFilters reports whether a watched movie passes the active
// filters. Unenriched movies (no metadata/credits) fail any active filter —
// their genre/year/people are unknown, and guessing would skew the stats.
func movieMatchesStatsFilters(md *domain.MovieMetadata, credits []domain.MovieCredit, filters statsFilters) bool {
	if filters.Genre != "" {
		// ToLower (not EqualFold): the cache key folds the genre with ToLower,
		// and the two relations differ on exotic runes (U+0130 "İ" lowercases
		// to "i" but has no simple case folding). Matching with the same fold
		// keeps "same cache key" ⇒ "same match set".
		want := strings.ToLower(filters.Genre)
		if md == nil || !slices.ContainsFunc(md.Genres, func(genre string) bool {
			return strings.ToLower(genre) == want
		}) {
			return false
		}
	}
	if filters.ReleaseYear != 0 && releaseYearOf(md) != filters.ReleaseYear {
		return false
	}
	if filters.ReleaseDecade != 0 {
		if y := releaseYearOf(md); y < filters.ReleaseDecade || y >= filters.ReleaseDecade+10 {
			return false
		}
	}
	// People filters are any-of within a list, AND-ed across lists. Crew rows
	// are already whitelisted to a handful of jobs at ingest, so crewIds need
	// no job check here.
	if len(filters.ActorIDs) > 0 && !creditsContainPerson(credits, domain.CreditKindCast, filters.ActorIDs) {
		return false
	}
	if len(filters.CrewIDs) > 0 && !creditsContainPerson(credits, domain.CreditKindCrew, filters.CrewIDs) {
		return false
	}
	return true
}

// creditsContainPerson reports whether any credit of the given kind references
// one of the (sorted) person ids.
func creditsContainPerson(credits []domain.MovieCredit, kind string, ids []int) bool {
	return slices.ContainsFunc(credits, func(c domain.MovieCredit) bool {
		if c.Kind != kind {
			return false
		}
		_, found := slices.BinarySearch(ids, c.Person.ID)
		return found
	})
}

// releaseYearOf extracts the year from the metadata's "YYYY-MM-DD" release
// date, or 0 when the metadata or date is absent/unparseable.
func releaseYearOf(md *domain.MovieMetadata) int {
	if md == nil || len(md.ReleaseDate) < 4 {
		return 0
	}
	year, err := strconv.Atoi(md.ReleaseDate[:4])
	if err != nil {
		return 0
	}
	return year
}

type customDateRange struct {
	StartUTC  time.Time
	EndUTC    time.Time
	StartDate string
	EndDate   string
}

func parseCustomDateRange(
	startRaw, endRaw string,
	location *time.Location,
	selectedWindow statsWindow,
) (*customDateRange, error) {
	if selectedWindow != statsWindowCustom {
		return nil, nil
	}

	startRaw = strings.TrimSpace(startRaw)
	endRaw = strings.TrimSpace(endRaw)
	if startRaw == "" || endRaw == "" {
		return nil, fmt.Errorf("%w: start and end are required for custom window", domain.ErrInvalidInput)
	}

	startLocal, err := time.ParseInLocation(statsDateFormat, startRaw, location)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid start date %q (expected YYYY-MM-DD)", domain.ErrInvalidInput, startRaw)
	}
	endLocal, err := time.ParseInLocation(statsDateFormat, endRaw, location)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid end date %q (expected YYYY-MM-DD)", domain.ErrInvalidInput, endRaw)
	}
	if startLocal.After(endLocal) {
		return nil, fmt.Errorf("%w: start date must be before or equal to end date", domain.ErrInvalidInput)
	}

	return &customDateRange{
		StartUTC:  startLocal.UTC(),
		EndUTC:    endLocal.AddDate(0, 0, 1).UTC(),
		StartDate: startLocal.Format(statsDateFormat),
		EndDate:   endLocal.Format(statsDateFormat),
	}, nil
}

type statsRange struct {
	StartInclusive *time.Time
	EndExclusive   *time.Time
}

func containsTimeRange(watchedAt time.Time, now time.Time, windowRange statsRange) bool {
	if watchedAt.After(now) {
		return false
	}
	if windowRange.StartInclusive != nil && watchedAt.Before(*windowRange.StartInclusive) {
		return false
	}
	if windowRange.EndExclusive != nil && !watchedAt.Before(*windowRange.EndExclusive) {
		return false
	}
	return true
}

func rangeForPresetWindow(window statsWindow, now time.Time) statsRange {
	switch window {
	case statsWindow24h:
		start := now.Add(-24 * time.Hour)
		return statsRange{StartInclusive: &start}
	case statsWindow7d:
		start := now.AddDate(0, 0, -7)
		return statsRange{StartInclusive: &start}
	case statsWindow30d:
		start := now.AddDate(0, 0, -30)
		return statsRange{StartInclusive: &start}
	case statsWindow90d:
		start := now.AddDate(0, 0, -90)
		return statsRange{StartInclusive: &start}
	case statsWindow1y:
		start := now.AddDate(-1, 0, 0)
		return statsRange{StartInclusive: &start}
	default:
		return statsRange{}
	}
}

func rangeForSelectedWindow(selectedWindow statsWindow, now time.Time, customRange *customDateRange) statsRange {
	if selectedWindow == statsWindowCustom && customRange != nil {
		start := customRange.StartUTC
		end := customRange.EndUTC
		return statsRange{
			StartInclusive: &start,
			EndExclusive:   &end,
		}
	}
	return rangeForPresetWindow(selectedWindow, now)
}

func buildStatsResponse(
	watched []*domain.Movie,
	meta metaByID,
	credits creditsByID,
	members []string,
	filters statsFilters,
	selectedWindow statsWindow,
	customRange *customDateRange,
	location *time.Location,
	timezone string,
	now time.Time,
) statsResponse {
	countsByWindow := make(map[statsWindow]int, len(statsWindowOrder))
	watchedByUser := make(map[string]int)
	allTimeByUser := make(map[string]int)
	weekdayCounts := make(map[time.Weekday]int)
	hourCounts := make([]int, 24)
	genreCounts := make(map[string]int)
	directorCounts := make(map[int]*statsPersonCount)
	actorCounts := make(map[int]*statsPersonCount)
	releaseYearCounts := make(map[int]int)
	selectedRange := rangeForSelectedWindow(selectedWindow, now, customRange)
	selectedWindowCount := 0
	// The concrete films behind selectedWindowCount, in watch-recency order (the
	// watched list arrives most-recent-first) — the client renders them as a
	// poster rail, so this stays the single source of truth for "what matched".
	matchedIDs := make([]int, 0)
	runtimeTotal, runtimeMovies, longestRuntime := 0, 0, 0
	longestTitle := ""
	ratingTotal, ratedMovies := 0.0, 0

	for i := range watched {
		if watched[i].WatchedAt == nil {
			continue
		}

		md := meta[watched[i].ID]
		movieCredits := credits[watched[i].ID]
		// Filters narrow EVERY aggregate — countsByWindow and the all-time
		// member ordering included — to the matching subset.
		if !movieMatchesStatsFilters(md, movieCredits, filters) {
			continue
		}
		// Added-by is a movie-level filter (not metadata), so it gates here rather
		// than in movieMatchesStatsFilters — any-of across the selected pickers.
		if len(filters.AddedByIDs) > 0 && !slices.Contains(filters.AddedByIDs, watched[i].AddedByID) {
			continue
		}

		watchedAt := watched[i].WatchedAt.UTC()

		name := watched[i].AddedByName
		if strings.TrimSpace(name) == "" {
			name = "Unknown"
		}

		for j := range statsWindowOrder {
			if containsTimeRange(watchedAt, now, rangeForPresetWindow(statsWindowOrder[j], now)) {
				countsByWindow[statsWindowOrder[j]]++
				// All-time total per member — used purely for a stable row order
				// so the member list doesn't jump when switching windows.
				if statsWindowOrder[j] == statsWindowAllTime {
					allTimeByUser[name]++
				}
			}
		}

		if !containsTimeRange(watchedAt, now, selectedRange) {
			continue
		}
		selectedWindowCount++
		matchedIDs = append(matchedIDs, watched[i].ID)

		localWatchedAt := watchedAt.In(location)
		weekdayCounts[localWatchedAt.Weekday()]++
		hourCounts[localWatchedAt.Hour()]++

		watchedByUser[name]++

		// Enrichment-derived tallies for the selected window. Unenriched
		// movies simply don't contribute (and runtime/rating skip their
		// zero-value denominators, so averages stay honest).
		if md != nil {
			for _, genre := range md.Genres {
				genreCounts[genre]++
			}
			if year := releaseYearOf(md); year > 0 {
				releaseYearCounts[year]++
			}
			if md.Runtime > 0 {
				runtimeTotal += md.Runtime
				runtimeMovies++
				if md.Runtime > longestRuntime {
					longestRuntime = md.Runtime
					longestTitle = watched[i].Title
				}
			}
			if md.VoteAverage > 0 {
				ratingTotal += md.VoteAverage
				ratedMovies++
			}
		}
		for j := range movieCredits {
			credit := movieCredits[j]
			switch {
			case credit.Kind == domain.CreditKindCrew && credit.Job == "Director":
				tallyPerson(directorCounts, credit)
			case credit.Kind == domain.CreditKindCast:
				tallyPerson(actorCounts, credit)
			}
		}
	}

	// Seed every member AND every all-time picker into the window map (0 when
	// nothing of theirs matches the window or the active filters) so rows
	// never appear/disappear — not between ranges, and not under drill-downs.
	// Members cover the roster (including someone yet to pick); the all-time
	// pickers keep history visible for names no longer on the roster.
	for _, name := range members {
		if strings.TrimSpace(name) == "" {
			continue
		}
		if _, ok := watchedByUser[name]; !ok {
			watchedByUser[name] = 0
		}
	}
	for name := range allTimeByUser {
		if _, ok := watchedByUser[name]; !ok {
			watchedByUser[name] = 0
		}
	}

	averageRating := 0.0
	if ratedMovies > 0 {
		averageRating = ratingTotal / float64(ratedMovies)
	}
	averageMinutes := 0
	if runtimeMovies > 0 {
		averageMinutes = runtimeTotal / runtimeMovies
	}

	return statsResponse{
		SelectedWindow:      string(selectedWindow),
		SelectedWindowCount: selectedWindowCount,
		MatchedMovieIDs:     matchedIDs,
		Timezone:            timezone,
		TotalWatched:        countsByWindow[statsWindowAllTime],
		CountsByWindow:      buildWindowCounts(countsByWindow),
		WatchedByUser:       buildMemberCounts(watchedByUser, allTimeByUser),
		WeekdayActivity:     buildWeekdayCounts(weekdayCounts),
		HourActivity:        buildHourCounts(hourCounts),
		TopGenres:           buildGenreCounts(genreCounts),
		TopDirectors:        buildPersonCounts(directorCounts, statsTopPeopleLimit),
		TopActors:           buildPersonCounts(actorCounts, statsTopPeopleLimit),
		ReleaseYears:        buildYearCounts(releaseYearCounts),
		Runtime: statsRuntime{
			TotalMinutes:   runtimeTotal,
			AverageMinutes: averageMinutes,
			LongestMinutes: longestRuntime,
			LongestTitle:   longestTitle,
		},
		AverageRating:    averageRating,
		Filters:          buildFiltersEcho(filters, meta, credits),
		CustomRangeStart: customRangeStart(customRange),
		CustomRangeEnd:   customRangeEnd(customRange),
	}
}

// tallyPerson bumps a person's in-window count, capturing their name/photo on
// first sight. Credits are deduped per (movie, person, kind, job) at ingest,
// so each movie contributes at most one tally per person and role.
func tallyPerson(counts map[int]*statsPersonCount, credit domain.MovieCredit) {
	entry, ok := counts[credit.Person.ID]
	if !ok {
		entry = &statsPersonCount{
			PersonID: credit.Person.ID,
			Name:     credit.Person.Name,
		}
		if credit.Person.ProfilePath != nil {
			entry.ProfilePath = *credit.Person.ProfilePath
		}
		counts[credit.Person.ID] = entry
	}
	entry.Count++
}

// buildFiltersEcho echoes the active filters back, resolving each person
// filter to a display name from any credit row that references them and the
// genre to its stored canonical casing (matching is case-insensitive, and the
// cache key folds case — canonicalizing keeps the echo identical across cache
// hits).
func buildFiltersEcho(filters statsFilters, meta metaByID, credits creditsByID) statsFiltersEcho {
	echo := statsFiltersEcho{
		Genre:         filters.Genre,
		Actors:        resolveFilterPeople(filters.ActorIDs, credits),
		Crew:          resolveFilterPeople(filters.CrewIDs, credits),
		ReleaseYear:   filters.ReleaseYear,
		ReleaseDecade: filters.ReleaseDecade,
	}
	if echo.Genre != "" {
		// Same ToLower fold as the matcher and the cache key.
		want := strings.ToLower(echo.Genre)
	genres:
		for _, md := range meta {
			if md == nil {
				continue
			}
			for _, genre := range md.Genres {
				if strings.ToLower(genre) == want {
					echo.Genre = genre
					break genres
				}
			}
		}
	}
	return echo
}

// resolveFilterPeople maps filter ids (already sorted, so the echo order is
// deterministic across cache hits) to display names from any credit row that
// references them. Ids with no credit row keep an empty name — the client
// carries its own labels for those.
func resolveFilterPeople(ids []int, credits creditsByID) []statsFilterPerson {
	if len(ids) == 0 {
		return nil
	}

	names := make(map[int]string, len(ids))
	for _, movieCredits := range credits {
		for i := range movieCredits {
			if _, found := slices.BinarySearch(ids, movieCredits[i].Person.ID); found {
				names[movieCredits[i].Person.ID] = movieCredits[i].Person.Name
			}
		}
	}

	people := make([]statsFilterPerson, len(ids))
	for i, id := range ids {
		people[i] = statsFilterPerson{PersonID: id, Name: names[id]}
	}
	return people
}

func customRangeStart(customRange *customDateRange) string {
	if customRange == nil {
		return ""
	}
	return customRange.StartDate
}

func customRangeEnd(customRange *customDateRange) string {
	if customRange == nil {
		return ""
	}
	return customRange.EndDate
}

func buildWindowCounts(counts map[statsWindow]int) []statsWindowCount {
	output := make([]statsWindowCount, 0, len(statsWindowOrder))
	for i := range statsWindowOrder {
		output = append(output, statsWindowCount{
			Window: string(statsWindowOrder[i]),
			Count:  counts[statsWindowOrder[i]],
		})
	}
	return output
}

// buildMemberCounts returns one row per member with their in-window count,
// ordered by all-time total (descending) then name. Ordering by the stable
// all-time total — not the window count — keeps rows in the same positions as
// the user switches ranges, so the list no longer jumps around.
func buildMemberCounts(windowCounts, allTime map[string]int) []statsNamedCount {
	output := make([]statsNamedCount, 0, len(windowCounts))
	for name, count := range windowCounts {
		output = append(output, statsNamedCount{Name: name, Count: count})
	}

	slices.SortFunc(output, func(a, b statsNamedCount) int {
		if allTime[a.Name] != allTime[b.Name] {
			return allTime[b.Name] - allTime[a.Name]
		}
		switch {
		case a.Name < b.Name:
			return -1
		case a.Name > b.Name:
			return 1
		default:
			return 0
		}
	})

	return output
}

// buildGenreCounts returns the full genre tally, count descending then name.
func buildGenreCounts(counts map[string]int) []statsNamedCount {
	output := make([]statsNamedCount, 0, len(counts))
	for name, count := range counts {
		output = append(output, statsNamedCount{Name: name, Count: count})
	}

	slices.SortFunc(output, func(a, b statsNamedCount) int {
		if a.Count != b.Count {
			return b.Count - a.Count
		}
		return strings.Compare(a.Name, b.Name)
	})

	return output
}

// buildPersonCounts flattens the per-person tallies, ordered by count
// descending then name, capped at limit.
func buildPersonCounts(counts map[int]*statsPersonCount, limit int) []statsPersonCount {
	output := make([]statsPersonCount, 0, len(counts))
	for _, entry := range counts {
		output = append(output, *entry)
	}

	slices.SortFunc(output, func(a, b statsPersonCount) int {
		if a.Count != b.Count {
			return b.Count - a.Count
		}
		// Distinct people can share a display name; break the tie on the id so
		// the order — and which of them survives the cap — is deterministic.
		if c := strings.Compare(a.Name, b.Name); c != 0 {
			return c
		}
		return a.PersonID - b.PersonID
	})

	if len(output) > limit {
		output = output[:limit]
	}
	return output
}

// buildYearCounts returns the release-year histogram, year ascending. Decade
// bucketing is a presentation concern, left to the frontend.
func buildYearCounts(counts map[int]int) []statsYearCount {
	output := make([]statsYearCount, 0, len(counts))
	for year, count := range counts {
		output = append(output, statsYearCount{Year: year, Count: count})
	}

	slices.SortFunc(output, func(a, b statsYearCount) int {
		return a.Year - b.Year
	})

	return output
}

func buildWeekdayCounts(counts map[time.Weekday]int) []statsNamedCount {
	orderedWeekdays := []struct {
		day   time.Weekday
		label string
	}{
		{day: time.Monday, label: "Mon"},
		{day: time.Tuesday, label: "Tue"},
		{day: time.Wednesday, label: "Wed"},
		{day: time.Thursday, label: "Thu"},
		{day: time.Friday, label: "Fri"},
		{day: time.Saturday, label: "Sat"},
		{day: time.Sunday, label: "Sun"},
	}

	output := make([]statsNamedCount, 0, len(orderedWeekdays))
	for i := range orderedWeekdays {
		output = append(output, statsNamedCount{
			Name:  orderedWeekdays[i].label,
			Count: counts[orderedWeekdays[i].day],
		})
	}
	return output
}

func buildHourCounts(counts []int) []statsHourCount {
	output := make([]statsHourCount, 0, 24)
	for hour := range 24 {
		output = append(output, statsHourCount{
			Hour:  hour,
			Label: fmt.Sprintf("%02d:00", hour),
			Count: counts[hour],
		})
	}
	return output
}
