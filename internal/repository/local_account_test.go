package repository

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"moviepickarr/internal/db"
	"moviepickarr/internal/domain"
)

func setupLocalAccountRepo(t *testing.T) (context.Context, *SqliteLocalAccountRepository, *SqliteUserRepository, *db.Pool) {
	t.Helper()

	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "local-account-test.db")
	dbConn, err := db.OpenSQLite(dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.RunMigrations(ctx, dbConn.Write); err != nil {
		t.Fatalf("run migrations: %v", err)
	}
	t.Cleanup(func() {
		if err := dbConn.Close(); err != nil {
			t.Fatalf("close sqlite: %v", err)
		}
	})

	return ctx, NewSqliteLocalAccountRepository(dbConn), NewSqliteUserRepository(dbConn), dbConn
}

func markUserArchived(t *testing.T, ctx context.Context, pool *db.Pool, userID int) {
	t.Helper()
	if _, err := pool.Write.ExecContext(ctx,
		"UPDATE users SET archived_at = unixepoch() WHERE id = ?", userID); err != nil {
		t.Fatalf("mark member archived: %v", err)
	}
}

func TestLocalAccountRepo_CreateAndFind(t *testing.T) {
	ctx, accounts, users, _ := setupLocalAccountRepo(t)
	alice, err := users.Create(ctx, "Alice")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	if err := accounts.Create(ctx, alice.ID, "alice", "hash-1"); err != nil {
		t.Fatalf("create local account: %v", err)
	}

	// Username lookup is case-insensitive (NOCASE).
	got, err := accounts.FindByUsername(ctx, "ALICE")
	if err != nil {
		t.Fatalf("find by username: %v", err)
	}
	if got.UserID != alice.ID || got.PasswordHash != "hash-1" {
		t.Fatalf("got %+v, want user %d hash-1", got, alice.ID)
	}
	if got.FailedAttempts != 0 || got.LockedUntil != nil || got.LastLoginAt != nil {
		t.Fatalf("fresh account has non-zero lockout/login state: %+v", got)
	}

	byID, err := accounts.FindByUserID(ctx, alice.ID)
	if err != nil {
		t.Fatalf("find by user id: %v", err)
	}
	if byID.Username != "alice" {
		t.Fatalf("username = %q, want alice", byID.Username)
	}
}

func TestLocalAccountRepo_MissingReturnsNoRows(t *testing.T) {
	ctx, accounts, _, _ := setupLocalAccountRepo(t)
	if _, err := accounts.FindByUsername(ctx, "ghost"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("find username err = %v, want ErrNoRows", err)
	}
	if _, err := accounts.FindByUserID(ctx, 999); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("find id err = %v, want ErrNoRows", err)
	}
}

func TestLocalAccountRepo_ArchivedCredentialIsInvisible(t *testing.T) {
	ctx, accounts, users, pool := setupLocalAccountRepo(t)
	alice, err := users.Create(ctx, "Alice")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := accounts.Create(ctx, alice.ID, "alice", "hash-1"); err != nil {
		t.Fatalf("create local account: %v", err)
	}
	if _, err := pool.Write.ExecContext(ctx,
		"INSERT INTO oidc_identities (user_id, issuer, subject) VALUES (?, 'https://idp.test', 'alice')",
		alice.ID,
	); err != nil {
		t.Fatalf("create linked identity: %v", err)
	}
	markUserArchived(t, ctx, pool, alice.ID)

	if _, err := accounts.FindByUsername(ctx, "alice"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("find archived username err = %v, want sql.ErrNoRows", err)
	}
	if _, err := accounts.FindByUserID(ctx, alice.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("find archived user id err = %v, want sql.ErrNoRows", err)
	}
	if _, err := accounts.GetMemberIdentity(ctx, alice.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("archived identity err = %v, want sql.ErrNoRows", err)
	}
	if _, err := accounts.HasLinkedIdentity(ctx, alice.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("archived linked-identity check err = %v, want sql.ErrNoRows", err)
	}

	now := time.Now().UTC().Truncate(time.Second)
	lock := now.Add(time.Hour)
	mutations := []struct {
		name string
		run  func() error
		want error
	}{
		{"change password", func() error {
			return accounts.UpdatePasswordHash(ctx, alice.ID, "hash-2", now)
		}, sql.ErrNoRows},
		{"admin reset", func() error {
			return accounts.UpdatePasswordAndClearLockout(ctx, alice.ID, "hash-3", now)
		}, sql.ErrNoRows},
		{"failed attempt", func() error {
			return accounts.RecordFailedAttempt(ctx, alice.ID, "hash-1", 10, lock, now)
		}, domain.ErrInvalidCredentials},
		{"successful login", func() error {
			return accounts.RecordSuccessfulLogin(ctx, alice.ID, "hash-1", nil, now, now)
		}, domain.ErrInvalidCredentials},
	}
	for _, mutation := range mutations {
		if err := mutation.run(); !errors.Is(err, mutation.want) {
			t.Fatalf("%s on archived credential err = %v, want %v", mutation.name, err, mutation.want)
		}
	}
}

