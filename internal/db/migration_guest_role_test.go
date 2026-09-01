package db

import (
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
)

func TestMigration018_AddsGuestRoleWithoutLosingReferences(t *testing.T) {
	ctx := t.Context()
	pool, err := OpenSQLite(filepath.Join(t.TempDir(), "guest-role.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = pool.Close() }()

	applyThrough(t, ctx, pool.Write, 17)
	statements := []string{
		`INSERT INTO users (id, name, role) VALUES (1, 'Admin', 'admin'), (2, 'Guest', 'member')`,
		`INSERT INTO movies (id, title, status, added_by_id, tmdb_id) VALUES (1, 'Heat', 'stash', 2, 949)`,
		`INSERT INTO local_accounts (user_id, username, password_hash) VALUES (2, 'guest', 'hash')`,
		`UPDATE next_up SET user_id = 2 WHERE id = 1`,
	}
	for _, statement := range statements {
		if _, err := pool.Write.ExecContext(ctx, statement); err != nil {
			t.Fatalf("seed migration fixture: %v", err)
		}
	}

	applyOne(t, ctx, pool.Write, 18, "018_guest_role.sql")

	if _, err := pool.Write.ExecContext(ctx, `UPDATE users SET role = 'guest' WHERE id = 2`); err != nil {
		t.Fatalf("set guest role: %v", err)
	}
	if _, err := pool.Write.ExecContext(ctx, `UPDATE users SET role = 'viewer' WHERE id = 2`); err == nil {
		t.Fatal("role outside enum was accepted")
	}

	for table, want := range map[string]int{"users": 2, "movies": 1, "local_accounts": 1} {
		var got int
		if err := pool.Read.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+table).Scan(&got); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		if got != want {
			t.Fatalf("%s rows = %d, want %d", table, got, want)
		}
	}

	var nextUpID int
	if err := pool.Read.QueryRowContext(ctx, `SELECT user_id FROM next_up WHERE id = 1`).Scan(&nextUpID); err != nil {
		t.Fatal(err)
	}
	if nextUpID != 2 {
		t.Fatalf("next up id = %d, want preserved id 2", nextUpID)
	}

	var brokenTable string
	err = pool.Read.QueryRowContext(ctx, `SELECT "table" FROM pragma_foreign_key_check LIMIT 1`).Scan(&brokenTable)
	if err == nil {
		t.Fatalf("foreign key violation in %s", brokenTable)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		t.Fatal(err)
	}
}
