package repository

import (
	"database/sql"
	"errors"
	"testing"
	"time"

	"moviepickarr/internal/domain"
)

func TestAuthTransition_CredentialAndInviteResolveInEitherWriterOrder(t *testing.T) {
	t.Run("credential commits first", func(t *testing.T) {
		e := setupInviteRepo(t)
		ben := e.member(t, "Ben")
		store := NewSqliteAuthTransitionStore(e.pool)

		if _, err := store.SetLocalCredential(e.ctx, domain.LocalCredentialChange{
			UserID:       ben,
			Username:     "ben",
			PasswordHash: "hash",
			Mode:         domain.LocalCredentialFirst,
		}, e.now); err != nil {
			t.Fatalf("set credential: %v", err)
		}
		err := e.repo.Create(
			e.ctx,
			ben,
			"public-after-credential-0000",
			"token-after-credential",
			e.now.Add(time.Hour),
			e.now,
			&e.adminID,
		)
		if !errors.Is(err, domain.ErrConflict) {
			t.Fatalf("onboarding invite after credential = %v, want ErrConflict", err)
		}
	})

	t.Run("invite commits first", func(t *testing.T) {
		e := setupInviteRepo(t)
		ben := e.member(t, "Ben")
		e.create(t, ben, "before-credential", e.now.Add(time.Hour))
		store := NewSqliteAuthTransitionStore(e.pool)

		if _, err := store.SetLocalCredential(e.ctx, domain.LocalCredentialChange{
			UserID:       ben,
			Username:     "ben",
			PasswordHash: "hash",
			Mode:         domain.LocalCredentialFirst,
		}, e.now); err != nil {
			t.Fatalf("set credential: %v", err)
		}
		var current int
		if err := e.pool.Read.QueryRowContext(e.ctx, `
			SELECT COUNT(*)
			FROM invites
			WHERE user_id = ? AND used_at IS NULL AND revoked_at IS NULL
		`, ben).Scan(&current); err != nil {
			t.Fatal(err)
		}
		if current != 0 {
			t.Fatalf("current invites = %d, want 0", current)
		}
	})
}

func TestAuthTransition_PasswordClaimRollsBackCredentialWhenConsumeFails(t *testing.T) {
	e := setupInviteRepo(t)
	ben := e.member(t, "Ben")
	e.create(t, ben, "rollback", e.now.Add(time.Hour))
	if _, err := e.pool.Write.ExecContext(e.ctx, `
		CREATE TRIGGER fail_invite_consume
		BEFORE UPDATE OF used_at ON invites
		BEGIN
			SELECT RAISE(ABORT, 'forced invite consume failure');
		END
	`); err != nil {
		t.Fatalf("create failure trigger: %v", err)
	}

	_, err := NewSqliteAuthTransitionStore(e.pool).RedeemPasswordInvite(
		e.ctx,
		domain.PasswordInviteClaim{
			TokenHash:    "hash-rollback",
			Username:     "ben",
			PasswordHash: "password-hash",
		},
		e.now,
	)
	if err == nil {
		t.Fatal("claim succeeded through forced consume failure")
	}
	if _, err := e.accounts.FindByUserID(e.ctx, ben); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("credential after rollback = %v, want sql.ErrNoRows", err)
	}
	var usedAt, revokedAt sql.NullInt64
	if err := e.pool.Read.QueryRowContext(e.ctx, `
		SELECT used_at, revoked_at FROM invites WHERE token_hash = 'hash-rollback'
	`).Scan(&usedAt, &revokedAt); err != nil {
		t.Fatal(err)
	}
	if usedAt.Valid || revokedAt.Valid {
		t.Fatalf("invite changed after rollback: used=%v revoked=%v", usedAt, revokedAt)
	}
}

