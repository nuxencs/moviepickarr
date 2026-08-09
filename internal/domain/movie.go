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
	AddToStash(ctx context.Context, title string, userID int, tmdbID *int, imdbID *string) (*Movie, error)
	SetExternalIDs(ctx context.Context, id int, tmdbID *int, imdbID *string) error
	UpdateStatus(ctx context.Context, id int, status string) error
	// UpdateStatusIf conditionally transitions a movie: it sets status=to only
	// WHERE the row currently has status=from, returning the number of rows
	// affected (1 = transitioned, 0 = the precondition did not hold). This makes
	// a status move idempotent and race-safe without a read-modify-write.
	UpdateStatusIf(ctx context.Context, id int, to, from string) (int64, error)
	// PromoteToPoolIfRoom atomically moves a stashed movie into its owner's pool,
	// but only when that pool holds fewer than maxPool movies. The source-status
	// check and the per-user pool-count check happen in one statement, so
	// concurrent promotions of distinct movies cannot overshoot the cap. Returns
	// rows affected (1 = promoted, 0 = not — already pooled, not stashed, or full).
	PromoteToPoolIfRoom(ctx context.Context, id, maxPool int) (int64, error)
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
	Status      string
	AddedAt     *time.Time
	AddedByID   int
	AddedByName string
	// AddedByArchived distinguishes preserved attribution from a member whose
	// active board still exists.
	AddedByArchived bool
	WatchedAt       *time.Time
	TMDBID          *int    // stable TMDB identity (nullable)
	IMDbID          *string // stable IMDb identity (nullable)
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

// MovieIdentity is the stable external identity used to decide whether fetched
// enrichment still belongs to the current movie.
type MovieIdentity struct {
	TMDBID *int
	IMDbID *string
}

// MovieIdentityTarget is the one provider identity requested by an authored
// edit. Exactly one field must be set. Unlike MovieIdentity, which snapshots
// every known id for enrichment staleness, this is a selector: matching its
// provider preserves all stored ids; a different target replaces them.
type MovieIdentityTarget struct {
	TMDBID *int
	IMDbID *string
}

// MovieEnrichmentWrite carries one complete enrichment commit. Expected is the
// identity observed before remote TMDB work; Resolved and the derived rows may
// be stored only while that identity still matches.
type MovieEnrichmentWrite struct {
	MovieID  int
	Expected MovieIdentity
	Resolved MovieIdentity
	Metadata MovieMetadata
	Credits  []MovieCredit
}

type MovieMetadataRepo interface {
	UpsertMetadata(ctx context.Context, md MovieMetadata) error
	GetMetadata(ctx context.Context, movieID int) (*MovieMetadata, error)
	// GetMetadataByMovieIDs batch-loads metadata for the given movie ids,
	// keyed by movie id. Ids without a metadata row are simply absent from the
	// returned map (enrichment is async, so a movie may not be enriched yet).
	GetMetadataByMovieIDs(ctx context.Context, ids []int) (map[int]*MovieMetadata, error)
	// NeedsEnrichment returns candidates that have no metadata row (backfill
	// — pass the zero time), whose enriched_at is older than staleBefore
	// (periodic refresh), or whose credits were never ingested (credits
	// backfill). Results are capped at limit.
	NeedsEnrichment(ctx context.Context, staleBefore time.Time, limit int) ([]EnrichmentCandidate, error)
	// MarkEnrichmentStale clears the credits marker so NeedsEnrichment re-selects
	// the movie on the next drain. Used when a movie's external identity
	// changes: the enrich queue is in-memory, so without this backstop a lost
	// enqueue would leave the previous movie's metadata and credits in place
	// until the periodic refresh TTL.
	MarkEnrichmentStale(ctx context.Context, movieID int) error
}