func TestLocalAccountRepo_CreateRejectsArchivedMember(t *testing.T) {
	ctx, accounts, users, pool := setupLocalAccountRepo(t)
	alice, err := users.Create(ctx, "Alice")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	markUserArchived(t, ctx, pool, alice.ID)

	err = accounts.Create(ctx, alice.ID, "alice", "hash-1")
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("create archived credential err = %v, want ErrNotFound", err)
	}
}

func TestLocalAccountRepo_NocaseCollision(t *testing.T) {
	ctx, accounts, users, _ := setupLocalAccountRepo(t)
	alice, _ := users.Create(ctx, "Alice")
	bob, _ := users.Create(ctx, "Bob")

	if err := accounts.Create(ctx, alice.ID, "shared", "h1"); err != nil {
		t.Fatalf("first create: %v", err)
	}
	// A different-cased username collides on the NOCASE unique index.
	err := accounts.Create(ctx, bob.ID, "SHARED", "h2")
	if !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("collision err = %v, want ErrConflict", err)
	}
}

func TestLocalAccountRepo_CreateMissingMember(t *testing.T) {
	ctx, accounts, _, _ := setupLocalAccountRepo(t)
	err := accounts.Create(ctx, 999, "orphan", "h")
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("orphan create err = %v, want ErrNotFound", err)
	}
}

