package db

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
)

func TestMigration011_PreservesSessionsAndAddsStablePublicIDs(t *testing.T) {
	ctx := context.Background()
	pool, err := OpenSQLite(filepath.Join(t.TempDir(), "m.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = pool.Close() }()

	applyThrough(t, ctx, pool.Write, 10)
	if _, err := pool.Write.ExecContext(ctx, `INSERT INTO users (id, name) VALUES (1, 'alice')`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Write.ExecContext(ctx, `
		INSERT INTO sessions (
			id, token_hash, user_id, expires_at, last_seen_at, user_agent, ip, created_at
		) VALUES (7, 'token-a', 1, 200, 100, 'agent-a', '192.0.2.1', 50)
	`); err != nil {
		t.Fatal(err)
	}

	applyOne(t, ctx, pool.Write, 11, "011_session_public_id.sql")

	var (
		id         int64
		publicID   string
		tokenHash  string
		userID     int
		expiresAt  int64
		lastSeenAt int64
		userAgent  sql.NullString
		createdAt  int64
	)
	if err := pool.Read.QueryRowContext(ctx, `
		SELECT id, public_id, token_hash, user_id, expires_at, last_seen_at,
		       user_agent, created_at
		FROM sessions
	`).Scan(
		&id, &publicID, &tokenHash, &userID, &expiresAt, &lastSeenAt,
		&userAgent, &createdAt,
	); err != nil {
		t.Fatal(err)
	}
	if id != 7 || tokenHash != "token-a" || userID != 1 || expiresAt != 200 || lastSeenAt != 100 || createdAt != 50 {
		t.Fatalf("migrated session fields changed: id=%d token=%q user=%d expires=%d seen=%d created=%d",
			id, tokenHash, userID, expiresAt, lastSeenAt, createdAt)
	}
	if len(publicID) != 32 {
		t.Fatalf("migrated public id length = %d, want 32 hex chars", len(publicID))
	}
	if !userAgent.Valid || userAgent.String != "agent-a" {
		t.Fatalf("migrated user agent = %v", userAgent)
	}
	if _, err := pool.Read.ExecContext(ctx, `SELECT ip FROM sessions`); err == nil {
		t.Fatal("obsolete session IP column survived migration")
	}

	if _, err := pool.Write.ExecContext(ctx, `
		INSERT INTO sessions (public_id, token_hash, user_id, expires_at)
		VALUES (?, 'token-b', 1, 300)
	`, publicID); err == nil {
		t.Fatal("duplicate public id was accepted")
	}
	if _, err := pool.Write.ExecContext(ctx, `
		INSERT INTO sessions (token_hash, user_id, expires_at)
		VALUES ('token-c', 1, 300)
	`); err == nil {
		t.Fatal("session without a public id was accepted")
	}

	if _, err := pool.Write.ExecContext(ctx, `DELETE FROM users WHERE id = 1`); err != nil {
		t.Fatal(err)
	}
	var sessions int
	if err := pool.Read.QueryRowContext(ctx, `SELECT COUNT(*) FROM sessions`).Scan(&sessions); err != nil {
		t.Fatal(err)
	}
	if sessions != 0 {
		t.Fatalf("sessions after member delete = %d, want 0", sessions)
	}

	var applied int
	if err := pool.Read.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM schema_migrations WHERE version = 11`,
	).Scan(&applied); err != nil {
		t.Fatal(err)
	}
	if applied != 1 {
		t.Fatalf("migration version 11 count = %d, want 1", applied)
	}
}
