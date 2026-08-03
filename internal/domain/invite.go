package domain

import (
	"context"
	"time"
)

// InviteContext is an invite joined to the member fields the claim page needs:
// the raw invite state (to decide valid / used / no-longer-valid), the display
// name to greet by, and whether the member already holds a local login (a reset)
// or not (a fresh placeholder claim). One read backs the whole claim state
// machine. Validity is time-derived, never a status column: an invite is usable
// while used_at IS NULL AND revoked_at IS NULL AND expires_at > now.
type InviteContext struct {
	ID            int64
	UserID        int
	ExpiresAt     time.Time
	UsedAt        *time.Time
	RevokedAt     *time.Time
	DisplayName   string
	HasLocalLogin bool
}

// InviteOverview is one row of the admin invites surface: a current invite
// joined to the member it was issued for and the admin who issued it. It carries
// no status word, for the same reason InviteContext doesn't: open vs expired is
// derived by comparing ExpiresAt to the caller's clock, never stored. IssuedBy
// is nil when created_by was null (a seeded or system invite, or an issuing
// admin since deleted), which the surface renders by omitting the line rather
// than naming an issuer that isn't there.
type InviteOverview struct {
	PublicID   string
	UserID     int
	MemberName string
	ExpiresAt  time.Time
	CreatedAt  time.Time
	IssuedBy   *string
}

// InviteRepo is the persistence port for the invite/claim flow over the current
// invites schema (joined to users + local_accounts for claim context). The raw
// token never reaches the store: callers pass its SHA-256 hash. Timestamps are
// passed in rather than defaulted in SQL so the whole flow runs off one
// injectable clock and expiry is testable without real sleeps.
type InviteRepo interface {
	// Create inserts the first current generation for an active member. When
	// passwordReset is false the member must still be credential-less; when true
	// they must hold a local login. The database rejects a second current
	// generation with ErrConflict.
	Create(ctx context.Context, userID int, publicID, tokenHash string, expiresAt, createdAt time.Time, createdBy *int, passwordReset ...bool) error
	// ReplaceCurrent atomically retires the exact generation the caller saw and
	// inserts its replacement for the same member. A stale handle or spent row
	// returns ErrConflict; a missing or archived owner returns ErrNotFound. Either
	// failure leaves the old generation unchanged.
	ReplaceCurrent(ctx context.Context, currentPublicID, replacementPublicID, tokenHash string, expiresAt, createdAt time.Time, createdBy *int) error
	// RevokeOpen retires exactly one current generation while it is still open.
	// A stale handle or wrong state returns ErrConflict.
	RevokeOpen(ctx context.Context, publicID string, now, revokedAt time.Time) error
	// DismissExpired retires exactly one current generation only after it has
	// expired. A stale handle or wrong state returns ErrConflict.
	DismissExpired(ctx context.Context, publicID string, now, revokedAt time.Time) error
	// FindContextByTokenHash returns the invite joined to its member's display
	// name and local-login presence, or sql.ErrNoRows when no active member's
	// invite matches the hash.
	FindContextByTokenHash(ctx context.Context, tokenHash string) (*InviteContext, error)
	// ListCurrent returns each active member's one unused and unrevoked invite
	// generation, joined to the member and issuer names. Expiry is left to the
	// caller's clock so expired generations remain dismissible. Credentialed
	// members remain visible while a password-reset invite is current.
	ListCurrent(ctx context.Context) ([]InviteOverview, error)
}
