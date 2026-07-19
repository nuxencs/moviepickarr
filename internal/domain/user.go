package domain

import (
	"context"
	"time"
)

// RemoveOutcome is which of the two member-removal paths ran: a hard delete of
// a member who authored nothing, or an archive of a member whose authored
// movies must keep their attribution. The caller reports it back so the UI can
// tell "gone" from "archived, restorable".
type RemoveOutcome string

const (
	// OutcomeDeleted is the hard-delete path: the member authored no movies, so
	// the users row is removed and its credentials/sessions/invites cascade away.
	OutcomeDeleted RemoveOutcome = "deleted"
	// OutcomeArchived is the archive path: the member authored movies, so the
	// users row survives (attribution intact) with archived_at set and every
	// credential/session/invite row explicitly removed so the login dies.
	OutcomeArchived RemoveOutcome = "archived"
)

type UserRepo interface {
	FindByID(ctx context.Context, id int) (*User, error)
	List(ctx context.Context) ([]*User, error)
	Create(ctx context.Context, name string) (*User, error)
	// Remove deletes or archives a member as one action, chosen by whether they
	// authored any movies (movies.added_by_id is ON DELETE RESTRICT). Zero
	// authored movies hard-deletes the row (credentials/sessions/invites cascade,
	// next_up nulls, name freed); one or more archives it (archived_at set,
	// login rows stripped, row and attribution kept). A missing member returns
	// ErrNotFound.
	Remove(ctx context.Context, id int) (RemoveOutcome, error)
	// Restore reactivates an archived member by clearing archived_at. A member
	// that is not archived (or does not exist) returns ErrNotFound: there is
	// nothing to restore. Re-issuing a claim invite is the caller's job, since
	// archiving stripped the credentials.
	Restore(ctx context.Context, id int) error
}

type User struct {
	ID        int
	Name      string
	CreatedAt *time.Time
	UpdatedAt *time.Time
}
