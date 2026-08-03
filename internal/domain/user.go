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

// Role is the app-owned member role. It is single-valued and never derived
// from a credential (link-state is), so the two axes stay independent.
const (
	RoleMember = "member"
	RoleAdmin  = "admin"
)

// RosterMember is one row of the admin roster: identity plus the presence-derived
// facts the admin surface renders. Link-state is never a stored flag: HasLocalLogin
// and HasLinkedIdentity are the existence of a local_accounts / oidc_identities
// row, InvitePending the existence of a still-valid unredeemed invite, Archived
// the archived_at column. MoviesAuthored decides delete-vs-archive on removal, so
// the surface can preview which path a remove will take before committing.
type RosterMember struct {
	ID   int
	Name string
	// Username is the local-login handle, present only when a local account
	// exists (the same row HasLocalLogin is derived from). Empty for members with
	// no password credential.
	Username          string
	Role              string
	Archived          bool
	HasLocalLogin     bool
	HasLinkedIdentity bool
	InvitePending     bool
	MoviesAuthored    int
	LastSeenAt        *time.Time
}

// RosterRepo is the admin read/write surface over members, kept separate from
// UserRepo so the roster read (which spans credential/invite/archive presence)
// and the role write don't widen the movie-board UserRepo the rest of the app
// depends on.
type RosterRepo interface {
	// Roster returns every member, active and archived, with their presence-derived
	// login state. Ordering is active-before-archived, then oldest-first, so the
	// surface can split the two sections without a second pass.
	Roster(ctx context.Context) ([]*RosterMember, error)
	// SetRole changes an active member's role. Demoting the last remaining admin
	// is refused with ErrConflict so the roster can never be left with no admin;
	// a missing or archived member returns ErrNotFound. Setting the role a member
	// already holds is a no-op success. Sessions are untouched: role is read live
	// per request, so the change reflects on the member's next call without a
	// re-login.
	SetRole(ctx context.Context, id int, role string) error
}

type UserRepo interface {
	FindByID(ctx context.Context, id int) (*User, error)
	List(ctx context.Context) ([]*User, error)
	Create(ctx context.Context, name string) (*User, error)
	// Remove deletes or archives a member as one action, chosen by whether they
	// authored any movies (movies.added_by_id is ON DELETE RESTRICT). Zero
	// authored movies hard-deletes the row (credentials/sessions/invites cascade,
	// next_up nulls, name freed); one or more archives it (archived_at set,
	// login rows stripped, row and attribution kept). Removing the last active
	// admin returns ErrConflict. A missing member returns ErrNotFound.
	Remove(ctx context.Context, id int) (RemoveOutcome, error)
	// Restore reactivates an archived member after re-stripping any residual
	// credential, session, and invite rows. A member that is not archived (or
	// does not exist) returns ErrNotFound: there is nothing to restore.
	// Re-issuing a claim invite is the caller's job.
	Restore(ctx context.Context, id int) error
}

type User struct {
	ID        int
	Name      string
	CreatedAt *time.Time
	UpdatedAt *time.Time
}