func TestAuthTransition_PasswordClaimRollsBackWhenSessionInsertFails(t *testing.T) {
	e := setupInviteRepo(t)
	ben := e.member(t, "Ben")
	e.create(t, ben, "session-rollback", e.now.Add(time.Hour))
	sessions := NewSqliteSessionRepository(e.pool)
	mustCreateSession(t, e.ctx, sessions, "existing-session", ben, e.now.Add(time.Hour), e.now)
	if _, err := e.pool.Write.ExecContext(e.ctx, `
		CREATE TRIGGER fail_claim_session_insert
		BEFORE INSERT ON sessions
		BEGIN
			SELECT RAISE(ABORT, 'forced session insert failure');
		END
	`); err != nil {
		t.Fatal(err)
	}
	session := domain.Session{
		PublicID:   "public-claim-session",
		TokenHash:  "claim-session",
		ExpiresAt:  e.now.Add(time.Hour),
		LastSeenAt: e.now,
		CreatedAt:  e.now,
	}
	_, err := NewSqliteAuthTransitionStore(e.pool).RedeemPasswordInvite(
		e.ctx,
		domain.PasswordInviteClaim{
			TokenHash:    "hash-session-rollback",
			Username:     "ben",
			PasswordHash: "password-hash",
			Session:      &session,
		},
		e.now,
	)
	if err == nil {
		t.Fatal("claim succeeded through forced session insert failure")
	}
	if _, err := e.accounts.FindByUserID(e.ctx, ben); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("credential after rollback = %v, want sql.ErrNoRows", err)
	}
	var current int
	if err := e.pool.Read.QueryRowContext(e.ctx, `
		SELECT COUNT(*) FROM invites
		WHERE user_id = ? AND used_at IS NULL AND revoked_at IS NULL
	`, ben).Scan(&current); err != nil {
		t.Fatal(err)
	}
	if current != 1 {
		t.Fatalf("current invite after rollback = %d, want 1", current)
	}
	if _, err := sessions.FindByTokenHash(e.ctx, "existing-session"); err != nil {
		t.Fatalf("existing session after rollback: %v", err)
	}
}

func TestAuthTransition_OIDCClaimRollsBackLinkWhenConsumeFails(t *testing.T) {
	e := setupInviteRepo(t)
	ben := e.member(t, "Ben")
	e.create(t, ben, "oidc-rollback", e.now.Add(time.Hour))
	sessions := NewSqliteSessionRepository(e.pool)
	mustCreateSession(t, e.ctx, sessions, "existing-oidc-session", ben, e.now.Add(time.Hour), e.now)
	if _, err := e.pool.Write.ExecContext(e.ctx, `
		CREATE TRIGGER fail_oidc_invite_consume
		BEFORE UPDATE OF used_at ON invites
		BEGIN
			SELECT RAISE(ABORT, 'forced oidc consume failure');
		END
	`); err != nil {
		t.Fatalf("create failure trigger: %v", err)
	}

	_, err := NewSqliteAuthTransitionStore(e.pool).RedeemOIDCInvite(
		e.ctx,
		"hash-oidc-rollback",
		domain.OIDCIdentity{Issuer: "https://idp.test", Subject: "ben-sub"},
		nil,
		e.now,
	)
	if err == nil {
		t.Fatal("OIDC claim succeeded through forced consume failure")
	}
	var identities int
	if err := e.pool.Read.QueryRowContext(e.ctx,
		"SELECT COUNT(*) FROM oidc_identities WHERE user_id = ?", ben,
	).Scan(&identities); err != nil {
		t.Fatal(err)
	}
	if identities != 0 {
		t.Fatalf("linked identities after rollback = %d, want 0", identities)
	}
	if _, err := sessions.FindByTokenHash(e.ctx, "existing-oidc-session"); err != nil {
		t.Fatalf("existing session after rollback: %v", err)
	}
}

