package db

import (
	"context"
	"database/sql"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// applyThrough runs every migration with version <= maxVersion, in order,
// dispatching on the fk_off marker exactly as the real runner does. It lets a
// test reach a specific pre-migration schema (here: post-008) before seeding
// the rows the migration under test has to survive.
func applyThrough(t *testing.T, ctx context.Context, db *sql.DB, maxVersion int) {
	t.Helper()
	entries, err := fs.ReadDir(migrationsFS, "migrations")
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		names = append(names, e.Name())
	}
	sort.Strings(names) // zero-padded prefixes sort by version
	for _, name := range names {
		version, ok := parseMigrationVersion(name)
		if !ok || version > maxVersion {
			continue
		}
		applyOne(t, ctx, db, version, name)
	}
}

// applyOne applies a single migration file the way the runner would, recording
// it in schema_migrations.
func applyOne(t *testing.T, ctx context.Context, db *sql.DB, version int, name string) {
	t.Helper()
	if _, err := db.ExecContext(ctx,
		`CREATE TABLE IF NOT EXISTS schema_migrations (version INTEGER PRIMARY KEY)`); err != nil {
		t.Fatal(err)
	}
	content, err := fs.ReadFile(migrationsFS, "migrations/"+name)
	if err != nil {
		t.Fatal(err)
	}
	if err := applyMigrationContent(ctx, db, migration{version: version, name: name}, string(content)); err != nil {
		t.Fatalf("apply %s: %v", name, err)
	}
}

