package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"moviepickarr/internal/domain"
)

// Session lifetime, fixed policy. A session is valid only inside both windows.
const (
	// SessionAbsoluteTTL caps a session's total life regardless of activity.
	SessionAbsoluteTTL = 90 * 24 * time.Hour
	// SessionIdleTTL logs a session out after this long without a request.
	SessionIdleTTL = 30 * 24 * time.Hour
	// sessionSlideThreshold throttles the last_seen_at write to one per hour.
	sessionSlideThreshold = time.Hour
)

// ErrSessionInvalid is the one 401 sentinel. It deliberately does not say
// which check failed.
var ErrSessionInvalid = domain.ErrSessionInvalid

// SessionManager owns session tokens, their lifecycle, and the two lifetime
// windows. It never touches an http.Request.
type SessionManager struct {
	repo domain.SessionRepo
	// now is injectable so tests advance time instead of sleeping.
	now func() time.Time
}

// Option configures a SessionManager at construction.
type Option func(*SessionManager)

// WithClock overrides the wall clock for tests.
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

// Mint stores a new session and returns the raw token for the cookie.
func (m *SessionManager) Mint(ctx context.Context, userID int, userAgent *string) (rawToken string, expiresAt time.Time, err error) {
	rawToken, session, err := m.PrepareMint(userID, userAgent)
	if err != nil {
		return "", time.Time{}, err
	}
	if err := m.repo.Create(ctx, session); err != nil {
		return "", time.Time{}, err
	}
	return rawToken, session.ExpiresAt, nil
}

// PrepareMint builds a session row without writing it, so the local-login path
// can insert it in the credential-CAS transaction.
func (m *SessionManager) PrepareMint(userID int, userAgent *string) (rawToken string, session domain.Session, err error) {
	tok, err := GenerateToken()
	if err != nil {
		return "", domain.Session{}, err
	}
	publicID, err := GeneratePublicID()
	if err != nil {
		return "", domain.Session{}, err
	}

	now := m.now()
	session = domain.Session{
		PublicID:   publicID,
		UserID:     userID,
		TokenHash:  tok.Hash,
		ExpiresAt:  now.Add(SessionAbsoluteTTL),
		LastSeenAt: now,
		UserAgent:  userAgent,
		CreatedAt:  now,
	}
	return tok.Raw, session, nil
}

// Authenticate turns a raw cookie token into a live actor and slides the idle
// window. Any error other than ErrSessionInvalid is a 500, not a 401.
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

	if !now.Before(as.ExpiresAt) {
		return nil, ErrSessionInvalid
	}
	if !now.Before(as.LastSeenAt.Add(SessionIdleTTL)) {
		return nil, ErrSessionInvalid
	}

	if now.Sub(as.LastSeenAt) > sessionSlideThreshold {
		if err := m.repo.TouchLastSeen(ctx, as.ID, now); err == nil {
			as.LastSeenAt = now
		}
		// A failed slide is not fatal; the next request retries it.
	}

	return as, nil
}

// Revalidate reports whether a session is still live without sliding its idle
// window, so a long-held SSE stream cannot keep an idle session alive.
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

// RevokeCurrent revokes the session carried by rawToken. It is idempotent.
func (m *SessionManager) RevokeCurrent(ctx context.Context, rawToken string) error {
	if rawToken == "" {
		return nil
	}
	return m.repo.DeleteByTokenHash(ctx, HashToken(rawToken))
}

// RevokeAll revokes every session for a member.
func (m *SessionManager) RevokeAll(ctx context.Context, userID int) error {
	_, err := m.repo.DeleteByUserID(ctx, userID)
	return err
}

// RevokeOthers revokes every session for a member except keepRawToken's.
func (m *SessionManager) RevokeOthers(ctx context.Context, userID int, keepRawToken string) error {
	_, err := m.repo.DeleteOthersByUserID(ctx, userID, HashToken(keepRawToken))
	return err
}

// SessionView is one live session as its owner sees it. Current marks the
// requesting device.
type SessionView struct {
	domain.Session
	Current bool
}

// List returns the member's live sessions, most recently active first, against
// the same two windows Authenticate enforces.
func (m *SessionManager) List(ctx context.Context, userID int, currentRawToken string) ([]SessionView, error) {
	now := m.now()
	rows, err := m.repo.ListLiveByUserID(ctx, userID, now, now.Add(-SessionIdleTTL))
	if err != nil {
		return nil, err
	}

	currentHash := ""
	if currentRawToken != "" {
		currentHash = HashToken(currentRawToken)
	}

	views := make([]SessionView, 0, len(rows))
	for _, s := range rows {
		views = append(views, SessionView{Session: s, Current: currentHash != "" && s.TokenHash == currentHash})
	}
	return views, nil
}

// RevokeByPublicID revokes one of the member's own sessions and reports whether
// it was the current one. The member id is in the delete predicate, so another
// member's session cannot match (ErrNotFound).
func (m *SessionManager) RevokeByPublicID(ctx context.Context, userID int, publicID string, currentRawToken string) (wasCurrent bool, err error) {
	deletedHash, err := m.repo.DeleteByPublicIDForUser(ctx, publicID, userID)
	if err != nil {
		return false, err
	}
	if deletedHash == "" {
		return false, fmt.Errorf("%w: session %s", domain.ErrNotFound, publicID)
	}
	return currentRawToken != "" && deletedHash == HashToken(currentRawToken), nil
}

// Sweep deletes expired sessions and returns the count. Housekeeping only:
// Authenticate already rejects expired rows.
func (m *SessionManager) Sweep(ctx context.Context) (int64, error) {
	now := m.now()
	return m.repo.DeleteExpired(ctx, now, now.Add(-SessionIdleTTL))
}
