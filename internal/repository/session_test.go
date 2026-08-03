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
		PublicID:   "public-" + hash,
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

func TestSessionRepo_ArchivedSessionIsInvisible(t *testing.T) {
	ctx, sessions, users, pool := setupSessionRepo(t)
	alice, err := users.Create(ctx, "Alice")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	mustCreateSession(t, ctx, sessions, "hash-a", alice.ID, now.Add(24*time.Hour), now)
	markUserArchived(t, ctx, pool, alice.ID)

	if _, err := sessions.FindByTokenHash(ctx, "hash-a"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("find archived session err = %v, want sql.ErrNoRows", err)
	}
}

func TestSessionRepo_CreateRejectsArchivedMember(t *testing.T) {
	ctx, sessions, users, pool := setupSessionRepo(t)
	alice, err := users.Create(ctx, "Alice")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	markUserArchived(t, ctx, pool, alice.ID)
	now := time.Now().UTC().Truncate(time.Second)

	err = sessions.Create(ctx, domain.Session{
		PublicID:   "public-a",
		TokenHash:  "hash-a",
		UserID:     alice.ID,
		ExpiresAt:  now.Add(24 * time.Hour),
		LastSeenAt: now,
		CreatedAt:  now,
	})
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("create archived session err = %v, want ErrNotFound", err)
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

func TestSessionRepo_ListLiveOrdersByActivity(t *testing.T) {
	ctx, sessions, users, _ := setupSessionRepo(t)
	alice, _ := users.Create(ctx, "Alice")
	bob, _ := users.Create(ctx, "Bob")
	now := time.Now().UTC().Truncate(time.Second)
	exp := now.Add(90 * 24 * time.Hour)
	idleCutoff := now.Add(-30 * 24 * time.Hour)

	// Alice: two live sessions with distinct activity, one capped-out, one
	// idle-expired. Bob's live session must never appear in Alice's list.
	mustCreateSession(t, ctx, sessions, "a-stale", alice.ID, exp, now.Add(-2*time.Hour))
	mustCreateSession(t, ctx, sessions, "a-fresh", alice.ID, exp, now)
	mustCreateSession(t, ctx, sessions, "a-capped", alice.ID, now.Add(-time.Hour), now)
	mustCreateSession(t, ctx, sessions, "a-idle", alice.ID, exp, now.Add(-31*24*time.Hour))
	mustCreateSession(t, ctx, sessions, "b1", bob.ID, exp, now)

	live, err := sessions.ListLiveByUserID(ctx, alice.ID, now, idleCutoff)
	if err != nil {
		t.Fatalf("list live: %v", err)
	}
	if len(live) != 2 {
		t.Fatalf("listed %d sessions, want 2 (dead rows and Bob excluded)", len(live))
	}
	if live[0].TokenHash != "a-fresh" || live[1].TokenHash != "a-stale" {
		t.Fatalf("order = %q, %q, want a-fresh then a-stale", live[0].TokenHash, live[1].TokenHash)
	}
	if live[0].UserID != alice.ID {
		t.Fatalf("listed user id = %d, want %d", live[0].UserID, alice.ID)
	}

	// Nothing live at all is an empty list, not a nil-vs-empty distinction the
	// handler has to special-case.
	empty, err := sessions.ListLiveByUserID(ctx, bob.ID, now.Add(200*24*time.Hour), idleCutoff)
	if err != nil {
		t.Fatalf("list live (none): %v", err)
	}
	if len(empty) != 0 {
		t.Fatalf("listed %d sessions past every window, want 0", len(empty))
	}
}

func TestSessionRepo_ListLiveCarriesDeviceFields(t *testing.T) {
	ctx, sessions, users, _ := setupSessionRepo(t)
	alice, _ := users.Create(ctx, "Alice")
	now := time.Now().UTC().Truncate(time.Second)

	ua := "Mozilla/5.0 (Macintosh) Chrome/126.0.0.0"
	if err := sessions.Create(ctx, domain.Session{
		PublicID: "public-a1", TokenHash: "a1", UserID: alice.ID, ExpiresAt: now.Add(24 * time.Hour),
		LastSeenAt: now, CreatedAt: now.Add(-time.Hour), UserAgent: &ua,
	}); err != nil {
		t.Fatalf("create session: %v", err)
	}
	// A session with neither recorded (an API client, a stripped agent) stays
	// nil rather than becoming an empty string.
	mustCreateSession(t, ctx, sessions, "a2", alice.ID, now.Add(24*time.Hour), now.Add(-time.Minute))

	live, err := sessions.ListLiveByUserID(ctx, alice.ID, now, now.Add(-30*24*time.Hour))
	if err != nil {
		t.Fatalf("list live: %v", err)
	}
	if len(live) != 2 {
		t.Fatalf("listed %d sessions, want 2", len(live))
	}
	if live[0].UserAgent == nil || *live[0].UserAgent != ua {
		t.Errorf("user agent = %v, want %q", live[0].UserAgent, ua)
	}
	if !live[0].CreatedAt.Equal(now.Add(-time.Hour)) {
		t.Errorf("created at = %v, want %v", live[0].CreatedAt, now.Add(-time.Hour))
	}
	if live[1].UserAgent != nil {
		t.Errorf("missing agent scanned as %v, want nil", live[1].UserAgent)
	}
}

func TestSessionRepo_DeleteByPublicIDForUserIsScopedToOwner(t *testing.T) {
	ctx, sessions, users, _ := setupSessionRepo(t)
	alice, _ := users.Create(ctx, "Alice")
	bob, _ := users.Create(ctx, "Bob")
	now := time.Now().UTC().Truncate(time.Second)
	exp := now.Add(90 * 24 * time.Hour)

	mustCreateSession(t, ctx, sessions, "a1", alice.ID, exp, now)
	mustCreateSession(t, ctx, sessions, "b1", bob.ID, exp, now)

	live, err := sessions.ListLiveByUserID(ctx, bob.ID, now, now.Add(-30*24*time.Hour))
	if err != nil {
		t.Fatalf("list live: %v", err)
	}
	bobSessionID := live[0].PublicID

	// Alice aiming at Bob's public handle removes nothing and reports nothing removed.
	hash, err := sessions.DeleteByPublicIDForUser(ctx, bobSessionID, alice.ID)
	if err != nil {
		t.Fatalf("delete another member's session: %v", err)
	}
	if hash != "" {
		t.Fatalf("deleted hash = %q, want empty (nothing removed)", hash)
	}
	if _, err := sessions.FindByTokenHash(ctx, "b1"); err != nil {
		t.Fatalf("another member's session was removed: %v", err)
	}

	// Its owner removes it, and learns which row went.
	hash, err = sessions.DeleteByPublicIDForUser(ctx, bobSessionID, bob.ID)
	if err != nil {
		t.Fatalf("delete own session: %v", err)
	}
	if hash != "b1" {
		t.Fatalf("deleted hash = %q, want b1", hash)
	}
	if _, err := sessions.FindByTokenHash(ctx, "b1"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("own session survived the delete")
	}
}

func TestSessionRepo_StalePublicIDCannotDeleteAReusedRowID(t *testing.T) {
	ctx, sessions, users, _ := setupSessionRepo(t)
	alice, _ := users.Create(ctx, "Alice")
	now := time.Now().UTC().Truncate(time.Second)
	expires := now.Add(90 * 24 * time.Hour)

	mustCreateSession(t, ctx, sessions, "old", alice.ID, expires, now)
	oldRows, err := sessions.ListLiveByUserID(ctx, alice.ID, now, now.Add(-30*24*time.Hour))
	if err != nil {
		t.Fatalf("list old session: %v", err)
	}
	oldRowID := oldRows[0].ID
	stalePublicID := oldRows[0].PublicID
	if _, err := sessions.DeleteByPublicIDForUser(ctx, stalePublicID, alice.ID); err != nil {
		t.Fatalf("delete old session: %v", err)
	}

	mustCreateSession(t, ctx, sessions, "new", alice.ID, expires, now)
	newRows, err := sessions.ListLiveByUserID(ctx, alice.ID, now, now.Add(-30*24*time.Hour))
	if err != nil {
		t.Fatalf("list new session: %v", err)
	}
	if newRows[0].ID != oldRowID {
		t.Fatalf("internal row id = %d, want reused id %d for the regression setup", newRows[0].ID, oldRowID)
	}
	if newRows[0].PublicID == stalePublicID {
		t.Fatal("new session reused the deleted public id")
	}

	deleted, err := sessions.DeleteByPublicIDForUser(ctx, stalePublicID, alice.ID)
	if err != nil {
		t.Fatalf("stale delete: %v", err)
	}
	if deleted != "" {
		t.Fatalf("stale delete removed token hash %q", deleted)
	}
	if _, err := sessions.FindByTokenHash(ctx, "new"); err != nil {
		t.Fatalf("new session did not survive stale delete: %v", err)
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

	// Alice authored no movies, so Remove hard-deletes her row and the session
	// cascades away with it.
	outcome, err := users.Remove(ctx, alice.ID)
	if err != nil {
		t.Fatalf("remove user: %v", err)
	}
	if outcome != domain.OutcomeDeleted {
		t.Fatalf("expected deleted outcome, got %q", outcome)
	}
	if _, err := sessions.FindByTokenHash(ctx, "a1"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("session survived member delete: %v", err)
	}
}
