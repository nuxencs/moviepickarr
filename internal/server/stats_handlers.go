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

	now := time.Now().UTC()
	cacheKey := buildStatsCacheKey(selectedWindow, timezone, customRange)
	if cached, ok := h.getCachedStats(cacheKey, now); ok {
		return c.Status(fiber.StatusOK).JSON(cached)
	}

	watched, err := h.movieService.Watched(c.UserContext())
	if err != nil {
		return writeError(c, err)
	}

	payload := buildStatsResponse(watched, selectedWindow, customRange, location, timezone, now)
	h.setCachedStats(cacheKey, payload, now)

	return c.Status(fiber.StatusOK).JSON(payload)
}

func buildStatsCacheKey(selectedWindow statsWindow, timezone string, customRange *customDateRange) string {
	start := ""
	end := ""
	if customRange != nil {
		start = customRange.StartDate
		end = customRange.EndDate
	}

	return fmt.Sprintf("%s|%s|%s|%s", selectedWindow, timezone, start, end)
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
	timezone := strings.TrimSpace(raw)
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
	selectedWindow statsWindow,
	customRange *customDateRange,
	location *time.Location,
	timezone string,
	now time.Time,
) statsResponse {
	countsByWindow := make(map[statsWindow]int, len(statsWindowOrder))
	watchedByUser := make(map[string]int)
	weekdayCounts := make(map[time.Weekday]int)
	hourCounts := make([]int, 24)
	selectedRange := rangeForSelectedWindow(selectedWindow, now, customRange)
	selectedWindowCount := 0

	for _, movie := range watched {
		if movie.WatchedAt == nil {
			continue
		}

		watchedAt := movie.WatchedAt.UTC()
		for _, window := range statsWindowOrder {
			if containsTimeRange(watchedAt, now, rangeForPresetWindow(window, now)) {
				countsByWindow[window]++
			}
		}

		if !containsTimeRange(watchedAt, now, selectedRange) {
			continue
		}
		selectedWindowCount++

		localWatchedAt := watchedAt.In(location)
		weekdayCounts[localWatchedAt.Weekday()]++
		hourCounts[localWatchedAt.Hour()]++

		name := movie.AddedByName
		if strings.TrimSpace(name) == "" {
			name = "Unknown"
		}
		watchedByUser[name]++
	}

	return statsResponse{
		SelectedWindow:      string(selectedWindow),
		SelectedWindowCount: selectedWindowCount,
		Timezone:            timezone,
		TotalWatched:        countsByWindow[statsWindowAllTime],
		CountsByWindow:      buildWindowCounts(countsByWindow),
		WatchedByUser:       buildSortedNamedCounts(watchedByUser),
		WeekdayActivity:     buildWeekdayCounts(weekdayCounts),
		HourActivity:        buildHourCounts(hourCounts),
		CustomRangeStart:    customRangeStart(customRange),
		CustomRangeEnd:      customRangeEnd(customRange),
	}
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
	for _, window := range statsWindowOrder {
		output = append(output, statsWindowCount{
			Window: string(window),
			Count:  counts[window],
		})
	}
	return output
}

func buildSortedNamedCounts(counts map[string]int) []statsNamedCount {
	output := make([]statsNamedCount, 0, len(counts))
	for name, count := range counts {
		output = append(output, statsNamedCount{Name: name, Count: count})
	}

	slices.SortFunc(output, func(a, b statsNamedCount) int {
		if a.Count != b.Count {
			return b.Count - a.Count
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
	for _, entry := range orderedWeekdays {
		output = append(output, statsNamedCount{
			Name:  entry.label,
			Count: counts[entry.day],
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
