package seed

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"

	"moviepickarr/internal/auth"
	"moviepickarr/internal/db"
	"moviepickarr/internal/repository"

	"github.com/rs/zerolog"
)

// setup opens a real temp SQLite DB, runs every migration (through 009 so the
// role column and local_accounts table exist), and returns the seed repo plus
// the raw pool for direct row assertions. Real DB, real repo, no mocks: the
// seed is a boot-time step with no HTTP seam, so it is exercised directly.
func setup(t *testing.T) (*repository.SqliteAdminSeedRepository, *db.Pool) {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "seed-test.db")
	pool, err := db.OpenSQLite(dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.RunMigrations(context.Background(), pool.Write); err != nil {
		t.Fatalf("run migrations: %v", err)
	}
	t.Cleanup(func() {
		if err := pool.Close(); err != nil {
			t.Fatalf("close sqlite: %v", err)
		}
	})

	return repository.NewSqliteAdminSeedRepository(pool), pool
}

// createMember inserts a member row with the given name and role, returning its
// id, the fixture for the adopt/ambiguous paths.
func createMember(t *testing.T, pool *db.Pool, name, role string) int {
	t.Helper()
	res, err := pool.Write.ExecContext(context.Background(),
		"INSERT INTO users (name, role) VALUES (?, ?)", name, role)
	if err != nil {
		t.Fatalf("insert member %q: %v", name, err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("last insert id: %v", err)
	}
	return int(id)
}

type localLogin struct {
	userID       int
	username     string
	passwordHash string
}

// readLocalLogins returns every local_accounts row, ordered by user_id, so a
// test can assert on both the count and the stored credential.
func readLocalLogins(t *testing.T, pool *db.Pool) []localLogin {
	t.Helper()
	rows, err := pool.Read.QueryContext(context.Background(),
		"SELECT user_id, username, password_hash FROM local_accounts ORDER BY user_id")
	if err != nil {
		t.Fatalf("query local_accounts: %v", err)
	}
	defer rows.Close()

	var logins []localLogin
	for rows.Next() {
		var l localLogin
		if err := rows.Scan(&l.userID, &l.username, &l.passwordHash); err != nil {
			t.Fatalf("scan local_accounts: %v", err)
		}
		logins = append(logins, l)
	}
	return logins
}

func roleOf(t *testing.T, pool *db.Pool, id int) string {
	t.Helper()
	var role string
	err := pool.Read.QueryRowContext(context.Background(),
		"SELECT role FROM users WHERE id = ?", id).Scan(&role)
	if err != nil {
		t.Fatalf("read role for %d: %v", id, err)
	}
	return role
}

func countUsers(t *testing.T, pool *db.Pool) int {
	t.Helper()
	var n int
	if err := pool.Read.QueryRowContext(context.Background(),
		"SELECT COUNT(*) FROM users").Scan(&n); err != nil {
		t.Fatalf("count users: %v", err)
	}
	return n
}

var testCfg = AdminConfig{Name: "Ada", Username: "ada", Password: "correct-horse"}

// TestFreshDBCreatesWorkingAdmin covers acceptance criterion 1: on a fresh DB,
// the trio creates an admin member with a working local login.
func TestFreshDBCreatesWorkingAdmin(t *testing.T) {
	repo, pool := setup(t)
	ctx := context.Background()

	if err := BreakGlassAdmin(ctx, repo, testCfg, true, zerolog.Nop()); err != nil {
		t.Fatalf("seed: %v", err)
	}

	if got := countUsers(t, pool); got != 1 {
		t.Fatalf("user count = %d, want 1", got)
	}
	logins := readLocalLogins(t, pool)
	if len(logins) != 1 {
		t.Fatalf("local_accounts count = %d, want 1", len(logins))
	}
	if roleOf(t, pool, logins[0].userID) != "admin" {
		t.Fatalf("seeded member is not an admin")
	}
	if logins[0].username != testCfg.Username {
		t.Fatalf("username = %q, want %q", logins[0].username, testCfg.Username)
	}
	// The stored hash must verify against the seeded password: a working login.
	match, _, err := auth.VerifyPassword(testCfg.Password, logins[0].passwordHash)
	if err != nil || !match {
		t.Fatalf("seeded password does not verify: match=%v err=%v", match, err)
	}
}

// TestReRunIsNoOp covers acceptance criterion 2: re-running boot with the same
// env never overwrites the existing password and creates no duplicate row.
func TestReRunIsNoOp(t *testing.T) {
	repo, pool := setup(t)
	ctx := context.Background()

	if err := BreakGlassAdmin(ctx, repo, testCfg, true, zerolog.Nop()); err != nil {
		t.Fatalf("first seed: %v", err)
	}
	firstHash := readLocalLogins(t, pool)[0].passwordHash

	if err := BreakGlassAdmin(ctx, repo, testCfg, true, zerolog.Nop()); err != nil {
		t.Fatalf("second seed: %v", err)
	}

	if got := countUsers(t, pool); got != 1 {
		t.Fatalf("user count after re-run = %d, want 1", got)
	}
	logins := readLocalLogins(t, pool)
	if len(logins) != 1 {
		t.Fatalf("local_accounts count after re-run = %d, want 1", len(logins))
	}
	if logins[0].passwordHash != firstHash {
		t.Fatalf("password hash changed on re-run: seed clobbered the existing password")
	}
}

// TestAdoptsExistingMemberCaseInsensitive covers acceptance criterion 3: an
// existing member matching the name case-insensitively is adopted and ensured
// admin, preserving its identity (same row).
func TestAdoptsExistingMemberCaseInsensitive(t *testing.T) {
	repo, pool := setup(t)
	ctx := context.Background()

	// Existing member differs only in case and is not yet an admin.
	existingID := createMember(t, pool, "ada", "member")

	if err := BreakGlassAdmin(ctx, repo, testCfg, true, zerolog.Nop()); err != nil {
		t.Fatalf("seed: %v", err)
	}

	if got := countUsers(t, pool); got != 1 {
		t.Fatalf("user count = %d, want 1 (adopted, not duplicated)", got)
	}
	if roleOf(t, pool, existingID) != "admin" {
		t.Fatalf("adopted member was not promoted to admin")
	}
	logins := readLocalLogins(t, pool)
	if len(logins) != 1 || logins[0].userID != existingID {
		t.Fatalf("local login not attached to the adopted member: %+v", logins)
	}
}

// TestAdoptedMemberPasswordPreserved covers the non-clobber rule on the adopt
// path: an already-present local login keeps its password.
func TestAdoptedMemberPasswordPreserved(t *testing.T) {
	repo, pool := setup(t)
	ctx := context.Background()

	id := createMember(t, pool, "Ada", "member")
	existingHash, err := auth.HashPassword("original-password")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	if err := repo.CreateLocalAccount(ctx, id, "ada-old", existingHash); err != nil {
		t.Fatalf("seed existing login: %v", err)
	}

	if err := BreakGlassAdmin(ctx, repo, testCfg, true, zerolog.Nop()); err != nil {
		t.Fatalf("seed: %v", err)
	}

	if roleOf(t, pool, id) != "admin" {
		t.Fatalf("member not promoted to admin")
	}
	logins := readLocalLogins(t, pool)
	if len(logins) != 1 {
		t.Fatalf("local_accounts count = %d, want 1 (no new row)", len(logins))
	}
	if logins[0].passwordHash != existingHash || logins[0].username != "ada-old" {
		t.Fatalf("existing local login was overwritten: %+v", logins[0])
	}
}

// An archived member is an authentication tombstone, not an adoptable seed
// target. This is the documented "leave the seed configured" restart path:
// seed an admin, archive them, then boot again with the same trio.
func TestArchivedMemberIsNotReAdoptedOnRestart(t *testing.T) {
	repo, pool := setup(t)
	ctx := context.Background()

	if err := BreakGlassAdmin(ctx, repo, testCfg, true, zerolog.Nop()); err != nil {
		t.Fatalf("initial seed: %v", err)
	}
	adminID := readLocalLogins(t, pool)[0].userID

	movies := repository.NewSqliteMoviesRepository(pool)
	if _, err := movies.Add(ctx, "Arrival", "pool", adminID); err != nil {
		t.Fatalf("add authored movie: %v", err)
	}
	users := repository.NewSqliteUserRepository(pool)
	if _, err := users.Remove(ctx, adminID); err != nil {
		t.Fatalf("archive seeded admin: %v", err)
	}

	if got := len(readLocalLogins(t, pool)); got != 0 {
		t.Fatalf("archive left %d local logins, want 0", got)
	}
	err := BreakGlassAdmin(ctx, repo, testCfg, true, zerolog.Nop())
	if err == nil {
		t.Fatal("restart seed adopted an archived member, want boot error")
	}
	for _, key := range []string{"MPA_ADMIN_NAME", "MPA_ADMIN_USERNAME"} {
		if !strings.Contains(err.Error(), key) {
			t.Fatalf("restart error %q does not identify %s as part of the recovery", err, key)
		}
	}
	if got := len(readLocalLogins(t, pool)); got != 0 {
		t.Fatalf("restart recredentialed archived member: %d logins", got)
	}
	if role := roleOf(t, pool, adminID); role != "admin" {
		t.Fatalf("archived role changed to %q, want preserved admin", role)
	}
}

// TestAmbiguousMatchSkips covers acceptance criterion 4a: when several members
// fold to the same name, the seed skips without touching anything and without
// failing boot.
func TestAmbiguousMatchSkips(t *testing.T) {
	repo, pool := setup(t)
	ctx := context.Background()

	createMember(t, pool, "Ada", "member")
	createMember(t, pool, "ada", "member")

	if err := BreakGlassAdmin(ctx, repo, testCfg, true, zerolog.Nop()); err != nil {
		t.Fatalf("ambiguous match should skip, not error: %v", err)
	}

	admins, err := repo.CountAdmins(ctx)
	if err != nil {
		t.Fatalf("count admins: %v", err)
	}
	if admins != 0 {
		t.Fatalf("ambiguous match promoted a member: admins = %d, want 0", admins)
	}
	if got := len(readLocalLogins(t, pool)); got != 0 {
		t.Fatalf("ambiguous match attached a login: %d rows, want 0", got)
	}
}

// TestSeedErrorFailsBoot covers acceptance criterion 4b: with the seed
// configured, a persistence failure surfaces as a boot error. Here the seeded
// username already belongs to another member, so attaching the login hits the
// NOCASE-unique constraint.
func TestSeedErrorFailsBoot(t *testing.T) {
	repo, pool := setup(t)
	ctx := context.Background()

	// Another member already owns the seed username (case-folded collision).
	otherID := createMember(t, pool, "Someone Else", "member")
	otherHash, err := auth.HashPassword("whatever")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	if err := repo.CreateLocalAccount(ctx, otherID, "ADA", otherHash); err != nil {
		t.Fatalf("seed conflicting login: %v", err)
	}

	err = BreakGlassAdmin(ctx, repo, testCfg, true, zerolog.Nop())
	if err == nil {
		t.Fatalf("expected boot to fail loudly on a seed persistence error, got nil")
	}
}

// TestShortPasswordFailsBoot covers the seed-path password bound: a configured
// trio with an out-of-range password fails boot loudly and writes nothing,
// rather than hashing it unchecked.
func TestShortPasswordFailsBoot(t *testing.T) {
	repo, pool := setup(t)
	ctx := context.Background()

	cfg := AdminConfig{Name: "Ada", Username: "ada", Password: "short"} // 5 chars, below the min of 8

	if err := BreakGlassAdmin(ctx, repo, cfg, true, zerolog.Nop()); err == nil {
		t.Fatalf("expected boot to fail on a too-short seeded password, got nil")
	}
	if got := countUsers(t, pool); got != 0 {
		t.Fatalf("failed validation still wrote %d users, want 0", got)
	}
}

// TestNoSeedWarnsWhenZeroAdmins covers acceptance criterion 4c: with no seed
// configured and zero admins, boot proceeds (no error) but the caller is
// expected to warn. We assert the non-fatal contract and that nothing is
// written.
func TestNoSeedWarnsWhenZeroAdmins(t *testing.T) {
	repo, pool := setup(t)
	ctx := context.Background()

	if err := BreakGlassAdmin(ctx, repo, AdminConfig{}, false, zerolog.Nop()); err != nil {
		t.Fatalf("no-seed path must not fail boot: %v", err)
	}
	if got := countUsers(t, pool); got != 0 {
		t.Fatalf("no-seed path wrote %d users, want 0", got)
	}
}

// TestNoSeedWithExistingAdminIsQuietNoOp confirms the no-seed path is a plain
// no-op when an admin already exists.
func TestNoSeedWithExistingAdminIsQuietNoOp(t *testing.T) {
	repo, pool := setup(t)
	ctx := context.Background()

	createMember(t, pool, "Existing Admin", "admin")

	if err := BreakGlassAdmin(ctx, repo, AdminConfig{}, false, zerolog.Nop()); err != nil {
		t.Fatalf("no-seed path must not fail: %v", err)
	}
	if got := countUsers(t, pool); got != 1 {
		t.Fatalf("no-seed path mutated the roster: %d users, want 1", got)
	}
}

func TestArchivedAdminDoesNotSuppressNoActiveAdminWarning(t *testing.T) {
	repo, pool := setup(t)
	ctx := context.Background()

	adminID := createMember(t, pool, "Former Admin", "admin")
	movies := repository.NewSqliteMoviesRepository(pool)
	if _, err := movies.Add(ctx, "Heat", "pool", adminID); err != nil {
		t.Fatalf("add authored movie: %v", err)
	}
	users := repository.NewSqliteUserRepository(pool)
	if _, err := users.Remove(ctx, adminID); err != nil {
		t.Fatalf("archive admin: %v", err)
	}

	var logs bytes.Buffer
	if err := BreakGlassAdmin(ctx, repo, AdminConfig{}, false, zerolog.New(&logs)); err != nil {
		t.Fatalf("no-seed path: %v", err)
	}
	if !strings.Contains(logs.String(), "no admin members exist") {
		t.Fatalf("missing zero-active-admin warning: %s", logs.String())
	}
}

func TestAdminConfigFromEnv(t *testing.T) {
	cases := []struct {
		name           string
		env            map[string]string
		wantConfigured bool
		want           AdminConfig
	}{
		{
			name:           "all three set",
			env:            map[string]string{"MPA_ADMIN_NAME": "Ada", "MPA_ADMIN_USERNAME": "ada", "MPA_ADMIN_PASSWORD": "pw"},
			wantConfigured: true,
			want:           AdminConfig{Name: "Ada", Username: "ada", Password: "pw"},
		},
		{
			name:           "trims whitespace",
			env:            map[string]string{"MPA_ADMIN_NAME": "  Ada  ", "MPA_ADMIN_USERNAME": " ada ", "MPA_ADMIN_PASSWORD": " pw "},
			wantConfigured: true,
			want:           AdminConfig{Name: "Ada", Username: "ada", Password: "pw"},
		},
		{
			name:           "partial is not configured",
			env:            map[string]string{"MPA_ADMIN_NAME": "Ada", "MPA_ADMIN_USERNAME": "ada"},
			wantConfigured: false,
		},
		{
			name:           "none set",
			env:            map[string]string{},
			wantConfigured: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, k := range []string{"MPA_ADMIN_NAME", "MPA_ADMIN_USERNAME", "MPA_ADMIN_PASSWORD"} {
				t.Setenv(k, "")
			}
			for k, v := range tc.env {
				t.Setenv(k, v)
			}

			cfg, configured := AdminConfigFromEnv(zerolog.Nop())
			if configured != tc.wantConfigured {
				t.Fatalf("configured = %v, want %v", configured, tc.wantConfigured)
			}
			if configured && cfg != tc.want {
				t.Fatalf("cfg = %+v, want %+v", cfg, tc.want)
			}
		})
	}
}
