package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"moviepickarr/internal/domain"

	"github.com/google/uuid"
)

const (
	minUsernameLength = 3
	minPasswordLength = 10
)

type Config struct {
	SessionTTL        time.Duration
	MaxFailedAttempts int
	LockoutDuration   time.Duration
}

type LoginMetadata struct {
	UserAgent string
	IP        string
}

type LoginResult struct {
	Principal domain.AuthPrincipal
	Token     string
	ExpiresAt time.Time
}

type Service interface {
	Login(ctx context.Context, username, password string, metadata LoginMetadata) (*LoginResult, error)
	Authenticate(ctx context.Context, rawToken string) (*domain.AuthPrincipal, error)
	Logout(ctx context.Context, rawToken string) error
	HasAnyAccount(ctx context.Context) (bool, error)
	CanDeleteAccount(ctx context.Context, userID int) error
	UpsertAccount(ctx context.Context, userID int, username, password string, role domain.UserRole) (*domain.LocalAccount, error)
}

type service struct {
	repo domain.AuthRepo
	cfg  Config
}

func NewService(repo domain.AuthRepo, cfg Config) Service {
	if cfg.SessionTTL <= 0 {
		cfg.SessionTTL = 7 * 24 * time.Hour
	}
	if cfg.MaxFailedAttempts <= 0 {
		cfg.MaxFailedAttempts = 5
	}
	if cfg.LockoutDuration <= 0 {
		cfg.LockoutDuration = 15 * time.Minute
	}

	return &service{repo: repo, cfg: cfg}
}

func (s *service) Login(ctx context.Context, username, password string, metadata LoginMetadata) (*LoginResult, error) {
	normalized, err := normalizeUsername(username)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(password) == "" {
		return nil, fmt.Errorf("%w: password is required", domain.ErrInvalidInput)
	}

	account, user, err := s.repo.FindAccountByUsername(ctx, normalized)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return nil, domain.ErrUnauthorized
		}
		return nil, err
	}

	now := time.Now().UTC()
	if account.LockedUntil != nil && account.LockedUntil.After(now) {
		return nil, domain.ErrUnauthorized
	}

	ok, err := verifyPassword(password, account.PasswordHash)
	if err != nil {
		return nil, domain.ErrUnauthorized
	}
	if !ok {
		failedAttempts := account.FailedAttempts + 1
		var lockedUntil *time.Time
		if failedAttempts >= s.cfg.MaxFailedAttempts {
			lock := now.Add(s.cfg.LockoutDuration)
			lockedUntil = &lock
			failedAttempts = 0
		}
		if updateErr := s.repo.UpdateLoginState(ctx, account.UserID, failedAttempts, lockedUntil, account.LastLoginAt); updateErr != nil {
			return nil, updateErr
		}
		return nil, domain.ErrUnauthorized
	}

	if err := s.repo.UpdateLoginState(ctx, account.UserID, 0, nil, &now); err != nil {
		return nil, err
	}

	token, tokenHash, err := generateSessionToken()
	if err != nil {
		return nil, err
	}

	expiresAt := now.Add(s.cfg.SessionTTL)
	if err := s.repo.CreateSession(ctx, &domain.Session{
		ID:        uuid.NewString(),
		UserID:    account.UserID,
		TokenHash: tokenHash,
		ExpiresAt: expiresAt,
		UserAgent: metadata.UserAgent,
		IP:        metadata.IP,
	}); err != nil {
		return nil, err
	}

	_ = s.repo.DeleteExpiredSessions(ctx, now)

	principal := domain.AuthPrincipal{
		UserID:   user.ID,
		Name:     user.Name,
		Username: account.Username,
		Role:     account.Role,
	}

	return &LoginResult{
		Principal: principal,
		Token:     token,
		ExpiresAt: expiresAt,
	}, nil
}