func TestAuthTransition_OIDCClaimRejectsPasswordResetInvite(t *testing.T) {
	e := setupInviteRepo(t)
	ben := e.member(t, "Ben")
	if err := e.accounts.Create(e.ctx, ben, "ben", "password-hash"); err != nil {
		t.Fatal(err)
	}
	if err := e.repo.Create(
		e.ctx,
		ben,
		"public-reset-oidc-000000",
		"hash-reset-oidc",
		e.now.Add(time.Hour),
		e.now,
		&e.adminID,
		true,
	); err != nil {
		t.Fatal(err)
	}

	_, err := NewSqliteAuthTransitionStore(e.pool).RedeemOIDCInvite(
		e.ctx,
		"hash-reset-oidc",
		domain.OIDCIdentity{Issuer: "https://idp.test", Subject: "ben-sub"},
		nil,
		e.now,
	)
	if !errors.Is(err, domain.ErrInviteInvalid) {
		t.Fatalf("OIDC reset claim = %v, want ErrInviteInvalid", err)
	}

	var identities, current int
	if err := e.pool.Read.QueryRowContext(e.ctx,
		"SELECT COUNT(*) FROM oidc_identities WHERE user_id = ?", ben,
	).Scan(&identities); err != nil {
		t.Fatal(err)
	}
	if err := e.pool.Read.QueryRowContext(e.ctx, `
		SELECT COUNT(*) FROM invites
		WHERE user_id = ? AND used_at IS NULL AND revoked_at IS NULL
	`, ben).Scan(&current); err != nil {
		t.Fatal(err)
	}
	if identities != 0 || current != 1 {
		t.Fatalf("rejected OIDC reset claim left identities=%d currentInvites=%d", identities, current)
	}
}

func TestAuthTransition_InviteRedemptionReplacesResidualSessions(t *testing.T) {
	t.Run("password", func(t *testing.T) {
		e := setupInviteRepo(t)
		ben := e.member(t, "Ben")
		e.create(t, ben, "password-session-rotation", e.now.Add(time.Hour))
		sessions := NewSqliteSessionRepository(e.pool)
		mustCreateSession(t, e.ctx, sessions, "old-password-session", ben, e.now.Add(time.Hour), e.now)
		replacement := domain.Session{
			PublicID:   "public-new-password-session",
			TokenHash:  "new-password-session",
			ExpiresAt:  e.now.Add(time.Hour),
			LastSeenAt: e.now,
			CreatedAt:  e.now,
		}

		_, err := NewSqliteAuthTransitionStore(e.pool).RedeemPasswordInvite(
			e.ctx,
			domain.PasswordInviteClaim{
				TokenHash:    "hash-password-session-rotation",
				Username:     "ben",
				PasswordHash: "password-hash",
				Session:      &replacement,
			},
			e.now,
		)
		if err != nil {
			t.Fatalf("redeem password invite: %v", err)
		}
		if _, err := sessions.FindByTokenHash(e.ctx, "old-password-session"); !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("old password session after claim = %v, want sql.ErrNoRows", err)
		}
		if _, err := sessions.FindByTokenHash(e.ctx, "new-password-session"); err != nil {
			t.Fatalf("replacement password session: %v", err)
		}
	})

	t.Run("oidc", func(t *testing.T) {
		e := setupInviteRepo(t)
		ben := e.member(t, "Ben")
		e.create(t, ben, "oidc-session-rotation", e.now.Add(time.Hour))
		sessions := NewSqliteSessionRepository(e.pool)
		mustCreateSession(t, e.ctx, sessions, "old-oidc-session", ben, e.now.Add(time.Hour), e.now)
		replacement := domain.Session{
			PublicID:   "public-new-oidc-session",
			TokenHash:  "new-oidc-session",
			ExpiresAt:  e.now.Add(time.Hour),
			LastSeenAt: e.now,
			CreatedAt:  e.now,
		}

		_, err := NewSqliteAuthTransitionStore(e.pool).RedeemOIDCInvite(
			e.ctx,
			"hash-oidc-session-rotation",
			domain.OIDCIdentity{Issuer: "https://idp.test", Subject: "ben-sub"},
			&replacement,
			e.now,
		)
		if err != nil {
			t.Fatalf("redeem oidc invite: %v", err)
		}
		if _, err := sessions.FindByTokenHash(e.ctx, "old-oidc-session"); !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("old OIDC session after claim = %v, want sql.ErrNoRows", err)
		}
		if _, err := sessions.FindByTokenHash(e.ctx, "new-oidc-session"); err != nil {
			t.Fatalf("replacement OIDC session: %v", err)
		}
	})
}

