package domain

import (
	"context"
	"time"
)

// Session is one row of the revocable session store. Only the token's SHA-256
// is stored, so a stolen row cannot be replayed.
type Session struct {
	ID         int64
	PublicID   string
	UserID     int
	TokenHash  string
	ExpiresAt  time.Time
	LastSeenAt time.Time
	UserAgent  *string
	CreatedAt  time.Time
}

// AuthSession is a session joined to its member's live role, read per request
// so a role change applies without touching any session.
type AuthSession struct {
	Session
	Role Role
}

// SessionRepo persists sessions.
type SessionRepo interface {
	// Create returns ErrNotFound for a missing or archived member.
	Create(ctx context.Context, s Session) error
	// FindByTokenHash returns the session with its member's live role, or
	// sql.ErrNoRows. An archived member's session reads as absent.
	FindByTokenHash(ctx context.Context, tokenHash string) (*AuthSession, error)
	TouchLastSeen(ctx context.Context, id int64, lastSeen time.Time) error
	DeleteByTokenHash(ctx context.Context, tokenHash string) error
	// DeleteByUserID returns the number of rows removed.
	DeleteByUserID(ctx context.Context, userID int) (int64, error)
	// DeleteOthersByUserID keeps only keepTokenHash and returns the rows removed.
	DeleteOthersByUserID(ctx context.Context, userID int, keepTokenHash string) (int64, error)
	// DeleteByPublicIDForUser revokes one of userID's sessions; the user_id
	// predicate is the authorization. It returns the deleted token hash (empty on
	// no match) so the caller needs no racing second read.
	DeleteByPublicIDForUser(ctx context.Context, publicID string, userID int) (deletedTokenHash string, err error)
	// DeleteExpired returns the rows removed.
	DeleteExpired(ctx context.Context, now, idleCutoff time.Time) (int64, error)
	// ListLiveByUserID returns the member's sessions inside both windows, newest
	// activity first.
	ListLiveByUserID(ctx context.Context, userID int, now, idleCutoff time.Time) ([]Session, error)
}
