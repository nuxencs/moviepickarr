package server

type statsWindowCount struct {
	Window string `json:"window"`
	Count  int    `json:"count"`
}

type statsNamedCount struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

type statsHourCount struct {
	Hour  int    `json:"hour"`
	Label string `json:"label"`
	Count int    `json:"count"`
}

type statsPersonCount struct {
	PersonID    int    `json:"personId"` // TMDB person id
	Name        string `json:"name"`
	ProfilePath string `json:"profilePath,omitempty"`
	Count       int    `json:"count"`
}

type statsYearCount struct {
	Year  int `json:"year"`
	Count int `json:"count"`
}

type statsRuntime struct {
	TotalMinutes   int    `json:"totalMinutes"`
	AverageMinutes int    `json:"averageMinutes"`
	LongestMinutes int    `json:"longestMinutes"`
	LongestTitle   string `json:"longestTitle,omitempty"`
}

// statsFilterPerson is one person in the filters echo, resolved to a display
// name when any credit row references them.
type statsFilterPerson struct {
	PersonID int    `json:"personId"` // TMDB person id
	Name     string `json:"name,omitempty"`
}

// statsFiltersEcho mirrors the active filters back to the client, with the
// people filters resolved to display names.
type statsFiltersEcho struct {
	Genre         string              `json:"genre,omitempty"`
	Actors        []statsFilterPerson `json:"actors,omitempty"`
	Crew          []statsFilterPerson `json:"crew,omitempty"`
	ReleaseYear   int                 `json:"releaseYear,omitempty"`
	ReleaseDecade int                 `json:"releaseDecade,omitempty"`
}

type statsResponse struct {
	SelectedWindow      string             `json:"selectedWindow"`
	SelectedWindowCount int                `json:"selectedWindowCount"`
	Timezone            string             `json:"timezone"`
	TotalWatched        int                `json:"totalWatched"`
	CountsByWindow      []statsWindowCount `json:"countsByWindow"`
	WatchedByUser       []statsNamedCount  `json:"watchedByUser"`
	WeekdayActivity     []statsNamedCount  `json:"weekdayActivity"`
	HourActivity        []statsHourCount   `json:"hourActivity"`
	TopGenres           []statsNamedCount  `json:"topGenres"`
	TopDirectors        []statsPersonCount `json:"topDirectors"`
	TopActors           []statsPersonCount `json:"topActors"`
	ReleaseYears        []statsYearCount   `json:"releaseYears"`
	Runtime             statsRuntime       `json:"runtime"`
	AverageRating       float64            `json:"averageRating"`
	Filters             statsFiltersEcho   `json:"filters"`
	CustomRangeStart    string             `json:"customRangeStart,omitempty"`
	CustomRangeEnd      string             `json:"customRangeEnd,omitempty"`
}
