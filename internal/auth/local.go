package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"moviepickarr/internal/domain"
)

// Password bounds. No composition rules: lockout and argon2id carry the
// stuffing defense. The max closes an argon2id DoS.
const (
	MinPasswordLen = 8
	MaxPasswordLen = 128
)

// Lockout policy, fixed. Only a success clears the counter, so one wrong
// attempt after the lock expires locks again.
const (
	maxFailedAttempts = 10
	lockoutDuration   = 15 * time.Minute
)

// Username length bounds; the alphabet is in isUsernameRune.
const (
	minUsernameLen = 3
	maxUsernameLen = 32
)

// ErrInvalidCredentials is the one failure for every credential miss, so status,
// body, and (via the dummy verify) timing cannot tell the cases apart.
var ErrInvalidCredentials = domain.ErrInvalidCredentials

// ErrNoLocalLogin means ChangePassword found no local login to change (409).
var ErrNoLocalLogin = domain.ErrNoLocalLogin

// LocalAuth owns the username/password login path: verification, the dummy
// verify, lockout, rehash-on-login, and admin set/remove. It never touches an
// http.Request or a session.
type LocalAuth struct {
	repo domain.LocalAccountRepo
	// now is injectable so tests advance time instead of sleeping.
	now func() time.Time
}

// NewLocalAuth builds a LocalAuth over the given repo.
func NewLocalAuth(repo domain.LocalAccountRepo, opts ...LocalAuthOption) *LocalAuth {
	a := &LocalAuth{repo: repo, now: time.Now}
	for _, opt := range opts {
		opt(a)
	}
	return a
}

// LocalAuthOption configures a LocalAuth at construction.
type LocalAuthOption func(*LocalAuth)

// WithLocalClock overrides the wall clock for tests.
func WithLocalClock(clock func() time.Time) LocalAuthOption {
	return func(a *LocalAuth) { a.now = clock }
}

// Login verifies a username/password and returns the member id on success.
// Every failure returns ErrInvalidCredentials.
func (a *LocalAuth) Login(ctx context.Context, username, password string) (int, error) {
	login, err := a.PrepareLogin(ctx, username, password)
	if err != nil {
		return 0, err
	}
	now := a.now()
	if err := a.repo.RecordSuccessfulLogin(
		ctx,
		login.UserID,
		login.ExpectedPasswordHash,
		login.NewPasswordHash,
		now,
		now,
	); err != nil {
		return 0, err
	}
	return login.UserID, nil
}

// PrepareLogin verifies a password and returns the expected credential hash
// plus an optional rehash. Production commits it through AuthTransitionStore.
func (a *LocalAuth) PrepareLogin(
	ctx context.Context,
	username, password string,
) (domain.VerifiedLocalLogin, error) {
	username = strings.TrimSpace(username)
	if username == "" || password == "" {
		return domain.VerifiedLocalLogin{}, ErrInvalidCredentials
	}
	// Argon2id DoS guard. No dummy verify: an oversized body is not an enumeration probe.
	if len(password) > MaxPasswordLen {
		return domain.VerifiedLocalLogin{}, ErrInvalidCredentials
	}

	acct, err := a.repo.FindByUsername(ctx, username)
	if errors.Is(err, sql.ErrNoRows) {
		// Spend the same argon2id cost so timing cannot reveal unknown usernames.
		DummyVerify(password)
		return domain.VerifiedLocalLogin{}, ErrInvalidCredentials
	}
	if err != nil {
		return domain.VerifiedLocalLogin{}, err
	}

	now := a.now()

	// Silent lockout, even for the correct password; the dummy keeps timing equal.
	if acct.LockedUntil != nil && now.Before(*acct.LockedUntil) {
		DummyVerify(password)
		return domain.VerifiedLocalLogin{}, ErrInvalidCredentials
	}

	match, needsRehash, err := VerifyPassword(password, acct.PasswordHash)
	if err != nil || !match {
		return domain.VerifiedLocalLogin{}, a.recordFailure(ctx, acct, now)
	}

	var newHash *string
	if needsRehash {
		if h, err := HashPassword(password); err == nil {
			newHash = &h
		}
	}
	return domain.VerifiedLocalLogin{
		UserID:               acct.UserID,
		ExpectedPasswordHash: acct.PasswordHash,
		NewPasswordHash:      newHash,
	}, nil
}

