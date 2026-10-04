package domain

import (
	"context"
	"time"
)

// OIDCIdentity is a member's link to the SSO provider (at most one). (Issuer,
// Subject) is the only match key; Email and PreferredUsername are snapshots.
type OIDCIdentity struct {
	ID                int64
	UserID            int
	Issuer            string
	Subject           string
	Email             *string
	PreferredUsername *string
	LastLoginAt       *time.Time
}

// OIDCIdentityRepo persists linked identities. A UNIQUE violation becomes
// ErrConflict here, so the service layer never imports the driver.
type OIDCIdentityRepo interface {
	// FindByIssuerSubject returns sql.ErrNoRows when no active member owns the
	// key. An archived link reads as unlinked.
	FindByIssuerSubject(ctx context.Context, issuer, subject string) (*OIDCIdentity, error)
	// FindByUserID returns sql.ErrNoRows when the member has none or is archived.
	FindByUserID(ctx context.Context, userID int) (*OIDCIdentity, error)
	// Insert returns ErrConflict on a UNIQUE violation and ErrNotFound for a
	// missing or archived member.
	Insert(ctx context.Context, id OIDCIdentity, createdAt time.Time) error
	// TouchLogin refreshes the snapshots and bumps last_login_at.
	TouchLogin(ctx context.Context, id int64, email, preferredUsername *string, lastLoginAt, updatedAt time.Time) error
	// DeleteByUserID returns the rows affected, so a no-op unlink is visible.
	DeleteByUserID(ctx context.Context, userID int) (int64, error)
}
