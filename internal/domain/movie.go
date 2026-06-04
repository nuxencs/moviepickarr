package domain

import (
	"context"
	"time"
)

type MovieRepo interface {
	FindByID(ctx context.Context, id int) (*Movie, error)
	List(ctx context.Context) ([]*Movie, error)
	FindByUserID(ctx context.Context, userID int) ([]*Movie, error)
	FindByStatus(ctx context.Context, status string) ([]*Movie, error)
	FindByUserIDAndStatus(ctx context.Context, userID int, status string) ([]*Movie, error)
	CountByStatus(ctx context.Context, status string) (int, error)
	CountByUserIDAndStatus(ctx context.Context, userID int, status string) (int, error)
	Add(ctx context.Context, title, link, status string, userID int) (*Movie, error)
	SetExternalIDs(ctx context.Context, id int, tmdbID *int, imdbID *string) error
	UpdateTitleAndLink(ctx context.Context, id int, title, link string) error
	UpdateWatchedAt(ctx context.Context, id int, watchedAt time.Time) error
	UpdateStatus(ctx context.Context, id int, status string) error
	MarkAsWatched(ctx context.Context, id int, time time.Time) error
	GetRandomPooled(ctx context.Context) (*Movie, error)
	GetCurrent(ctx context.Context) (*Movie, error)
	Delete(ctx context.Context, id int) error
}

type MovieStatus string

const (
	MovieStatusPool    MovieStatus = "pool"
	MovieStatusStash   MovieStatus = "stash"
	MovieStatusCurrent MovieStatus = "current"
	MovieStatusWatched MovieStatus = "watched"
)

type Movie struct {
	ID          int
	Title       string
	Link        string // fallback URL; the effective link is derived from TMDBID/IMDbID
	Status      string
	AddedAt     *time.Time
	AddedByID   int
	AddedByName string
	WatchedAt   *time.Time
	TMDBID      *int    // stable TMDB identity (nullable)
	IMDbID      *string // stable IMDb identity (nullable)
}

// MovieMetadata holds TMDB-derived display data for a movie, stored 1:1 in the
// movie_metadata table. Stable identity (TMDB/IMDb ids) lives on the Movie row;
// this holds only enriched display fields.
type MovieMetadata struct {
	MovieID      int
	Overview     string
	PosterPath   *string // nullable -> SQL NULL
	BackdropPath *string // nullable -> SQL NULL
	ReleaseDate  string  // TMDB "YYYY-MM-DD", stored verbatim
	Runtime      int
	Genres       []string // marshaled to/from the genres JSON TEXT column in the repo
	VoteAverage  float64
	VoteCount    int
	Tagline      string
	EnrichedAt   *time.Time
}

// EnrichmentCandidate identifies a movie that needs enrichment. The worker
// re-loads the full movie (incl. its ids/link) via FindByID before enriching.
type EnrichmentCandidate struct {
	MovieID int
}

type MovieMetadataRepo interface {
	UpsertMetadata(ctx context.Context, md MovieMetadata) error
	GetMetadata(ctx context.Context, movieID int) (*MovieMetadata, error)
	// NeedsEnrichment returns candidates that either have no metadata row
	// (backfill — pass the zero time) or whose enriched_at is older than
	// staleBefore (periodic refresh). Results are capped at limit.
	NeedsEnrichment(ctx context.Context, staleBefore time.Time, limit int) ([]EnrichmentCandidate, error)
}