func (s *service) Authenticate(ctx context.Context, rawToken string) (*domain.AuthPrincipal, error) {
	token := strings.TrimSpace(rawToken)
	if token == "" {
		return nil, domain.ErrUnauthenticated
	}

	tokenHash := hashSessionToken(token)
	session, account, user, err := s.repo.FindSessionByTokenHash(ctx, tokenHash)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return nil, domain.ErrUnauthenticated
		}
		return nil, err
	}

	now := time.Now().UTC()
	if session.ExpiresAt.Before(now) || session.ExpiresAt.Equal(now) {
		_ = s.repo.DeleteSessionByTokenHash(ctx, tokenHash)
		return nil, domain.ErrUnauthenticated
	}

	principal := &domain.AuthPrincipal{
		UserID:   user.ID,
		Name:     user.Name,
		Username: account.Username,
		Role:     account.Role,
	}

	return principal, nil
}

func (s *service) Logout(ctx context.Context, rawToken string) error {
	token := strings.TrimSpace(rawToken)
	if token == "" {
		return nil
	}

	return s.repo.DeleteSessionByTokenHash(ctx, hashSessionToken(token))
}

func (s *service) HasAnyAccount(ctx context.Context) (bool, error) {
	count, err := s.repo.CountAccounts(ctx)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

func (s *service) CanDeleteAccount(ctx context.Context, userID int) error {
	if userID <= 0 {
		return fmt.Errorf("%w: user id is required", domain.ErrInvalidInput)
	}

	account, err := s.repo.FindAccountByUserID(ctx, userID)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return nil
		}
		return err
	}
	if account.Role != domain.RoleAdmin {
		return nil
	}

	adminCount, err := s.repo.CountAdmins(ctx)
	if err != nil {
		return err
	}
	if adminCount <= 1 {
		return fmt.Errorf("%w: cannot delete the last admin account", domain.ErrForbidden)
	}

	return nil
}

func (s *service) UpsertAccount(ctx context.Context, userID int, username, password string, role domain.UserRole) (*domain.LocalAccount, error) {
	if userID <= 0 {
		return nil, fmt.Errorf("%w: user id is required", domain.ErrInvalidInput)
	}

	normalizedUsername, err := normalizeUsername(username)
	if err != nil {
		return nil, err
	}

	if len(password) < minPasswordLength {
		return nil, fmt.Errorf("%w: password must be at least %d characters", domain.ErrInvalidInput, minPasswordLength)
	}

	if role != domain.RoleAdmin && role != domain.RoleMember {
		return nil, fmt.Errorf("%w: role must be member or admin", domain.ErrInvalidInput)
	}
	if role == domain.RoleMember {
		existingAccount, err := s.repo.FindAccountByUserID(ctx, userID)
		if err != nil && !errors.Is(err, domain.ErrNotFound) {
			return nil, err
		}
		if err == nil && existingAccount.Role == domain.RoleAdmin {
			adminCount, countErr := s.repo.CountAdmins(ctx)
			if countErr != nil {
				return nil, countErr
			}
			if adminCount <= 1 {
				return nil, fmt.Errorf("%w: cannot demote the last admin account", domain.ErrForbidden)
			}
		}
	}

	passwordHash, err := hashPassword(password)
	if err != nil {
		return nil, err
	}

	account, err := s.repo.UpsertAccount(ctx, userID, normalizedUsername, passwordHash, role)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			return nil, fmt.Errorf("%w: username already exists", domain.ErrInvalidInput)
		}
		return nil, err
	}

	return account, nil
}

func normalizeUsername(raw string) (string, error) {
	username := strings.TrimSpace(strings.ToLower(raw))
	if len(username) < minUsernameLength {
		return "", fmt.Errorf("%w: username must be at least %d characters", domain.ErrInvalidInput, minUsernameLength)
	}

	for _, r := range username {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '.' || r == '_' || r == '-' {
			continue
		}
		return "", fmt.Errorf("%w: username contains unsupported characters", domain.ErrInvalidInput)
	}

	return username, nil
}

func generateSessionToken() (rawToken string, tokenHash string, err error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", "", err
	}

	rawToken = base64.RawURLEncoding.EncodeToString(buf)
	return rawToken, hashSessionToken(rawToken), nil
}

func hashSessionToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}
