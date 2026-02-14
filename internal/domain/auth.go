package domain

import (
	"context"
	"time"
)

type UserRole string

const (
	RoleMember UserRole = "member"
	RoleAdmin  UserRole = "admin"
)

type LocalAccount struct {
	UserID         int
	Username       string
	PasswordHash   string
	Role           UserRole
	FailedAttempts int
	LockedUntil    *time.Time
	LastLoginAt    *time.Time
	CreatedAt      *time.Time
	UpdatedAt      *time.Time
}

type Session struct {
	ID         string
	UserID     int
	TokenHash  string
	ExpiresAt  time.Time
	LastSeenAt *time.Time
	UserAgent  string
	IP         string
	CreatedAt  *time.Time
}

type AuthPrincipal struct {
	UserID   int
	Name     string
	Username string
	Role     UserRole
}

func (p AuthPrincipal) IsAdmin() bool {
	return p.Role == RoleAdmin
}

type AuthRepo interface {
	CountAccounts(ctx context.Context) (int, error)
	FindAccountByUsername(ctx context.Context, username string) (*LocalAccount, *User, error)
	FindAccountByUserID(ctx context.Context, userID int) (*LocalAccount, error)
	UpsertAccount(ctx context.Context, userID int, username, passwordHash string, role UserRole) (*LocalAccount, error)
	UpdateLoginState(ctx context.Context, userID int, failedAttempts int, lockedUntil *time.Time, lastLoginAt *time.Time) error
	CreateSession(ctx context.Context, session *Session) error
	FindSessionByTokenHash(ctx context.Context, tokenHash string) (*Session, *LocalAccount, *User, error)
	DeleteSessionByTokenHash(ctx context.Context, tokenHash string) error
	DeleteExpiredSessions(ctx context.Context, now time.Time) error
}