func TestAuthTransition_OldTokenCannotClaimReusedInviteRow(t *testing.T) {
	e := setupInviteRepo(t)
	oldMember := e.member(t, "Old member")
	e.create(t, oldMember, "old-token", e.now.Add(time.Hour))
	var oldInviteID int64
	if err := e.pool.Read.QueryRowContext(e.ctx,
		"SELECT id FROM invites WHERE token_hash = 'hash-old-token'",
	).Scan(&oldInviteID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.pool.Write.ExecContext(e.ctx, "DELETE FROM users WHERE id = ?", oldMember); err != nil {
		t.Fatal(err)
	}

	newMember := e.member(t, "New member")
	e.create(t, newMember, "new-token", e.now.Add(time.Hour))
	var newInviteID int64
	if err := e.pool.Read.QueryRowContext(e.ctx,
		"SELECT id FROM invites WHERE token_hash = 'hash-new-token'",
	).Scan(&newInviteID); err != nil {
		t.Fatal(err)
	}
	if newInviteID != oldInviteID {
		t.Fatalf("fixture did not reuse invite id: old=%d new=%d", oldInviteID, newInviteID)
	}

	_, err := NewSqliteAuthTransitionStore(e.pool).RedeemPasswordInvite(
		e.ctx,
		domain.PasswordInviteClaim{
			TokenHash:    "hash-old-token",
			Username:     "attacker",
			PasswordHash: "password-hash",
		},
		e.now,
	)
	if !errors.Is(err, domain.ErrInviteInvalid) {
		t.Fatalf("old-token claim = %v, want ErrInviteInvalid", err)
	}
	if _, err := e.accounts.FindByUserID(e.ctx, newMember); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("new member credential after old-token claim = %v, want sql.ErrNoRows", err)
	}
}

func TestAuthTransition_ChangePasswordRetiresResetInviteAndSessions(t *testing.T) {
	e := setupInviteRepo(t)
	ben := e.member(t, "Ben")
	if err := e.accounts.Create(e.ctx, ben, "ben", "old-hash"); err != nil {
		t.Fatal(err)
	}
	if err := e.repo.Create(
		e.ctx,
		ben,
		"public-reset-change-000000",
		"hash-reset-change",
		e.now.Add(time.Hour),
		e.now,
		&e.adminID,
		true,
	); err != nil {
		t.Fatal(err)
	}
	sessions := NewSqliteSessionRepository(e.pool)
	mustCreateSession(t, e.ctx, sessions, "change-session", ben, e.now.Add(time.Hour), e.now)

	err := NewSqliteAuthTransitionStore(e.pool).ChangeVerifiedPassword(
		e.ctx,
		domain.VerifiedPasswordChange{
			UserID:               ben,
			ExpectedPasswordHash: "old-hash",
			PasswordHash:         "new-hash",
		},
		e.now,
	)
	if err != nil {
		t.Fatalf("change password: %v", err)
	}
	account, err := e.accounts.FindByUserID(e.ctx, ben)
	if err != nil {
		t.Fatal(err)
	}
	if account.PasswordHash != "new-hash" {
		t.Fatalf("password hash = %q, want new-hash", account.PasswordHash)
	}
	if _, err := sessions.FindByTokenHash(e.ctx, "change-session"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("session after password change = %v, want sql.ErrNoRows", err)
	}
	var current int
	if err := e.pool.Read.QueryRowContext(e.ctx, `
		SELECT COUNT(*) FROM invites
		WHERE user_id = ? AND used_at IS NULL AND revoked_at IS NULL
	`, ben).Scan(&current); err != nil {
		t.Fatal(err)
	}
	if current != 0 {
		t.Fatalf("current reset invites = %d, want 0", current)
	}
}

