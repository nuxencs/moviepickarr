package server

import (
	"fmt"
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

// movieLink derives the effective link from the movie's stable identity,
// preferring IMDb, then TMDB. Returns "" if the movie has no identity (every
// row carries an id post-enrichment, so this is effectively unreachable).
func movieLink(movie *domain.Movie) string {
	if movie.IMDbID != nil && *movie.IMDbID != "" {
		return "https://www.imdb.com/title/" + *movie.IMDbID + "/"
	}
	if movie.TMDBID != nil {
		return fmt.Sprintf("https://www.themoviedb.org/movie/%d", *movie.TMDBID)
	}
	return ""
}

func toAPIMovie(movie *domain.Movie) movieResponse {
	return movieResponse{
		ID:          movie.ID,
		Title:       movie.Title,
		Link:        movieLink(movie),
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
		CurrentPool: moviesToMap(poolMovies),
		Stash:       moviesToMap(stashMovies),
		CreatedAt:   formatTime(user.CreatedAt),
	}
}
