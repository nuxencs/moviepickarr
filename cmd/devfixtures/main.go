// Command devfixtures loads a coherent developer dataset into the local DB:
// a roster with working logins, movies across every lifecycle state, watched
// history spread over time, and an active turn holder. It is dev-only tooling
// (see docs/DEVELOPMENT.md), driven by `make dev/fixtures`.
//
// By default it refuses to touch a non-empty DB. Pass -reset (or run
// `make dev/fixtures-reset`) to wipe and reload from empty.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/joho/godotenv"

	"moviepickarr/internal/db"
	"moviepickarr/internal/devfixtures"
	"moviepickarr/internal/domain"
	"moviepickarr/internal/server"
)

func main() {
	reset := flag.Bool("reset", false, "wipe all existing data before loading (destructive)")
	flag.Parse()

	if err := run(*reset); err != nil {
		fmt.Fprintln(os.Stderr, "dev-fixtures:", err)
		os.Exit(1)
	}
}

func run(reset bool) error {
	ctx := context.Background()

	// Load .env so DB_FILE resolves the same way the server's Run does, then
	// resolve through the shared helper so the two never disagree on the file.
	_ = godotenv.Load()
	dbFile := server.ResolveDBFile("")

	pool, err := db.OpenSQLite(dbFile)
	if err != nil {
		return fmt.Errorf("open %s: %w", dbFile, err)
	}
	defer pool.Close()

	// Migrate first: a fresh file needs the schema before anything is written,
	// and an existing dev DB is brought up to date the same as on server boot.
	if err := db.RunMigrations(ctx, pool.Write); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}

	empty, err := devfixtures.IsEmpty(ctx, pool.Read)
	if err != nil {
		return err
	}
	if !empty && !reset {
		return fmt.Errorf("%s already holds data; re-run with `make dev/fixtures-reset` to wipe and reload", dbFile)
	}

	films, err := devfixtures.LoadFilms()
	if err != nil {
		return err
	}
	now := time.Now()
	plan, err := devfixtures.BuildPlan(films, now)
	if err != nil {
		return err
	}

	tx, err := pool.Write.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	// Roll back on any error path; a successful Commit makes this a no-op.
	defer func() { _ = tx.Rollback() }()

	if reset && !empty {
		if err := devfixtures.Wipe(ctx, tx); err != nil {
			return err
		}
	}
	if err := devfixtures.Apply(ctx, tx, plan, now); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}

	printSummary(dbFile, plan)
	return nil
}

func printSummary(dbFile string, plan devfixtures.Plan) {
	var pool, stash, watched int
	for _, m := range plan.Movies {
		switch m.Status {
		case domain.MovieStatusPool:
			pool++
		case domain.MovieStatusStash:
			stash++
		case domain.MovieStatusWatched:
			watched++
		}
	}

	fmt.Printf("Loaded dev fixtures into %s\n", dbFile)
	fmt.Printf("  %d members (%d with logins), %d movies (%d watched, %d stash, %d pool)\n",
		len(plan.Members), countLogins(plan), len(plan.Movies), watched, stash, pool)
	fmt.Println("  Log in with any of these (all share the same dev password):")
	for _, m := range plan.Members {
		if m.Login == nil {
			continue
		}
		role := ""
		if m.Role == domain.RoleAdmin {
			role = "  [admin]"
		}
		fmt.Printf("    %-8s password: %s%s\n", m.Login.Username, m.Login.Password, role)
	}
	fmt.Println("  Note: leave MPA_ADMIN_* unset in dev; fixtures seed their own logins.")
}

func countLogins(plan devfixtures.Plan) int {
	n := 0
	for _, m := range plan.Members {
		if m.Login != nil {
			n++
		}
	}
	return n
}