func TestAuthTransition_ChangePasswordRollsBackWhenInviteRetirementFails(t *testing.T) {
	e := setupInviteRepo(t)
	ben := e.member(t, "Ben")
	if err := e.accounts.Create(e.ctx, ben, "ben", "old-hash"); err != nil {
		t.Fatal(err)
	}
	if err := e.repo.Create(
		e.ctx,
		ben,
		"public-reset-rollback-0000",
		"hash-reset-rollback",
		e.now.Add(time.Hour),
		e.now,
		&e.adminID,
		true,
	); err != nil {
		t.Fatal(err)
	}
	sessions := NewSqliteSessionRepository(e.pool)
	mustCreateSession(t, e.ctx, sessions, "rollback-session", ben, e.now.Add(time.Hour), e.now)
	if _, err := e.pool.Write.ExecContext(e.ctx, `
		CREATE TRIGGER fail_reset_invite_retirement
		BEFORE UPDATE OF revoked_at ON invites
		BEGIN
			SELECT RAISE(ABORT, 'forced reset invite retirement failure');
		END
	`); err != nil {
		t.Fatal(err)
	}

	err := NewSqliteAuthTransitionStore(e.pool).ChangeVerifiedPassword(
		e.ctx,
		domain.VerifiedPasswordChange{
			UserID:               ben,
			ExpectedPasswordHash: "old-hash",
			PasswordHash:         "new-hash",
		},
		e.now,
	)
	if err == nil {
		t.Fatal("password change succeeded through forced invite retirement failure")
	}
	account, findErr := e.accounts.FindByUserID(e.ctx, ben)
	if findErr != nil {
		t.Fatal(findErr)
	}
	if account.PasswordHash != "old-hash" {
		t.Fatalf("password hash after rollback = %q, want old-hash", account.PasswordHash)
	}
	if _, findErr := sessions.FindByTokenHash(e.ctx, "rollback-session"); findErr != nil {
		t.Fatalf("session after rollback: %v", findErr)
	}
}

func TestAuthTransition_DeleteLocalCredentialRetiresResetInvite(t *testing.T) {
	e := setupInviteRepo(t)
	ben := e.member(t, "Ben")
	if err := e.accounts.Create(e.ctx, ben, "ben", "old-hash"); err != nil {
		t.Fatal(err)
	}
	if err := e.repo.Create(
		e.ctx,
		ben,
		"public-reset-delete-000000",
		"hash-reset-delete",
		e.now.Add(time.Hour),
		e.now,
		&e.adminID,
		true,
	); err != nil {
		t.Fatal(err)
	}
	sessions := NewSqliteSessionRepository(e.pool)
	mustCreateSession(t, e.ctx, sessions, "local-delete-session", ben, e.now.Add(time.Hour), e.now)

	err := NewSqliteAuthTransitionStore(e.pool).DeleteLocalCredential(
		e.ctx,
		ben,
		e.adminID,
		e.now,
	)
	if err != nil {
		t.Fatalf("delete local credential: %v", err)
	}
	if _, err := e.accounts.FindByUserID(e.ctx, ben); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("local credential after delete = %v, want sql.ErrNoRows", err)
	}
	var current int
	if err := e.pool.Read.QueryRowContext(e.ctx, `
		SELECT COUNT(*) FROM invites
		WHERE user_id = ? AND used_at IS NULL AND revoked_at IS NULL
	`, ben).Scan(&current); err != nil {
		t.Fatal(err)
	}
	if current != 0 {
		t.Fatalf("current reset invites = %d, want 0", current)
	}
	if _, err := sessions.FindByTokenHash(e.ctx, "local-delete-session"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("session after last local credential delete = %v, want sql.ErrNoRows", err)
	}
}

