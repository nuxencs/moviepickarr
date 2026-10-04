package db

import (
	"context"
	"database/sql"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The seed rows hold the three timestamp text formats found in production;
// 007 must turn each into INTEGER epoch seconds.
func TestMigration007_NormalizesTimestamps(t *testing.T) {
	ctx := context.Background()
	pool, err := OpenSQLite(filepath.Join(t.TempDir(), "m.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = pool.Close() }()

	stmts := []string{
		`CREATE TABLE schema_migrations (version INTEGER PRIMARY KEY)`,
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
		`INSERT INTO users (id, name, created_at, updated_at)
			VALUES (1, 'alice', '2025-11-17 15:08:44 +0100 CET', '2025-11-17 15:08:44 +0100 CET')`,
		`INSERT INTO movies (title, status, added_at, added_by_id, watched_at) VALUES
			('cet',   'watched', '2025-11-17 15:08:44 +0100 CET', 1, '2026-03-06 21:28:41.97003533 +0000 UTC'),
			('cest',  'watched', '2025-06-01 10:00:00 +0200 CEST', 1, '2026-03-06 22:00:00 +0100 CET'),
			('bare',  'watched', '2026-06-27 11:20:58', 1, '2026-03-06 21:45:00'),
			('inpool', 'pool',   '2026-06-27 11:20:58', 1, NULL)`,
		`INSERT INTO movie_metadata (movie_id, enriched_at, credits_refreshed_at)
			VALUES (1, '2026-06-27 11:20:58', NULL)`,
		// Raise the AUTOINCREMENT high-water mark, as if the top row was deleted.
		`INSERT INTO movies (id, title, status, added_at, added_by_id) VALUES
			(99, 'deleted-later', 'pool', '2026-06-27 11:20:58', 1)`,
		`DELETE FROM movies WHERE id = 99`,
	}
	for _, stmt := range stmts {
		if _, err := pool.Write.ExecContext(ctx, stmt); err != nil {
			t.Fatal(err)
		}
	}

	content, err := fs.ReadFile(migrationsFS, "migrations/007_timestamps_and_constraints.sql")
	if err != nil {
		t.Fatal(err)
	}
	if err := applyMigrationFKOff(ctx, pool.Write, migration{version: 7, name: "007"}, string(content)); err != nil {
		t.Fatalf("apply 007: %v", err)
	}

	epoch := func(y int, mo time.Month, d, h, mi, s int) int64 {
		return time.Date(y, mo, d, h, mi, s, 0, time.UTC).Unix()
	}
	want := map[string][2]int64{
		"cet":    {epoch(2025, 11, 17, 14, 8, 44), epoch(2026, 3, 6, 21, 28, 41)},
		"cest":   {epoch(2025, 6, 1, 8, 0, 0), epoch(2026, 3, 6, 21, 0, 0)},
		"bare":   {epoch(2026, 6, 27, 11, 20, 58), epoch(2026, 3, 6, 21, 45, 0)},
		"inpool": {epoch(2026, 6, 27, 11, 20, 58), 0},
	}
	for title, w := range want {
		var addedAt int64
		var watchedAt sql.NullInt64
		if err := pool.Read.QueryRowContext(ctx,
			`SELECT added_at, watched_at FROM movies WHERE title = ?`, title,
		).Scan(&addedAt, &watchedAt); err != nil {
			t.Fatal(err)
		}
		if addedAt != w[0] {
			t.Errorf("%s added_at = %d, want %d", title, addedAt, w[0])
		}
		if watchedAt.Int64 != w[1] {
			t.Errorf("%s watched_at = %d, want %d", title, watchedAt.Int64, w[1])
		}
	}

	// Text sort broke the watched list order (the reason for 007).
	rows, err := pool.Read.QueryContext(ctx,
		`SELECT title FROM movies WHERE status='watched' ORDER BY watched_at DESC`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var order []string
	for rows.Next() {
		var title string
		if err := rows.Scan(&title); err != nil {
			t.Fatal(err)
		}
		order = append(order, title)
	}
	if got := strings.Join(order, ","); got != "bare,cet,cest" {
		t.Errorf("watched DESC order = %s, want bare,cet,cest", got)
	}

	var nonInt int
	if err := pool.Read.QueryRowContext(ctx, `
		SELECT (SELECT COUNT(*) FROM movies WHERE typeof(added_at) != 'integer'
			OR (watched_at IS NOT NULL AND typeof(watched_at) != 'integer'))
		+ (SELECT COUNT(*) FROM users WHERE typeof(created_at) != 'integer' OR typeof(updated_at) != 'integer')
		+ (SELECT COUNT(*) FROM movie_metadata WHERE typeof(enriched_at) != 'integer'
			OR (credits_refreshed_at IS NOT NULL AND typeof(credits_refreshed_at) != 'integer'))
	`).Scan(&nonInt); err != nil {
		t.Fatal(err)
	}
	if nonInt != 0 {
		t.Errorf("%d non-integer timestamps remain", nonInt)
	}

	var userCreated int64
	if err := pool.Read.QueryRowContext(ctx, `SELECT created_at FROM users WHERE id = 1`).Scan(&userCreated); err != nil {
		t.Fatal(err)
	}
	if wantUser := epoch(2025, 11, 17, 14, 8, 44); userCreated != wantUser {
		t.Errorf("user created_at = %d, want %d", userCreated, wantUser)
	}

	// The rebuild must not lower AUTOINCREMENT, or deleted ids get reused.
	res, err := pool.Write.ExecContext(ctx,
		`INSERT INTO movies (title, status, added_by_id) VALUES ('fresh', 'pool', 1)`)
	if err != nil {
		t.Fatal(err)
	}
	newID, err := res.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	if newID <= 99 {
		t.Errorf("post-rebuild insert got id %d, want > 99 (sequence not preserved)", newID)
	}
}

func TestMigration007_Constraints(t *testing.T) {
	ctx := context.Background()
	pool, err := OpenSQLite(filepath.Join(t.TempDir(), "m.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = pool.Close() }()

	if err := RunMigrations(ctx, pool.Write); err != nil {
		t.Fatalf("run migrations: %v", err)
	}

	mustExec := func(stmt string, args ...any) {
		t.Helper()
		if _, err := pool.Write.ExecContext(ctx, stmt, args...); err != nil {
			t.Fatalf("exec %s: %v", stmt, err)
		}
	}
	wantErr := func(name, stmt string, args ...any) {
		t.Helper()
		if _, err := pool.Write.ExecContext(ctx, stmt, args...); err == nil {
			t.Errorf("%s: expected constraint error, got none", name)
		}
	}

	mustExec(`INSERT INTO users (name) VALUES ('alice')`)
	mustExec(`INSERT INTO movies (title, status, added_by_id, tmdb_id) VALUES ('Heat', 'pool', 1, 949)`)

	// A raw time.Time binds as TEXT, which STRICT rejects.
	wantErr("raw time.Time bind rejected by STRICT",
		`INSERT INTO movies (title, status, added_at, added_by_id) VALUES ('Bad', 'pool', ?, 1)`,
		time.Date(2026, 7, 7, 12, 0, 0, 0, time.FixedZone("CET", 3600)))
	wantErr("text timestamp rejected by STRICT",
		`INSERT INTO movies (title, status, added_at, added_by_id) VALUES ('Bad', 'pool', '2026-07-07 12:00:00', 1)`)
	wantErr("watched without watched_at rejected",
		`INSERT INTO movies (title, status, added_by_id) VALUES ('Bad', 'watched', 1)`)
	wantErr("watched_at on non-watched rejected",
		`INSERT INTO movies (title, status, added_by_id, watched_at) VALUES ('Bad', 'pool', 1, ?)`, ToUnix(time.Now()))
	wantErr("duplicate tmdb_id rejected",
		`INSERT INTO movies (title, status, added_by_id, tmdb_id) VALUES ('Heat again', 'pool', 1, 949)`)
	wantErr("user delete restricted while movies exist",
		`DELETE FROM users WHERE id = 1`)

	mustExec(`INSERT INTO movies (title, status, added_by_id, watched_at) VALUES ('Seen', 'watched', 1, ?)`,
		ToUnix(time.Now()))
	mustExec(`INSERT INTO movies (title, status, added_by_id) VALUES ('No tmdb yet', 'pool', 1)`)

	mustExec(`INSERT INTO movies (title, status, added_by_id) VALUES ('Now playing', 'current', 1)`)
	wantErr("second current movie rejected",
		`INSERT INTO movies (title, status, added_by_id) VALUES ('Also playing?', 'current', 1)`)

	mustExec(`UPDATE users SET updated_at = 0 WHERE id = 1`)
	mustExec(`UPDATE users SET name = 'alice2' WHERE id = 1`)
	var updatedAt int64
	if err := pool.Read.QueryRowContext(ctx, `SELECT updated_at FROM users WHERE id = 1`).Scan(&updatedAt); err != nil {
		t.Fatal(err)
	}
	if updatedAt == 0 {
		t.Error("rename did not touch users.updated_at")
	}
}
