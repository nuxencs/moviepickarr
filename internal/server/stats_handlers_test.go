package server

import (
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
		{AddedByName: "Alice", WatchedAt: ptrTime(now.Add(-2 * time.Hour))},
		{AddedByName: "Bob", WatchedAt: ptrTime(now.Add(-25 * time.Hour))},
		{AddedByName: "Alice", WatchedAt: ptrTime(now.AddDate(0, 0, -8))},
		{AddedByName: "Cara", WatchedAt: ptrTime(now.AddDate(0, 0, -31))},
		{AddedByName: "Bob", WatchedAt: ptrTime(now.AddDate(0, 0, -95))},
		{AddedByName: "Alice", WatchedAt: ptrTime(now.AddDate(0, 0, -200))},
		{AddedByName: "Alice", WatchedAt: ptrTime(now.AddDate(0, 0, -370))},
		{AddedByName: "Nobody", WatchedAt: nil},
		{AddedByName: "Future", WatchedAt: ptrTime(now.Add(1 * time.Hour))},
	}

	got := buildStatsResponse(movies, statsWindow30d, nil, location, "UTC", now)

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

	if len(got.WatchedByUser) != 2 {
		t.Fatalf("expected 2 users in selected window, got %d", len(got.WatchedByUser))
	}
	if got.WatchedByUser[0].Name != "Alice" || got.WatchedByUser[0].Count != 2 {
		t.Fatalf("unexpected top user %+v", got.WatchedByUser[0])
	}
	if got.WatchedByUser[1].Name != "Bob" || got.WatchedByUser[1].Count != 1 {
		t.Fatalf("unexpected second user %+v", got.WatchedByUser[1])
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

	utcStats := buildStatsResponse(movies, statsWindow24h, nil, time.UTC, "UTC", now)
	pst, err := time.LoadLocation("America/Los_Angeles")
	if err != nil {
		t.Fatalf("load location: %v", err)
	}
	pstStats := buildStatsResponse(movies, statsWindow24h, nil, pst, "America/Los_Angeles", now)

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
		{AddedByName: "Alice", WatchedAt: ptrTime(time.Date(2026, 2, 1, 10, 0, 0, 0, time.UTC))},
		{AddedByName: "Bob", WatchedAt: ptrTime(time.Date(2026, 2, 3, 23, 59, 59, 0, time.UTC))},
		{AddedByName: "Cara", WatchedAt: ptrTime(time.Date(2026, 2, 4, 0, 0, 0, 0, time.UTC))},
	}

	got := buildStatsResponse(movies, statsWindowCustom, custom, location, "UTC", now)
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

func ptrTime(v time.Time) *time.Time {
	return &v
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

	got := buildStatsCacheKey(statsWindowCustom, "Europe/Berlin", custom)
	want := "custom|Europe/Berlin|2026-01-01|2026-01-31"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}

	gotPreset := buildStatsCacheKey(statsWindow30d, "UTC", nil)
	wantPreset := "30d|UTC||"
	if gotPreset != wantPreset {
		t.Fatalf("expected %q, got %q", wantPreset, gotPreset)
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
