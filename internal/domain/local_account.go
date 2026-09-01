package domain

import (
	"context"
	"time"
)

// LocalAccount is one row of the local_accounts table: a member's
// username/password login plus the self-healing lockout counters. UserID is the
// primary key, so a member holds at most one local login. Username is stored
// case-folded-unique (NOCASE) because login treats it trimmed and
// case-insensitive. FailedAttempts / LockedUntil back the login throttle;
// LockedUntil and LastLoginAt are nil when never set.
type LocalAccount struct {
	UserID         int
	Username       string
	PasswordHash   string
	FailedAttempts int
	LockedUntil    *time.Time
	LastLoginAt    *time.Time
}

// MemberIdentity is the projection GET /auth/me returns: the member's identity
// plus the two presence-derived link-state flags. Username is nil when the
// member has no local login. HasLocalLogin and HasLinkedIdentity are derived
// from credential-row presence, never stored as flags.
type MemberIdentity struct {
	ID                int
	DisplayName       string
	Username          *string
	Role              Role
	HasLocalLogin     bool
	HasLinkedIdentity bool
}

// LocalAccountRepo is the persistence port for the local-login flow over the
// local_accounts table (plus a read of oidc_identities for derived link-state).
// Timestamps are passed in rather than defaulted in SQL so the whole flow runs
// off one injectable clock and the lockout windows are testable without sleeps.
//
// Constraint violations are translated to domain errors at this boundary: a
// NOCASE username collision surfaces as ErrConflict and an insert against a
// missing or archived member as ErrNotFound, so the service layer never imports
// the driver.
type LocalAccountRepo interface {
	// FindByUsername looks an active member's local login up by its NOCASE
	// username, or returns sql.ErrNoRows when none matches. Archived credentials
	// are indistinguishable from an unknown username.
	FindByUsername(ctx context.Context, username string) (*LocalAccount, error)
	// FindByUserID looks an active member's local login up by user id, or returns
	// sql.ErrNoRows when the member has no local login or is archived.
	FindByUserID(ctx context.Context, userID int) (*LocalAccount, error)
	// Create inserts a member's first local login. A NOCASE username collision
	// returns ErrConflict; a missing or archived member returns ErrNotFound.
	Create(ctx context.Context, userID int, username, passwordHash string) error
	// UpdatePasswordHash rewrites just the password hash (the self-serve
	// password change). Lockout counters are left untouched.
	UpdatePasswordHash(ctx context.Context, userID int, passwordHash string, updatedAt time.Time) error
	// UpdatePasswordAndClearLockout rewrites the password hash and clears the
	// lockout (failed_attempts=0, locked_until=NULL): the admin-reset path.
	UpdatePasswordAndClearLockout(ctx context.Context, userID int, passwordHash string, updatedAt time.Time) error
	// RecordFailedAttempt atomically increments the failed-attempt count and sets
	// the lockout deadline when the incremented count reaches lockThreshold. The
	// write applies only while the verified credential hash is still current.
	RecordFailedAttempt(ctx context.Context, userID int, expectedPasswordHash string, lockThreshold int, lockUntil, updatedAt time.Time) error
	// RecordSuccessfulLogin resets the lockout counters and bumps last_login_at
	// only while the verified hash is current. A non-nil newPasswordHash performs
	// maintenance rehashing without overwriting a concurrent recovery.
	RecordSuccessfulLogin(ctx context.Context, userID int, expectedPasswordHash string, newPasswordHash *string, lastLoginAt, updatedAt time.Time) error
	// Delete removes a member's local login (admin credential removal).
	Delete(ctx context.Context, userID int) error
	// HasLinkedIdentity reports whether the member holds an oidc_identities row,
	// the derived hasLinkedIdentity flag and the self-last-credential guard.
	HasLinkedIdentity(ctx context.Context, userID int) (bool, error)
	// GetMemberIdentity returns the /auth/me projection for an active member, or
	// sql.ErrNoRows if the member does not exist or is archived.
	GetMemberIdentity(ctx context.Context, userID int) (*MemberIdentity, error)
}
