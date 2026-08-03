package domain

import (
	"context"
	"time"
)

// Session is one row of the server-side, revocable session store: the backbone
// every login path shares. The raw cookie token is never stored; TokenHash is
// SHA-256 of it, so a stolen row can't be replayed. ExpiresAt is the absolute
// 90-day cap set at mint; LastSeenAt drives the 30-day idle slide.
type Session struct {
	ID         int64
	UserID     int
	TokenHash  string
	ExpiresAt  time.Time
	LastSeenAt time.Time
	UserAgent  *string
	IP         *string
	CreatedAt  time.Time
}

// AuthSession is a session joined to its member's live role: the exact shape
// requireSession needs to both validate the session and authorize the request.
// Role is read live per request (never cached in the row) so a role change
// takes effect on the next call without touching any session.
type AuthSession struct {
	Session
	Role string
}

// SessionRepo is the persistence port for the session store. Timestamps are
// passed in rather than defaulted in SQL so the whole store runs off one
// injectable clock and time-based behavior is testable without real sleeps.
type SessionRepo interface {
	// Create inserts a freshly minted session row for an active member. A
	// missing or archived member returns ErrNotFound.
	Create(ctx context.Context, s Session) error
	// FindByTokenHash returns the session for a cookie's token hash joined to the
	// active member's live role, or sql.ErrNoRows if none matches. A residual
	// session belonging to an archived member is treated as absent.
	FindByTokenHash(ctx context.Context, tokenHash string) (*AuthSession, error)
	// TouchLastSeen slides a session's last_seen_at forward (the idle refresh).
	TouchLastSeen(ctx context.Context, id int64, lastSeen time.Time) error
	// DeleteByTokenHash revokes one session (the current-device logout).
	DeleteByTokenHash(ctx context.Context, tokenHash string) error
	// DeleteByUserID revokes every session for a member (logout-everywhere,
	// admin/invite reset). Returns the number of rows removed.
	DeleteByUserID(ctx context.Context, userID int) (int64, error)
	// DeleteOthersByUserID revokes every session for a member except the one
	// whose token hash is keepTokenHash (password-change: drop others, keep
	// current). Returns the number of rows removed.
	DeleteOthersByUserID(ctx context.Context, userID int, keepTokenHash string) (int64, error)
	// DeleteByIDForUser revokes one session by row id, scoped to its owner: the
	// per-device sign-out. The user_id predicate is the authorization, so a
	// guessed id belonging to someone else removes nothing. It returns the
	// deleted row's token hash (empty when nothing matched), so the caller can
	// tell "revoked another device" from "revoked the one I'm holding" without a
	// second read racing the delete.
	DeleteByIDForUser(ctx context.Context, id int64, userID int) (deletedTokenHash string, err error)
	// DeleteExpired sweeps rows past their absolute cap (expires_at <= now) or
	// their idle window (last_seen_at <= idleCutoff). Returns rows removed.
	DeleteExpired(ctx context.Context, now, idleCutoff time.Time) (int64, error)
	// ListLiveByUserID returns the member's sessions that are still inside both
	// windows (expires_at > now and last_seen_at > idleCutoff), newest activity
	// first. It backs the member's own device list, so it lists live rows only:
	// a session that would no longer authenticate is not a device you are
	// signed in on.
	ListLiveByUserID(ctx context.Context, userID int, now, idleCutoff time.Time) ([]Session, error)
}
