package auth

import (
	"cmp"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"moviepickarr/internal/domain"
)

// InviteTTL is how long a freshly issued claim link stays valid. Fixed policy,
// not operator config (like the session windows): an admin delivers the link
// out-of-band, so a week is enough to hand it over without leaving a long-lived
// credential outstanding. A lost or stale link is replaced, which revokes the
// old generation, so the window never needs to be long.
const InviteTTL = 7 * 24 * time.Hour

// ErrInviteInvalid is the single sentinel every unusable-invite case collapses
// to for the claim page: no such token, expired, or revoked all return it so
// the SPA shows one "no longer valid" screen. It is deliberately distinct from
// ErrInviteUsed, the already-set-up case, which gets its own screen.
var ErrInviteInvalid = domain.ErrInviteInvalid

// ErrInviteUsed marks an invite that was already redeemed, so the claim page
// shows the distinct "already set up, go log in" screen instead of the generic
// no-longer-valid one.
var ErrInviteUsed = domain.ErrInviteUsed

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
// Password is always available. OIDC is offered only for onboarding when a
// provider is configured; the HTTP layer adds that presence-derived option.
type ClaimOptions struct {
	Password bool
	OIDC     bool
}

// The two invite states an admin can still act on, and the only two the
// overview renders. Open is issued, unclaimed and not yet lapsed; Expired is
// lapsed unclaimed. Used and revoked invites have no row: nothing about them is
// actionable, and a revoked one is what Dismiss writes.
const (
	InviteOpen    = "open"
	InviteExpired = "expired"
)

// InviteSummary is one row of the admin invites overview: the stored invite plus
// the open/expired word derived from the manager's clock at read time. The word
// is computed, never stored, the same way the claim path derives validity.
type InviteSummary struct {
	domain.InviteOverview
	Status string
}

// InviteOverviewResult anchors every status word and client-side expiry timer
// to the same whole-second server-clock sample used on the wire and in SQLite.
type InviteOverviewResult struct {
	ServerNow time.Time
	Items     []InviteSummary
}

// ClaimResult reports the atomic credential-and-invite transition. A supplied
// session replaces every prior session; the HTTP layer only sets its cookie.
type ClaimResult struct {
	MemberID int
	// WasReset is true when the claim reset an existing local login, false when
	// it created a placeholder's first credential.
	WasReset bool
}

