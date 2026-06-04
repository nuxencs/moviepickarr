package db

import (
	"context"
	"io/fs"
	"path/filepath"
	"testing"
)

// TestRunMigrations_DropsLinkColumn runs the full chain on a fresh DB and
// confirms 005 applied: the link column is gone and version 5 is recorded.
func TestRunMigrations_DropsLinkColumn(t *testing.T) {
	ctx := context.Background()
	conn, err := OpenSQLite(filepath.Join(t.TempDir(), "m.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()

	if err := RunMigrations(ctx, conn); err != nil {
		t.Fatalf("run migrations: %v", err)
	}

	var linkCols int
	if err := conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM pragma_table_info('movies') WHERE name='link'`).Scan(&linkCols); err != nil {
		t.Fatal(err)
	}
	if linkCols != 0 {
		t.Fatalf("expected link column dropped, found %d", linkCols)
	}

	var v5 int
	if err := conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM schema_migrations WHERE version=5`).Scan(&v5); err != nil {
		t.Fatal(err)
	}
	if v5 != 1 {
		t.Fatalf("expected migration version 5 recorded, got %d", v5)
	}
}

// TestMigration005_BackfillsIMDbFromLink exercises the 005 SQL directly against
// a post-004 schema seeded with legacy rows, then asserts the backfill + drop.
func TestMigration005_BackfillsIMDbFromLink(t *testing.T) {
	ctx := context.Background()
	conn, err := OpenSQLite(filepath.Join(t.TempDir(), "m.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()

	// Post-004 movies shape (FK omitted; this test targets only the 005 SQL).
	if _, err := conn.ExecContext(ctx, `CREATE TABLE movies (
		id INTEGER PRIMARY KEY AUTOINCREMENT, title TEXT NOT NULL, link TEXT NOT NULL,
		status TEXT NOT NULL, added_by_id INTEGER NOT NULL, tmdb_id INTEGER, imdb_id TEXT)`); err != nil {
		t.Fatal(err)
	}
	seed := func(title, link string, tmdb *int) {
		if _, err := conn.ExecContext(ctx,
			`INSERT INTO movies (title, link, status, added_by_id, tmdb_id) VALUES (?, ?, 'pool', 1, ?)`,
			title, link, tmdb); err != nil {
			t.Fatal(err)
		}
	}
	tmdb := 603
	seed("Matrix", "https://www.imdb.com/title/tt0133093/", nil)  // 7-digit, backfilled
	seed("Big", "https://www.imdb.com/title/tt12345678/", nil)    // 8-digit, backfilled
	seed("Search", "https://www.themoviedb.org/movie/603", &tmdb) // already has tmdb id, skipped

	content, err := fs.ReadFile(migrationsFS, "migrations/005_drop_movie_link.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.ExecContext(ctx, string(content)); err != nil {
		t.Fatalf("apply 005: %v", err)
	}

	imdbFor := func(title string) (string, bool) {
		var v *string
		if err := conn.QueryRowContext(ctx, `SELECT imdb_id FROM movies WHERE title=?`, title).Scan(&v); err != nil {
			t.Fatal(err)
		}
		if v == nil {
			return "", false
		}
		return *v, true
	}

	if v, ok := imdbFor("Matrix"); !ok || v != "tt0133093" {
		t.Fatalf("Matrix imdb_id = %q (%v), want tt0133093", v, ok)
	}
	if v, ok := imdbFor("Big"); !ok || v != "tt12345678" {
		t.Fatalf("Big imdb_id = %q (%v), want tt12345678", v, ok)
	}
	if _, ok := imdbFor("Search"); ok {
		t.Fatalf("Search should keep NULL imdb_id (already has tmdb_id)")
	}

	var linkCols int
	if err := conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM pragma_table_info('movies') WHERE name='link'`).Scan(&linkCols); err != nil {
		t.Fatal(err)
	}
	if linkCols != 0 {
		t.Fatalf("expected link column dropped, found %d", linkCols)
	}
}
