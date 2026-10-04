// Command devfixtures loads the developer dataset into the local DB (see
// docs/DEVELOPMENT.md). It refuses a non-empty DB unless -reset is passed.
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
	reset := flag.Bool("reset", false, "delete the DB file and load into a new one (destructive)")
	flag.Parse()

	if err := run(*reset); err != nil {
		fmt.Fprintln(os.Stderr, "dev-fixtures:", err)
		os.Exit(1)
	}
}

func run(reset bool) error {
	ctx := context.Background()

	// Resolve DB_FILE the same way server.Run does.
	_ = godotenv.Load()
	dbFile := server.ResolveDBFile("")

	if reset {
		if err := devfixtures.RemoveDB(dbFile); err != nil {
			return err
		}
	}

	pool, err := db.OpenSQLite(dbFile)
	if err != nil {
		return fmt.Errorf("open %s: %w", dbFile, err)
	}
	defer pool.Close()

	if err := db.RunMigrations(ctx, pool.Write); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}

	empty, err := devfixtures.IsEmpty(ctx, pool.Read)
	if err != nil {
		return err
	}
	if !empty {
		return fmt.Errorf("%s already holds data; re-run with `make dev/fixtures-reset` to replace it", dbFile)
	}

	movies, err := devfixtures.LoadMovies()
	if err != nil {
		return err
	}
	now := time.Now()
	plan, err := devfixtures.BuildPlan(movies, now)
	if err != nil {
		return err
	}

	tx, err := pool.Write.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

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
