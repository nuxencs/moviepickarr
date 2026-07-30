package repository

import (
	"database/sql"
	"errors"
	"testing"
	"time"

	"moviepickarr/internal/domain"
)

func TestInviteRepo_ArchivedInviteIsInvisible(t *testing.T) {
	ctx, _, users, pool := setupLocalAccountRepo(t)
	invites := NewSqliteInviteRepository(pool)
	alice, err := users.Create(ctx, "Alice")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	if err := invites.Create(ctx, alice.ID, "invite-a", now.Add(time.Hour), nil); err != nil {
		t.Fatalf("create invite: %v", err)
	}
	markUserArchived(t, ctx, pool, alice.ID)

	if _, err := invites.FindContextByTokenHash(ctx, "invite-a"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("find archived invite err = %v, want sql.ErrNoRows", err)
	}
}

func TestInviteRepo_CreateRejectsArchivedMember(t *testing.T) {
	ctx, _, users, pool := setupLocalAccountRepo(t)
	invites := NewSqliteInviteRepository(pool)
	alice, err := users.Create(ctx, "Alice")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	markUserArchived(t, ctx, pool, alice.ID)

	err = invites.Create(ctx, alice.ID, "invite-a", time.Now().UTC().Add(time.Hour), nil)
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("create archived invite err = %v, want ErrNotFound", err)
	}
}