func TestAuthTransition_LocalLoginCannotCrossPasswordRecovery(t *testing.T) {
	t.Run("recovery commits first", func(t *testing.T) {
		e := setupInviteRepo(t)
		ben := e.member(t, "Ben")
		if err := e.accounts.Create(e.ctx, ben, "ben", "old-hash"); err != nil {
			t.Fatal(err)
		}
		store := NewSqliteAuthTransitionStore(e.pool)
		if err := store.ChangeVerifiedPassword(
			e.ctx,
			domain.VerifiedPasswordChange{
				UserID:               ben,
				ExpectedPasswordHash: "old-hash",
				PasswordHash:         "recovered-hash",
			},
			e.now,
		); err != nil {
			t.Fatal(err)
		}
		oldRehash := "old-password-rehash"
		err := store.CompleteLocalLogin(
			e.ctx,
			domain.VerifiedLocalLogin{
				UserID:               ben,
				ExpectedPasswordHash: "old-hash",
				NewPasswordHash:      &oldRehash,
			},
			domain.Session{
				PublicID:   "public-stale-login",
				TokenHash:  "stale-login",
				UserID:     ben,
				ExpiresAt:  e.now.Add(time.Hour),
				LastSeenAt: e.now,
				CreatedAt:  e.now,
			},
			e.now,
		)
		if !errors.Is(err, domain.ErrInvalidCredentials) {
			t.Fatalf("stale login = %v, want ErrInvalidCredentials", err)
		}
		account, findErr := e.accounts.FindByUserID(e.ctx, ben)
		if findErr != nil {
			t.Fatal(findErr)
		}
		if account.PasswordHash != "recovered-hash" {
			t.Fatalf("password hash = %q, want recovered-hash", account.PasswordHash)
		}
		if _, findErr := NewSqliteSessionRepository(e.pool).FindByTokenHash(e.ctx, "stale-login"); !errors.Is(findErr, sql.ErrNoRows) {
			t.Fatalf("stale login session = %v, want sql.ErrNoRows", findErr)
		}
	})

	t.Run("login commits first", func(t *testing.T) {
		e := setupInviteRepo(t)
		ben := e.member(t, "Ben")
		if err := e.accounts.Create(e.ctx, ben, "ben", "old-hash"); err != nil {
			t.Fatal(err)
		}
		store := NewSqliteAuthTransitionStore(e.pool)
		if err := store.CompleteLocalLogin(
			e.ctx,
			domain.VerifiedLocalLogin{
				UserID:               ben,
				ExpectedPasswordHash: "old-hash",
			},
			domain.Session{
				PublicID:   "public-before-recovery",
				TokenHash:  "before-recovery",
				UserID:     ben,
				ExpiresAt:  e.now.Add(time.Hour),
				LastSeenAt: e.now,
				CreatedAt:  e.now,
			},
			e.now,
		); err != nil {
			t.Fatal(err)
		}
		if err := store.ChangeVerifiedPassword(
			e.ctx,
			domain.VerifiedPasswordChange{
				UserID:               ben,
				ExpectedPasswordHash: "old-hash",
				PasswordHash:         "recovered-hash",
			},
			e.now,
		); err != nil {
			t.Fatal(err)
		}
		if _, err := NewSqliteSessionRepository(e.pool).FindByTokenHash(e.ctx, "before-recovery"); !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("pre-recovery session = %v, want sql.ErrNoRows", err)
		}
	})
}

