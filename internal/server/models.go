package server

import (
	"strconv"
	"time"

	"moviepickarr/internal/domain"
)

const (
	maxPoolSize = 3
	timeFormat  = time.RFC3339
)

type settingsResponse struct {
	PoolLocked bool `json:"poolLocked"`
}

type userResponse struct {
	ID          int                      `json:"userID"`
	Name        string                   `json:"name"`
	Username    string                   `json:"username"`
	Role        string                   `json:"role"`
	HasAccount  bool                     `json:"hasAccount"`
	CurrentPool map[string]movieResponse `json:"currentPool"`
	Stash       map[string]movieResponse `json:"stash"`
	CreatedAt   string                   `json:"createdAt"`
}

type movieResponse struct {
	ID          int    `json:"movieID"`
	Title       string `json:"title"`
	Link        string `json:"link"`
	AddedAt     string `json:"addedAt"`
	AddedByID   int    `json:"addedByID"`
	AddedByName string `json:"addedByName"`
	WatchedAt   string `json:"watchedAt"`
}

func formatTime(value *time.Time) string {
	if value == nil {
		return ""
	}
	return value.UTC().Format(timeFormat)
}

func toAPIMovie(movie *domain.Movie) movieResponse {
	return movieResponse{
		ID:          movie.ID,
		Title:       movie.Title,
		Link:        movie.Link,
		AddedAt:     formatTime(movie.AddedAt),
		AddedByID:   movie.AddedByID,
		AddedByName: movie.AddedByName,
		WatchedAt:   formatTime(movie.WatchedAt),
	}
}

func toAPIMovies(movies []*domain.Movie) []movieResponse {
	result := make([]movieResponse, 0, len(movies))
	for i := range movies {
		result = append(result, toAPIMovie(movies[i]))
	}
	return result
}

func moviesToMap(movies []*domain.Movie) map[string]movieResponse {
	result := make(map[string]movieResponse, len(movies))
	for i := range movies {
		result[strconv.Itoa(movies[i].ID)] = toAPIMovie(movies[i])
	}
	return result
}

func toAPIUser(user *domain.User, poolMovies, stashMovies []*domain.Movie) userResponse {
	return userResponse{
		ID:          user.ID,
		Name:        user.Name,
		Username:    user.Username,
		Role:        string(user.Role),
		HasAccount:  user.HasAccount,
		CurrentPool: moviesToMap(poolMovies),
		Stash:       moviesToMap(stashMovies),
		CreatedAt:   formatTime(user.CreatedAt),
	}
}
