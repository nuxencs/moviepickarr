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

// Password bounds applied everywhere a password is set. The min is the only
// composition rule (no zxcvbn/HIBP: lockout + argon2id carry the stuffing
// defense); the max closes an unbounded-input argon2id DoS and is why the login
// path caps the submitted password before it ever reaches a verify.
const (
	MinPasswordLen = 8
	MaxPasswordLen = 128
)

// Lockout policy is fixed, not operator config: after this many consecutive
// failed verifies the account locks for lockoutDuration. A success resets the
// counter; the lock auto-expires (a past locked_until is ignored), and one
// wrong attempt after expiry re-locks because the counter is only cleared on
// success.
const (
	maxFailedAttempts = 10
	lockoutDuration   = 15 * time.Minute
)

// usernameCharset is the allowed username alphabet (letters, digits, and
// . _ -), length-bounded 3–32. Kept as a set check rather than a regexp so the
// bound and the alphabet read in one place.
const (
	minUsernameLen = 3
	maxUsernameLen = 32
)

// ErrInvalidCredentials is the single, uniform failure every credential miss
// collapses to: unknown username, wrong password, no local login, or a locked
// account all return this so status, body, and (via the dummy verify) timing
// stay indistinguishable. It also backs the current-password mismatch on a
// password change ("generic 401").
var ErrInvalidCredentials = errors.New("invalid credentials")

// ErrNoLocalLogin is returned by ChangePassword when the member has no local
// login to change (the change-not-create rule): the caller maps it to 409.
var ErrNoLocalLogin = errors.New("no local login")

// LocalAuth is the deep module over the username/password login path: it owns
// credential verification, the timing-equalization dummy verify, the
// self-healing lockout, rehash-on-login, and the admin set/remove operations,
// so every HTTP handler shares one implementation of "what a local login
// means". Session minting and cookie handling stay in the HTTP layer; this
// module never touches an http.Request or a session.
type LocalAuth struct {
	repo domain.LocalAccountRepo
	// now is the injectable clock every lockout decision reads, so tests advance
	// time instead of sleeping.
	now func() time.Time
}

// NewLocalAuth builds a LocalAuth over the given repo. WithClock (shared with
// SessionManager) overrides the wall clock for deterministic lockout tests.
func NewLocalAuth(repo domain.LocalAccountRepo, opts ...LocalAuthOption) *LocalAuth {
	a := &LocalAuth{repo: repo, now: time.Now}
	for _, opt := range opts {
		opt(a)
	}
	return a
}

// LocalAuthOption configures a LocalAuth at construction.
type LocalAuthOption func(*LocalAuth)

// WithLocalClock overrides the wall clock so tests drive lockout expiry
// deterministically.
func WithLocalClock(clock func() time.Time) LocalAuthOption {
	return func(a *LocalAuth) { a.now = clock }
}

// Login verifies a username/password and returns the member id on success.
// Every failure path returns ErrInvalidCredentials, and every failure that has
// no real hash to check (unknown username, locked account) still burns one
// dummy argon2id verify so timing can't separate the cases. A wrong password on
// an unlocked account bumps the failed-attempt counter and locks at the
// threshold; a correct password resets the counters, bumps last_login_at, and
// rehashes when the stored argon2id params have drifted.
func (a *LocalAuth) Login(ctx context.Context, username, password string) (int, error) {
	username = strings.TrimSpace(username)
	if username == "" || password == "" {
		return 0, ErrInvalidCredentials
	}
	// DoS guard: an oversized password would make argon2id grind arbitrarily, so
	// cap it before any hashing. Skipping the dummy verify here is deliberate:
	// timing equalization defends username enumeration, and an oversized body is
	// not an enumeration probe.
	if len(password) > MaxPasswordLen {
		return 0, ErrInvalidCredentials
	}

	acct, err := a.repo.FindByUsername(ctx, username)
	if errors.Is(err, sql.ErrNoRows) {
		// Unknown username (or a member with no local login): no row to verify,
		// so spend the same argon2id cost on a throwaway hash.
		DummyVerify(password)
		return 0, ErrInvalidCredentials
	}
	if err != nil {
		return 0, err
	}

	now := a.now()

	// Locked: skip the real verify but still spend the dummy so a locked account
	// is timing-indistinguishable from a wrong password, and never reveal the
	// lock (silent lockout, even for the correct password).
	if acct.LockedUntil != nil && now.Before(*acct.LockedUntil) {
		DummyVerify(password)
		return 0, ErrInvalidCredentials
	}

	match, needsRehash, err := VerifyPassword(password, acct.PasswordHash)
	if err != nil || !match {
		return 0, a.recordFailure(ctx, acct, now)
	}

	if err := a.recordSuccess(ctx, acct, password, needsRehash, now); err != nil {
		return 0, err
	}
	return acct.UserID, nil
}

// recordFailure persists a wrong-password attempt: bump the consecutive-failure
// count and, at the threshold, set the lockout deadline. The counter is only
// cleared on success, so a failure past the threshold re-arms the lock.
func (a *LocalAuth) recordFailure(ctx context.Context, acct *domain.LocalAccount, now time.Time) error {
	failed := acct.FailedAttempts + 1
	var lockedUntil *time.Time
	if failed >= maxFailedAttempts {
		t := now.Add(lockoutDuration)
		lockedUntil = &t
	}
	if err := a.repo.RecordFailedAttempt(ctx, acct.UserID, failed, lockedUntil, now); err != nil {
		return err
	}
	return ErrInvalidCredentials
}

