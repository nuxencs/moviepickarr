package repository

import (
	"testing"

	"moviepickarr/internal/domain"
)

func TestSeedAdminProbeDoesNotMutate(t *testing.T) {
	tests := []struct {
		name       string
		seedMember bool
	}{
		{name: "fresh database"},
		{name: "existing member", seedMember: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := setupUserRemoveEnv(t)
			var memberID int
			if tt.seedMember {
				member, err := e.users.Create(e.ctx, "Ada")
				if err != nil {
					t.Fatalf("create member: %v", err)
				}
				memberID = member.ID
			}

			result, err := NewSqliteAdminSeedRepository(e.pool).SeedAdmin(
				e.ctx,
				"Ada",
				"ada",
				nil,
			)
			if err != nil {
				t.Fatalf("probe seed: %v", err)
			}
			if !result.NeedsPasswordHash {
				t.Fatalf("probe result = %+v, want password hash request", result)
			}

			var users int
			if err := e.pool.Read.QueryRowContext(e.ctx, "SELECT COUNT(*) FROM users").Scan(&users); err != nil {
				t.Fatalf("count users: %v", err)
			}
			wantUsers := 0
			if tt.seedMember {
				wantUsers = 1
			}
			if users != wantUsers {
				t.Fatalf("probe left %d users, want %d", users, wantUsers)
			}

			if tt.seedMember {
				var role string
				if err := e.pool.Read.QueryRowContext(e.ctx,
					"SELECT role FROM users WHERE id = ?", memberID,
				).Scan(&role); err != nil {
					t.Fatalf("read role: %v", err)
				}
				if role != string(domain.RoleMember) {
					t.Fatalf("probe changed role to %q, want member", role)
				}
			}

			var logins int
			if err := e.pool.Read.QueryRowContext(e.ctx,
				"SELECT COUNT(*) FROM local_accounts",
			).Scan(&logins); err != nil {
				t.Fatalf("count local accounts: %v", err)
			}
			if logins != 0 {
				t.Fatalf("probe left %d local accounts, want 0", logins)
			}
		})
	}
}

func TestSeedAdminCreatingLoginRetiresCurrentInvite(t *testing.T) {
	e := setupUserRemoveEnv(t)
	member, err := e.users.Create(e.ctx, "Ada")
	if err != nil {
		t.Fatalf("create member: %v", err)
	}
	if _, err := e.pool.Write.ExecContext(e.ctx, `
		INSERT INTO invites (public_id, user_id, token_hash, expires_at)
		VALUES ('seed-invite-public-handle', ?, 'seed-invite-token', unixepoch() + 3600)
	`, member.ID); err != nil {
		t.Fatalf("create invite: %v", err)
	}

	passwordHash := "stored-password-hash"
	result, err := NewSqliteAdminSeedRepository(e.pool).SeedAdmin(
		e.ctx,
		"Ada",
		"ada",
		&passwordHash,
	)
	if err != nil {
		t.Fatalf("seed admin: %v", err)
	}
	if !result.LoginCreated || !result.Promoted {
		t.Fatalf("seed result = %+v, want login creation and promotion", result)
	}

	var currentInvites int
	if err := e.pool.Read.QueryRowContext(e.ctx, `
		SELECT COUNT(*)
		FROM invites
		WHERE user_id = ? AND used_at IS NULL AND revoked_at IS NULL
	`, member.ID).Scan(&currentInvites); err != nil {
		t.Fatalf("count current invites: %v", err)
	}
	if currentInvites != 0 {
		t.Fatalf("current invites after seed = %d, want 0", currentInvites)
	}
}
