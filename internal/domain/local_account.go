package domain

import (
	"context"
	"time"
)

// LocalAccount is a member's username/password login (at most one) with its
// lockout counters. Username is NOCASE unique.
type LocalAccount struct {
	UserID         int
	Username       string
	PasswordHash   string
	FailedAttempts int
	LockedUntil    *time.Time
	LastLoginAt    *time.Time
}

// MemberIdentity is the GET /auth/me projection. The link-state flags are
// derived from credential rows, never stored.
type MemberIdentity struct {
	ID                int
	DisplayName       string
	Username          *string
	Role              Role
	HasLocalLogin     bool
	HasLinkedIdentity bool
}

// LocalAccountRepo persists local logins. Constraint violations become domain
// errors here, so the service layer never imports the driver.
type LocalAccountRepo interface {
	// FindByUsername returns an active member's login, or sql.ErrNoRows. An
	// archived login reads as an unknown username.
	FindByUsername(ctx context.Context, username string) (*LocalAccount, error)
	// FindByUserID returns an active member's login, or sql.ErrNoRows.
	FindByUserID(ctx context.Context, userID int) (*LocalAccount, error)
	// Create returns ErrConflict on a username collision and ErrNotFound for a
	// missing or archived member.
	Create(ctx context.Context, userID int, username, passwordHash string) error
	// UpdatePasswordHash leaves the lockout counters untouched.
	UpdatePasswordHash(ctx context.Context, userID int, passwordHash string, updatedAt time.Time) error
	// UpdatePasswordAndClearLockout is the admin-reset write.
	UpdatePasswordAndClearLockout(ctx context.Context, userID int, passwordHash string, updatedAt time.Time) error
	// RecordFailedAttempt atomically increments the count and locks at
	// lockThreshold, only while expectedPasswordHash is still current.
	RecordFailedAttempt(ctx context.Context, userID int, expectedPasswordHash string, lockThreshold int, lockUntil, updatedAt time.Time) error
	// RecordSuccessfulLogin resets the lockout only while expectedPasswordHash is
	// current, so a rehash cannot overwrite a concurrent recovery.
	RecordSuccessfulLogin(ctx context.Context, userID int, expectedPasswordHash string, newPasswordHash *string, lastLoginAt, updatedAt time.Time) error
	Delete(ctx context.Context, userID int) error
	// HasLinkedIdentity backs the self-last-credential guard.
	HasLinkedIdentity(ctx context.Context, userID int) (bool, error)
	// GetMemberIdentity returns sql.ErrNoRows for a missing or archived member.
	GetMemberIdentity(ctx context.Context, userID int) (*MemberIdentity, error)
}