// recordFailure persists a wrong-password attempt. The repo increments and
// tests the count in one statement, so parallel verifies cannot lose attempts.
func (a *LocalAuth) recordFailure(ctx context.Context, acct *domain.LocalAccount, now time.Time) error {
	if err := a.repo.RecordFailedAttempt(
		ctx,
		acct.UserID,
		acct.PasswordHash,
		maxFailedAttempts,
		now.Add(lockoutDuration),
		now,
	); err != nil {
		return err
	}
	return ErrInvalidCredentials
}

// ChangePassword is the repo-only path for tests. Production uses
// PreparePasswordChange plus AuthTransitionStore for one atomic commit.
func (a *LocalAuth) ChangePassword(ctx context.Context, userID int, current, next string) error {
	change, err := a.PreparePasswordChange(ctx, userID, current, next)
	if err != nil {
		return err
	}
	return a.repo.UpdatePasswordHash(ctx, userID, change.PasswordHash, a.now())
}

// PreparePasswordChange verifies and hashes outside SQLite's writer. The
// expected hash lets the transaction reject a credential changed since.
func (a *LocalAuth) PreparePasswordChange(
	ctx context.Context,
	userID int,
	current, next string,
) (domain.VerifiedPasswordChange, error) {
	acct, err := a.repo.FindByUserID(ctx, userID)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.VerifiedPasswordChange{}, ErrNoLocalLogin
	}
	if err != nil {
		return domain.VerifiedPasswordChange{}, err
	}

	// Same argon2id DoS guard as the login path.
	if len(current) > MaxPasswordLen {
		return domain.VerifiedPasswordChange{}, ErrInvalidCredentials
	}
	match, _, err := VerifyPassword(current, acct.PasswordHash)
	if err != nil || !match {
		return domain.VerifiedPasswordChange{}, ErrInvalidCredentials
	}

	if err := validatePassword(next); err != nil {
		return domain.VerifiedPasswordChange{}, err
	}
	hash, err := HashPassword(next)
	if err != nil {
		return domain.VerifiedPasswordChange{}, err
	}
	return domain.VerifiedPasswordChange{
		UserID:               userID,
		ExpectedPasswordHash: acct.PasswordHash,
		PasswordHash:         hash,
	}, nil
}

// SetLocalLoginResult reports which branch an admin PUT took.
type SetLocalLoginResult struct {
	// WasReset tells the caller to revoke all of the target's sessions.
	WasReset bool
}

// SetLocalLogin is the admin upsert: it creates a first local login, or resets
// the password and clears the lockout. The username is immutable on a reset.
func (a *LocalAuth) SetLocalLogin(ctx context.Context, targetID int, username, password string) (SetLocalLoginResult, error) {
	if err := validatePassword(password); err != nil {
		return SetLocalLoginResult{}, err
	}

	existing, err := a.repo.FindByUserID(ctx, targetID)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return a.createLocalLogin(ctx, targetID, username, password)
	case err != nil:
		return SetLocalLoginResult{}, err
	default:
		return a.resetLocalLogin(ctx, existing, username, password)
	}
}

func (a *LocalAuth) createLocalLogin(ctx context.Context, targetID int, username, password string) (SetLocalLoginResult, error) {
	username = strings.TrimSpace(username)
	if err := validateUsername(username); err != nil {
		return SetLocalLoginResult{}, err
	}
	hash, err := HashPassword(password)
	if err != nil {
		return SetLocalLoginResult{}, err
	}
	if err := a.repo.Create(ctx, targetID, username, hash); err != nil {
		return SetLocalLoginResult{}, err
	}
	return SetLocalLoginResult{WasReset: false}, nil
}