func TestAuthTransition_DeleteOIDCIdentityRetiresResetInvite(t *testing.T) {
	e := setupInviteRepo(t)
	ben := e.member(t, "Ben")
	if err := e.accounts.Create(e.ctx, ben, "ben", "password-hash"); err != nil {
		t.Fatal(err)
	}
	identities := NewSqliteOIDCIdentityRepository(e.pool)
	if err := identities.Insert(e.ctx, domain.OIDCIdentity{
		UserID:  ben,
		Issuer:  "https://idp.test",
		Subject: "ben-sub",
	}, e.now); err != nil {
		t.Fatal(err)
	}
	if err := e.repo.Create(
		e.ctx,
		ben,
		"public-reset-unlink-00000",
		"hash-reset-unlink",
		e.now.Add(time.Hour),
		e.now,
		&e.adminID,
		true,
	); err != nil {
		t.Fatal(err)
	}

	err := NewSqliteAuthTransitionStore(e.pool).DeleteOIDCIdentity(
		e.ctx,
		ben,
		e.adminID,
		e.now,
	)
	if err != nil {
		t.Fatalf("unlink OIDC: %v", err)
	}
	if _, err := identities.FindByUserID(e.ctx, ben); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("identity after unlink = %v, want sql.ErrNoRows", err)
	}
	var current int
	if err := e.pool.Read.QueryRowContext(e.ctx, `
		SELECT COUNT(*) FROM invites
		WHERE user_id = ? AND used_at IS NULL AND revoked_at IS NULL
	`, ben).Scan(&current); err != nil {
		t.Fatal(err)
	}
	if current != 0 {
		t.Fatalf("current reset invites = %d, want 0", current)
	}
}

func TestAuthTransition_DeleteLastOIDCIdentityRevokesSessions(t *testing.T) {
	e := setupInviteRepo(t)
	ben := e.member(t, "Ben")
	identities := NewSqliteOIDCIdentityRepository(e.pool)
	if err := identities.Insert(e.ctx, domain.OIDCIdentity{
		UserID: ben, Issuer: "https://idp.test", Subject: "ben-sub",
	}, e.now); err != nil {
		t.Fatal(err)
	}
	sessions := NewSqliteSessionRepository(e.pool)
	mustCreateSession(t, e.ctx, sessions, "oidc-delete-session", ben, e.now.Add(time.Hour), e.now)

	if err := NewSqliteAuthTransitionStore(e.pool).DeleteOIDCIdentity(
		e.ctx,
		ben,
		e.adminID,
		e.now,
	); err != nil {
		t.Fatalf("delete last OIDC identity: %v", err)
	}
	if _, err := identities.FindByUserID(e.ctx, ben); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("identity after delete = %v, want sql.ErrNoRows", err)
	}
	if _, err := sessions.FindByTokenHash(e.ctx, "oidc-delete-session"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("session after last OIDC identity delete = %v, want sql.ErrNoRows", err)
	}
}

func TestAuthTransition_OIDCLoginAndUnlinkHaveOneWriterOrder(t *testing.T) {
	t.Run("unlink commits first", func(t *testing.T) {
		e := setupInviteRepo(t)
		ben := e.member(t, "Ben")
		if err := e.accounts.Create(e.ctx, ben, "ben", "password-hash"); err != nil {
			t.Fatal(err)
		}
		identities := NewSqliteOIDCIdentityRepository(e.pool)
		if err := identities.Insert(e.ctx, domain.OIDCIdentity{
			UserID: ben, Issuer: "https://idp.test", Subject: "ben-sub",
		}, e.now); err != nil {
			t.Fatal(err)
		}
		store := NewSqliteAuthTransitionStore(e.pool)
		if err := store.DeleteOIDCIdentity(e.ctx, ben, e.adminID, e.now); err != nil {
			t.Fatal(err)
		}
		_, err := store.CompleteOIDCLogin(
			e.ctx,
			domain.OIDCIdentity{Issuer: "https://idp.test", Subject: "ben-sub"},
			domain.Session{
				PublicID: "public-stale-oidc", TokenHash: "stale-oidc", ExpiresAt: e.now.Add(time.Hour), LastSeenAt: e.now, CreatedAt: e.now,
			},
			e.now,
		)
		if !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("OIDC login after unlink = %v, want ErrNotFound", err)
		}
		if _, err := NewSqliteSessionRepository(e.pool).FindByTokenHash(e.ctx, "stale-oidc"); !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("stale OIDC session = %v, want sql.ErrNoRows", err)
		}
	})

	t.Run("login commits first", func(t *testing.T) {
		e := setupInviteRepo(t)
		ben := e.member(t, "Ben")
		if err := e.accounts.Create(e.ctx, ben, "ben", "password-hash"); err != nil {
			t.Fatal(err)
		}
		identities := NewSqliteOIDCIdentityRepository(e.pool)
		if err := identities.Insert(e.ctx, domain.OIDCIdentity{
			UserID: ben, Issuer: "https://idp.test", Subject: "ben-sub",
		}, e.now); err != nil {
			t.Fatal(err)
		}
		store := NewSqliteAuthTransitionStore(e.pool)
		if _, err := store.CompleteOIDCLogin(
			e.ctx,
			domain.OIDCIdentity{Issuer: "https://idp.test", Subject: "ben-sub"},
			domain.Session{
				PublicID: "public-live-oidc", TokenHash: "live-oidc", ExpiresAt: e.now.Add(time.Hour), LastSeenAt: e.now, CreatedAt: e.now,
			},
			e.now,
		); err != nil {
			t.Fatal(err)
		}
		if err := store.DeleteOIDCIdentity(e.ctx, ben, e.adminID, e.now); err != nil {
			t.Fatal(err)
		}
		if _, err := NewSqliteSessionRepository(e.pool).FindByTokenHash(e.ctx, "live-oidc"); err != nil {
			t.Fatalf("session authorized before unlink: %v", err)
		}
	})
}

