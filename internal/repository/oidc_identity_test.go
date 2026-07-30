package repository

import (
	"database/sql"
	"errors"
	"testing"
	"time"

	"moviepickarr/internal/domain"
)

func TestOIDCIdentityRepo_ArchivedIdentityIsInvisible(t *testing.T) {
	ctx, _, users, pool := setupLocalAccountRepo(t)
	identities := NewSqliteOIDCIdentityRepository(pool)
	alice, err := users.Create(ctx, "Alice")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	if err := identities.Insert(ctx, domain.OIDCIdentity{
		UserID:  alice.ID,
		Issuer:  "https://idp.test",
		Subject: "alice",
	}, now); err != nil {
		t.Fatalf("insert identity: %v", err)
	}
	stored, err := identities.FindByUserID(ctx, alice.ID)
	if err != nil {
		t.Fatalf("find identity before archive: %v", err)
	}
	markUserArchived(t, ctx, pool, alice.ID)

	if _, err := identities.FindByIssuerSubject(ctx, "https://idp.test", "alice"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("find archived issuer/subject err = %v, want sql.ErrNoRows", err)
	}
	if _, err := identities.FindByUserID(ctx, alice.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("find archived user id err = %v, want sql.ErrNoRows", err)
	}
	if err := identities.TouchLogin(ctx, stored.ID, nil, nil, now, now); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("touch archived identity err = %v, want sql.ErrNoRows", err)
	}
}

func TestOIDCIdentityRepo_InsertRejectsArchivedMember(t *testing.T) {
	ctx, _, users, pool := setupLocalAccountRepo(t)
	identities := NewSqliteOIDCIdentityRepository(pool)
	alice, err := users.Create(ctx, "Alice")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	markUserArchived(t, ctx, pool, alice.ID)

	err = identities.Insert(ctx, domain.OIDCIdentity{
		UserID:  alice.ID,
		Issuer:  "https://idp.test",
		Subject: "alice",
	}, time.Now().UTC())
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("insert archived identity err = %v, want ErrNotFound", err)
	}
}