func (a *LocalAuth) resetLocalLogin(ctx context.Context, existing *domain.LocalAccount, username, password string) (SetLocalLoginResult, error) {
	// A caller may echo the current username, but a different one is not a rename.
	if u := strings.TrimSpace(username); u != "" && !strings.EqualFold(u, existing.Username) {
		return SetLocalLoginResult{}, fmt.Errorf("%w: username is immutable through this flow", domain.ErrInvalidInput)
	}
	hash, err := HashPassword(password)
	if err != nil {
		return SetLocalLoginResult{}, err
	}
	if err := a.repo.UpdatePasswordAndClearLockout(ctx, existing.UserID, hash, a.now()); err != nil {
		return SetLocalLoginResult{}, err
	}
	return SetLocalLoginResult{WasReset: true}, nil
}

// SetFirstLocalLogin lets a logged-in member with no local login create one.
// The session proves identity. An existing login returns ErrConflict.
func (a *LocalAuth) SetFirstLocalLogin(ctx context.Context, userID int, username, password string) error {
	if err := validatePassword(password); err != nil {
		return err
	}

	switch _, err := a.repo.FindByUserID(ctx, userID); {
	case err == nil:
		return fmt.Errorf("%w: member already has a local login", domain.ErrConflict)
	case errors.Is(err, sql.ErrNoRows):
		// No row yet: fall through and create the first login.
	default:
		return err
	}

	_, err := a.createLocalLogin(ctx, userID, username, password)
	return err
}

// DeleteLocalLogin removes a member's local login. An admin cannot remove their
// own last credential (ErrConflict).
func (a *LocalAuth) DeleteLocalLogin(ctx context.Context, targetID, actorID int) error {
	if _, err := a.repo.FindByUserID(ctx, targetID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("%w: member %d has no local login", domain.ErrNotFound, targetID)
		}
		return err
	}

	if targetID == actorID {
		linked, err := a.repo.HasLinkedIdentity(ctx, targetID)
		if err != nil {
			return err
		}
		if !linked {
			return fmt.Errorf("%w: cannot remove your own last credential", domain.ErrConflict)
		}
	}

	return a.repo.Delete(ctx, targetID)
}

// Identity returns the GET /auth/me projection for a member, or a wrapped
// ErrNotFound when the member does not exist.
func (a *LocalAuth) Identity(ctx context.Context, userID int) (*domain.MemberIdentity, error) {
	id, err := a.repo.GetMemberIdentity(ctx, userID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("%w: member %d", domain.ErrNotFound, userID)
	}
	if err != nil {
		return nil, err
	}
	return id, nil
}

// validatePassword enforces the length bound. No composition rules by design.
func validatePassword(password string) error {
	if n := len(password); n < MinPasswordLen || n > MaxPasswordLen {
		return fmt.Errorf("%w: password must be %d-%d characters", domain.ErrInvalidInput, MinPasswordLen, MaxPasswordLen)
	}
	return nil
}

// validateUsername checks length and charset on a trimmed name. The store owns
// case folding and uniqueness (NOCASE UNIQUE).
func validateUsername(username string) error {
	if n := len(username); n < minUsernameLen || n > maxUsernameLen {
		return fmt.Errorf("%w: username must be %d-%d characters", domain.ErrInvalidInput, minUsernameLen, maxUsernameLen)
	}
	for _, r := range username {
		if !isUsernameRune(r) {
			return fmt.Errorf("%w: username may contain only letters, digits, and . _ -", domain.ErrInvalidInput)
		}
	}
	return nil
}

func isUsernameRune(r rune) bool {
	switch {
	case r >= 'a' && r <= 'z':
		return true
	case r >= 'A' && r <= 'Z':
		return true
	case r >= '0' && r <= '9':
		return true
	case r == '.' || r == '_' || r == '-':
		return true
	default:
		return false
	}
}
