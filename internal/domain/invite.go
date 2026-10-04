package domain

import (
	"context"
	"time"
)

// InviteContext is an invite joined to what the claim page needs. Validity is
// derived from the timestamps, never a status column.
type InviteContext struct {
	ID            int64
	UserID        int
	ExpiresAt     time.Time
	UsedAt        *time.Time
	RevokedAt     *time.Time
	DisplayName   string
	HasLocalLogin bool
}

// InviteOverview is one row of the admin invites surface. Status is derived by
// the caller. IssuedBy is nil when the issuer is unknown or deleted.
type InviteOverview struct {
	PublicID   string
	UserID     int
	MemberName string
	ExpiresAt  time.Time
	CreatedAt  time.Time
	IssuedBy   *string
}

// InviteRepo persists invites. The raw token never reaches the store, only its
// SHA-256 hash.
type InviteRepo interface {
	// Create inserts the first current generation for an active member, who must
	// be credential-less unless passwordReset is true. A second current
	// generation returns ErrConflict.
	Create(ctx context.Context, userID int, publicID, tokenHash string, expiresAt, createdAt time.Time, createdBy *int, passwordReset ...bool) error
	// ReplaceCurrent atomically swaps the exact generation the caller saw. A
	// stale or spent handle returns ErrConflict; a missing or archived owner
	// returns ErrNotFound.
	ReplaceCurrent(ctx context.Context, currentPublicID, replacementPublicID, tokenHash string, expiresAt, createdAt time.Time, createdBy *int) error
	// RevokeOpen retires one open generation, or returns ErrConflict.
	RevokeOpen(ctx context.Context, publicID string, now, revokedAt time.Time) error
	// DismissExpired retires one expired generation, or returns ErrConflict.
	DismissExpired(ctx context.Context, publicID string, now, revokedAt time.Time) error
	// FindContextByTokenHash returns sql.ErrNoRows when no active member's invite
	// matches.
	FindContextByTokenHash(ctx context.Context, tokenHash string) (*InviteContext, error)
	// ListCurrent returns each active member's unused, unrevoked generation.
	// Expiry is left to the caller, so expired ones stay dismissible.
	ListCurrent(ctx context.Context) ([]InviteOverview, error)
}
