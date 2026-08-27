package db

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
)

func TestMigration017PreservesLibraryAndDrawAcquisition(t *testing.T) {
	ctx := context.Background()
	pool, err := OpenSQLite(filepath.Join(t.TempDir(), "wildcard-upgrade.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = pool.Close() }()

	applyThrough(t, ctx, pool.Write, 15)
	mustExec := func(query string, args ...any) {
		t.Helper()
		if _, err := pool.Write.ExecContext(ctx, query, args...); err != nil {
			t.Fatalf("exec %q: %v", query, err)
		}
	}
	mustExec(`INSERT INTO users (id, name) VALUES (1, 'Alice')`)
	mustExec(`
		INSERT INTO movies (id, title, status, added_by_id, watched_at, tmdb_id)
		VALUES (7, 'Heat', 'current', 1, NULL, 949),
		       (8, 'Arrival', 'pool', 1, NULL, 329865),
		       (9, 'Moon', 'watched', 1, 1700000000000, 17431)
	`)

	applyOne(t, ctx, pool.Write, 16, "016_radarr.sql")
	applyOne(t, ctx, pool.Write, 17, "017_wildcards.sql")

	var count int
	if err := pool.Read.QueryRowContext(ctx, `SELECT COUNT(*) FROM movies`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 3 {
		t.Fatalf("movie count after migration = %d, want 3", count)
	}

	var source string
	var wildcardID, canceledAt sql.NullInt64
	if err := pool.Read.QueryRowContext(ctx, `
		SELECT source, wildcard_id, canceled_at
		FROM radarr_acquisitions WHERE movie_id = 7
	`).Scan(&source, &wildcardID, &canceledAt); err != nil {
		t.Fatalf("read current acquisition: %v", err)
	}
	if source != "draw" || wildcardID.Valid || canceledAt.Valid {
		t.Fatalf("upgraded acquisition source=%q wildcard=%v canceled=%v", source, wildcardID, canceledAt)
	}

	rows, err := pool.Read.QueryContext(ctx, `PRAGMA foreign_key_check`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	if rows.Next() {
		t.Fatal("migration 017 left a foreign-key violation")
	}

	if _, err := pool.Write.ExecContext(ctx, `
		INSERT INTO movies (title, status, added_by_id, tmdb_id)
		VALUES ('Wildcard candidate', 'wildcard', 1, 1234567)
	`); err != nil {
		t.Fatalf("new wildcard movie status rejected: %v", err)
	}
}
