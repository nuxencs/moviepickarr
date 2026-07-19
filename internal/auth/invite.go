package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"moviepickarr/internal/domain"
)

// InviteTTL is how long a freshly issued claim link stays valid. Fixed policy,
// not operator config (like the session windows): an admin delivers the link
// out-of-band, so a week is enough to hand it over without leaving a long-lived
// credential outstanding. A lost or stale link is regenerated, which revokes the
// old one, so the window never needs to be long.
const InviteTTL = 7 * 24 * time.Hour

// ErrInviteInvalid is the single sentinel every unusable-invite case collapses
// to for the claim page: no such token, expired, or revoked all return it so
// the SPA shows one "no longer valid" screen. It is deliberately distinct from
// ErrInviteUsed, the already-set-up case, which gets its own screen.
var ErrInviteInvalid = errors.New("invite invalid")

// ErrInviteUsed marks an invite that was already redeemed, so the claim page
// shows the distinct "already set up, go log in" screen instead of the generic
// no-longer-valid one.
var ErrInviteUsed = errors.New("invite already used")

// ClaimContext is what the claim page needs to render: who the invite greets,
// whether it is a fresh setup (placeholder) or a reset (the member already holds
// a local login), and which credential options to offer.
type ClaimContext struct {
	DisplayName string
	// IsReset is true when the target already has a local login: the claim resets
	// the password (username immutable) rather than creating a first credential.
	IsReset bool
	Options ClaimOptions
}

// ClaimOptions reports which credential paths the claim page should present.
// Password is always available; OIDC is offered only when a provider is
// configured, which the OIDC ticket wires up; it stays false until then.
type ClaimOptions struct {
	Password bool
	OIDC     bool
}

// ClaimResult reports the outcome of a password claim so the HTTP layer can
// apply the reset-only session revocation, mint a fresh session, and finally
// consume the invite (Consume) as the last step.
type ClaimResult struct {
	MemberID int
	// WasReset is true when the claim reset an existing local login (revoke every
	// existing session), false when it created a placeholder's first credential.
	WasReset bool
	// InviteID is the invite to consume once the session work has succeeded, so
	// consumption is the last effect and a mid-flow failure leaves the link usable.
	InviteID int64
}

// InviteManager is the deep module over the invite/claim flow: issuance,
// regenerate, revoke, claim validation, and password-claim consumption, all off
// one injectable clock. It reuses LocalAuth for the credential upsert so the
// claim path shares the same username/password rules as every other login path.
// Session minting and cookie handling stay in the HTTP layer; this module never
// touches an http.Request or a session.
type InviteManager struct {
	repo  domain.InviteRepo
	local *LocalAuth
	// now is the injectable clock every expiry decision reads, so tests advance
	// time instead of sleeping.
	now func() time.Time
}

// InviteOption configures an InviteManager at construction.
type InviteOption func(*InviteManager)

// WithInviteClock overrides the wall clock so tests drive invite expiry
// deterministically.
func WithInviteClock(clock func() time.Time) InviteOption {
	return func(m *InviteManager) { m.now = clock }
}

// NewInviteManager builds an InviteManager over the invite store and the shared
// LocalAuth (whose SetLocalLogin the password claim reuses).
func NewInviteManager(repo domain.InviteRepo, local *LocalAuth, opts ...InviteOption) *InviteManager {
	m := &InviteManager{repo: repo, local: local, now: time.Now}
	for _, opt := range opts {
		opt(m)
	}
	return m
}

// Issue mints a fresh claim link for a member, enforcing at-most-one-valid: any
// current valid invite is revoked first, then a new row is inserted. It returns
// the raw token for the one-time claim URL (never stored). Serves both the first
// invite (POST /members) and re-issue/regenerate (POST /members/{id}/invite). A
// missing member surfaces as ErrNotFound from the insert's foreign key.
func (m *InviteManager) Issue(ctx context.Context, userID, createdBy int) (string, error) {
	now := m.now()
	if _, err := m.repo.RevokeValidByUserID(ctx, userID, now, now); err != nil {
		return "", err
	}
	tok, err := GenerateToken()
	if err != nil {
		return "", err
	}
	if err := m.repo.Create(ctx, userID, tok.Hash, now.Add(InviteTTL), &createdBy); err != nil {
		return "", err
	}
	return tok.Raw, nil
}

