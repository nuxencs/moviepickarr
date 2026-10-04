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

// InviteTTL is how long a claim link stays valid. Fixed policy, not config: a
// lost link is replaced, so the window never needs to be long.
const InviteTTL = 7 * 24 * time.Hour

// ErrInviteInvalid covers an unknown, expired, or revoked invite: one "no longer
// valid" screen. ErrInviteUsed gets its own screen.
var ErrInviteInvalid = domain.ErrInviteInvalid

// ErrInviteUsed marks an already redeemed invite ("already set up, go log in").
var ErrInviteUsed = domain.ErrInviteUsed

// ClaimContext is what the claim page needs to render.
type ClaimContext struct {
	DisplayName string
	// IsReset means the target already has a local login, so only the password changes.
	IsReset bool
	Options ClaimOptions
}

// ClaimOptions reports which credential paths the claim page offers. The HTTP
// layer sets OIDC when a provider is configured.
type ClaimOptions struct {
	Password bool
	OIDC     bool
}

// The two invite states an admin can still act on. Used and revoked invites
// are not actionable, so the overview has no row for them.
const (
	InviteOpen    = "open"
	InviteExpired = "expired"
)

// InviteSummary is one row of the admin invites overview. Status is derived at
// read time, never stored.
type InviteSummary struct {
	domain.InviteOverview
	Status string
}

// InviteOverviewResult anchors every status and client expiry timer to one
// whole-second server clock sample.
type InviteOverviewResult struct {
	ServerNow time.Time
	Items     []InviteSummary
}

// ClaimResult reports the atomic credential-and-invite transition.
type ClaimResult struct {
	MemberID int
	// WasReset is false when the claim created a placeholder's first credential.
	WasReset bool
}

// InviteManager owns invite issuance, claims, and the atomic credential and
// member-lifecycle transitions. Cookies stay in the HTTP layer.
type InviteManager struct {
	repo        domain.InviteRepo
	transitions domain.InviteTransitionStore
	// now is injectable so tests advance time instead of sleeping.
	now func() time.Time
}

// InviteOption configures an InviteManager at construction.
type InviteOption func(*InviteManager)

// WithInviteClock overrides the wall clock for tests.
func WithInviteClock(clock func() time.Time) InviteOption {
	return func(m *InviteManager) { m.now = clock }
}

// NewInviteManager builds an InviteManager.
func NewInviteManager(repo domain.InviteRepo, transitions domain.InviteTransitionStore, opts ...InviteOption) *InviteManager {
	m := &InviteManager{repo: repo, transitions: transitions, now: time.Now}
	for _, opt := range opts {
		opt(m)
	}
	return m
}

// Issue creates a member's first current invite generation. If one already
// exists it returns ErrConflict; use Replace instead.
func (m *InviteManager) Issue(ctx context.Context, userID, createdBy int) (string, error) {
	return m.issue(ctx, userID, createdBy, false)
}

// IssuePasswordReset creates a reset generation for a member with a local login.
func (m *InviteManager) IssuePasswordReset(ctx context.Context, userID, createdBy int) (string, error) {
	return m.issue(ctx, userID, createdBy, true)
}

// CreateMemberWithInvite commits a new placeholder, its next-up assignment when
// needed, and its first claim generation in one transaction.
func (m *InviteManager) CreateMemberWithInvite(
	ctx context.Context,
	name string,
	role domain.Role,
	createdBy int,
) (*domain.User, string, error) {
	if !role.Valid() {
		return nil, "", fmt.Errorf("%w: invalid member role", domain.ErrInvalidInput)
	}
	invite, rawToken, err := m.newMemberInvite(createdBy)
	if err != nil {
		return nil, "", err
	}
	member, err := m.transitions.CreateMemberWithInvite(ctx, name, role, invite)
	if err != nil {
		return nil, "", err
	}
	return member, rawToken, nil
}

// RestoreMemberWithInvite reopens an archived member and creates a new claim
// generation in one transaction. No fallible read follows the commit.
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

// Replace atomically retires the exact generation the admin saw and returns the
// replacement's raw token. A stale handle returns ErrConflict.
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

// Revoke cancels the exact open generation. Any other handle returns ErrConflict.
func (m *InviteManager) Revoke(ctx context.Context, publicID string) error {
	now := m.now()
	return m.repo.RevokeOpen(ctx, publicID, now, now)
}

// Overview lists every actionable invite, open before expired. Inside each
// group the row nearest needing attention leads.
func (m *InviteManager) Overview(ctx context.Context) (InviteOverviewResult, error) {
	now := m.now().UTC().Truncate(time.Second)
	rows, err := m.repo.ListCurrent(ctx)
	if err != nil {
		return InviteOverviewResult{}, err
	}

	summaries := make([]InviteSummary, 0, len(rows))
	for _, row := range rows {
		// Same strict now < expiry predicate as the claim path.
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

// Dismiss retires the exact expired generation. Any other handle returns
// ErrConflict.
func (m *InviteManager) Dismiss(ctx context.Context, publicID string) error {
	now := m.now()
	return m.repo.DismissExpired(ctx, publicID, now, now)
}

// Validate resolves a raw claim token into the claim-page context, or
// ErrInviteUsed or ErrInviteInvalid.
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

// ClaimPassword sets the member's local login from a valid invite. A reset
// changes only the password. The credential, session revocation, token use, and
// new session commit in one writer transaction.
func (m *InviteManager) ClaimPassword(
	ctx context.Context,
	rawToken, username, password string,
	sessions ...domain.Session,
) (ClaimResult, error) {
	if rawToken == "" {
		return ClaimResult{}, ErrInviteInvalid
	}
	// Reject a dead link before Argon2 work; the transaction re-checks it.
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

// SetLocalLogin atomically creates or resets a local login, retires any current
// invite, and revokes the member's sessions on a reset.
func (m *InviteManager) SetLocalLogin(ctx context.Context, userID int, username, password string) (SetLocalLoginResult, error) {
	res, err := m.setLocalCredential(ctx, userID, username, password, domain.LocalCredentialUpsert, true)
	return SetLocalLoginResult{WasReset: res.WasReset}, err
}

// SetFirstLocalLogin is the self-serve first credential. It keeps the current
// session and conflicts if a local login already exists.
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

// CompleteOIDCLogin refreshes a linked identity and creates its session in one
// transaction. found is false if unlink won the writer race.
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

// lookup resolves a raw token to its live invite context or a failure sentinel.
func (m *InviteManager) lookup(ctx context.Context, rawToken string) (*domain.InviteContext, error) {
	if rawToken == "" {
		return nil, ErrInviteInvalid
	}
	return m.lookupByHash(ctx, HashToken(rawToken))
}

func (m *InviteManager) lookupByHash(ctx context.Context, tokenHash string) (*domain.InviteContext, error) {
	return m.lookupByHashAt(ctx, tokenHash, m.now())
}

// lookupByHashAt checks used first, so a redeemed invite never reads as expired.
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
