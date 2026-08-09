package server

import (
	"fmt"
	"slices"
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

	// Sanity bounds for the releaseYear filter (movie history through a
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

	// The filter segment comes from the filters value itself, which owns the
	// genre fold shared with the matcher (see statsFilters).
	return fmt.Sprintf("%s|%s|%s|%s|%s",
		selectedWindow, timezone, start, end, filters.cacheKeySegment())
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
	clear(h.statsCache)
	h.statsCacheMu.Unlock()

	// The filter options derive from the same watched metadata/credits, so they
	// go stale on exactly the same events (watch, edit, enrich, user add/remove).
	h.invalidateFilterOptionsCache()
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
	// The six preset window ranges depend only on `now`, never on a movie, so
	// compute them once here rather than recomputing all six (each allocating a
	// heap time pointer) for every watched movie inside the loop below.
	presetRanges := make([]statsRange, len(statsWindowOrder))
	for j := range statsWindowOrder {
		presetRanges[j] = rangeForPresetWindow(statsWindowOrder[j], now)
	}
	selectedWindowCount := 0
	// The concrete movies behind selectedWindowCount, in watch-recency order (the
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
		if !filters.matches(md, movieCredits) {
			continue
		}
		// Added-by is a movie-level filter (not metadata), so it gates here rather
		// than in statsFilters.matches: any-of across the selected adders.
		if len(filters.AddedByIDs) > 0 && !slices.Contains(filters.AddedByIDs, watched[i].AddedByID) {
			continue
		}

		watchedAt := watched[i].WatchedAt.UTC()

		name := watched[i].AddedByName
		if strings.TrimSpace(name) == "" {
			name = "Unknown"
		}

		for j := range statsWindowOrder {
			if containsTimeRange(watchedAt, now, presetRanges[j]) {
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

	// Seed every member AND every all-time adder into the window map (0 when
	// nothing of theirs matches the window or the active filters) so rows
	// never appear/disappear — not between ranges, and not under drill-downs.
	// Members cover the roster (including someone yet to add); the all-time
	// adders keep history visible for names no longer on the roster.
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
		Filters:          filters.echo(meta, credits),
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
