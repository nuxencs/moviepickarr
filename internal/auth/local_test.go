package auth

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"moviepickarr/internal/domain"

	"github.com/alexedwards/argon2id"
)

// fakeLocalRepo is an in-memory LocalAccountRepo so the service's lockout,
// rehash, and guard logic is asserted against an injected clock, no SQL.
type fakeLocalRepo struct {
	byID       map[int]*domain.LocalAccount
	linked     map[int]bool // user_id -> has oidc identity
	identities map[int]*domain.MemberIdentity
	failNext   error // if set, the next mutating call returns it

	failureCalls int
	successHash  *string
	successCalls int
}

func newFakeLocalRepo() *fakeLocalRepo {
	return &fakeLocalRepo{
		byID:       map[int]*domain.LocalAccount{},
		linked:     map[int]bool{},
		identities: map[int]*domain.MemberIdentity{},
	}
}

func (f *fakeLocalRepo) FindByUsername(_ context.Context, username string) (*domain.LocalAccount, error) {
	for _, a := range f.byID {
		if strings.EqualFold(a.Username, strings.TrimSpace(username)) {
			clone := *a
			return &clone, nil
		}
	}
	return nil, sql.ErrNoRows
}

func (f *fakeLocalRepo) FindByUserID(_ context.Context, userID int) (*domain.LocalAccount, error) {
	a, ok := f.byID[userID]
	if !ok {
		return nil, sql.ErrNoRows
	}
	clone := *a
	return &clone, nil
}

func (f *fakeLocalRepo) Create(_ context.Context, userID int, username, passwordHash string) error {
	if f.failNext != nil {
		err := f.failNext
		f.failNext = nil
		return err
	}
	f.byID[userID] = &domain.LocalAccount{UserID: userID, Username: username, PasswordHash: passwordHash}
	return nil
}

func (f *fakeLocalRepo) UpdatePasswordHash(_ context.Context, userID int, passwordHash string, _ time.Time) error {
	a, ok := f.byID[userID]
	if !ok {
		return sql.ErrNoRows
	}
	a.PasswordHash = passwordHash
	return nil
}

func (f *fakeLocalRepo) UpdatePasswordAndClearLockout(_ context.Context, userID int, passwordHash string, _ time.Time) error {
	a, ok := f.byID[userID]
	if !ok {
		return sql.ErrNoRows
	}
	a.PasswordHash = passwordHash
	a.FailedAttempts = 0
	a.LockedUntil = nil
	return nil
}

func (f *fakeLocalRepo) RecordFailedAttempt(_ context.Context, userID int, expectedPasswordHash string, lockThreshold int, lockUntil, _ time.Time) error {
	f.failureCalls++
	a, ok := f.byID[userID]
	if !ok {
		return sql.ErrNoRows
	}
	if a.PasswordHash != expectedPasswordHash {
		return domain.ErrInvalidCredentials
	}
	a.FailedAttempts++
	if a.FailedAttempts >= lockThreshold {
		a.LockedUntil = &lockUntil
	} else {
		a.LockedUntil = nil
	}
	return nil
}

func (f *fakeLocalRepo) RecordSuccessfulLogin(_ context.Context, userID int, expectedPasswordHash string, newPasswordHash *string, lastLoginAt, _ time.Time) error {
	f.successCalls++
	f.successHash = newPasswordHash
	a, ok := f.byID[userID]
	if !ok {
		return sql.ErrNoRows
	}
	if a.PasswordHash != expectedPasswordHash {
		return domain.ErrInvalidCredentials
	}
	a.FailedAttempts = 0
	a.LockedUntil = nil
	a.LastLoginAt = &lastLoginAt
	if newPasswordHash != nil {
		a.PasswordHash = *newPasswordHash
	}
	return nil
}

func (f *fakeLocalRepo) Delete(_ context.Context, userID int) error {
	delete(f.byID, userID)
	return nil
}

func (f *fakeLocalRepo) HasLinkedIdentity(_ context.Context, userID int) (bool, error) {
	return f.linked[userID], nil
}

