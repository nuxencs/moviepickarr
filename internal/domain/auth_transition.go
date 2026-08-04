package domain

import (
	"context"
	"time"
)

// LocalCredentialMode names the two local-login writes that must retire a
// current invite in the same transaction.
type LocalCredentialMode uint8

const (
	LocalCredentialFirst LocalCredentialMode = iota
	LocalCredentialUpsert
)

type LocalCredentialChange struct {
	UserID                int
	Username              string
	PasswordHash          string
	Mode                  LocalCredentialMode
	RevokeSessionsOnReset bool
}

type LocalCredentialResult struct {
	WasReset bool
}

// VerifiedPasswordChange carries a password rewrite whose current password was
// already verified outside SQLite. ExpectedPasswordHash makes the write a CAS,
// so another reset between verification and commit cannot be overwritten.
type VerifiedPasswordChange struct {
	UserID               int
	ExpectedPasswordHash string
	PasswordHash         string
	Session              *Session
}

// VerifiedLocalLogin carries a successful password verification into the
// transaction that records login success and creates its session. The expected
// hash prevents an old-password login from completing after recovery.
type VerifiedLocalLogin struct {
	UserID               int
	ExpectedPasswordHash string
	NewPasswordHash      *string
}

type PasswordInviteClaim struct {
	TokenHash    string
	Username     string
	PasswordHash string
	Session      *Session
}

type InviteClaimResult struct {
	MemberID    int
	WasReset    bool
	WasRecovery bool
}

// AuthTransitionStore owns the narrow cross-table transactions where a
// credential and an invite generation must change together. Password hashing
// and provider exchange happen before these calls, outside SQLite's writer.
type AuthTransitionStore interface {
	RedeemPasswordInvite(ctx context.Context, claim PasswordInviteClaim, now time.Time) (InviteClaimResult, error)
	SetLocalCredential(ctx context.Context, change LocalCredentialChange, now time.Time) (LocalCredentialResult, error)
	ChangeVerifiedPassword(ctx context.Context, change VerifiedPasswordChange, now time.Time) error
	CompleteLocalLogin(ctx context.Context, login VerifiedLocalLogin, session Session, now time.Time) error
	DeleteLocalCredential(ctx context.Context, userID, actorID int, now time.Time) error
	RedeemOIDCInvite(ctx context.Context, tokenHash string, identity OIDCIdentity, session *Session, now time.Time) (InviteClaimResult, error)
	CompleteOIDCLogin(ctx context.Context, identity OIDCIdentity, session Session, now time.Time) (int, error)
	LinkOIDCAndRetireInvite(ctx context.Context, identity OIDCIdentity, sessionTokenHash string, now, idleCutoff time.Time) error
	DeleteOIDCIdentity(ctx context.Context, userID, actorID int, now time.Time) error
}
