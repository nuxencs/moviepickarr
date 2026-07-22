package devfixtures

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"moviepickarr/internal/auth"
	"moviepickarr/internal/db"
)

// migratedPool opens a fresh temp DB with the full schema applied.
func migratedPool(t *testing.T) *db.Pool {
	t.Helper()
	ctx := context.Background()
	pool, err := db.OpenSQLite(filepath.Join(t.TempDir(), "fixtures-test.db"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = pool.Close() })
	if err := db.RunMigrations(ctx, pool.Write); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return pool
}

// applyRealPlan builds and applies the real embedded plan into a migrated DB.
func applyRealPlan(t *testing.T, pool *db.Pool, now time.Time) Plan {
	t.Helper()
	ctx := context.Background()

	films, err := LoadFilms()
	if err != nil {
		t.Fatalf("load films: %v", err)
	}
	plan, err := BuildPlan(films, now)
	if err != nil {
		t.Fatalf("build plan: %v", err)
	}

	tx, err := pool.Write.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if err := Apply(ctx, tx, plan, now); err != nil {
		_ = tx.Rollback()
		t.Fatalf("apply: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}
	return plan
}

func TestLoadFilmsHasEnough(t *testing.T) {
	films, err := LoadFilms()
	if err != nil {
		t.Fatalf("load films: %v", err)
	}
	if len(films) < 206 {
		t.Fatalf("embedded dataset has %d films, need at least 206 for a full plan", len(films))
	}
	seen := map[int]bool{}
	for _, f := range films {
		if seen[f.TMDBID] {
			t.Fatalf("embedded dataset has a duplicate tmdb_id %d", f.TMDBID)
		}
		seen[f.TMDBID] = true
	}
}

func TestApplyWritesFullWorld(t *testing.T) {
	ctx := context.Background()
	pool := migratedPool(t)
	now := time.Now()
	plan := applyRealPlan(t, pool, now)

	count := func(q string, args ...any) int {
		t.Helper()
		var n int
		if err := pool.Read.QueryRowContext(ctx, q, args...).Scan(&n); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
		return n
	}

	if got := count("SELECT COUNT(*) FROM users"); got != len(plan.Members) {
		t.Errorf("users = %d, want %d", got, len(plan.Members))
	}
	if got := count("SELECT COUNT(*) FROM movies"); got != len(plan.Movies) {
		t.Errorf("movies = %d, want %d", got, len(plan.Movies))
	}
	if got := count("SELECT COUNT(*) FROM local_accounts"); got != len(loginMemberIndices) {
		t.Errorf("local_accounts = %d, want %d", got, len(loginMemberIndices))
	}
	if got := count("SELECT COUNT(*) FROM users WHERE role = 'admin'"); got != 1 {
		t.Errorf("admins = %d, want 1", got)
	}
	if got := count("SELECT COUNT(*) FROM users WHERE archived_at IS NOT NULL"); got != 1 {
		t.Errorf("archived = %d, want 1", got)
	}
	if got := count("SELECT COUNT(*) FROM movies WHERE status = 'watched'"); got != watchedCount {
		t.Errorf("watched = %d, want %d", got, watchedCount)
	}
	if got := count("SELECT COUNT(*) FROM movies WHERE status = 'current'"); got != 0 {
		t.Errorf("current = %d, want 0", got)
	}

	// next_up points at an active login member.
	var nextUp sql.NullInt64
	if err := pool.Read.QueryRowContext(ctx, "SELECT user_id FROM next_up WHERE id = 1").Scan(&nextUp); err != nil {
		t.Fatalf("next_up: %v", err)
	}
	if !nextUp.Valid {
		t.Fatal("next_up.user_id is NULL, want an active member")
	}

	// pool_locked seeded false.
	var locked string
	if err := pool.Read.QueryRowContext(ctx, "SELECT value FROM settings WHERE key = 'pool_locked'").Scan(&locked); err != nil {
		t.Fatalf("pool_locked: %v", err)
	}
	if locked != "false" {
		t.Errorf("pool_locked = %q, want false", locked)
	}
}

func TestApplySeededLoginsVerify(t *testing.T) {
	ctx := context.Background()
	pool := migratedPool(t)
	applyRealPlan(t, pool, time.Now())

	// Every seeded login must actually authenticate with the dev password.
	rows, err := pool.Read.QueryContext(ctx, "SELECT username, password_hash FROM local_accounts")
	if err != nil {
		t.Fatalf("query logins: %v", err)
	}
	defer rows.Close()

	n := 0
	for rows.Next() {
		var username, hash string
		if err := rows.Scan(&username, &hash); err != nil {
			t.Fatalf("scan: %v", err)
		}
		match, _, err := auth.VerifyPassword(devPassword, hash)
		if err != nil {
			t.Fatalf("verify %q: %v", username, err)
		}
		if !match {
			t.Errorf("login %q does not verify with the dev password", username)
		}
		n++
	}
	if n != len(loginMemberIndices) {
		t.Errorf("verified %d logins, want %d", n, len(loginMemberIndices))
	}
}

func TestIsEmpty(t *testing.T) {
	ctx := context.Background()
	pool := migratedPool(t)

	empty, err := IsEmpty(ctx, pool.Read)
	if err != nil {
		t.Fatalf("IsEmpty: %v", err)
	}
	if !empty {
		t.Fatal("freshly migrated DB should read as empty")
	}

	applyRealPlan(t, pool, time.Now())

	empty, err = IsEmpty(ctx, pool.Read)
	if err != nil {
		t.Fatalf("IsEmpty: %v", err)
	}
	if empty {
		t.Fatal("populated DB should not read as empty")
	}
}

func TestWipeResetsToEmptyAndReseedsIdentically(t *testing.T) {
	ctx := context.Background()
	pool := migratedPool(t)
	now := time.Now()

	applyRealPlan(t, pool, now)

	tx, err := pool.Write.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if err := Wipe(ctx, tx); err != nil {
		_ = tx.Rollback()
		t.Fatalf("wipe: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}

	empty, err := IsEmpty(ctx, pool.Read)
	if err != nil {
		t.Fatalf("IsEmpty: %v", err)
	}
	if !empty {
		t.Fatal("wipe left rows behind")
	}

	// Reseed after wipe: ids restart from 1 because the sequence was reset.
	applyRealPlan(t, pool, now)
	var minID int
	if err := pool.Read.QueryRowContext(ctx, "SELECT MIN(id) FROM users").Scan(&minID); err != nil {
		t.Fatalf("min id: %v", err)
	}
	if minID != 1 {
		t.Errorf("first member id = %d after reseed, want 1 (sequence not reset)", minID)
	}
}
