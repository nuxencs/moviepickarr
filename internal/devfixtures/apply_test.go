package devfixtures

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"moviepickarr/internal/auth"
	"moviepickarr/internal/db"
)

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

func applyRealPlan(t *testing.T, pool *db.Pool, now time.Time) Plan {
	t.Helper()
	ctx := context.Background()

	movies, err := LoadMovies()
	if err != nil {
		t.Fatalf("load movies: %v", err)
	}
	plan, err := BuildPlan(movies, now)
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

func TestLoadMoviesHasEnough(t *testing.T) {
	movies, err := LoadMovies()
	if err != nil {
		t.Fatalf("load movies: %v", err)
	}
	if len(movies) < 206 {
		t.Fatalf("embedded dataset has %d movies, need at least 206 for a full plan", len(movies))
	}
	seen := map[int]bool{}
	for _, f := range movies {
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

	var nextUp sql.NullInt64
	if err := pool.Read.QueryRowContext(ctx, "SELECT user_id FROM next_up WHERE id = 1").Scan(&nextUp); err != nil {
		t.Fatalf("next_up: %v", err)
	}
	if !nextUp.Valid {
		t.Fatal("next_up.user_id is NULL, want an active member")
	}

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

// The version-only migration ledger never repairs objects left by an unmerged
// migration that reused a version number.
func TestRemoveDBDropsSchemaDrift(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "drifted.db")

	pool, err := db.OpenSQLite(path)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.RunMigrations(ctx, pool.Write); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	applyRealPlan(t, pool, time.Now())
	for _, stmt := range []string{
		"CREATE TABLE stray_draft (id INTEGER PRIMARY KEY)",
		"DROP VIEW turn_participants",
	} {
		if _, err := pool.Write.ExecContext(ctx, stmt); err != nil {
			t.Fatalf("drift (%s): %v", stmt, err)
		}
	}
	if err := pool.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	if err := RemoveDB(path); err != nil {
		t.Fatalf("RemoveDB: %v", err)
	}
	for _, f := range []string{path, path + "-wal", path + "-shm"} {
		if _, err := os.Stat(f); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("%s still exists after RemoveDB (stat err %v)", f, err)
		}
	}
	if err := RemoveDB(path); err != nil {
		t.Fatalf("RemoveDB on a missing file: %v", err)
	}

	fresh, err := db.OpenSQLite(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	t.Cleanup(func() { _ = fresh.Close() })
	if err := db.RunMigrations(ctx, fresh.Write); err != nil {
		t.Fatalf("migrate fresh: %v", err)
	}
	var stray, view int
	if err := fresh.Read.QueryRowContext(ctx, `
		SELECT
			(SELECT COUNT(*) FROM sqlite_master WHERE name = 'stray_draft'),
			(SELECT COUNT(*) FROM sqlite_master WHERE type = 'view' AND name = 'turn_participants')
	`).Scan(&stray, &view); err != nil {
		t.Fatalf("read schema: %v", err)
	}
	if stray != 0 || view != 1 {
		t.Fatalf("fresh schema: stray_draft=%d turn_participants=%d, want 0 and 1", stray, view)
	}
	empty, err := IsEmpty(ctx, fresh.Read)
	if err != nil || !empty {
		t.Fatalf("fresh DB empty = %v (err %v), want true", empty, err)
	}
}