// Revoke cancels a member's current valid invite. It returns a wrapped
// ErrNotFound (→404) when there is nothing valid to revoke, so revoking an
// already-dead invite is an honest miss rather than a silent success.
func (m *InviteManager) Revoke(ctx context.Context, userID int) error {
	now := m.now()
	n, err := m.repo.RevokeValidByUserID(ctx, userID, now, now)
	if err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("%w: no valid invite for member %d", domain.ErrNotFound, userID)
	}
	return nil
}

// Validate resolves a raw claim token into the claim-page context, or one of the
// two distinct failure sentinels: ErrInviteUsed for an already-redeemed invite
// (the "already set up" screen) and ErrInviteInvalid for everything else (no
// such token, expired, revoked: the single "no longer valid" screen).
func (m *InviteManager) Validate(ctx context.Context, rawToken string) (*ClaimContext, error) {
	ic, err := m.lookup(ctx, rawToken)
	if err != nil {
		return nil, err
	}
	return &ClaimContext{
		DisplayName: ic.DisplayName,
		IsReset:     ic.HasLocalLogin,
		Options:     ClaimOptions{Password: true},
	}, nil
}

// ClaimPassword establishes the member's local login from a valid invite:
// placeholder → username + password (first login), reset → password only
// (username immutable). It returns the member id, whether this was a reset, and
// the invite id to consume, but does NOT consume the invite itself: the HTTP
// layer revokes the member's existing sessions (reset), mints a fresh session,
// and only then calls Consume, so consumption is the last effect. A rejected
// password (too short, username taken) or a failure before Consume leaves the
// link still usable.
func (m *InviteManager) ClaimPassword(ctx context.Context, rawToken, username, password string) (ClaimResult, error) {
	ic, err := m.lookup(ctx, rawToken)
	if err != nil {
		return ClaimResult{}, err
	}
	// SetLocalLogin is the shared upsert: with no existing row it creates the
	// placeholder's first login (username required); with a row it is a reset
	// (password only, username immutable). Reusing it keeps the claim path on the
	// same username/password rules as the admin and self-serve paths.
	res, err := m.local.SetLocalLogin(ctx, ic.UserID, username, password)
	if err != nil {
		return ClaimResult{}, err
	}
	return ClaimResult{MemberID: ic.UserID, WasReset: res.WasReset, InviteID: ic.ID}, nil
}

// Consume stamps used_at on the invite: the final step of a password claim, run
// by the HTTP layer only after the session revoke + mint have succeeded, so the
// invite is spent last (spec order: revoke all, then consume). Turns the invite
// into the distinct "already set up" state.
func (m *InviteManager) Consume(ctx context.Context, inviteID int64) error {
	return m.repo.MarkUsed(ctx, inviteID, m.now())
}

// ResolveClaimByHash resolves a stored token hash to its live invite context,
// applying the same validity state machine as Validate. The OIDC claim callback
// uses it: initiation stashed the token HASH (not the raw token) in the AEAD tx
// cookie, so the callback re-checks the invite is still claimable after the
// provider round trip and learns which member and invite to link/consume.
func (m *InviteManager) ResolveClaimByHash(ctx context.Context, tokenHash string) (*domain.InviteContext, error) {
	if tokenHash == "" {
		return nil, ErrInviteInvalid
	}
	return m.lookupByHash(ctx, tokenHash)
}

// lookup resolves a raw token to its live invite context or a failure sentinel,
// applying the validity state machine in one place. The already-used check comes
// first so a redeemed invite always reads as "already set up", never as merely
// expired or revoked.
func (m *InviteManager) lookup(ctx context.Context, rawToken string) (*domain.InviteContext, error) {
	if rawToken == "" {
		return nil, ErrInviteInvalid
	}
	return m.lookupByHash(ctx, HashToken(rawToken))
}

// lookupByHash is the shared post-hash resolution: it reads the invite context
// by token hash and runs the validity checks. The already-used check comes first
// so a redeemed invite always reads as "already set up", never as merely expired
// or revoked.
func (m *InviteManager) lookupByHash(ctx context.Context, tokenHash string) (*domain.InviteContext, error) {
	ic, err := m.repo.FindContextByTokenHash(ctx, tokenHash)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrInviteInvalid
	}
	if err != nil {
		return nil, err
	}
	if ic.UsedAt != nil {
		return nil, ErrInviteUsed
	}
	if ic.RevokedAt != nil || !m.now().Before(ic.ExpiresAt) {
		return nil, ErrInviteInvalid
	}
	return ic, nil
}
