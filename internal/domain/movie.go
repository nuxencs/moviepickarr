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
	// UpdateStatusIf sets status=to only where status=from and returns rows
	// affected, so a move is idempotent and race-safe.
	UpdateStatusIf(ctx context.Context, id int, to, from string) (int64, error)
	// PromoteToPoolIfRoom moves a stashed movie to its owner's pool only while
	// the pool holds fewer than maxPool movies. One statement, so concurrent
	// promotions cannot overshoot the cap.
	PromoteToPoolIfRoom(ctx context.Context, id, maxPool int) (int64, error)
	GetCurrent(ctx context.Context) (*Movie, error)
	Delete(ctx context.Context, id int) error
}

type MovieStatus string

const (
	MovieStatusPool     MovieStatus = "pool"
	MovieStatusStash    MovieStatus = "stash"
	MovieStatusCurrent  MovieStatus = "current"
	MovieStatusWildcard MovieStatus = "wildcard"
	MovieStatusWatched  MovieStatus = "watched"
)

type Movie struct {
	ID          int
	Title       string
	Status      string
	AddedAt     *time.Time
	AddedByID   int
	AddedByName string
	// AddedByArchived marks attribution kept from an archived member.
	AddedByArchived bool
	WatchedAt       *time.Time
	TMDBID          *int
	IMDbID          *string
	// WildcardOfMovieID is the Current draw this watched Wildcard preserved.
	WildcardOfMovieID *int
}

type WildcardStatus string

const (
	WildcardStatusActive   WildcardStatus = "active"
	WildcardStatusWatched  WildcardStatus = "watched"
	WildcardStatusCanceled WildcardStatus = "canceled"
)

// WildcardSelection identifies either an existing movie or a new external
// movie. ExistingMovieID is exclusive with the title and provider identities.
type WildcardSelection struct {
	ExpectedHostMovieID int
	ExistingMovieID     *int
	Title               string
	TMDBID              *int
	IMDbID              *string
}

type Wildcard struct {
	ID                 int64
	HostMovieID        int
	Movie              *Movie
	SelectedByID       *int
	CanceledByID       *int
	SourceStatus       MovieStatus
	CreatedForWildcard bool
	Status             WildcardStatus
	SelectedAt         time.Time
	WatchedAt          *time.Time
	CanceledAt         *time.Time
}

// MovieMetadata holds TMDB display data. Stable ids live on the Movie row.
type MovieMetadata struct {
	MovieID      int
	Overview     string
	PosterPath   *string
	BackdropPath *string
	ReleaseDate  string // TMDB "YYYY-MM-DD", stored verbatim
	Runtime      int
	Genres       []string // JSON TEXT column
	VoteAverage  float64
	VoteCount    int
	Tagline      string
	EnrichedAt   *time.Time
}

// EnrichmentCandidate identifies a movie that needs enrichment.
type EnrichmentCandidate struct {
	MovieID int
}

// MovieIdentity is the external identity that decides whether fetched
// enrichment still belongs to the movie.
type MovieIdentity struct {
	TMDBID *int
	IMDbID *string
}

// MovieIdentityTarget selects the one provider identity an edit asks for.
// Matching the stored provider keeps all ids; a different one replaces them.
type MovieIdentityTarget struct {
	TMDBID *int
	IMDbID *string
}

// MovieEnrichmentWrite is one enrichment commit. It applies only while the
// identity still matches Expected, observed before the TMDB call.
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
	// GetMetadataByMovieIDs omits ids not enriched yet.
	GetMetadataByMovieIDs(ctx context.Context, ids []int) (map[int]*MovieMetadata, error)
	// NeedsEnrichment returns movies with no metadata, metadata older than
	// staleBefore, or no credits, capped at limit.
	NeedsEnrichment(ctx context.Context, staleBefore time.Time, limit int) ([]EnrichmentCandidate, error)
	// MarkEnrichmentStale makes NeedsEnrichment re-select the movie. The enrich
	// queue is in-memory, so this is the backstop for a lost enqueue.
	MarkEnrichmentStale(ctx context.Context, movieID int) error
}
