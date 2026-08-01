package db

import (
	"context"
	"database/sql"
	"io/fs"
	"path/filepath"
	"testing"
)

func TestMigration010_DeduplicatesIMDbWithoutDroppingMovieData(t *testing.T) {
	ctx := context.Background()
	pool, err := OpenSQLite(filepath.Join(t.TempDir(), "m.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = pool.Close() }()

	applyThrough(t, ctx, pool.Write, 9)

	mustExec := func(stmt string, args ...any) {
		t.Helper()
		if _, err := pool.Write.ExecContext(ctx, stmt, args...); err != nil {
			t.Fatalf("exec %s: %v", stmt, err)
		}
	}
	mustExec(`INSERT INTO users (id, name) VALUES (1, 'alice')`)
	mustExec(`
		INSERT INTO movies (
			id, title, status, added_at, added_by_id, watched_at, imdb_id
		) VALUES
			(30, 'Third', 'stash', 1600000030, 1, NULL, 'tt0000100'),
			(10, 'Canonical', 'pool', 1600000010, 1, NULL, '  TT0000100  '),
			(20, 'Second', 'watched', 1600000020, 1, 1700000020, 'tt0000100'),
			(40, 'Blank', 'stash', 1600000040, 1, NULL, '   ')
	`)
	mustExec(`INSERT INTO movie_metadata (movie_id, overview) VALUES (20, 'keep me')`)
	mustExec(`INSERT INTO people (id, name) VALUES (99, 'Shared Person')`)
	mustExec(`
		INSERT INTO movie_credits (movie_id, person_id, kind, character)
		VALUES (20, 99, 'cast', 'Lead')
	`)

	applyOne(t, ctx, pool.Write, 10, "010_imdb_identity.sql")

	var movieCount, metadataCount, creditCount, peopleCount int
	for label, query := range map[string]string{
		"movies":   `SELECT COUNT(*) FROM movies`,
		"metadata": `SELECT COUNT(*) FROM movie_metadata`,
		"credits":  `SELECT COUNT(*) FROM movie_credits`,
		"people":   `SELECT COUNT(*) FROM people`,
	} {
		var target *int
		switch label {
		case "movies":
			target = &movieCount
		case "metadata":
			target = &metadataCount
		case "credits":
			target = &creditCount
		default:
			target = &peopleCount
		}
		if err := pool.Read.QueryRowContext(ctx, query).Scan(target); err != nil {
			t.Fatalf("count %s: %v", label, err)
		}
	}
	if movieCount != 4 || metadataCount != 1 || creditCount != 1 || peopleCount != 1 {
		t.Fatalf(
			"related data after migration = movies:%d metadata:%d credits:%d people:%d",
			movieCount, metadataCount, creditCount, peopleCount,
		)
	}
	var title, status string
	var addedAt, watchedAt int64
	var addedByID int
	if err := pool.Read.QueryRowContext(ctx, `
		SELECT title, status, added_at, added_by_id, watched_at
		FROM movies
		WHERE id = 20
	`).Scan(&title, &status, &addedAt, &addedByID, &watchedAt); err != nil {
		t.Fatalf("read displaced movie fields: %v", err)
	}
	if title != "Second" || status != "watched" || addedAt != 1600000020 ||
		addedByID != 1 || watchedAt != 1700000020 {
		t.Fatalf(
			"displaced movie fields = %q/%q/%d/%d/%d",
			title, status, addedAt, addedByID, watchedAt,
		)
	}

	rows, err := pool.Read.QueryContext(ctx,
		`SELECT id, imdb_id FROM movies ORDER BY id`,
	)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	gotIDs := make(map[int]sql.NullString)
	for rows.Next() {
		var id int
		var imdbID sql.NullString
		if err := rows.Scan(&id, &imdbID); err != nil {
			t.Fatal(err)
		}
		gotIDs[id] = imdbID
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if got := gotIDs[10]; !got.Valid || got.String != "tt0000100" {
		t.Fatalf("canonical IMDb id = %+v, want tt0000100", got)
	}
	for _, id := range []int{20, 30, 40} {
		if gotIDs[id].Valid {
			t.Fatalf("movie %d IMDb id = %q, want NULL", id, gotIDs[id].String)
		}
	}

	conflicts, err := pool.Read.QueryContext(ctx, `
		SELECT movie_id, imdb_id, canonical_movie_id
		FROM movie_imdb_conflicts
		ORDER BY movie_id
	`)
	if err != nil {
		t.Fatal(err)
	}
	defer conflicts.Close()
	type conflict struct {
		movieID          int
		imdbID           string
		canonicalMovieID int
	}
	var gotConflicts []conflict
	for conflicts.Next() {
		var got conflict
		if err := conflicts.Scan(&got.movieID, &got.imdbID, &got.canonicalMovieID); err != nil {
			t.Fatal(err)
		}
		gotConflicts = append(gotConflicts, got)
	}
	if err := conflicts.Err(); err != nil {
		t.Fatal(err)
	}
	wantConflicts := []conflict{
		{movieID: 20, imdbID: "tt0000100", canonicalMovieID: 10},
		{movieID: 30, imdbID: "tt0000100", canonicalMovieID: 10},
	}
	if len(gotConflicts) != len(wantConflicts) {
		t.Fatalf("conflicts = %+v, want %+v", gotConflicts, wantConflicts)
	}
	for i := range wantConflicts {
		if gotConflicts[i] != wantConflicts[i] {
			t.Fatalf("conflict %d = %+v, want %+v", i, gotConflicts[i], wantConflicts[i])
		}
	}

	if _, err := pool.Write.ExecContext(ctx, `
		INSERT INTO movies (title, status, added_by_id, imdb_id)
		VALUES ('Duplicate', 'stash', 1, 'tt0000100')
	`); err == nil {
		t.Fatal("case-insensitive duplicate IMDb id was accepted")
	}
	if _, err := pool.Write.ExecContext(ctx, `
		INSERT INTO movies (title, status, added_by_id, imdb_id)
		VALUES ('No identity', 'stash', 1, NULL)
	`); err != nil {
		t.Fatalf("second NULL IMDb id rejected: %v", err)
	}

	var applied int
	if err := pool.Read.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM schema_migrations WHERE version = 10`,
	).Scan(&applied); err != nil {
		t.Fatal(err)
	}
	if applied != 1 {
		t.Fatalf("migration version 10 count = %d, want 1", applied)
	}
	var fkViolations int
	if err := pool.Read.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM pragma_foreign_key_check`,
	).Scan(&fkViolations); err != nil {
		t.Fatal(err)
	}
	if fkViolations != 0 {
		t.Fatalf("foreign key violations after migration = %d", fkViolations)
	}
}

func TestMigration010_RollsBackCleanupOnFailure(t *testing.T) {
	ctx := context.Background()
	pool, err := OpenSQLite(filepath.Join(t.TempDir(), "m.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = pool.Close() }()

	applyThrough(t, ctx, pool.Write, 9)
	if _, err := pool.Write.ExecContext(ctx, `INSERT INTO users (id, name) VALUES (1, 'alice')`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Write.ExecContext(ctx, `
		INSERT INTO movies (id, title, status, added_by_id, imdb_id) VALUES
			(1, 'Canonical', 'pool', 1, 'tt0000200'),
			(2, 'Duplicate', 'stash', 1, 'TT0000200')
	`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Write.ExecContext(ctx, `
		CREATE TRIGGER fail_imdb_cleanup
		BEFORE UPDATE OF imdb_id ON movies
		WHEN OLD.id = 2
		BEGIN
			SELECT RAISE(ABORT, 'forced IMDb cleanup failure');
		END
	`); err != nil {
		t.Fatal(err)
	}

	content, err := fs.ReadFile(migrationsFS, "migrations/010_imdb_identity.sql")
	if err != nil {
		t.Fatal(err)
	}
	err = applyMigrationContent(ctx, pool.Write, migration{
		version: 10,
		name:    "010_imdb_identity.sql",
	}, string(content))
	if err == nil {
		t.Fatal("expected forced cleanup failure")
	}

	rows, err := pool.Read.QueryContext(ctx,
		`SELECT id, imdb_id FROM movies ORDER BY id`,
	)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	want := []string{"tt0000200", "TT0000200"}
	for i := 0; rows.Next(); i++ {
		var id int
		var imdbID string
		if err := rows.Scan(&id, &imdbID); err != nil {
			t.Fatal(err)
		}
		if id != i+1 || imdbID != want[i] {
			t.Fatalf("movie %d after rollback = %q, want %q", id, imdbID, want[i])
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}

	for objectType, name := range map[string]string{
		"table": "movie_imdb_conflicts",
		"index": "movies_imdb_id_unique",
	} {
		var count int
		if err := pool.Read.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM sqlite_master WHERE type = ? AND name = ?`,
			objectType, name,
		).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Fatalf("%s %s survived rolled-back migration", objectType, name)
		}
	}
	var applied int
	if err := pool.Read.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM schema_migrations WHERE version = 10`,
	).Scan(&applied); err != nil {
		t.Fatal(err)
	}
	if applied != 0 {
		t.Fatalf("migration version 10 count after rollback = %d, want 0", applied)
	}
}
