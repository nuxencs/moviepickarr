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

// InviteRepo is the persistence port for the invite/claim flow over the 009
// invites table (joined to users + local_accounts for claim context). The raw
// token never reaches the store: callers pass its SHA-256 hash. Timestamps are
// passed in rather than defaulted in SQL so the whole flow runs off one
// injectable clock and expiry is testable without real sleeps.
type InviteRepo interface {
	// Create inserts a fresh invite for an active member. A missing or archived
	// member returns ErrNotFound.
	Create(ctx context.Context, userID int, tokenHash string, expiresAt time.Time, createdBy *int) error
	// RevokeValidByUserID stamps revoked_at on every currently-valid invite for a
	// member and returns the number of rows affected, so the caller can enforce
	// one-valid-invite-per-member (regenerate) and tell a real revoke from a
	// no-op (nothing to revoke).
	RevokeValidByUserID(ctx context.Context, userID int, now, revokedAt time.Time) (int64, error)
	// FindContextByTokenHash returns the invite joined to its member's display
	// name and local-login presence, or sql.ErrNoRows when no active member's
	// invite matches the hash.
	FindContextByTokenHash(ctx context.Context, tokenHash string) (*InviteContext, error)
	// MarkUsed stamps used_at on an invite: the consume-on-first-credential step
	// that turns a live invite into the "already set up" state.
	MarkUsed(ctx context.Context, id int64, usedAt time.Time) error
}