func (f *fakeLocalRepo) GetMemberIdentity(_ context.Context, userID int) (*domain.MemberIdentity, error) {
	id, ok := f.identities[userID]
	if !ok {
		return nil, sql.ErrNoRows
	}
	clone := *id
	return &clone, nil
}

// seedAccount inserts a local login with a real argon2id hash of password.
func (f *fakeLocalRepo) seedAccount(t *testing.T, userID int, username, password string) {
	t.Helper()
	hash, err := HashPassword(password)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	f.byID[userID] = &domain.LocalAccount{UserID: userID, Username: username, PasswordHash: hash}
}

func fixedClock(t time.Time) func() time.Time { return func() time.Time { return t } }

func TestLogin_Success(t *testing.T) {
	repo := newFakeLocalRepo()
	repo.seedAccount(t, 7, "alice", "correct horse")
	repo.byID[7].FailedAttempts = 3 // a prior streak that a success must clear
	a := NewLocalAuth(repo)

	id, err := a.Login(context.Background(), "  Alice  ", "correct horse")
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	if id != 7 {
		t.Fatalf("id = %d, want 7", id)
	}
	if repo.successCalls != 1 {
		t.Fatalf("success recorded %d times, want 1", repo.successCalls)
	}
	if repo.successHash != nil {
		t.Fatal("current-params hash should not be rehashed")
	}
	if repo.byID[7].FailedAttempts != 0 {
		t.Fatalf("failed attempts = %d, want reset to 0", repo.byID[7].FailedAttempts)
	}
}

func TestLogin_UnknownUsername(t *testing.T) {
	repo := newFakeLocalRepo()
	a := NewLocalAuth(repo)

	_, err := a.Login(context.Background(), "ghost", "whatever")
	if !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("err = %v, want ErrInvalidCredentials", err)
	}
}

func TestLogin_EmptyFields(t *testing.T) {
	a := NewLocalAuth(newFakeLocalRepo())
	for _, tc := range []struct{ u, p string }{{"", "pw"}, {"user", ""}, {"  ", "pw"}} {
		if _, err := a.Login(context.Background(), tc.u, tc.p); !errors.Is(err, ErrInvalidCredentials) {
			t.Fatalf("login(%q,%q) err = %v, want ErrInvalidCredentials", tc.u, tc.p, err)
		}
	}
}

func TestLogin_WrongPasswordIncrementsCounter(t *testing.T) {
	repo := newFakeLocalRepo()
	repo.seedAccount(t, 1, "bob", "the right one")
	a := NewLocalAuth(repo)

	_, err := a.Login(context.Background(), "bob", "the wrong one")
	if !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("err = %v, want ErrInvalidCredentials", err)
	}
	if repo.byID[1].FailedAttempts != 1 {
		t.Fatalf("failed attempts = %d, want 1", repo.byID[1].FailedAttempts)
	}
	if repo.byID[1].LockedUntil != nil {
		t.Fatal("account locked after a single failure")
	}
}

func TestLogin_LocksAtThreshold(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	repo := newFakeLocalRepo()
	repo.seedAccount(t, 1, "bob", "the right one")
	repo.byID[1].FailedAttempts = maxFailedAttempts - 1 // one away from the lock
	a := NewLocalAuth(repo, WithLocalClock(fixedClock(now)))

	if _, err := a.Login(context.Background(), "bob", "nope"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("err = %v, want ErrInvalidCredentials", err)
	}
	locked := repo.byID[1].LockedUntil
	if locked == nil {
		t.Fatal("account not locked at threshold")
	}
	if want := now.Add(lockoutDuration); !locked.Equal(want) {
		t.Fatalf("locked_until = %v, want %v", locked, want)
	}
}

