package server

import (
	"fmt"
	"strconv"
	"time"

	"moviepickarr/internal/domain"
)

const timeFormat = time.RFC3339

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

	// Draw-reveal coordination. Set only on the movie:drawn event and the
	// current-movie endpoint, never on list payloads. DrawnAt is when the
	// current movie was drawn (drives resuming the cross-client reveal spin
	// after a reload); RevealAt is the server's auto-reveal deadline (clients
	// time the confirm countdown off it — the server owns the reveal timing);
	// ServerNow is the server clock at response time so the client computes
	// elapsed without trusting its own clock. All omitted when no draw is
	// active.
	DrawnAt   string `json:"drawnAt,omitempty"`
	RevealAt  string `json:"revealAt,omitempty"`
	ServerNow string `json:"serverNow,omitempty"`
	// DrawClientID is the client that initiated the draw; only that client shows
	// the reel's confirm button. Revealed reports whether the draw was confirmed,
	// so a reload after the reveal shows the result instead of re-opening the reel.
	DrawClientID string `json:"drawClientId,omitempty"`
	Revealed     bool   `json:"revealed,omitempty"`

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

// formatTimePrecise keeps sub-second precision (RFC3339Nano). Used for
// revealAt: the client derives the confirm countdown from revealAt − drawnAt,
// and second-truncating both would jitter that window by up to ±0.5s per
// draw. drawnAt itself stays second-precision — it is the draw's identity
// string and must remain byte-identical across every payload that carries it.
func formatTimePrecise(value time.Time) string {
	return value.UTC().Format(time.RFC3339Nano)
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

// toAPIMovieBase builds a response with identity + the tile-level enriched
// fields the list grids render (poster, rating, release date, runtime, genres).
// It deliberately omits the heavy modal-only fields (backdrop, tagline, overview,
// cast, crew); toAPIMovieMeta layers those on for the detail/current paths.
func toAPIMovieBase(movie *domain.Movie, md *domain.MovieMetadata) movieResponse {
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
		resp.ReleaseDate = md.ReleaseDate
		resp.Runtime = md.Runtime
		resp.Genres = md.Genres
		resp.VoteAverage = md.VoteAverage
	}
	return resp
}

// toAPIMovieLean is the list/tile shape: base fields only, no backdrop/tagline/
// overview/credits. The watched list ships hundreds of these, so dropping the
// per-movie credits + prose is the bulk of the payload — the modal lazy-loads
// the full record from GET /movies/:id when it opens.
func toAPIMovieLean(movie *domain.Movie, md *domain.MovieMetadata) movieResponse {
	return toAPIMovieBase(movie, md)
}

// toAPIMovieMeta builds the full detail response, folding the modal-only
// metadata (backdrop, tagline, overview) and credits onto the base shape.
func toAPIMovieMeta(movie *domain.Movie, md *domain.MovieMetadata, credits []domain.MovieCredit) movieResponse {
	resp := toAPIMovieBase(movie, md)
	if md != nil {
		if md.BackdropPath != nil {
			resp.BackdropPath = *md.BackdropPath
		}
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

func toAPIMoviesLean(movies []*domain.Movie, meta metaByID) []movieResponse {
	result := make([]movieResponse, 0, len(movies))
	for i := range movies {
		result = append(result, toAPIMovieLean(movies[i], meta[movies[i].ID]))
	}
	return result
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
