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

type statsResponse struct {
	SelectedWindow      string             `json:"selectedWindow"`
	SelectedWindowCount int                `json:"selectedWindowCount"`
	Timezone            string             `json:"timezone"`
	TotalWatched        int                `json:"totalWatched"`
	CountsByWindow      []statsWindowCount `json:"countsByWindow"`
	WatchedByUser       []statsNamedCount  `json:"watchedByUser"`
	WeekdayActivity     []statsNamedCount  `json:"weekdayActivity"`
	HourActivity        []statsHourCount   `json:"hourActivity"`
	CustomRangeStart    string             `json:"customRangeStart,omitempty"`
	CustomRangeEnd      string             `json:"customRangeEnd,omitempty"`
}
