package db

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func listBackups(t *testing.T, dbPath string) []string {
	t.Helper()
	matches, err := filepath.Glob(dbPath + ".v*" + backupSuffix)
	if err != nil {
		t.Fatal(err)
	}
	return matches
}

// TestRunMigrationsWithBackup_FreshDB: a brand-new database has all migrations
// pending but nothing applied — there is nothing worth snapshotting, so no
// backup file may appear.
func TestRunMigrationsWithBackup_FreshDB(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "m.db")
	pool, err := OpenSQLite(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = pool.Close() }()

	if err := RunMigrationsWithBackup(ctx, pool.Write, BackupConfig{Path: dbPath, MaxBackups: 3}); err != nil {
		t.Fatalf("run migrations: %v", err)
	}
	if got := listBackups(t, dbPath); len(got) != 0 {
		t.Errorf("fresh DB produced backups: %v", got)
	}
}

// TestRunMigrationsWithBackup_UpToDate: nothing pending means the backup path
// is never entered, no matter how often startup runs.
func TestRunMigrationsWithBackup_UpToDate(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "m.db")
	pool, err := OpenSQLite(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = pool.Close() }()

	if err := RunMigrations(ctx, pool.Write); err != nil {
		t.Fatalf("run migrations: %v", err)
	}
	if err := RunMigrationsWithBackup(ctx, pool.Write, BackupConfig{Path: dbPath, MaxBackups: 3}); err != nil {
		t.Fatalf("rerun migrations: %v", err)
	}
	if got := listBackups(t, dbPath); len(got) != 0 {
		t.Errorf("up-to-date DB produced backups: %v", got)
	}
}

// TestRunMigrationsWithBackup_PendingCreatesBackup seeds a post-006 database
// (versions 1..6 applied, 007 pending) and asserts the pre-007 state is
// snapshotted before the rebuild — and that the snapshot is a readable SQLite
// file still holding the old schema.
func TestRunMigrationsWithBackup_PendingCreatesBackup(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "m.db")
	pool, err := OpenSQLite(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = pool.Close() }()

	stmts := []string{
		`CREATE TABLE schema_migrations (version INTEGER PRIMARY KEY)`,
		`INSERT INTO schema_migrations (version) VALUES (1), (3), (4), (5), (6)`,
		`CREATE TABLE users (
			id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT NOT NULL UNIQUE,
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP)`,
		`CREATE TABLE movies (
			id INTEGER PRIMARY KEY AUTOINCREMENT, title TEXT NOT NULL,
			status TEXT NOT NULL, added_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			added_by_id INTEGER NOT NULL, watched_at TIMESTAMP, tmdb_id INTEGER, imdb_id TEXT,
			FOREIGN KEY (added_by_id) REFERENCES users(id) ON UPDATE CASCADE ON DELETE CASCADE)`,
		`CREATE TABLE movie_metadata (
			movie_id INTEGER PRIMARY KEY,
			overview TEXT NOT NULL DEFAULT '', poster_path TEXT, backdrop_path TEXT,
			release_date TEXT NOT NULL DEFAULT '', runtime INTEGER NOT NULL DEFAULT 0,
			genres TEXT NOT NULL DEFAULT '[]', vote_average REAL NOT NULL DEFAULT 0,
			vote_count INTEGER NOT NULL DEFAULT 0, tagline TEXT NOT NULL DEFAULT '',
			enriched_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			credits_refreshed_at TIMESTAMP,
			FOREIGN KEY (movie_id) REFERENCES movies(id) ON UPDATE CASCADE ON DELETE CASCADE)`,
		`INSERT INTO users (id, name) VALUES (1, 'alice')`,
		`INSERT INTO movies (title, status, added_at, added_by_id) VALUES
			('pre-backup', 'pool', '2026-06-27 11:20:58', 1)`,
	}
	for _, stmt := range stmts {
		if _, err := pool.Write.ExecContext(ctx, stmt); err != nil {
			t.Fatal(err)
		}
	}

	if err := RunMigrationsWithBackup(ctx, pool.Write, BackupConfig{Path: dbPath, MaxBackups: 3}); err != nil {
		t.Fatalf("run migrations: %v", err)
	}

	backups := listBackups(t, dbPath)
	if len(backups) != 1 {
		t.Fatalf("got %d backups, want 1: %v", len(backups), backups)
	}
	if !strings.Contains(filepath.Base(backups[0]), ".v006-") {
		t.Errorf("backup %s does not carry pre-migration version v006", backups[0])
	}

	// The live DB moved on to 007 (epoch INTEGER timestamps)...
	var liveType string
	if err := pool.Read.QueryRowContext(ctx,
		`SELECT typeof(added_at) FROM movies WHERE title = 'pre-backup'`).Scan(&liveType); err != nil {
		t.Fatal(err)
	}
	if liveType != "integer" {
		t.Errorf("live DB added_at type = %s, want integer (007 applied)", liveType)
	}

	// ...while the snapshot still holds the pre-007 state and opens cleanly.
	snap, err := OpenSQLite(backups[0])
	if err != nil {
		t.Fatalf("open backup: %v", err)
	}
	defer func() { _ = snap.Close() }()

	var version int
	if err := snap.Read.QueryRowContext(ctx,
		`SELECT MAX(version) FROM schema_migrations`).Scan(&version); err != nil {
		t.Fatal(err)
	}
	if version != 6 {
		t.Errorf("backup schema version = %d, want 6", version)
	}
	var snapType string
	if err := snap.Read.QueryRowContext(ctx,
		`SELECT typeof(added_at) FROM movies WHERE title = 'pre-backup'`).Scan(&snapType); err != nil {
		t.Fatal(err)
	}
	if snapType != "text" {
		t.Errorf("backup added_at type = %s, want text (pre-007 state)", snapType)
	}
}

// TestCleanupBackups_PrunesOldest fabricates backup files (retention only
// looks at names) and checks the newest max survive, oldest go first, and
// unrelated files are untouched.
func TestCleanupBackups_PrunesOldest(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "m.db")

	names := []string{
		"m.db.v004-20260101T000000Z.backup",
		"m.db.v005-20260201T000000Z.backup",
		"m.db.v006-20260301T000000Z.backup",
		"m.db.v006-20260401T000000Z.backup",
		"m.db",         // the live DB itself
		"m.db.v-notes", // prefix match but wrong suffix
		"other.backup", // suffix match but wrong prefix
	}
	for _, name := range names {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	if err := cleanupBackups(dbPath, 2); err != nil {
		t.Fatal(err)
	}

	got := listBackups(t, dbPath)
	want := []string{
		filepath.Join(dir, "m.db.v006-20260301T000000Z.backup"),
		filepath.Join(dir, "m.db.v006-20260401T000000Z.backup"),
	}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("surviving backups = %v, want %v", got, want)
	}
	for _, name := range []string{"m.db", "m.db.v-notes", "other.backup"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Errorf("unrelated file %s was removed: %v", name, err)
		}
	}
}
