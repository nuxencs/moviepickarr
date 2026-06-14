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

	// Stable external identities, exposed so the frontend can build links to
	// IMDb / TMDB / Letterboxd (Letterboxd resolves via /tmdb/{id} or /imdb/{id}).
	// Omitted when the movie carries no such id.
	TMDBID *int   `json:"tmdbId,omitempty"`
	IMDbID string `json:"imdbId,omitempty"`

	// Enriched TMDB metadata. All optional — a movie may not be enriched yet
	// (enrichment is async), so these are omitted when absent and the frontend
	// degrades gracefully (placeholder poster, hidden chips). Poster/backdrop
	// are raw TMDB paths (e.g. "/abc.jpg"); the frontend builds the image URL.
	PosterPath   string   `json:"posterPath,omitempty"`
	BackdropPath string   `json:"backdropPath,omitempty"`
	ReleaseDate  string   `json:"releaseDate,omitempty"`
	Runtime      int      `json:"runtime,omitempty"`
	Genres       []string `json:"genres,omitempty"`
	VoteAverage  float64  `json:"voteAverage,omitempty"`
	Tagline      string   `json:"tagline,omitempty"`
	Overview     string   `json:"overview,omitempty"`

	// Trimmed TMDB credits: top-billed cast (in billing order) and
	// whitelisted crew jobs. Omitted until the movie's credits are ingested.
	Cast []creditPerson `json:"cast,omitempty"`
	Crew []creditPerson `json:"crew,omitempty"`
}

// creditPerson is the wire shape of one cast or crew member on a movie.
type creditPerson struct {
	ID          int    `json:"id"` // TMDB person id
	Name        string `json:"name"`
	ProfilePath string `json:"profilePath,omitempty"` // raw TMDB path, like posterPath
	Character   string `json:"character,omitempty"`   // cast only
	Job         string `json:"job,omitempty"`         // crew only
}

// metaByID maps a movie id to its enriched metadata (absent when unenriched).
type metaByID map[int]*domain.MovieMetadata

// creditsByID maps a movie id to its ingested credits (absent when none).
type creditsByID map[int][]domain.MovieCredit

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

// toAPIMovie builds a response without enriched metadata or credits. Used for
// SSE broadcast payloads, where the frontend refetches enriched data from the
// GET endpoints rather than reading the event body.
func toAPIMovie(movie *domain.Movie) movieResponse {
	return toAPIMovieMeta(movie, nil, nil)
}

// toAPIMovieMeta builds a response, folding in enriched metadata and credits
// when present.
func toAPIMovieMeta(movie *domain.Movie, md *domain.MovieMetadata, credits []domain.MovieCredit) movieResponse {
	resp := movieResponse{
		ID:          movie.ID,
		Title:       movie.Title,
		Link:        movieLink(movie),
		AddedAt:     formatTime(movie.AddedAt),
		AddedByID:   movie.AddedByID,
		AddedByName: movie.AddedByName,
		WatchedAt:   formatTime(movie.WatchedAt),
		TMDBID:      movie.TMDBID,
	}
	if movie.IMDbID != nil {
		resp.IMDbID = *movie.IMDbID
	}
	if md != nil {
		if md.PosterPath != nil {
			resp.PosterPath = *md.PosterPath
		}
		if md.BackdropPath != nil {
			resp.BackdropPath = *md.BackdropPath
		}
		resp.ReleaseDate = md.ReleaseDate
		resp.Runtime = md.Runtime
		resp.Genres = md.Genres
		resp.VoteAverage = md.VoteAverage
		resp.Tagline = md.Tagline
		resp.Overview = md.Overview
	}
	// Credits arrive cast-first in billing order (repo ORDER BY), so a plain
	// split by kind keeps both arrays in their display order.
	for i := range credits {
		person := creditPerson{
			ID:   credits[i].Person.ID,
			Name: credits[i].Person.Name,
		}
		if credits[i].Person.ProfilePath != nil {
			person.ProfilePath = *credits[i].Person.ProfilePath
		}
		switch credits[i].Kind {
		case domain.CreditKindCast:
			person.Character = credits[i].Character
			resp.Cast = append(resp.Cast, person)
		case domain.CreditKindCrew:
			person.Job = credits[i].Job
			resp.Crew = append(resp.Crew, person)
		}
	}
	return resp
}

func toAPIMoviesMeta(movies []*domain.Movie, meta metaByID, credits creditsByID) []movieResponse {
	result := make([]movieResponse, 0, len(movies))
	for i := range movies {
		result = append(result, toAPIMovieMeta(movies[i], meta[movies[i].ID], credits[movies[i].ID]))
	}
	return result
}

func moviesToMapMeta(movies []*domain.Movie, meta metaByID, credits creditsByID) map[string]movieResponse {
	result := make(map[string]movieResponse, len(movies))
	for i := range movies {
		result[strconv.Itoa(movies[i].ID)] = toAPIMovieMeta(movies[i], meta[movies[i].ID], credits[movies[i].ID])
	}
	return result
}

func toAPIUserMeta(user *domain.User, poolMovies, stashMovies []*domain.Movie, meta metaByID, credits creditsByID) userResponse {
	return userResponse{
		ID:          user.ID,
		Name:        user.Name,
		CurrentPool: moviesToMapMeta(poolMovies, meta, credits),
		Stash:       moviesToMapMeta(stashMovies, meta, credits),
		CreatedAt:   formatTime(user.CreatedAt),
	}
}