func TestAuthTransition_OIDCLinkRequiresLiveAuthorizingSession(t *testing.T) {
	t.Run("live session", func(t *testing.T) {
		e := setupInviteRepo(t)
		ben := e.member(t, "Ben")
		if err := e.accounts.Create(e.ctx, ben, "ben", "password-hash"); err != nil {
			t.Fatal(err)
		}
		sessions := NewSqliteSessionRepository(e.pool)
		mustCreateSession(t, e.ctx, sessions, "link-session", ben, e.now.Add(time.Hour), e.now)

		err := NewSqliteAuthTransitionStore(e.pool).LinkOIDCAndRetireInvite(
			e.ctx,
			domain.OIDCIdentity{UserID: ben, Issuer: "https://idp.test", Subject: "ben-sub"},
			"link-session",
			e.now,
			e.now.Add(-30*24*time.Hour),
		)
		if err != nil {
			t.Fatalf("link with live session: %v", err)
		}
		if _, err := NewSqliteOIDCIdentityRepository(e.pool).FindByUserID(e.ctx, ben); err != nil {
			t.Fatalf("linked identity: %v", err)
		}
	})

	t.Run("revoked after verification", func(t *testing.T) {
		e := setupInviteRepo(t)
		ben := e.member(t, "Ben")
		if err := e.accounts.Create(e.ctx, ben, "ben", "password-hash"); err != nil {
			t.Fatal(err)
		}
		sessions := NewSqliteSessionRepository(e.pool)
		mustCreateSession(t, e.ctx, sessions, "revoked-link-session", ben, e.now.Add(time.Hour), e.now)
		if err := sessions.DeleteByTokenHash(e.ctx, "revoked-link-session"); err != nil {
			t.Fatal(err)
		}

		err := NewSqliteAuthTransitionStore(e.pool).LinkOIDCAndRetireInvite(
			e.ctx,
			domain.OIDCIdentity{UserID: ben, Issuer: "https://idp.test", Subject: "ben-sub"},
			"revoked-link-session",
			e.now,
			e.now.Add(-30*24*time.Hour),
		)
		if !errors.Is(err, domain.ErrSessionInvalid) {
			t.Fatalf("link after session revocation = %v, want ErrSessionInvalid", err)
		}
		if _, err := NewSqliteOIDCIdentityRepository(e.pool).FindByUserID(e.ctx, ben); !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("identity after rejected link = %v, want sql.ErrNoRows", err)
		}
	})
}
