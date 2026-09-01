package repository

import (
	"database/sql"
	"fmt"
	"sync"
	"testing"
	"time"

	"moviepickarr/internal/db"
	"moviepickarr/internal/domain"
)

func memberInviteGeneration(e *inviteRepoEnv, suffix string) domain.MemberInviteGeneration {
	return domain.MemberInviteGeneration{
		PublicID:  "member-invite-public-" + suffix,
		TokenHash: "member-invite-hash-" + suffix,
		ExpiresAt: e.now.Add(7 * 24 * time.Hour),
		CreatedAt: e.now,
		CreatedBy: e.adminID,
	}
}

func TestMemberInviteTransition_CreateRollsBackMemberAndNextUpWhenInviteFails(t *testing.T) {
	e := setupInviteRepo(t)
	if _, err := e.pool.Write.ExecContext(e.ctx, `
		CREATE TRIGGER fail_member_create_invite
		BEFORE INSERT ON invites
		BEGIN
			SELECT RAISE(ABORT, 'forced member invite failure');
		END
	`); err != nil {
		t.Fatalf("create failure trigger: %v", err)
	}

	_, err := NewSqliteAuthTransitionStore(e.pool).CreateMemberWithInvite(
		e.ctx,
		"Rollback",
		domain.RoleMember,
		memberInviteGeneration(e, "create-rollback"),
	)
	if err == nil {
		t.Fatal("member create succeeded through forced invite failure")
	}

	var members, invites int
	if err := e.pool.Read.QueryRowContext(e.ctx,
		"SELECT COUNT(*) FROM users WHERE name = 'Rollback'",
	).Scan(&members); err != nil {
		t.Fatal(err)
	}
	if err := e.pool.Read.QueryRowContext(e.ctx,
		"SELECT COUNT(*) FROM invites WHERE token_hash = 'member-invite-hash-create-rollback'",
	).Scan(&invites); err != nil {
		t.Fatal(err)
	}
	var nextUp sql.NullInt64
	if err := e.pool.Read.QueryRowContext(e.ctx,
		"SELECT user_id FROM next_up WHERE id = 1",
	).Scan(&nextUp); err != nil {
		t.Fatal(err)
	}
	if members != 0 || invites != 0 || nextUp.Valid {
		t.Fatalf("failed create left members=%d invites=%d nextUp=%v", members, invites, nextUp)
	}
}

func TestMemberInviteTransition_ConcurrentInitialCreatesKeepFirstCreatedNextUp(t *testing.T) {
	e := setupInviteRepo(t)
	store := NewSqliteAuthTransitionStore(e.pool)

	type result struct {
		member *domain.User
		err    error
	}
	start := make(chan struct{})
	results := make(chan result, 2)
	var wg sync.WaitGroup
	for i := range 2 {
		wg.Go(func() {
			<-start
			member, err := store.CreateMemberWithInvite(
				e.ctx,
				fmt.Sprintf("Concurrent %d", i),
				domain.RoleMember,
				memberInviteGeneration(e, fmt.Sprintf("concurrent-%d", i)),
			)
			results <- result{member: member, err: err}
		})
	}
	close(start)
	wg.Wait()
	close(results)

	created := make([]*domain.User, 0, 2)
	for result := range results {
		if result.err != nil {
			t.Fatalf("concurrent create: %v", result.err)
		}
		created = append(created, result.member)
	}
	if len(created) != 2 {
		t.Fatalf("created members = %d, want 2", len(created))
	}
	firstCreatedID := min(created[1].ID, created[0].ID)

	var nextUpID, inviteCount int
	if err := e.pool.Read.QueryRowContext(e.ctx,
		"SELECT user_id FROM next_up WHERE id = 1",
	).Scan(&nextUpID); err != nil {
		t.Fatal(err)
	}
	if err := e.pool.Read.QueryRowContext(e.ctx, `
		SELECT COUNT(*) FROM invites WHERE user_id IN (?, ?)
	`, created[0].ID, created[1].ID).Scan(&inviteCount); err != nil {
		t.Fatal(err)
	}
	if nextUpID != firstCreatedID {
		t.Fatalf("next up = %d, want first-created member %d", nextUpID, firstCreatedID)
	}
	if inviteCount != 2 {
		t.Fatalf("concurrent invite count = %d, want 2", inviteCount)
	}
}

