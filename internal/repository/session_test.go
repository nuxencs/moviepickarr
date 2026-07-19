package repository

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"moviepickarr/internal/db"
	"moviepickarr/internal/domain"
)

func setupSessionRepo(t *testing.T) (context.Context, *SqliteSessionRepository, *SqliteUserRepository, *db.Pool) {
	t.Helper()

	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "session-test.db")
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

	return ctx, NewSqliteSessionRepository(dbConn), NewSqliteUserRepository(dbConn), dbConn
}

func mustCreateSession(t *testing.T, ctx context.Context, repo *SqliteSessionRepository, hash string, userID int, expiresAt, lastSeen time.Time) {
	t.Helper()
	err := repo.Create(ctx, domain.Session{
		TokenHash:  hash,
		UserID:     userID,
		ExpiresAt:  expiresAt,
		LastSeenAt: lastSeen,
		CreatedAt:  lastSeen,
	})
	if err != nil {
		t.Fatalf("create session %q: %v", hash, err)
	}
}

func TestSessionRepo_FindJoinsLiveRole(t *testing.T) {
	ctx, sessions, users, pool := setupSessionRepo(t)

	alice, err := users.Create(ctx, "Alice")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	mustCreateSession(t, ctx, sessions, "hash-a", alice.ID, now.Add(90*24*time.Hour), now)

	got, err := sessions.FindByTokenHash(ctx, "hash-a")
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	if got.UserID != alice.ID {
		t.Fatalf("user id = %d, want %d", got.UserID, alice.ID)
	}
	if got.Role != "member" {
		t.Fatalf("role = %q, want member", got.Role)
	}
	if !got.ExpiresAt.Equal(now.Add(90 * 24 * time.Hour)) {
		t.Fatalf("expires_at = %v, want %v", got.ExpiresAt, now.Add(90*24*time.Hour))
	}

	// Role is read live: promoting the member surfaces on the next read with the
	// same token, no session row touched.
	if _, err := pool.Write.ExecContext(ctx, "UPDATE users SET role = 'admin' WHERE id = ?", alice.ID); err != nil {
		t.Fatalf("promote: %v", err)
	}
	got, err = sessions.FindByTokenHash(ctx, "hash-a")
	if err != nil {
		t.Fatalf("find after promote: %v", err)
	}
	if got.Role != "admin" {
		t.Fatalf("role after promote = %q, want admin", got.Role)
	}
}

func TestSessionRepo_FindMissingIsNoRows(t *testing.T) {
	ctx, sessions, _, _ := setupSessionRepo(t)

	_, err := sessions.FindByTokenHash(ctx, "absent")
	if !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("err = %v, want sql.ErrNoRows", err)
	}
}

func TestSessionRepo_TouchLastSeen(t *testing.T) {
	ctx, sessions, users, _ := setupSessionRepo(t)
	alice, _ := users.Create(ctx, "Alice")
	base := time.Now().UTC().Truncate(time.Second)
	mustCreateSession(t, ctx, sessions, "hash-a", alice.ID, base.Add(90*24*time.Hour), base)

	slid := base.Add(3 * time.Hour)
	got, _ := sessions.FindByTokenHash(ctx, "hash-a")
	if err := sessions.TouchLastSeen(ctx, got.ID, slid); err != nil {
		t.Fatalf("touch: %v", err)
	}
	got, _ = sessions.FindByTokenHash(ctx, "hash-a")
	if !got.LastSeenAt.Equal(slid) {
		t.Fatalf("last_seen_at = %v, want %v", got.LastSeenAt, slid)
	}
}

func TestSessionRepo_RevokeVariants(t *testing.T) {
	ctx, sessions, users, _ := setupSessionRepo(t)
	alice, _ := users.Create(ctx, "Alice")
	bob, _ := users.Create(ctx, "Bob")
	now := time.Now().UTC().Truncate(time.Second)
	exp := now.Add(90 * 24 * time.Hour)

	mustCreateSession(t, ctx, sessions, "a1", alice.ID, exp, now)
	mustCreateSession(t, ctx, sessions, "a2", alice.ID, exp, now)
	mustCreateSession(t, ctx, sessions, "a3", alice.ID, exp, now)
	mustCreateSession(t, ctx, sessions, "b1", bob.ID, exp, now)

	// Revoke-others for Alice keeps a1, drops a2/a3, never touches Bob.
	n, err := sessions.DeleteOthersByUserID(ctx, alice.ID, "a1")
	if err != nil {
		t.Fatalf("revoke others: %v", err)
	}
	if n != 2 {
		t.Fatalf("revoke-others removed %d, want 2", n)
	}
	if _, err := sessions.FindByTokenHash(ctx, "a1"); err != nil {
		t.Fatalf("a1 should survive: %v", err)
	}
	if _, err := sessions.FindByTokenHash(ctx, "b1"); err != nil {
		t.Fatalf("b1 should survive: %v", err)
	}

	// Revoke-current drops exactly a1.
	if err := sessions.DeleteByTokenHash(ctx, "a1"); err != nil {
		t.Fatalf("revoke current: %v", err)
	}
	if _, err := sessions.FindByTokenHash(ctx, "a1"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("a1 err = %v, want sql.ErrNoRows", err)
	}

	// Revoke-all for Bob.
	n, err = sessions.DeleteByUserID(ctx, bob.ID)
	if err != nil {
		t.Fatalf("revoke all: %v", err)
	}
	if n != 1 {
		t.Fatalf("revoke-all removed %d, want 1", n)
	}
}

func TestSessionRepo_DeleteExpired(t *testing.T) {
	ctx, sessions, users, _ := setupSessionRepo(t)
	alice, _ := users.Create(ctx, "Alice")
	now := time.Now().UTC().Truncate(time.Second)

	// Live: inside both windows.
	mustCreateSession(t, ctx, sessions, "live", alice.ID, now.Add(24*time.Hour), now)
	// Absolute-expired: past the cap.
	mustCreateSession(t, ctx, sessions, "capped", alice.ID, now.Add(-time.Hour), now)
	// Idle-expired: cap far off, but last_seen older than the idle cutoff.
	mustCreateSession(t, ctx, sessions, "idle", alice.ID, now.Add(24*time.Hour), now.Add(-31*24*time.Hour))

	idleCutoff := now.Add(-30 * 24 * time.Hour)
	n, err := sessions.DeleteExpired(ctx, now, idleCutoff)
	if err != nil {
		t.Fatalf("delete expired: %v", err)
	}
	if n != 2 {
		t.Fatalf("swept %d, want 2", n)
	}
	if _, err := sessions.FindByTokenHash(ctx, "live"); err != nil {
		t.Fatalf("live session swept: %v", err)
	}
	if _, err := sessions.FindByTokenHash(ctx, "capped"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("capped session survived")
	}
	if _, err := sessions.FindByTokenHash(ctx, "idle"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("idle session survived")
	}
}

func TestSessionRepo_CascadesOnUserDelete(t *testing.T) {
	ctx, sessions, users, _ := setupSessionRepo(t)
	alice, _ := users.Create(ctx, "Alice")
	now := time.Now().UTC().Truncate(time.Second)
	mustCreateSession(t, ctx, sessions, "a1", alice.ID, now.Add(24*time.Hour), now)

	if err := users.Delete(ctx, alice.ID); err != nil {
		t.Fatalf("delete user: %v", err)
	}
	if _, err := sessions.FindByTokenHash(ctx, "a1"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("session survived member delete: %v", err)
	}
}
