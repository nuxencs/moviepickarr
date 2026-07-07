//go:build ignore

// One-off verification harness: runs the migration chain against a COPY of the
// production DB and checks 007's epoch conversion against Go-computed truth.
// Run: go run scripts/verify_migration_007.go <path-to-db-copy>
package main

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"moviepickarr/internal/db"
)

var legacyLayouts = []string{
	"2006-01-02 15:04:05.999999999 -0700 MST",
	"2006-01-02 15:04:05 -0700 MST",
	"2006-01-02 15:04:05", // bare CURRENT_TIMESTAMP text, UTC
}

func parseLegacy(v string) (time.Time, error) {
	for _, l := range legacyLayouts {
		if t, err := time.Parse(l, v); err == nil {
			return t.UTC(), nil
		}
	}
	return time.Time{}, fmt.Errorf("unparseable legacy timestamp: %q", v)
}

type rowTruth struct {
	id        int
	addedAt   int64
	watchedAt *int64
}

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: go run scripts/verify_migration_007.go <db-copy>")
		os.Exit(2)
	}
	path := os.Args[1]
	ctx := context.Background()

	pool, err := db.OpenSQLite(path)
	must(err)

	// --- BEFORE: capture raw text values and compute expected epochs in Go ---
	rows, err := pool.Read.QueryContext(ctx,
		`SELECT id, CAST(added_at AS TEXT), CAST(watched_at AS TEXT) FROM movies ORDER BY id`)
	must(err)
	var truth []rowTruth
	for rows.Next() {
		var id int
		var added string
		var watched *string
		must(rows.Scan(&id, &added, &watched))
		at, err := parseLegacy(added)
		must(err)
		rt := rowTruth{id: id, addedAt: db.ToUnix(at)}
		if watched != nil {
			wt, err := parseLegacy(*watched)
			must(err)
			rt.watchedAt = db.ToUnixPtr(&wt)
		}
		truth = append(truth, rt)
	}
	must(rows.Err())
	must(rows.Close())

	var moviesBefore, usersBefore, metaBefore, creditsBefore int
	must(pool.Read.QueryRowContext(ctx, `SELECT COUNT(*) FROM movies`).Scan(&moviesBefore))
	must(pool.Read.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&usersBefore))
	must(pool.Read.QueryRowContext(ctx, `SELECT COUNT(*) FROM movie_metadata`).Scan(&metaBefore))
	must(pool.Read.QueryRowContext(ctx, `SELECT COUNT(*) FROM movie_credits`).Scan(&creditsBefore))

	// --- MIGRATE ---
	must(db.RunMigrations(ctx, pool.Write))

	// --- AFTER ---
	failures := 0
	fail := func(format string, args ...any) {
		failures++
		fmt.Printf("FAIL: "+format+"\n", args...)
	}

	var moviesAfter, usersAfter, metaAfter, creditsAfter int
	must(pool.Read.QueryRowContext(ctx, `SELECT COUNT(*) FROM movies`).Scan(&moviesAfter))
	must(pool.Read.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&usersAfter))
	must(pool.Read.QueryRowContext(ctx, `SELECT COUNT(*) FROM movie_metadata`).Scan(&metaAfter))
	must(pool.Read.QueryRowContext(ctx, `SELECT COUNT(*) FROM movie_credits`).Scan(&creditsAfter))
	if moviesBefore != moviesAfter || usersBefore != usersAfter || metaBefore != metaAfter || creditsBefore != creditsAfter {
		fail("row counts changed: movies %d->%d users %d->%d metadata %d->%d credits %d->%d",
			moviesBefore, moviesAfter, usersBefore, usersAfter, metaBefore, metaAfter, creditsBefore, creditsAfter)
	}

	// Every row matches the Go-computed expectation exactly.
	for _, rt := range truth {
		var added int64
		var watched *int64
		must(pool.Read.QueryRowContext(ctx,
			`SELECT added_at, watched_at FROM movies WHERE id = ?`, rt.id,
		).Scan(&added, &watched))
		if added != rt.addedAt {
			fail("movie %d added_at = %d, want %d", rt.id, added, rt.addedAt)
		}
		switch {
		case (watched == nil) != (rt.watchedAt == nil):
			fail("movie %d watched_at nil mismatch", rt.id)
		case watched != nil && *watched != *rt.watchedAt:
			fail("movie %d watched_at = %d, want %d", rt.id, *watched, *rt.watchedAt)
		}
	}

	// Everything is a real INTEGER now — movies, users, and metadata alike
	// (the STRICT tables enforce it for future writes; this checks the copy).
	var nonInt int
	must(pool.Read.QueryRowContext(ctx, `
		SELECT (SELECT COUNT(*) FROM movies WHERE typeof(added_at) != 'integer'
			OR (watched_at IS NOT NULL AND typeof(watched_at) != 'integer'))
		+ (SELECT COUNT(*) FROM users WHERE typeof(created_at) != 'integer' OR typeof(updated_at) != 'integer')
		+ (SELECT COUNT(*) FROM movie_metadata WHERE typeof(enriched_at) != 'integer'
			OR (credits_refreshed_at IS NOT NULL AND typeof(credits_refreshed_at) != 'integer'))
	`).Scan(&nonInt))
	if nonInt != 0 {
		fail("%d non-integer timestamps remain", nonInt)
	}

	// SQL DESC order now equals Go chronological order.
	var goOrder []int64
	for _, rt := range truth {
		if rt.watchedAt != nil {
			goOrder = append(goOrder, *rt.watchedAt)
		}
	}
	sort.Slice(goOrder, func(i, j int) bool { return goOrder[i] > goOrder[j] })
	rows2, err := pool.Read.QueryContext(ctx,
		`SELECT watched_at FROM movies WHERE status='watched' ORDER BY watched_at DESC, id`)
	must(err)
	var sqlOrder []int64
	for rows2.Next() {
		var at int64
		must(rows2.Scan(&at))
		sqlOrder = append(sqlOrder, at)
	}
	must(rows2.Err())
	must(rows2.Close())
	if len(sqlOrder) != len(goOrder) {
		fail("watched count: sql %d, go %d", len(sqlOrder), len(goOrder))
	}
	for i := range min(len(sqlOrder), len(goOrder)) {
		if sqlOrder[i] != goOrder[i] {
			fail("watched order position %d: sql %d, go %d", i, sqlOrder[i], goOrder[i])
		}
	}

	// Integrity + schema shape.
	var integrity string
	must(pool.Read.QueryRowContext(ctx, `PRAGMA integrity_check`).Scan(&integrity))
	if integrity != "ok" {
		fail("integrity_check: %s", integrity)
	}
	var fkViolations int
	must(pool.Read.QueryRowContext(ctx, `SELECT COUNT(*) FROM pragma_foreign_key_check`).Scan(&fkViolations))
	if fkViolations != 0 {
		fail("%d foreign key violations", fkViolations)
	}
	for _, idx := range []string{
		"movies_added_by_id_status_index", "movies_status_index",
		"movies_tmdb_id_unique", "movies_single_current",
	} {
		var n int
		must(pool.Read.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND name=?`, idx).Scan(&n))
		if n != 1 {
			fail("index %s missing", idx)
		}
	}
	for _, table := range []string{"movies", "users", "movie_metadata"} {
		var ddl string
		must(pool.Read.QueryRowContext(ctx, `SELECT sql FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&ddl))
		if !strings.Contains(ddl, "STRICT") {
			fail("table %s is not STRICT", table)
		}
	}

	must(pool.Close())

	if failures > 0 {
		fmt.Printf("\n%d failure(s)\n", failures)
		os.Exit(1)
	}
	fmt.Printf("OK: %d movies (%d watched), %d users, %d metadata, %d credits — all epoch, ordered, constrained, STRICT\n",
		moviesAfter, len(goOrder), usersAfter, metaAfter, creditsAfter)
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}