func TestMemberInviteTransition_GuestDoesNotClaimNextUp(t *testing.T) {
	e := setupInviteRepo(t)
	store := NewSqliteAuthTransitionStore(e.pool)

	guest, err := store.CreateMemberWithInvite(
		e.ctx,
		"Guest",
		domain.RoleGuest,
		memberInviteGeneration(e, "guest"),
	)
	if err != nil {
		t.Fatalf("create guest: %v", err)
	}
	participant, err := store.CreateMemberWithInvite(
		e.ctx,
		"Participant",
		domain.RoleMember,
		memberInviteGeneration(e, "participant"),
	)
	if err != nil {
		t.Fatalf("create participant: %v", err)
	}

	var nextUpID int
	if err := e.pool.Read.QueryRowContext(e.ctx,
		"SELECT user_id FROM next_up WHERE id = 1",
	).Scan(&nextUpID); err != nil {
		t.Fatal(err)
	}
	if nextUpID != participant.ID || nextUpID == guest.ID {
		t.Fatalf("next up = %d, want participant %d", nextUpID, participant.ID)
	}
}

func TestMemberInviteTransition_RestoreRollsBackCleanupWhenInviteFails(t *testing.T) {
	e := setupInviteRepo(t)
	memberID := e.member(t, "Returning")
	now := db.ToUnix(e.now)
	for _, stmt := range []struct {
		query string
		args  []any
	}{
		{"INSERT INTO local_accounts (user_id, username, password_hash) VALUES (?, 'returning', 'hash')", []any{memberID}},
		{"INSERT INTO oidc_identities (user_id, issuer, subject) VALUES (?, 'https://idp.test', 'returning-sub')", []any{memberID}},
		{"INSERT INTO sessions (public_id, token_hash, user_id, expires_at, last_seen_at) VALUES ('returning-session-public', 'returning-session', ?, ?, ?)", []any{memberID, now + 3600, now}},
		{"INSERT INTO invites (public_id, token_hash, user_id, expires_at) VALUES ('returning-old-invite', 'returning-old-hash', ?, ?)", []any{memberID, now + 3600}},
		{"UPDATE users SET archived_at = ? WHERE id = ?", []any{now, memberID}},
	} {
		if _, err := e.pool.Write.ExecContext(e.ctx, stmt.query, stmt.args...); err != nil {
			t.Fatalf("seed restore state: %v", err)
		}
	}
	if _, err := e.pool.Write.ExecContext(e.ctx, `
		CREATE TRIGGER fail_member_restore_invite
		BEFORE INSERT ON invites
		BEGIN
			SELECT RAISE(ABORT, 'forced restored-member invite failure');
		END
	`); err != nil {
		t.Fatalf("create failure trigger: %v", err)
	}

	_, err := NewSqliteAuthTransitionStore(e.pool).RestoreMemberWithInvite(
		e.ctx,
		memberID,
		memberInviteGeneration(e, "restore-rollback"),
	)
	if err == nil {
		t.Fatal("member restore succeeded through forced invite failure")
	}

	var archivedAt sql.NullInt64
	if err := e.pool.Read.QueryRowContext(e.ctx,
		"SELECT archived_at FROM users WHERE id = ?", memberID,
	).Scan(&archivedAt); err != nil {
		t.Fatal(err)
	}
	if !archivedAt.Valid {
		t.Fatal("failed restore left member active")
	}
	for _, table := range []string{"local_accounts", "oidc_identities", "sessions", "invites"} {
		var count int
		if err := e.pool.Read.QueryRowContext(e.ctx,
			"SELECT COUNT(*) FROM "+table+" WHERE user_id = ?", memberID,
		).Scan(&count); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		if count != 1 {
			t.Fatalf("failed restore left %d %s rows, want rollback to 1", count, table)
		}
	}
}