func TestLogin_LockedRejectsCorrectPassword(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	repo := newFakeLocalRepo()
	repo.seedAccount(t, 1, "bob", "the right one")
	future := now.Add(5 * time.Minute)
	repo.byID[1].LockedUntil = &future
	repo.byID[1].FailedAttempts = maxFailedAttempts
	a := NewLocalAuth(repo, WithLocalClock(fixedClock(now)))

	// Even the correct password is refused while locked (silent lockout), and no
	// verify bookkeeping runs.
	if _, err := a.Login(context.Background(), "bob", "the right one"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("err = %v, want ErrInvalidCredentials", err)
	}
	if repo.successCalls != 0 {
		t.Fatal("a locked account recorded a successful login")
	}
	if repo.byID[1].FailedAttempts != maxFailedAttempts {
		t.Fatalf("failed attempts changed while locked: %d", repo.byID[1].FailedAttempts)
	}
}

func TestLogin_ExpiredLockAllowsSuccess(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	repo := newFakeLocalRepo()
	repo.seedAccount(t, 1, "bob", "the right one")
	past := now.Add(-time.Minute)
	repo.byID[1].LockedUntil = &past
	repo.byID[1].FailedAttempts = maxFailedAttempts
	a := NewLocalAuth(repo, WithLocalClock(fixedClock(now)))

	id, err := a.Login(context.Background(), "bob", "the right one")
	if err != nil {
		t.Fatalf("login after lock expiry: %v", err)
	}
	if id != 1 {
		t.Fatalf("id = %d, want 1", id)
	}
	if repo.byID[1].LockedUntil != nil || repo.byID[1].FailedAttempts != 0 {
		t.Fatal("success did not clear the expired lock counters")
	}
}

func TestLogin_OversizedPasswordRejected(t *testing.T) {
	repo := newFakeLocalRepo()
	repo.seedAccount(t, 1, "bob", "the right one")
	a := NewLocalAuth(repo)

	huge := strings.Repeat("x", MaxPasswordLen+1)
	if _, err := a.Login(context.Background(), "bob", huge); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("err = %v, want ErrInvalidCredentials", err)
	}
	// The DoS guard short-circuits before any bookkeeping.
	if repo.failureCalls != 0 {
		t.Fatal("oversized password touched the lockout counter")
	}
}

func TestLogin_RehashOnDriftedParams(t *testing.T) {
	// A hash made with weaker-than-configured params must trigger a rehash on a
	// successful login.
	weak := &argon2id.Params{Memory: 8192, Iterations: 1, Parallelism: 1, SaltLength: 16, KeyLength: 32}
	hash, err := argon2id.CreateHash("driftpass", weak)
	if err != nil {
		t.Fatalf("create weak hash: %v", err)
	}
	repo := newFakeLocalRepo()
	repo.byID[9] = &domain.LocalAccount{UserID: 9, Username: "carol", PasswordHash: hash}
	a := NewLocalAuth(repo)

	if _, err := a.Login(context.Background(), "carol", "driftpass"); err != nil {
		t.Fatalf("login: %v", err)
	}
	if repo.successHash == nil {
		t.Fatal("drifted params did not trigger a rehash")
	}
}

func TestChangePassword(t *testing.T) {
	repo := newFakeLocalRepo()
	repo.seedAccount(t, 1, "bob", "old password")
	a := NewLocalAuth(repo)
	ctx := context.Background()

	// No local login → ErrNoLocalLogin.
	if err := a.ChangePassword(ctx, 999, "x", "new password"); !errors.Is(err, ErrNoLocalLogin) {
		t.Fatalf("missing row err = %v, want ErrNoLocalLogin", err)
	}
	// Wrong current → generic invalid credentials.
	if err := a.ChangePassword(ctx, 1, "not it", "new password"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("wrong current err = %v, want ErrInvalidCredentials", err)
	}
	// Too-short new password → invalid input.
	if err := a.ChangePassword(ctx, 1, "old password", "short"); !errors.Is(err, domain.ErrInvalidInput) {
		t.Fatalf("short new err = %v, want ErrInvalidInput", err)
	}
	// Success rewrites the hash so the new password verifies and the old fails.
	if err := a.ChangePassword(ctx, 1, "old password", "brand new password"); err != nil {
		t.Fatalf("change: %v", err)
	}
	if match, _, _ := VerifyPassword("brand new password", repo.byID[1].PasswordHash); !match {
		t.Fatal("new password does not verify after change")
	}
}

