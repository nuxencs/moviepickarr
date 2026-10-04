package server

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"moviepickarr/internal/domain"
)

const timeFormat = time.RFC3339

type settingsResponse struct {
	PoolLocked     bool `json:"poolLocked"`
	DrawInProgress bool `json:"drawInProgress"`
}

// userResponse ships board movies as leanMovieTile: the modal lazy-loads the
// full record from GET /movies/:id.
type userResponse struct {
	ID          int                      `json:"userID"`
	Name        string                   `json:"name"`
	CurrentPool map[string]leanMovieTile `json:"currentPool"`
	Stash       map[string]leanMovieTile `json:"stash"`
	CreatedAt   string                   `json:"createdAt"`
}

// leanMovieTile is the list payload class. It has no credits or prose by type,
// so lists stay lean (see docs/backend-layout.md).
type leanMovieTile struct {
	ID                int    `json:"movieID"`
	Title             string `json:"title"`
	Link              string `json:"link"`
	AddedAt           string `json:"addedAt"`
	AddedByID         int    `json:"addedByID"`
	AddedByName       string `json:"addedByName"`
	AddedByArchived   bool   `json:"addedByArchived,omitzero"`
	WatchedAt         string `json:"watchedAt"`
	WildcardOfMovieID int    `json:"wildcardOfMovieId,omitzero"`

	// External ids for IMDb, TMDB, and Letterboxd links.
	TMDBID *int   `json:"tmdbId,omitzero"`
	IMDbID string `json:"imdbId,omitempty"`

	// Optional because enrichment is async. posterPath is a raw TMDB path.
	PosterPath  string   `json:"posterPath,omitempty"`
	ReleaseDate string   `json:"releaseDate,omitempty"`
	Runtime     int      `json:"runtime,omitzero"`
	Genres      []string `json:"genres,omitempty"`
	VoteAverage float64  `json:"voteAverage,omitzero"`
}

// fullMovie is the detail class: the tile plus draw fields, modal-only
// metadata, and credits.
type fullMovie struct {
	leanMovieTile

	// Status is the client-visible place. A held winner stays pooled until
	// reveal, matching the pool listings.
	Status domain.MovieStatus `json:"status"`

	// Set only on movie:drawn and GET /movies/current. RevealAt is the server's
	// auto-reveal deadline; ServerNow lets the client avoid its own clock.
	DrawnAt   string `json:"drawnAt,omitempty"`
	RevealAt  string `json:"revealAt,omitempty"`
	ServerNow string `json:"serverNow,omitempty"`
	// Only the DrawClientID client shows the confirm button. Revealed stops a
	// reload from reopening the reel.
	DrawClientID string `json:"drawClientId,omitempty"`
	Revealed     bool   `json:"revealed,omitzero"`

	// Modal-only enriched metadata; raw TMDB path for the backdrop.
	BackdropPath string `json:"backdropPath,omitempty"`
	Tagline      string `json:"tagline,omitempty"`
	Overview     string `json:"overview,omitempty"`

	// Trimmed credits (see mapCredits).
	Cast []creditPerson `json:"cast,omitempty"`
	Crew []creditPerson `json:"crew,omitempty"`
}

type creditPerson struct {
	ID          int    `json:"id"` // TMDB person id
	Name        string `json:"name"`
	ProfilePath string `json:"profilePath,omitempty"` // raw TMDB path, like posterPath
	Character   string `json:"character,omitempty"`   // cast only
	Job         string `json:"job,omitempty"`         // crew only
}

type metaByID map[int]*domain.MovieMetadata

type creditsByID map[int][]domain.MovieCredit

func formatTime(value *time.Time) string {
	if value == nil {
		return ""
	}
	return value.UTC().Format(timeFormat)
}

// formatTimePrecise is for revealAt, so the revealAt - drawnAt countdown does
// not jitter. drawnAt stays second-precision: it is the draw's identity string.
func formatTimePrecise(value time.Time) string {
	return value.UTC().Format(time.RFC3339Nano)
}

// movieLink prefers IMDb, then TMDB.
func movieLink(movie *domain.Movie) string {
	if movie.IMDbID != nil {
		imdbID := strings.ToLower(strings.TrimSpace(*movie.IMDbID))
		if canonicalIMDbIDRegex.MatchString(imdbID) {
			return "https://www.imdb.com/title/" + imdbID + "/"
		}
	}
	if movie.TMDBID != nil {
		return fmt.Sprintf("https://www.themoviedb.org/movie/%d", *movie.TMDBID)
	}
	return ""
}

func toLeanTile(movie *domain.Movie, md *domain.MovieMetadata) leanMovieTile {
	tile := leanMovieTile{
		ID:              movie.ID,
		Title:           movie.Title,
		Link:            movieLink(movie),
		AddedAt:         formatTime(movie.AddedAt),
		AddedByID:       movie.AddedByID,
		AddedByName:     movie.AddedByName,
		AddedByArchived: movie.AddedByArchived,
		WatchedAt:       formatTime(movie.WatchedAt),
		TMDBID:          movie.TMDBID,
	}
	if movie.WildcardOfMovieID != nil {
		tile.WildcardOfMovieID = *movie.WildcardOfMovieID
	}
	if movie.IMDbID != nil {
		tile.IMDbID = *movie.IMDbID
	}
	if md != nil {
		if md.PosterPath != nil {
			tile.PosterPath = *md.PosterPath
		}
		tile.ReleaseDate = md.ReleaseDate
		tile.Runtime = md.Runtime
		tile.Genres = md.Genres
		tile.VoteAverage = md.VoteAverage
	}
	return tile
}

func toFullMovie(movie *domain.Movie, md *domain.MovieMetadata, credits []domain.MovieCredit) fullMovie {
	resp := fullMovie{
		leanMovieTile: toLeanTile(movie, md),
		// domain.Movie.Status is a bare string; the wire class types it.
		Status: domain.MovieStatus(movie.Status),
	}
	if md != nil {
		if md.BackdropPath != nil {
			resp.BackdropPath = *md.BackdropPath
		}
		resp.Tagline = md.Tagline
		resp.Overview = md.Overview
	}
	// Depends on the repo's ORDER BY for billing order.
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

// toFullMovieBare is for SSE payloads; clients refetch enriched data.
func toFullMovieBare(movie *domain.Movie) fullMovie {
	return toFullMovie(movie, nil, nil)
}

func toLeanTiles(movies []*domain.Movie, meta metaByID) []leanMovieTile {
	result := make([]leanMovieTile, 0, len(movies))
	for i := range movies {
		result = append(result, toLeanTile(movies[i], meta[movies[i].ID]))
	}
	return result
}

func leanTilesToMap(movies []*domain.Movie, meta metaByID) map[string]leanMovieTile {
	result := make(map[string]leanMovieTile, len(movies))
	for i := range movies {
		result[strconv.Itoa(movies[i].ID)] = toLeanTile(movies[i], meta[movies[i].ID])
	}
	return result
}

func toAPIUserMeta(user *domain.User, poolMovies, stashMovies []*domain.Movie, meta metaByID) userResponse {
	return userResponse{
		ID:          user.ID,
		Name:        user.Name,
		CurrentPool: leanTilesToMap(poolMovies, meta),
		Stash:       leanTilesToMap(stashMovies, meta),
		CreatedAt:   formatTime(user.CreatedAt),
	}
}
