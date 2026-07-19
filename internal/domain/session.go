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
	// Create inserts a freshly minted session row.
	Create(ctx context.Context, s Session) error
	// FindByTokenHash returns the session for a cookie's token hash joined to
	// the member's live role, or sql.ErrNoRows if none matches.
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
	// DeleteExpired sweeps rows past their absolute cap (expires_at <= now) or
	// their idle window (last_seen_at <= idleCutoff). Returns rows removed.
	DeleteExpired(ctx context.Context, now, idleCutoff time.Time) (int64, error)
	// CountOthersByUserID counts the member's other live sessions: every row
	// except keepTokenHash that is still inside both windows (expires_at > now
	// and last_seen_at > idleCutoff). Drives the "other devices" number the
	// account page shows before a log-out-everywhere.
	CountOthersByUserID(ctx context.Context, userID int, keepTokenHash string, now, idleCutoff time.Time) (int, error)
}
