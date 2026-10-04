package domain

import "context"

const (
	CreditKindCast = "cast"
	CreditKindCrew = "crew"
)

// Person is a TMDB person, stored once and shared by every credit.
type Person struct {
	ID          int
	Name        string
	ProfilePath *string
}

// MovieCredit links a movie to a person as cast or crew. A person can appear
// as both, and as crew with several jobs.
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
	// ReplaceCredits replaces a movie's credits in one transaction and stamps
	// credits_refreshed_at even when empty, so credit-less titles leave the
	// backfill.
	ReplaceCredits(ctx context.Context, movieID int, credits []MovieCredit) error
	// GetCreditsByMovieIDs returns cast before crew, cast in billing order.
	GetCreditsByMovieIDs(ctx context.Context, ids []int) (map[int][]MovieCredit, error)
}
