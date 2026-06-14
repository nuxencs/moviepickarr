package domain

import "context"

// CreditKind values discriminate cast from crew rows in movie_credits.
const (
	CreditKindCast = "cast"
	CreditKindCrew = "crew"
)

// Person is a TMDB person, stored once (keyed by TMDB person id) and shared by
// every movie credit that references them.
type Person struct {
	ID          int
	Name        string
	ProfilePath *string // nullable -> SQL NULL
}

// MovieCredit links a movie to a person, either as cast (Character/CastOrder)
// or crew (Job/Department). A person can appear as both cast and crew on the
// same movie, and as crew with several jobs.
type MovieCredit struct {
	MovieID    int
	Person     Person
	Kind       string // CreditKindCast or CreditKindCrew
	Character  string
	Job        string
	Department string
	CastOrder  int
}

type MovieCreditsRepo interface {
	// ReplaceCredits transactionally replaces a movie's credits: people are
	// upserted, the movie's prior credit rows deleted, the new rows inserted,
	// and movie_metadata.credits_refreshed_at stamped — also for empty
	// credits, so credit-less titles don't stay backfill candidates forever.
	ReplaceCredits(ctx context.Context, movieID int, credits []MovieCredit) error
	// GetCreditsByMovieIDs batch-loads credits for the given movie ids, keyed
	// by movie id (cast before crew, cast in billing order). Ids without
	// credit rows are simply absent from the returned map.
	GetCreditsByMovieIDs(ctx context.Context, ids []int) (map[int][]MovieCredit, error)
}
