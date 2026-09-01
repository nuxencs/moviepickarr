package db

import (
	"path/filepath"
	"testing"
)

func TestMigration019_CentralizesActiveTurnParticipants(t *testing.T) {
	ctx := t.Context()
	pool, err := OpenSQLite(filepath.Join(t.TempDir(), "turn-participants.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = pool.Close() }()

	applyThrough(t, ctx, pool.Write, 18)
	if _, err := pool.Write.ExecContext(ctx, `
		INSERT INTO users (name, role, archived_at) VALUES
			('Member', 'member', NULL),
			('Admin', 'admin', NULL),
			('Guest', 'guest', NULL),
			('Archived', 'member', unixepoch())
	`); err != nil {
		t.Fatal(err)
	}

	applyOne(t, ctx, pool.Write, 19, "019_turn_participants_view.sql")

	rows, err := pool.Read.QueryContext(ctx, `SELECT name FROM turn_participants ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()

	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		names = append(names, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if len(names) != 2 || names[0] != "Member" || names[1] != "Admin" {
		t.Fatalf("turn participants = %v, want Member and Admin", names)
	}
}