// TestMigration009_AppliesOverExistingRows runs the full chain on a fresh DB
// carrying pre-009 users and movies, then asserts 009 landed the auth schema
// and every existing member survived as a credential-less placeholder.
func TestMigration009_AppliesOverExistingRows(t *testing.T) {
	ctx := context.Background()
	pool, err := OpenSQLite(filepath.Join(t.TempDir(), "m.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = pool.Close() }()

	// Apply everything up to 008 only, then seed the pre-009 rows an install
	// carries into the migration (members with authored movies).
	applyThrough(t, ctx, pool.Write, 8)

	mustExec := func(stmt string, args ...any) {
		t.Helper()
		if _, err := pool.Write.ExecContext(ctx, stmt, args...); err != nil {
			t.Fatalf("exec %s: %v", stmt, err)
		}
	}
	mustExec(`INSERT INTO users (id, name) VALUES (1, 'alice'), (2, 'bob')`)
	mustExec(`INSERT INTO movies (title, status, added_by_id, tmdb_id) VALUES ('Heat', 'pool', 1, 949)`)

	// Now apply 009 the way the runner does.
	applyOne(t, ctx, pool.Write, 9, "009_auth_schema.sql")

	var v9 int
	if err := pool.Read.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM schema_migrations WHERE version = 9`).Scan(&v9); err != nil {
		t.Fatal(err)
	}
	if v9 != 1 {
		t.Fatalf("expected migration version 9 recorded, got %d", v9)
	}

	// Existing members survive as active, role='member', no credential rows.
	rows, err := pool.Read.QueryContext(ctx,
		`SELECT name, role, archived_at FROM users ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	type member struct {
		name       string
		role       string
		archivedAt sql.NullInt64
	}
	var got []member
	for rows.Next() {
		var m member
		if err := rows.Scan(&m.name, &m.role, &m.archivedAt); err != nil {
			t.Fatal(err)
		}
		got = append(got, m)
	}
	want := []member{{"alice", "member", sql.NullInt64{}}, {"bob", "member", sql.NullInt64{}}}
	if len(got) != len(want) {
		t.Fatalf("got %d users, want %d", len(got), len(want))
	}
	for i, m := range got {
		if m.name != want[i].name || m.role != want[i].role || m.archivedAt.Valid {
			t.Errorf("user %d = %+v, want name=%s role=member archived_at=NULL",
				i, m, want[i].name)
		}
	}

	// Credential-less: no local logins, no linked identities, no invites for
	// the survivors.
	for _, tbl := range []string{"local_accounts", "oidc_identities", "invites"} {
		var n int
		if err := pool.Read.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM `+tbl).Scan(&n); err != nil {
			t.Fatalf("count %s: %v", tbl, err)
		}
		if n != 0 {
			t.Errorf("%s has %d rows after migration, want 0 (placeholders carry no credentials)", tbl, n)
		}
	}
}

// TestMigration009_ShapeAndConstraints applies the full chain on a fresh DB and
// exercises each invariant the added columns and tables enforce.
func TestMigration009_ShapeAndConstraints(t *testing.T) {
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

	mustExec(`INSERT INTO users (id, name) VALUES (1, 'alice'), (2, 'bob')`)

	// users.role CHECK rejects anything outside the enum.
	mustExec(`UPDATE users SET role = 'admin' WHERE id = 1`)
	wantErr("role outside enum rejected",
		`UPDATE users SET role = 'superadmin' WHERE id = 2`)

	// local_accounts: user_id PK => one local login per member; username is
	// UNIQUE NOCASE; STRICT rejects a text timestamp bind.
	mustExec(`INSERT INTO local_accounts (user_id, username, password_hash) VALUES (1, 'Alice', 'hash')`)
	wantErr("second local login for same member rejected",
		`INSERT INTO local_accounts (user_id, username, password_hash) VALUES (1, 'alice2', 'hash')`)
	wantErr("case-insensitive duplicate username rejected",
		`INSERT INTO local_accounts (user_id, username, password_hash) VALUES (2, 'ALICE', 'hash')`)
	wantErr("text timestamp rejected by STRICT",
		`INSERT INTO local_accounts (user_id, username, password_hash, created_at) VALUES (2, 'bob', 'hash', '2026-07-19 12:00:00')`)

	// oidc_identities: user_id UNIQUE (1:1) and (issuer, subject) UNIQUE.
	mustExec(`INSERT INTO oidc_identities (user_id, issuer, subject) VALUES (1, 'https://idp', 'sub-a')`)
	wantErr("second identity for same member rejected",
		`INSERT INTO oidc_identities (user_id, issuer, subject) VALUES (1, 'https://idp', 'sub-b')`)
	wantErr("duplicate (issuer, subject) rejected",
		`INSERT INTO oidc_identities (user_id, issuer, subject) VALUES (2, 'https://idp', 'sub-a')`)

	// sessions: token_hash UNIQUE; ON DELETE CASCADE clears a member's sessions.
	mustExec(`INSERT INTO sessions (token_hash, user_id, expires_at) VALUES ('t1', 2, 100)`)
	wantErr("duplicate session token_hash rejected",
		`INSERT INTO sessions (token_hash, user_id, expires_at) VALUES ('t1', 2, 200)`)

	// invites: token_hash UNIQUE; created_by SET NULL when the issuer is deleted.
	mustExec(`INSERT INTO invites (user_id, token_hash, expires_at, created_by) VALUES (2, 'i1', 100, 1)`)
	wantErr("duplicate invite token_hash rejected",
		`INSERT INTO invites (user_id, token_hash, expires_at, created_by) VALUES (2, 'i1', 100, 1)`)

	// Deleting the issuing admin (alice) nulls created_by but keeps the invite,
	// and cascades away alice's own credential/identity rows.
	mustExec(`DELETE FROM users WHERE id = 1`)
	var createdBy sql.NullInt64
	if err := pool.Read.QueryRowContext(ctx,
		`SELECT created_by FROM invites WHERE token_hash = 'i1'`).Scan(&createdBy); err != nil {
		t.Fatal(err)
	}
	if createdBy.Valid {
		t.Errorf("invite created_by = %d after issuer delete, want NULL", createdBy.Int64)
	}
	for _, tbl := range []string{"local_accounts", "oidc_identities"} {
		var n int
		if err := pool.Read.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM `+tbl+` WHERE user_id = 1`).Scan(&n); err != nil {
			t.Fatalf("count %s: %v", tbl, err)
		}
		if n != 0 {
			t.Errorf("%s still has %d rows for deleted member, want 0 (ON DELETE CASCADE)", tbl, n)
		}
	}

	// Deleting bob cascades his session and invite away.
	mustExec(`DELETE FROM users WHERE id = 2`)
	for _, tbl := range []string{"sessions", "invites"} {
		var n int
		if err := pool.Read.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM `+tbl+` WHERE user_id = 2`).Scan(&n); err != nil {
			t.Fatalf("count %s: %v", tbl, err)
		}
		if n != 0 {
			t.Errorf("%s still has %d rows for deleted member, want 0 (ON DELETE CASCADE)", tbl, n)
		}
	}

	// Every new table is STRICT (the whole point of matching the post-007 shape;
	// pragma_table_list.strict is 1 only for STRICT tables).
	for _, tbl := range []string{"local_accounts", "oidc_identities", "sessions", "invites"} {
		var strict int
		if err := pool.Read.QueryRowContext(ctx,
			`SELECT strict FROM pragma_table_list WHERE name = ?`, tbl).Scan(&strict); err != nil {
			t.Fatalf("pragma_table_list %s: %v", tbl, err)
		}
		if strict != 1 {
			t.Errorf("%s is not STRICT", tbl)
		}
	}

	// The non-unique sweep/revoke indexes exist (the UNIQUE ones are covered by
	// the collision assertions above).
	for _, idx := range []string{"sessions_user_id_index", "sessions_expires_at_index", "invites_user_id_index"} {
		var n int
		if err := pool.Read.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM sqlite_master WHERE type = 'index' AND name = ?`, idx).Scan(&n); err != nil {
			t.Fatalf("index lookup %s: %v", idx, err)
		}
		if n != 1 {
			t.Errorf("index %s missing", idx)
		}
	}
}