func TestSetLocalLogin_Create(t *testing.T) {
	repo := newFakeLocalRepo()
	a := NewLocalAuth(repo)
	ctx := context.Background()

	res, err := a.SetLocalLogin(ctx, 5, "New_User.1", "a good password")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if res.WasReset {
		t.Fatal("create reported as reset")
	}
	if _, ok := repo.byID[5]; !ok {
		t.Fatal("local login not created")
	}

	// Bad username charset and short password are rejected as invalid input.
	if _, err := a.SetLocalLogin(ctx, 6, "no spaces allowed", "a good password"); !errors.Is(err, domain.ErrInvalidInput) {
		t.Fatalf("bad username err = %v, want ErrInvalidInput", err)
	}
	if _, err := a.SetLocalLogin(ctx, 6, "gooduser", "short"); !errors.Is(err, domain.ErrInvalidInput) {
		t.Fatalf("short password err = %v, want ErrInvalidInput", err)
	}
}

func TestSetLocalLogin_CollisionPropagates(t *testing.T) {
	repo := newFakeLocalRepo()
	repo.failNext = domain.ErrConflict // repo translates a NOCASE collision
	a := NewLocalAuth(repo)

	if _, err := a.SetLocalLogin(context.Background(), 5, "taken", "a good password"); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("err = %v, want ErrConflict", err)
	}
}

func TestSetLocalLogin_Reset(t *testing.T) {
	repo := newFakeLocalRepo()
	repo.seedAccount(t, 1, "bob", "old password")
	locked := time.Now().Add(time.Hour)
	repo.byID[1].LockedUntil = &locked
	repo.byID[1].FailedAttempts = 4
	a := NewLocalAuth(repo)
	ctx := context.Background()

	res, err := a.SetLocalLogin(ctx, 1, "", "fresh password")
	if err != nil {
		t.Fatalf("reset: %v", err)
	}
	if !res.WasReset {
		t.Fatal("reset not reported")
	}
	if repo.byID[1].LockedUntil != nil || repo.byID[1].FailedAttempts != 0 {
		t.Fatal("reset did not clear the lockout")
	}
	if match, _, _ := VerifyPassword("fresh password", repo.byID[1].PasswordHash); !match {
		t.Fatal("reset password does not verify")
	}

	// A differing username on reset is rejected: username is immutable here.
	if _, err := a.SetLocalLogin(ctx, 1, "renamed", "another password"); !errors.Is(err, domain.ErrInvalidInput) {
		t.Fatalf("rename err = %v, want ErrInvalidInput", err)
	}
	// Echoing the current username (any case) is accepted.
	if _, err := a.SetLocalLogin(ctx, 1, "BOB", "another password"); err != nil {
		t.Fatalf("echoed username reset: %v", err)
	}
}

func TestDeleteLocalLogin(t *testing.T) {
	repo := newFakeLocalRepo()
	repo.seedAccount(t, 1, "admin", "pw")  // acting admin, only credential
	repo.seedAccount(t, 2, "target", "pw") // another member
	repo.seedAccount(t, 3, "linked", "pw") // admin with a linked identity too
	repo.linked[3] = true
	a := NewLocalAuth(repo)
	ctx := context.Background()

	// Self, last credential → refused.
	if err := a.DeleteLocalLogin(ctx, 1, 1); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("self-last-credential err = %v, want ErrConflict", err)
	}
	if _, ok := repo.byID[1]; !ok {
		t.Fatal("refused delete still removed the row")
	}
	// Another member → allowed.
	if err := a.DeleteLocalLogin(ctx, 2, 1); err != nil {
		t.Fatalf("delete other: %v", err)
	}
	if _, ok := repo.byID[2]; ok {
		t.Fatal("target local login not removed")
	}
	// Self but with a linked identity as fallback → allowed.
	if err := a.DeleteLocalLogin(ctx, 3, 3); err != nil {
		t.Fatalf("delete self with fallback: %v", err)
	}
	// Missing row → not found.
	if err := a.DeleteLocalLogin(ctx, 404, 1); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("missing err = %v, want ErrNotFound", err)
	}
}
