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

// InviteOverview is one row of the admin invites surface: an outstanding invite
// joined to the member it was issued for and the admin who issued it. It carries
// no status word, for the same reason InviteContext doesn't — open vs expired is
// derived by comparing ExpiresAt to the caller's clock, never stored. IssuedBy
// is nil when created_by was null (a seeded or system invite, or an issuing
// admin since deleted), which the surface renders by omitting the line rather
// than naming an issuer that isn't there.
type InviteOverview struct {
	ID         int64
	UserID     int
	MemberName string
	ExpiresAt  time.Time
	CreatedAt  time.Time
	IssuedBy   *string
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
	// RevokeByID stamps revoked_at on one invite addressed by row id, whatever
	// its expiry: the admin dismissing a lapsed row. It returns the number of
	// rows affected, so a second dismiss (or an id that was never outstanding)
	// reads as a miss rather than a silent success. Already-used and
	// already-revoked rows are left alone.
	RevokeByID(ctx context.Context, id int64, revokedAt time.Time) (int64, error)
	// ListOutstanding returns each active member's newest invite, when that
	// invite is still unused and unrevoked and the member holds no credential,
	// joined to the member and issuer names. It backs the admin invites
	// overview, so it answers "who is still waiting to set up a login": a
	// member who has since signed in with SSO or set a password drops out even
	// with a live invite row, and the newest-only rule keeps a re-invited
	// member one row rather than one per attempt. Newest means newest overall,
	// not newest still outstanding, so revoking a member's live invite drops
	// them off the list instead of resurfacing an older dead one.
	// Expiry is not filtered here: an expired invite is still outstanding, and
	// splitting open from expired is the caller's clock's job. The rows come
	// back unordered for the same reason, since only that clock can rank open
	// ahead of expired.
	ListOutstanding(ctx context.Context) ([]InviteOverview, error)
}
