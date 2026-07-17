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
	ID          int                  `json:"userID"`
	Name        string               `json:"name"`
	CurrentPool map[string]fullMovie `json:"currentPool"`
	Stash       map[string]fullMovie `json:"stash"`
	CreatedAt   string               `json:"createdAt"`
}

// The movie wire shape comes in exactly two payload classes, enforced by the
// type system: a handler returning []leanMovieTile CANNOT accidentally ship
// credits or prose, so the lean list payloads (the 196→16 KB watched-list
// win) are guarded by the compiler instead of by review.

// leanMovieTile is the list/tile class: identity + the tile-level enriched
// fields the grids render (poster, rating, release date, runtime, genres).
// The watched list ships hundreds of these, so the heavy modal-only fields
// (backdrop, tagline, overview, credits) are structurally absent — the modal
// lazy-loads the fullMovie from GET /movies/:id when it opens.
type leanMovieTile struct {
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

	// Enriched TMDB tile fields. All optional — a movie may not be enriched
	// yet (enrichment is async), so these are omitted when absent and the
	// frontend degrades gracefully (placeholder poster, hidden chips).
	// posterPath is a raw TMDB path (e.g. "/abc.jpg"); the frontend builds
	// the image URL.
	PosterPath  string   `json:"posterPath,omitempty"`
	ReleaseDate string   `json:"releaseDate,omitempty"`
	Runtime     int      `json:"runtime,omitempty"`
	Genres      []string `json:"genres,omitempty"`
	VoteAverage float64  `json:"voteAverage,omitempty"`
}

// fullMovie is the detail class: everything on the tile plus the draw
// coordination fields, the modal-only metadata, and the credits. Served by
// the current/pool/detail/board paths; JSON-flattened via the embedding.
type fullMovie struct {
	leanMovieTile

	// Draw-reveal coordination. Set only on the movie:drawn event and the
	// current-movie endpoint. DrawnAt is when the current movie was drawn
	// (drives resuming the cross-client reveal spin after a reload); RevealAt
	// is the server's auto-reveal deadline (clients time the confirm
	// countdown off it — the server owns the reveal timing); ServerNow is the
	// server clock at response time so the client computes elapsed without
	// trusting its own clock. All omitted when no draw is active.
	DrawnAt   string `json:"drawnAt,omitempty"`
	RevealAt  string `json:"revealAt,omitempty"`
	ServerNow string `json:"serverNow,omitempty"`
	// DrawClientID is the client that initiated the draw; only that client shows
	// the reel's confirm button. Revealed reports whether the draw was confirmed,
	// so a reload after the reveal shows the result instead of re-opening the reel.
	DrawClientID string `json:"drawClientId,omitempty"`
	Revealed     bool   `json:"revealed,omitempty"`

	// Modal-only enriched metadata; raw TMDB path for the backdrop.
	BackdropPath string `json:"backdropPath,omitempty"`
	Tagline      string `json:"tagline,omitempty"`
	Overview     string `json:"overview,omitempty"`

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

// toLeanTile builds the tile-class response.
func toLeanTile(movie *domain.Movie, md *domain.MovieMetadata) leanMovieTile {
	tile := leanMovieTile{
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

// toFullMovie builds the detail-class response, folding the modal-only
// metadata (backdrop, tagline, overview) and credits onto the tile.
func toFullMovie(movie *domain.Movie, md *domain.MovieMetadata, credits []domain.MovieCredit) fullMovie {
	resp := fullMovie{leanMovieTile: toLeanTile(movie, md)}
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

// toFullMovieBare builds a detail-class response without enriched metadata or
// credits. Used for SSE broadcast payloads, where the frontend refetches
// enriched data from the GET endpoints rather than reading the event body.
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

func toFullMovies(movies []*domain.Movie, meta metaByID, credits creditsByID) []fullMovie {
	result := make([]fullMovie, 0, len(movies))
	for i := range movies {
		result = append(result, toFullMovie(movies[i], meta[movies[i].ID], credits[movies[i].ID]))
	}
	return result
}

func fullMoviesToMap(movies []*domain.Movie, meta metaByID, credits creditsByID) map[string]fullMovie {
	result := make(map[string]fullMovie, len(movies))
	for i := range movies {
		result[strconv.Itoa(movies[i].ID)] = toFullMovie(movies[i], meta[movies[i].ID], credits[movies[i].ID])
	}
	return result
}

func toAPIUserMeta(user *domain.User, poolMovies, stashMovies []*domain.Movie, meta metaByID, credits creditsByID) userResponse {
	return userResponse{
		ID:          user.ID,
		Name:        user.Name,
		CurrentPool: fullMoviesToMap(poolMovies, meta, credits),
		Stash:       fullMoviesToMap(stashMovies, meta, credits),
		CreatedAt:   formatTime(user.CreatedAt),
	}
}