// InviteManager is the deep module over invite issuance, exact-generation
// actions, claim validation, and atomic credential or member-lifecycle
// transitions. Password and username rules are shared with LocalAuth through
// the package helpers. Session cookie handling stays in the HTTP layer.
type InviteManager struct {
	repo        domain.InviteRepo
	transitions domain.InviteTransitionStore
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

// NewInviteManager builds an InviteManager over the invite store and the scoped
// credential, membership, and invite transition store.
func NewInviteManager(repo domain.InviteRepo, transitions domain.InviteTransitionStore, opts ...InviteOption) *InviteManager {
	m := &InviteManager{repo: repo, transitions: transitions, now: time.Now}
	for _, opt := range opts {
		opt(m)
	}
	return m
}

// Issue creates a member's first current invite generation. If another current
// generation already exists, the repository invariant returns ErrConflict; a
// caller that saw an existing generation must use Replace with its exact handle.
func (m *InviteManager) Issue(ctx context.Context, userID, createdBy int) (string, error) {
	return m.issue(ctx, userID, createdBy, false)
}

// IssuePasswordReset creates an explicitly requested reset generation for a
// member who already holds a local login.
func (m *InviteManager) IssuePasswordReset(ctx context.Context, userID, createdBy int) (string, error) {
	return m.issue(ctx, userID, createdBy, true)
}

// CreateMemberWithInvite commits a new placeholder, its initial next-up
// assignment when needed, and its first claim generation together. The raw
// token is returned only after that transaction commits.
func (m *InviteManager) CreateMemberWithInvite(
	ctx context.Context,
	name string,
	createdBy int,
) (*domain.User, string, error) {
	invite, rawToken, err := m.newMemberInvite(createdBy)
	if err != nil {
		return nil, "", err
	}
	member, err := m.transitions.CreateMemberWithInvite(ctx, name, invite)
	if err != nil {
		return nil, "", err
	}
	return member, rawToken, nil
}

// RestoreMemberWithInvite reopens an archived member and creates the new claim
// generation in the same transaction. It returns the response member projection
// with the response-only raw token, so no fallible read follows the commit.
func (m *InviteManager) RestoreMemberWithInvite(
	ctx context.Context,
	userID, createdBy int,
) (*domain.User, string, error) {
	invite, rawToken, err := m.newMemberInvite(createdBy)
	if err != nil {
		return nil, "", err
	}
	member, err := m.transitions.RestoreMemberWithInvite(ctx, userID, invite)
	if err != nil {
		return nil, "", err
	}
	return member, rawToken, nil
}

func (m *InviteManager) newMemberInvite(createdBy int) (domain.MemberInviteGeneration, string, error) {
	now := m.now()
	tok, err := GenerateToken()
	if err != nil {
		return domain.MemberInviteGeneration{}, "", err
	}
	publicID, err := GeneratePublicID()
	if err != nil {
		return domain.MemberInviteGeneration{}, "", err
	}
	return domain.MemberInviteGeneration{
		PublicID:  publicID,
		TokenHash: tok.Hash,
		ExpiresAt: now.Add(InviteTTL),
		CreatedAt: now,
		CreatedBy: createdBy,
	}, tok.Raw, nil
}

func (m *InviteManager) issue(ctx context.Context, userID, createdBy int, passwordReset bool) (string, error) {
	now := m.now()
	tok, err := GenerateToken()
	if err != nil {
		return "", err
	}
	publicID, err := GeneratePublicID()
	if err != nil {
		return "", err
	}
	if err := m.repo.Create(ctx, userID, publicID, tok.Hash, now.Add(InviteTTL), now, &createdBy, passwordReset); err != nil {
		return "", err
	}
	return tok.Raw, nil
}

// Replace atomically retires the exact current generation the admin saw and
// returns the raw token for its replacement. A stale handle returns ErrConflict
// and cannot mutate the newer generation.
func (m *InviteManager) Replace(ctx context.Context, currentPublicID string, createdBy int) (string, error) {
	now := m.now()
	tok, err := GenerateToken()
	if err != nil {
		return "", err
	}
	publicID, err := GeneratePublicID()
	if err != nil {
		return "", err
	}
	if err := m.repo.ReplaceCurrent(
		ctx,
		currentPublicID,
		publicID,
		tok.Hash,
		now.Add(InviteTTL),
		now,
		&createdBy,
	); err != nil {
		return "", err
	}
	return tok.Raw, nil
}

// Revoke cancels the exact open generation addressed by the admin. Expired,
// spent, revoked, or stale handles return ErrConflict.
func (m *InviteManager) Revoke(ctx context.Context, publicID string) error {
	now := m.now()
	return m.repo.RevokeOpen(ctx, publicID, now, now)
}

// Overview lists every current invite an admin can still act on, including
// explicit password-reset links for credentialed members. Each row is tagged
// open or expired against the same whole-second clock returned to the client.
// Ordering is open before expired, then
// soonest-to-lapse first inside Open and most-recently-lapsed first inside
// Expired, so the row nearest needing attention leads each group.
func (m *InviteManager) Overview(ctx context.Context) (InviteOverviewResult, error) {
	now := m.now().UTC().Truncate(time.Second)
	rows, err := m.repo.ListCurrent(ctx)
	if err != nil {
		return InviteOverviewResult{}, err
	}

	summaries := make([]InviteSummary, 0, len(rows))
	for _, row := range rows {
		// Same predicate as the claim path's validity check (strict now < expiry),
		// so a link that would still redeem always reads as Open here.
		status := InviteExpired
		if now.Before(row.ExpiresAt) {
			status = InviteOpen
		}
		summaries = append(summaries, InviteSummary{InviteOverview: row, Status: status})
	}

	slices.SortFunc(summaries, func(a, b InviteSummary) int {
		if a.Status != b.Status {
			if a.Status == InviteOpen {
				return -1
			}
			return 1
		}
		if a.ExpiresAt.Equal(b.ExpiresAt) {
			return cmp.Compare(a.PublicID, b.PublicID)
		}
		if a.Status == InviteOpen {
			return a.ExpiresAt.Compare(b.ExpiresAt)
		}
		return b.ExpiresAt.Compare(a.ExpiresAt)
	})
	return InviteOverviewResult{ServerNow: now, Items: summaries}, nil
}

// Dismiss retires the exact expired generation addressed by its public handle.
// Open, spent, revoked, and stale generations conflict without changing state.
func (m *InviteManager) Dismiss(ctx context.Context, publicID string) error {
	now := m.now()
	return m.repo.DismissExpired(ctx, publicID, now, now)
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
// (username immutable). Password hashing happens before one writer transaction
// updates the credential, revokes existing sessions, consumes the exact token
// hash, and inserts the supplied replacement session.
func (m *InviteManager) ClaimPassword(
	ctx context.Context,
	rawToken, username, password string,
	sessions ...domain.Session,
) (ClaimResult, error) {
	if rawToken == "" {
		return ClaimResult{}, ErrInviteInvalid
	}
	// Reject a dead link before spending Argon2 work. The transition store repeats
	// this lookup authoritatively on the writer transaction after hashing.
	ic, err := m.lookup(ctx, rawToken)
	if err != nil {
		return ClaimResult{}, err
	}
	if err := validatePassword(password); err != nil {
		return ClaimResult{}, err
	}
	username = strings.TrimSpace(username)
	if !ic.HasLocalLogin {
		if err := validateUsername(username); err != nil {
			return ClaimResult{}, err
		}
	} else if username != "" {
		if err := validateUsername(username); err != nil {
			return ClaimResult{}, err
		}
	}
	hash, err := HashPassword(password)
	if err != nil {
		return ClaimResult{}, err
	}
	claim := domain.PasswordInviteClaim{
		TokenHash:    HashToken(rawToken),
		Username:     username,
		PasswordHash: hash,
	}
	if len(sessions) > 0 {
		claim.Session = &sessions[0]
	}
	res, err := m.transitions.RedeemPasswordInvite(ctx, claim, m.now())
	if err != nil {
		return ClaimResult{}, err
	}
	return ClaimResult{
		MemberID: res.MemberID,
		WasReset: res.WasReset,
	}, nil
}

// ClaimOIDCByHash links the verified identity and consumes the exact invite in
// one writer transaction after the provider round trip.
func (m *InviteManager) ClaimOIDCByHash(
	ctx context.Context,
	tokenHash string,
	claims OIDCClaims,
	sessions ...domain.Session,
) (int, error) {
	if tokenHash == "" {
		return 0, ErrInviteInvalid
	}
	var session *domain.Session
	if len(sessions) > 0 {
		session = &sessions[0]
	}
	res, err := m.transitions.RedeemOIDCInvite(
		ctx,
		tokenHash,
		identityFromClaims(0, claims),
		session,
		m.now(),
	)
	if err != nil {
		return 0, err
	}
	return res.MemberID, nil
}

// SetLocalLogin atomically creates/resets a local login and retires any current
// invite. Admin resets revoke the member's existing sessions in the same commit.
func (m *InviteManager) SetLocalLogin(ctx context.Context, userID int, username, password string) (SetLocalLoginResult, error) {
	res, err := m.setLocalCredential(ctx, userID, username, password, domain.LocalCredentialUpsert, true)
	return SetLocalLoginResult{WasReset: res.WasReset}, err
}

// SetFirstLocalLogin is the self-serve first-credential transition. It keeps the
// current authenticated session and conflicts if a local login already exists.
func (m *InviteManager) SetFirstLocalLogin(ctx context.Context, userID int, username, password string) error {
	_, err := m.setLocalCredential(ctx, userID, username, password, domain.LocalCredentialFirst, false)
	return err
}

// ChangePassword atomically applies a verified password rewrite, revokes the
// old sessions, and retires any current recovery link.
func (m *InviteManager) ChangePassword(ctx context.Context, change domain.VerifiedPasswordChange) error {
	return m.transitions.ChangeVerifiedPassword(ctx, change, m.now())
}

// CompleteLocalLogin records a verified credential success and creates its
// session under the same expected-hash guard.
func (m *InviteManager) CompleteLocalLogin(
	ctx context.Context,
	login domain.VerifiedLocalLogin,
	session domain.Session,
) error {
	return m.transitions.CompleteLocalLogin(ctx, login, session, m.now())
}

// CompleteOIDCLogin refreshes a verified linked identity and creates its
// session in one transaction. found is false if unlink won the writer race.
func (m *InviteManager) CompleteOIDCLogin(
	ctx context.Context,
	claims OIDCClaims,
	session domain.Session,
) (memberID int, found bool, err error) {
	memberID, err = m.transitions.CompleteOIDCLogin(
		ctx,
		identityFromClaims(0, claims),
		session,
		m.now(),
	)
	if errors.Is(err, domain.ErrNotFound) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	return memberID, true, nil
}

// DeleteLocalLogin atomically removes a local credential and retires any
// password-reset generation that could otherwise recreate it.
func (m *InviteManager) DeleteLocalLogin(ctx context.Context, userID, actorID int) error {
	return m.transitions.DeleteLocalCredential(ctx, userID, actorID, m.now())
}

func (m *InviteManager) setLocalCredential(
	ctx context.Context,
	userID int,
	username, password string,
	mode domain.LocalCredentialMode,
	revokeSessionsOnReset bool,
) (domain.LocalCredentialResult, error) {
	if err := validatePassword(password); err != nil {
		return domain.LocalCredentialResult{}, err
	}
	username = strings.TrimSpace(username)
	if username != "" {
		if err := validateUsername(username); err != nil {
			return domain.LocalCredentialResult{}, err
		}
	} else if mode == domain.LocalCredentialFirst {
		return domain.LocalCredentialResult{}, fmt.Errorf("%w: username is required", domain.ErrInvalidInput)
	}
	hash, err := HashPassword(password)
	if err != nil {
		return domain.LocalCredentialResult{}, err
	}
	return m.transitions.SetLocalCredential(ctx, domain.LocalCredentialChange{
		UserID:                userID,
		Username:              username,
		PasswordHash:          hash,
		Mode:                  mode,
		RevokeSessionsOnReset: revokeSessionsOnReset,
	}, m.now())
}

// LinkOIDC binds verified claims and retires any current invite only while the
// session that authorized the callback is still live in the writer snapshot.
func (m *InviteManager) LinkOIDC(ctx context.Context, userID int, claims OIDCClaims, sessionTokenHash string) error {
	now := m.now()
	return m.transitions.LinkOIDCAndRetireInvite(
		ctx,
		identityFromClaims(userID, claims),
		sessionTokenHash,
		now,
		now.Add(-SessionIdleTTL),
	)
}

// UnlinkOIDC removes a linked identity and retires any current invite under the
// same last-credential guard.
func (m *InviteManager) UnlinkOIDC(ctx context.Context, userID, actorID int) error {
	return m.transitions.DeleteOIDCIdentity(ctx, userID, actorID, m.now())
}

func identityFromClaims(userID int, claims OIDCClaims) domain.OIDCIdentity {
	return domain.OIDCIdentity{
		UserID:            userID,
		Issuer:            claims.Issuer,
		Subject:           claims.Subject,
		Email:             claims.Email,
		PreferredUsername: claims.PreferredUsername,
	}
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
	return m.lookupByHashAt(ctx, tokenHash, m.now())
}

func (m *InviteManager) lookupByHashAt(ctx context.Context, tokenHash string, now time.Time) (*domain.InviteContext, error) {
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
	if ic.RevokedAt != nil || !now.Before(ic.ExpiresAt) {
		return nil, ErrInviteInvalid
	}
	return ic, nil
}
