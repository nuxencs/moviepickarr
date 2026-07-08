package db

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
)

// Provokes real constraint failures so the code-based matchers are pinned to
// the driver's actual behavior, not to error text.
func TestConstraintErrorMatchers(t *testing.T) {
	ctx := context.Background()
	pool, err := OpenSQLite(filepath.Join(t.TempDir(), "m.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = pool.Close() }()

	if err := RunMigrations(ctx, pool.Write); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Write.ExecContext(ctx, `INSERT INTO users (name) VALUES ('alice')`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Write.ExecContext(ctx,
		`INSERT INTO movies (title, status, added_by_id, tmdb_id) VALUES ('Heat', 'pool', 1, 949)`); err != nil {
		t.Fatal(err)
	}

	_, uniqueErr := pool.Write.ExecContext(ctx,
		`INSERT INTO movies (title, status, added_by_id, tmdb_id) VALUES ('Heat 2', 'pool', 1, 949)`)
	if !IsUniqueViolation(uniqueErr) {
		t.Errorf("expected unique violation, got: %v", uniqueErr)
	}
	if IsForeignKeyViolation(uniqueErr) {
		t.Error("unique violation misclassified as foreign key")
	}

	_, fkErr := pool.Write.ExecContext(ctx, `DELETE FROM users WHERE id = 1`)
	if !IsForeignKeyViolation(fkErr) {
		t.Errorf("expected foreign key violation, got: %v", fkErr)
	}
	if IsUniqueViolation(fkErr) {
		t.Error("foreign key violation misclassified as unique")
	}

	if IsUniqueViolation(nil) || IsForeignKeyViolation(nil) {
		t.Error("nil must not match")
	}
	other := errors.New("plain")
	if IsUniqueViolation(other) || IsForeignKeyViolation(other) {
		t.Error("non-sqlite error must not match")
	}
}
