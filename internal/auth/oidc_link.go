package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"moviepickarr/internal/domain"
)

// IdentityLinker is the deep module over the linked-identity data operations:
// login match, the link/claim collision matrix on the two UNIQUEs, and the
// unlink last-credential guard. It reads local-login presence (for the guard)
// but never writes local accounts or sessions; session minting and cookie
// handling stay in the HTTP layer, and it never touches an http.Request or a
// go-oidc type (it takes already-verified OIDCClaims).
type IdentityLinker struct {
	identities domain.OIDCIdentityRepo
	local      domain.LocalAccountRepo
	// now is the injectable clock every snapshot/last-login write reads, so tests
	// advance time instead of sleeping.
	now func() time.Time
}

// LinkerOption configures an IdentityLinker at construction.
type LinkerOption func(*IdentityLinker)

// WithLinkerClock overrides the wall clock so tests drive last-login timestamps
// deterministically.
func WithLinkerClock(clock func() time.Time) LinkerOption {
	return func(l *IdentityLinker) { l.now = clock }
}

// NewIdentityLinker builds an IdentityLinker over the identity store and the
// local-account store (read only, for the self-last-credential guard).
func NewIdentityLinker(identities domain.OIDCIdentityRepo, local domain.LocalAccountRepo, opts ...LinkerOption) *IdentityLinker {
	l := &IdentityLinker{identities: identities, local: local, now: time.Now}
	for _, opt := range opts {
		opt(l)
	}
	return l
}

// Login resolves verified claims to a linked member. found is false when no
// member has linked this (issuer, subject): the caller rejects ephemerally
// (oidc_unlinked) and persists nothing. On a match it refreshes the
// informational snapshots and bumps last_login_at, then returns the member id.
func (l *IdentityLinker) Login(ctx context.Context, claims OIDCClaims) (memberID int, found bool, err error) {
	oi, err := l.identities.FindByIssuerSubject(ctx, claims.Issuer, claims.Subject)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	now := l.now()
	if err := l.identities.TouchLogin(ctx, oi.ID, claims.Email, claims.PreferredUsername, now, now); err != nil {
		return 0, false, err
	}
	return oi.UserID, true, nil
}

// Link binds a member to the claims' identity, applying the collision matrix on
// the (issuer, subject) and user_id UNIQUEs:
//
//   - the (issuer, subject) is already linked to THIS member → idempotent
//     success (snapshots refreshed, nothing else changes);
//   - the (issuer, subject) is linked to ANOTHER member → ErrConflict, nothing
//     written;
//   - this member already holds a DIFFERENT identity → ErrConflict, nothing
//     written;
//   - neither present → a fresh row is inserted.
//
// Both link and claim route through here, so the two intents share one matrix.
// The lookups pre-resolve the outcome; the repo's UNIQUE-violation mapping is a
// backstop for a concurrent insert racing between the check and the write.
func (l *IdentityLinker) Link(ctx context.Context, memberID int, claims OIDCClaims) error {
	now := l.now()

	existing, err := l.identities.FindByIssuerSubject(ctx, claims.Issuer, claims.Subject)
	switch {
	case err == nil:
		if existing.UserID == memberID {
			// Same member re-linking the same identity: idempotent, refresh snapshots.
			return l.identities.TouchLogin(ctx, existing.ID, claims.Email, claims.PreferredUsername, now, now)
		}
		return fmt.Errorf("%w: identity already linked to another member", domain.ErrConflict)
	case errors.Is(err, sql.ErrNoRows):
		// No existing owner of this (issuer, subject): fall through.
	default:
		return err
	}

	switch _, err := l.identities.FindByUserID(ctx, memberID); {
	case err == nil:
		return fmt.Errorf("%w: member already has a linked identity", domain.ErrConflict)
	case errors.Is(err, sql.ErrNoRows):
		// Member holds no identity yet: fall through to insert.
	default:
		return err
	}

	return l.identities.Insert(ctx, domain.OIDCIdentity{
		UserID:            memberID,
		Issuer:            claims.Issuer,
		Subject:           claims.Subject,
		Email:             claims.Email,
		PreferredUsername: claims.PreferredUsername,
		LastLoginAt:       &now,
	}, now)
}

// Unlink removes a member's linked identity. The self-last-credential guard
// forbids a member from unlinking their only remaining credential (an identity
// with no local login), which would lock them out: that returns ErrConflict
// (→409). An admin removing another member's identity is allowed even when it is
// that member's last credential (they fall back to a placeholder). A member with
// no linked identity to remove returns ErrNotFound.
func (l *IdentityLinker) Unlink(ctx context.Context, targetID, actorID int) error {
	if _, err := l.identities.FindByUserID(ctx, targetID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("%w: member %d has no linked identity", domain.ErrNotFound, targetID)
		}
		return err
	}

	if targetID == actorID {
		switch _, err := l.local.FindByUserID(ctx, targetID); {
		case errors.Is(err, sql.ErrNoRows):
			return fmt.Errorf("%w: cannot remove your own last credential", domain.ErrConflict)
		case err != nil:
			return err
		}
	}

	_, err := l.identities.DeleteByUserID(ctx, targetID)
	return err
}
