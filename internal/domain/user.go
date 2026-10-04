package domain

import (
	"context"
	"time"
)

// RemoveOutcome is which member-removal path ran, so the UI can tell "gone"
// from "archived, restorable".
type RemoveOutcome string

const (
	// OutcomeDeleted: the member authored no movies, so the row is deleted.
	OutcomeDeleted RemoveOutcome = "deleted"
	// OutcomeArchived: the member authored movies, so the row stays for
	// attribution and every login row is removed.
	OutcomeArchived RemoveOutcome = "archived"
)

// Role is the app-owned member role. It is never derived from a credential.
type Role string

const (
	RoleMember Role = "member"
	RoleGuest  Role = "guest"
	RoleAdmin  Role = "admin"
)

// ParseRole converts an HTTP or fixture value into the closed app-owned enum.
func ParseRole(value string) (Role, bool) {
	role := Role(value)
	return role, role.Valid()
}

// Valid reports whether role belongs to the app-owned role enum.
func (r Role) Valid() bool {
	return r == RoleMember || r == RoleGuest || r == RoleAdmin
}

// IsTurnParticipant reports whether the role may hold Next up and run group
// decisions. Guests only curate their stash.
func (r Role) IsTurnParticipant() bool {
	return r == RoleMember || r == RoleAdmin
}

// RoleChange is one requested membership transition. ConfirmTurnHandoff is the
// second step when the target still owns Next up.
type RoleChange struct {
	MemberID           int
	Role               Role
	ConfirmTurnHandoff bool
}

// RoleChangeResult reports committed effects. A nil NextUp with TurnChanged
// means no participant is eligible.
type RoleChangeResult struct {
	Changed     bool
	TurnChanged bool
	NextUp      *User
}

// RosterMember is one row of the admin roster. Link-state is never stored: it
// is derived from the presence of rows. MoviesAuthored decides delete or archive.
type RosterMember struct {
	ID   int
	Name string
	// Username is empty when the member has no local login.
	Username          string
	Role              Role
	Archived          bool
	HasLocalLogin     bool
	HasLinkedIdentity bool
	InvitePending     bool
	MoviesAuthored    int
	LastSeenAt        *time.Time
}

// RosterRepo is the admin member surface, kept apart so it does not widen
// UserRepo.
type RosterRepo interface {
	// Roster returns every member, active before archived, then oldest first.
	Roster(ctx context.Context) ([]*RosterMember, error)
	// SetRole changes an active member's role. It refuses to demote the last
	// admin (ErrConflict) and to make the Next up holder a Guest without
	// confirmation (ErrTurnHandoffConfirmationRequired). Role is read live per
	// request, so sessions need no re-login.
	SetRole(ctx context.Context, change RoleChange) (RoleChangeResult, error)
}

type UserRepo interface {
	FindByID(ctx context.Context, id int) (*User, error)
	List(ctx context.Context) ([]*User, error)
	Create(ctx context.Context, name string) (*User, error)
	// Remove hard-deletes a member who authored no movies and archives one who
	// did (movies.added_by_id is ON DELETE RESTRICT). Removing the last active
	// admin returns ErrConflict.
	Remove(ctx context.Context, id int) (RemoveOutcome, error)
	// Restore reactivates an archived member after re-stripping residual login
	// rows. The caller issues a new claim invite.
	Restore(ctx context.Context, id int) error
}

type User struct {
	ID        int
	Name      string
	CreatedAt *time.Time
	UpdatedAt *time.Time
}