func TestLocalAccountRepo_RecordFailedAndSuccess(t *testing.T) {
	ctx, accounts, users, _ := setupLocalAccountRepo(t)
	alice, _ := users.Create(ctx, "Alice")
	if err := accounts.Create(ctx, alice.ID, "alice", "h1"); err != nil {
		t.Fatalf("create: %v", err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	lockUntil := now.Add(15 * time.Minute)

	for i := 1; i <= 10; i++ {
		if err := accounts.RecordFailedAttempt(ctx, alice.ID, "h1", 10, lockUntil, now); err != nil {
			t.Fatalf("record failure %d: %v", i, err)
		}
	}
	got, _ := accounts.FindByUserID(ctx, alice.ID)
	if got.FailedAttempts != 10 || got.LockedUntil == nil || !got.LockedUntil.Equal(lockUntil) {
		t.Fatalf("after failure got %+v", got)
	}

	// A success with a rehash resets counters, sets last_login, and swaps the hash.
	newHash := "h2"
	if err := accounts.RecordSuccessfulLogin(ctx, alice.ID, "h1", &newHash, now, now); err != nil {
		t.Fatalf("record success: %v", err)
	}
	got, _ = accounts.FindByUserID(ctx, alice.ID)
	if got.FailedAttempts != 0 || got.LockedUntil != nil {
		t.Fatalf("success did not clear lockout: %+v", got)
	}
	if got.PasswordHash != "h2" || got.LastLoginAt == nil {
		t.Fatalf("success did not rehash/bump last_login: %+v", got)
	}

	// A nil hash keeps the stored one.
	if err := accounts.RecordSuccessfulLogin(ctx, alice.ID, "h2", nil, now, now); err != nil {
		t.Fatalf("record success no-rehash: %v", err)
	}
	got, _ = accounts.FindByUserID(ctx, alice.ID)
	if got.PasswordHash != "h2" {
		t.Fatalf("nil hash changed the stored hash to %q", got.PasswordHash)
	}
}

func TestLocalAccountRepo_ConcurrentFailedAttemptsAreAtomic(t *testing.T) {
	ctx, accounts, users, _ := setupLocalAccountRepo(t)
	alice, _ := users.Create(ctx, "Alice")
	if err := accounts.Create(ctx, alice.ID, "alice", "h1"); err != nil {
		t.Fatalf("create: %v", err)
	}

	const attempts = 16
	now := time.Now().UTC().Truncate(time.Second)
	lockUntil := now.Add(15 * time.Minute)
	start := make(chan struct{})
	errs := make(chan error, attempts)
	var wg sync.WaitGroup
	for range attempts {
		wg.Go(func() {
			<-start
			errs <- accounts.RecordFailedAttempt(
				ctx,
				alice.ID,
				"h1",
				10,
				lockUntil,
				now,
			)
		})
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("record concurrent failure: %v", err)
		}
	}

	got, err := accounts.FindByUserID(ctx, alice.ID)
	if err != nil {
		t.Fatalf("find account: %v", err)
	}
	if got.FailedAttempts != attempts {
		t.Fatalf("failed attempts = %d, want %d", got.FailedAttempts, attempts)
	}
	if got.LockedUntil == nil || !got.LockedUntil.Equal(lockUntil) {
		t.Fatalf("locked until = %v, want %v", got.LockedUntil, lockUntil)
	}
}

func TestLocalAccountRepo_LoginAccountingCannotCrossPasswordChange(t *testing.T) {
	ctx, accounts, users, _ := setupLocalAccountRepo(t)
	alice, _ := users.Create(ctx, "Alice")
	if err := accounts.Create(ctx, alice.ID, "alice", "old-hash"); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	if err := accounts.UpdatePasswordAndClearLockout(ctx, alice.ID, "recovered-hash", now); err != nil {
		t.Fatal(err)
	}
	lock := now.Add(time.Hour)
	if err := accounts.RecordFailedAttempt(ctx, alice.ID, "old-hash", 10, lock, now); !errors.Is(err, domain.ErrInvalidCredentials) {
		t.Fatalf("stale failure accounting = %v, want ErrInvalidCredentials", err)
	}
	oldRehash := "old-password-rehash"
	if err := accounts.RecordSuccessfulLogin(ctx, alice.ID, "old-hash", &oldRehash, now, now); !errors.Is(err, domain.ErrInvalidCredentials) {
		t.Fatalf("stale success accounting = %v, want ErrInvalidCredentials", err)
	}
	got, err := accounts.FindByUserID(ctx, alice.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.PasswordHash != "recovered-hash" || got.FailedAttempts != 0 || got.LockedUntil != nil {
		t.Fatalf("recovered credential changed by stale login: %+v", got)
	}
}

func TestLocalAccountRepo_UpdateAndDelete(t *testing.T) {
	ctx, accounts, users, _ := setupLocalAccountRepo(t)
	alice, _ := users.Create(ctx, "Alice")
	if err := accounts.Create(ctx, alice.ID, "alice", "h1"); err != nil {
		t.Fatalf("create: %v", err)
	}
	now := time.Now().UTC().Truncate(time.Second)

	if err := accounts.UpdatePasswordHash(ctx, alice.ID, "h2", now); err != nil {
		t.Fatalf("update hash: %v", err)
	}
	got, _ := accounts.FindByUserID(ctx, alice.ID)
	if got.PasswordHash != "h2" {
		t.Fatalf("hash = %q, want h2", got.PasswordHash)
	}

	// Seed a lockout, then confirm the admin-reset update clears it.
	lock := now.Add(time.Hour)
	_ = accounts.RecordFailedAttempt(ctx, alice.ID, "h2", 1, lock, now)
	if err := accounts.UpdatePasswordAndClearLockout(ctx, alice.ID, "h3", now); err != nil {
		t.Fatalf("reset update: %v", err)
	}
	got, _ = accounts.FindByUserID(ctx, alice.ID)
	if got.PasswordHash != "h3" || got.FailedAttempts != 0 || got.LockedUntil != nil {
		t.Fatalf("reset update left state %+v", got)
	}

	if err := accounts.Delete(ctx, alice.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := accounts.FindByUserID(ctx, alice.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("after delete err = %v, want ErrNoRows", err)
	}
	// Updating/deleting a missing row is a zero-row miss.
	if err := accounts.UpdatePasswordHash(ctx, alice.ID, "x", now); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("update missing err = %v, want ErrNoRows", err)
	}
	if err := accounts.Delete(ctx, alice.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("delete missing err = %v, want ErrNoRows", err)
	}
}

func TestLocalAccountRepo_MemberIdentity(t *testing.T) {
	ctx, accounts, users, pool := setupLocalAccountRepo(t)
	alice, _ := users.Create(ctx, "Alice")

	// Placeholder: no local login, no linked identity.
	id, err := accounts.GetMemberIdentity(ctx, alice.ID)
	if err != nil {
		t.Fatalf("identity: %v", err)
	}
	if id.DisplayName != "Alice" || id.Username != nil || id.HasLocalLogin || id.HasLinkedIdentity {
		t.Fatalf("placeholder identity = %+v", id)
	}
	if id.Role != "member" {
		t.Fatalf("role = %q, want member", id.Role)
	}

	// Add a local login → hasLocalLogin + username.
	if err := accounts.Create(ctx, alice.ID, "alice", "h1"); err != nil {
		t.Fatalf("create: %v", err)
	}
	id, _ = accounts.GetMemberIdentity(ctx, alice.ID)
	if !id.HasLocalLogin || id.Username == nil || *id.Username != "alice" {
		t.Fatalf("after local login identity = %+v", id)
	}

	// Add a linked identity row → hasLinkedIdentity.
	if _, err := pool.Write.ExecContext(ctx,
		"INSERT INTO oidc_identities (user_id, issuer, subject) VALUES (?, 'iss', 'sub')", alice.ID); err != nil {
		t.Fatalf("insert oidc: %v", err)
	}
	linked, err := accounts.HasLinkedIdentity(ctx, alice.ID)
	if err != nil || !linked {
		t.Fatalf("HasLinkedIdentity = %v, %v; want true, nil", linked, err)
	}
	id, _ = accounts.GetMemberIdentity(ctx, alice.ID)
	if !id.HasLinkedIdentity {
		t.Fatalf("identity missing linked flag: %+v", id)
	}

	// Unknown member → no rows.
	if _, err := accounts.GetMemberIdentity(ctx, 999); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("unknown identity err = %v, want ErrNoRows", err)
	}
}
