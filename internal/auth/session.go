package auth

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"moviepickarr/internal/domain"
)

// Session lifetime is fixed policy, not operator config: a session is valid
// while it is inside BOTH windows: the absolute cap set at mint and the idle
// window measured from the last request. Long-lived when you keep showing up,
// closed after a long gap away.
const (
	// SessionAbsoluteTTL caps a session's total life regardless of activity.
	SessionAbsoluteTTL = 90 * 24 * time.Hour
	// SessionIdleTTL logs a session out after this long without a request.
	SessionIdleTTL = 30 * 24 * time.Hour
	// sessionSlideThreshold throttles the last_seen_at write: the idle window
	// only slides forward once a session is more than this stale, so a burst of
	// requests is one read, not a write per request.
	sessionSlideThreshold = time.Hour
)

// ErrSessionInvalid is the single sentinel for every authentication miss the
// caller should turn into a 401: no cookie match, past the absolute cap, or
// past the idle window. It deliberately does not distinguish the cases: the
// client learns only that it must log in again.
var ErrSessionInvalid = errors.New("session invalid")

// SessionManager is the deep module over the session store: it owns token
// generation, the mint/validate/revoke lifecycle, and the two lifetime windows,
// so every login path and the request middleware share one implementation of
// "what makes a session live". Cookie handling stays in the HTTP layer; this
// module never touches an http.Request.
type SessionManager struct {
	repo domain.SessionRepo
	// now is the injectable clock. Every lifetime decision reads it, so tests
	// advance time instead of sleeping.
	now func() time.Time
}

// Option configures a SessionManager at construction.
type Option func(*SessionManager)

// WithClock overrides the wall clock, so tests drive expiry and the idle slide
// deterministically.
func WithClock(clock func() time.Time) Option {
	return func(m *SessionManager) { m.now = clock }
}

func NewSessionManager(repo domain.SessionRepo, opts ...Option) *SessionManager {
	m := &SessionManager{repo: repo, now: time.Now}
	for _, opt := range opts {
		opt(m)
	}
	return m
}

// Mint is the one entry point every login path shares: it generates a fresh
// opaque token, stores its hash with a 90-day absolute cap, and returns the raw
// token (for the cookie) plus its expiry. Login always mints fresh and never
// adopts an inbound cookie, so session fixation is impossible by construction.
func (m *SessionManager) Mint(ctx context.Context, userID int, userAgent, ip *string) (rawToken string, expiresAt time.Time, err error) {
	tok, err := GenerateToken()
	if err != nil {
		return "", time.Time{}, err
	}

	now := m.now()
	expiresAt = now.Add(SessionAbsoluteTTL)

	if err := m.repo.Create(ctx, domain.Session{
		UserID:     userID,
		TokenHash:  tok.Hash,
		ExpiresAt:  expiresAt,
		LastSeenAt: now,
		UserAgent:  userAgent,
		IP:         ip,
		CreatedAt:  now,
	}); err != nil {
		return "", time.Time{}, err
	}

	return tok.Raw, expiresAt, nil
}

// Authenticate turns a raw cookie token into a live actor. It hashes the token,
// looks it up joined to the member's live role, and rejects with
// ErrSessionInvalid on any miss or expiry. On a valid session more than an hour
// stale it slides last_seen_at forward (best effort, a failed slide never
// fails the request). Any non-nil error other than ErrSessionInvalid is an
// infrastructure fault the caller should surface as a 500, not a 401.
func (m *SessionManager) Authenticate(ctx context.Context, rawToken string) (*domain.AuthSession, error) {
	if rawToken == "" {
		return nil, ErrSessionInvalid
	}

	now := m.now()
	as, err := m.repo.FindByTokenHash(ctx, HashToken(rawToken))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrSessionInvalid
	}
	if err != nil {
		return nil, err
	}

	// Absolute cap: valid only strictly before expires_at.
	if !now.Before(as.ExpiresAt) {
		return nil, ErrSessionInvalid
	}
	// Idle window: valid only strictly before last_seen_at + idle TTL.
	if !now.Before(as.LastSeenAt.Add(SessionIdleTTL)) {
		return nil, ErrSessionInvalid
	}

	if now.Sub(as.LastSeenAt) > sessionSlideThreshold {
		if err := m.repo.TouchLastSeen(ctx, as.ID, now); err == nil {
			as.LastSeenAt = now
		}
		// A failed slide is not fatal: the session is still valid, and the next
		// request retries the slide. The caller may log the returned session.
	}

	return as, nil
}

// Revalidate reports whether a session is still live WITHOUT sliding its idle
// window. The SSE stream calls it on every heartbeat to drop a session revoked
// or expired mid-stream; unlike Authenticate it writes no last_seen_at, so a
// long-held stream can't keep an otherwise-idle session alive forever. It reads
// no role and returns only the sentinel, since the caller just needs live/not.
func (m *SessionManager) Revalidate(ctx context.Context, rawToken string) error {
	if rawToken == "" {
		return ErrSessionInvalid
	}

	now := m.now()
	as, err := m.repo.FindByTokenHash(ctx, HashToken(rawToken))
	if errors.Is(err, sql.ErrNoRows) {
		return ErrSessionInvalid
	}
	if err != nil {
		return err
	}

	// Same two windows as Authenticate, no slide.
	if !now.Before(as.ExpiresAt) || !now.Before(as.LastSeenAt.Add(SessionIdleTTL)) {
		return ErrSessionInvalid
	}
	return nil
}

// RevokeCurrent revokes exactly the session carried by rawToken (the
// current-device logout). Revoking a token that no longer exists is a no-op, so
// logout is idempotent.
func (m *SessionManager) RevokeCurrent(ctx context.Context, rawToken string) error {
	if rawToken == "" {
		return nil
	}
	return m.repo.DeleteByTokenHash(ctx, HashToken(rawToken))
}

// RevokeAll revokes every session for a member (logout-everywhere, admin reset,
// invite reset).
func (m *SessionManager) RevokeAll(ctx context.Context, userID int) error {
	_, err := m.repo.DeleteByUserID(ctx, userID)
	return err
}

// RevokeOthers revokes every session for a member except the one carried by
// keepRawToken (password change: close the other devices, keep this one).
func (m *SessionManager) RevokeOthers(ctx context.Context, userID int, keepRawToken string) error {
	_, err := m.repo.DeleteOthersByUserID(ctx, userID, HashToken(keepRawToken))
	return err
}

// Sweep deletes every session past its absolute cap or its idle window. It runs
// hourly and once at startup; lazy rejection in Authenticate keeps expired rows
// harmless between sweeps, so this is pure housekeeping. Returns rows removed.
func (m *SessionManager) Sweep(ctx context.Context) (int64, error) {
	now := m.now()
	return m.repo.DeleteExpired(ctx, now, now.Add(-SessionIdleTTL))
}