// recordSuccess resets the lockout counters, bumps last_login_at, and rehashes
// the stored password when its argon2id params have drifted from the configured
// set. A failed rehash is non-fatal: the login still succeeds on the old hash,
// and the next login retries the upgrade.
func (a *LocalAuth) recordSuccess(ctx context.Context, acct *domain.LocalAccount, password string, needsRehash bool, now time.Time) error {
	var newHash *string
	if needsRehash {
		if h, err := HashPassword(password); err == nil {
			newHash = &h
		}
	}
	return a.repo.RecordSuccessfulLogin(ctx, acct.UserID, newHash, now, now)
}

// ChangePassword verifies a member's current password and rewrites it. A wrong
// current password returns ErrInvalidCredentials (the generic 401); a member
// with no local login returns ErrNoLocalLogin (409). It does not touch
// sessions: revoking other devices and rotating the current token are the
// caller's concern, since only the HTTP layer holds the session cookie.
func (a *LocalAuth) ChangePassword(ctx context.Context, userID int, current, next string) error {
	acct, err := a.repo.FindByUserID(ctx, userID)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNoLocalLogin
	}
	if err != nil {
		return err
	}

	// Cap the submitted current password before verifying (the same argon2id DoS
	// guard the login path applies), then fold every current-password miss into
	// the uniform invalid-credentials error.
	if len(current) > MaxPasswordLen {
		return ErrInvalidCredentials
	}
	match, _, err := VerifyPassword(current, acct.PasswordHash)
	if err != nil || !match {
		return ErrInvalidCredentials
	}

	if err := validatePassword(next); err != nil {
		return err
	}
	hash, err := HashPassword(next)
	if err != nil {
		return err
	}
	return a.repo.UpdatePasswordHash(ctx, userID, hash, a.now())
}

// SetLocalLoginResult reports which branch an admin PUT took so the caller can
// apply the reset-only session revocation.
type SetLocalLoginResult struct {
	// WasReset is true when an existing local login was reset (password
	// overwritten, lockout cleared) rather than created fresh. The caller
	// revokes all of the target's sessions on a reset.
	WasReset bool
}

// SetLocalLogin is the admin upsert (PUT /members/{id}/local-login). With no
// existing row it creates the member's first local login from username +
// password (charset- and length-validated; a NOCASE collision surfaces as
// ErrConflict and a missing member as ErrNotFound from the repo). With an
// existing row it is an admin reset: the password is overwritten and the
// lockout cleared, the username is immutable through this flow (a differing
// username is rejected with ErrInvalidInput). The password bound is enforced on
// both branches.
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
	// Username is immutable through the admin flow: a caller may echo the current
	// one, but a different value is a mistake, not a rename.
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

// SetFirstLocalLogin is the self-serve credential-completeness path
// (POST /auth/local-login): a logged-in member with no local login sets their
// first username + password. The active session is the proof of identity, so
// there is no current-password check. A member who already has a local login
// gets ErrConflict (→409): adding a second is not a change (that is
// ChangePassword), so this path only ever creates. Username charset/length and
// the password bound are enforced as everywhere else; a NOCASE username
// collision surfaces as ErrConflict from the repo.
func (a *LocalAuth) SetFirstLocalLogin(ctx context.Context, userID int, username, password string) error {
	if err := validatePassword(password); err != nil {
		return err
	}

	// Change-not-create in reverse: this path refuses to touch an existing login.
	switch _, err := a.repo.FindByUserID(ctx, userID); {
	case err == nil:
		return fmt.Errorf("%w: member already has a local login", domain.ErrConflict)
	case errors.Is(err, sql.ErrNoRows):
		// No row yet: fall through and create the first login.
	default:
		return err
	}

	// Reuse the same create path as the admin upsert (trim + username validation
	// + hash + insert); the result flag is irrelevant here (always a create).
	_, err := a.createLocalLogin(ctx, userID, username, password)
	return err
}

// DeleteLocalLogin removes a member's local login (admin credential removal).
// The self-last-credential guard forbids an admin from deleting their own only
// login path (a local login with no linked identity), which would lock them out
// of their own account: that returns ErrConflict. Removing another member's
// login, or one backed by a linked identity, is allowed. A member with no local
// login to remove returns ErrNotFound.
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

// validatePassword enforces the min-8/max-128 bound applied everywhere a
// password is set. There are no composition rules by design.
func validatePassword(password string) error {
	if n := len(password); n < MinPasswordLen || n > MaxPasswordLen {
		return fmt.Errorf("%w: password must be %d-%d characters", domain.ErrInvalidInput, MinPasswordLen, MaxPasswordLen)
	}
	return nil
}

// validateUsername enforces the 3–32 length bound and the [a-zA-Z0-9._-]
// charset. Case folding and uniqueness are the store's job (NOCASE UNIQUE); the
// caller trims before calling.
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
