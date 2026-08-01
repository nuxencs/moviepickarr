package domain

import (
	"context"
	"time"
)

// OIDCIdentity is one row of the oidc_identities table: a member's link to the
// external SSO provider. The (Issuer, Subject) pair is the sole match key on
// login and is globally unique; UserID is unique too, so a member holds at most
// one linked identity. Email and PreferredUsername are informational snapshots
// refreshed on each login, never a match or gate key.
type OIDCIdentity struct {
	ID                int64
	UserID            int
	Issuer            string
	Subject           string
	Email             *string
	PreferredUsername *string
	LastLoginAt       *time.Time
}

// OIDCIdentityRepo is the persistence port for the linked-identity flow over the
// 009 oidc_identities table. The collision matrix (a member may hold at most one
// identity; an identity may belong to at most one member) is enforced by the two
// UNIQUE constraints and surfaced as ErrConflict at this boundary, so the
// service layer never imports the driver. Timestamps are passed in rather than
// defaulted in SQL so the flow runs off one injectable clock.
type OIDCIdentityRepo interface {
	// FindByIssuerSubject resolves the login/link match key for an active member,
	// or returns sql.ErrNoRows when no active member owns it. An archived link is
	// indistinguishable from an unlinked identity.
	FindByIssuerSubject(ctx context.Context, issuer, subject string) (*OIDCIdentity, error)
	// FindByUserID returns an active member's linked identity, or sql.ErrNoRows
	// when the member holds none or is archived.
	FindByUserID(ctx context.Context, userID int) (*OIDCIdentity, error)
	// Insert links a member to an (issuer, subject) with its snapshot fields. A
	// violation of either UNIQUE (user_id, or issuer+subject) surfaces as
	// ErrConflict; a missing or archived member returns ErrNotFound.
	Insert(ctx context.Context, id OIDCIdentity, createdAt time.Time) error
	// TouchLogin refreshes the informational snapshots (email, preferred_username)
	// and bumps last_login_at/updated_at: the login-dispatch and idempotent
	// same-member link both land here.
	TouchLogin(ctx context.Context, id int64, email, preferredUsername *string, lastLoginAt, updatedAt time.Time) error
	// DeleteByUserID removes a member's linked identity (unlink), returning the
	// rows affected so the caller can tell a real unlink from a no-op.
	DeleteByUserID(ctx context.Context, userID int) (int64, error)
}
