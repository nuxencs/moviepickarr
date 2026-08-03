package db

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
)

func TestMigration012_PreservesInvitesAndRetiresDuplicateCurrentGenerations(t *testing.T) {
	ctx := context.Background()
	pool, err := OpenSQLite(filepath.Join(t.TempDir(), "m.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = pool.Close() }()

	applyThrough(t, ctx, pool.Write, 11)
	if _, err := pool.Write.ExecContext(ctx, `
		INSERT INTO users (id, name) VALUES
			(1, 'Ada'), (2, 'Ben'), (3, 'Cleo'), (4, 'Dev'), (5, 'Erin')
	`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Write.ExecContext(ctx, `
		INSERT INTO local_accounts (user_id, username, password_hash)
		VALUES (4, 'dev', 'dev-password-hash');
		INSERT INTO oidc_identities (user_id, issuer, subject)
		VALUES (5, 'https://idp.example', 'erin-subject');
	`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Write.ExecContext(ctx, `
		INSERT INTO invites (
			id, user_id, token_hash, expires_at, used_at, revoked_at, created_by, created_at
		) VALUES
			(7, 2, 'ben-old', 200, NULL, NULL, 1, 50),
			(8, 2, 'ben-new', 300, NULL, NULL, 1, 60),
			(9, 3, 'cleo-old', 200, NULL, NULL, 1, 50),
			(10, 3, 'cleo-used', 300, 70, NULL, 1, 60),
			(11, 4, 'dev-password-reset', 4102444800, NULL, NULL, 1, 70),
			(12, 5, 'erin-stale-onboarding', 4102444800, NULL, NULL, 1, 70)
	`); err != nil {
		t.Fatal(err)
	}

	applyOne(t, ctx, pool.Write, 12, "012_invite_public_id.sql")

	rows, err := pool.Read.QueryContext(ctx, `
		SELECT id, public_id, token_hash, revoked_at, created_by, created_at
		FROM invites
		ORDER BY id
	`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()

	type migratedInvite struct {
		id        int64
		publicID  string
		tokenHash string
		revokedAt sql.NullInt64
		createdBy sql.NullInt64
		createdAt int64
	}
	var got []migratedInvite
	for rows.Next() {
		var invite migratedInvite
		if err := rows.Scan(
			&invite.id,
			&invite.publicID,
			&invite.tokenHash,
			&invite.revokedAt,
			&invite.createdBy,
			&invite.createdAt,
		); err != nil {
			t.Fatal(err)
		}
		got = append(got, invite)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if len(got) != 6 {
		t.Fatalf("migrated invites = %d, want 6", len(got))
	}
	for _, invite := range got {
		if len(invite.publicID) != 32 {
			t.Fatalf("invite %d public id length = %d, want 32", invite.id, len(invite.publicID))
		}
		if !invite.createdBy.Valid || invite.createdBy.Int64 != 1 || invite.createdAt == 0 {
			t.Fatalf("invite %d metadata changed: %+v", invite.id, invite)
		}
	}
	if !got[0].revokedAt.Valid {
		t.Fatal("older duplicate generation stayed current")
	}
	if got[1].revokedAt.Valid {
		t.Fatal("newest current generation was revoked")
	}
	if !got[2].revokedAt.Valid {
		t.Fatal("older current generation resurfaced behind a newer used invite")
	}
	if got[4].tokenHash != "dev-password-reset" || got[4].revokedAt.Valid {
		t.Fatalf("legacy password-reset invite was not preserved: %+v", got[4])
	}
	if got[5].tokenHash != "erin-stale-onboarding" || !got[5].revokedAt.Valid {
		t.Fatalf("legacy OIDC-only onboarding invite stayed current: %+v", got[5])
	}

	if _, err := pool.Write.ExecContext(ctx, `
		INSERT INTO invites (public_id, user_id, token_hash, expires_at)
		VALUES ('new-current', 2, 'ben-third', 400)
	`); err == nil {
		t.Fatal("second current generation for one member was accepted")
	}
	if _, err := pool.Write.ExecContext(ctx, `
		INSERT INTO invites (public_id, user_id, token_hash, expires_at)
		VALUES (?, 1, 'ada-first', 400)
	`, got[0].publicID); err == nil {
		t.Fatal("duplicate public id was accepted")
	}
	if _, err := pool.Write.ExecContext(ctx, `
		INSERT INTO invites (user_id, token_hash, expires_at)
		VALUES (1, 'ada-second', 400)
	`); err == nil {
		t.Fatal("invite without a public id was accepted")
	}

	if _, err := pool.Write.ExecContext(ctx, `DELETE FROM users WHERE id = 1`); err != nil {
		t.Fatal(err)
	}
	var issuer sql.NullInt64
	if err := pool.Read.QueryRowContext(ctx,
		`SELECT created_by FROM invites WHERE id = 8`,
	).Scan(&issuer); err != nil {
		t.Fatal(err)
	}
	if issuer.Valid {
		t.Fatalf("created_by after issuer delete = %d, want NULL", issuer.Int64)
	}
}
