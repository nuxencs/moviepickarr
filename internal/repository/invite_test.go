package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"moviepickarr/internal/db"
	"moviepickarr/internal/domain"
)

type inviteRepoEnv struct {
	ctx      context.Context
	repo     *SqliteInviteRepository
	accounts *SqliteLocalAccountRepository
	users    *SqliteUserRepository
	pool     *db.Pool
	adminID  int
	now      time.Time
}

func setupInviteRepo(t *testing.T) *inviteRepoEnv {
	t.Helper()
	ctx, accounts, users, pool := setupLocalAccountRepo(t)
	admin, err := users.Create(ctx, "Ada")
	if err != nil {
		t.Fatal(err)
	}
	return &inviteRepoEnv{
		ctx:      ctx,
		repo:     NewSqliteInviteRepository(pool),
		accounts: accounts,
		users:    users,
		pool:     pool,
		adminID:  admin.ID,
		now:      time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC),
	}
}

func (e *inviteRepoEnv) member(t *testing.T, name string) int {
	t.Helper()
	member, err := e.users.Create(e.ctx, name)
	if err != nil {
		t.Fatal(err)
	}
	return member.ID
}

func (e *inviteRepoEnv) create(t *testing.T, userID int, suffix string, expiry time.Time) {
	t.Helper()
	if err := e.repo.Create(
		e.ctx,
		userID,
		"public-"+suffix+"-000000000000",
		"hash-"+suffix,
		expiry,
		e.now,
		&e.adminID,
	); err != nil {
		t.Fatalf("create invite %s: %v", suffix, err)
	}
}

func TestInviteRepo_CreateRejectsArchivedAndSecondCurrentGeneration(t *testing.T) {
	e := setupInviteRepo(t)
	ben := e.member(t, "Ben")
	e.create(t, ben, "ben-1", e.now.Add(time.Hour))

	err := e.repo.Create(
		e.ctx,
		ben,
		"public-ben-2-000000000000",
		"hash-ben-2",
		e.now.Add(2*time.Hour),
		e.now,
		&e.adminID,
	)
	if !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("second current invite error = %v, want ErrConflict", err)
	}

	cleo := e.member(t, "Cleo")
	markUserArchived(t, e.ctx, e.pool, cleo)
	err = e.repo.Create(
		e.ctx,
		cleo,
		"public-cleo-1-00000000000",
		"hash-cleo-1",
		e.now.Add(time.Hour),
		e.now,
		nil,
	)
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("archived member error = %v, want ErrNotFound", err)
	}
}

func TestInviteRepo_ConcurrentCreateAllowsOneCurrentGeneration(t *testing.T) {
	e := setupInviteRepo(t)
	ben := e.member(t, "Ben")

	start := make(chan struct{})
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	for i := range 2 {
		wg.Go(func() {
			<-start
			errs <- e.repo.Create(
				e.ctx,
				ben,
				fmt.Sprintf("public-race-%d-00000000000", i),
				fmt.Sprintf("hash-race-%d", i),
				e.now.Add(time.Hour),
				e.now,
				&e.adminID,
			)
		})
	}
	close(start)
	wg.Wait()
	close(errs)

	successes, conflicts := 0, 0
	for err := range errs {
		switch {
		case err == nil:
			successes++
		case errors.Is(err, domain.ErrConflict):
			conflicts++
		default:
			t.Fatalf("unexpected create error: %v", err)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("successes/conflicts = %d/%d, want 1/1", successes, conflicts)
	}
}

func TestInviteRepo_ReplaceIsAtomicAndExact(t *testing.T) {
	e := setupInviteRepo(t)
	ben := e.member(t, "Ben")
	oldPublicID := "public-ben-old-000000000000"
	if err := e.repo.Create(e.ctx, ben, oldPublicID, "hash-old", e.now.Add(time.Hour), e.now, &e.adminID); err != nil {
		t.Fatal(err)
	}

	if err := e.repo.ReplaceCurrent(
		e.ctx,
		oldPublicID,
		"public-ben-new-00000000000",
		"hash-new",
		e.now.Add(2*time.Hour),
		e.now.Add(time.Minute),
		&e.adminID,
	); err != nil {
		t.Fatalf("replace: %v", err)
	}
	if err := e.repo.ReplaceCurrent(
		e.ctx,
		oldPublicID,
		"public-ben-stale-0000000000",
		"hash-stale",
		e.now.Add(3*time.Hour),
		e.now.Add(2*time.Minute),
		&e.adminID,
	); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("stale replacement error = %v, want ErrConflict", err)
	}

	rows, err := e.repo.ListCurrent(e.ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].PublicID != "public-ben-new-00000000000" {
		t.Fatalf("current rows = %+v, want replacement only", rows)
	}
}

func TestInviteRepo_ReplacementInsertFailureRollsBackRetirement(t *testing.T) {
	e := setupInviteRepo(t)
	ben := e.member(t, "Ben")
	publicID := "public-ben-old-00000000000"
	if err := e.repo.Create(e.ctx, ben, publicID, "hash-old", e.now.Add(time.Hour), e.now, &e.adminID); err != nil {
		t.Fatal(err)
	}

	err := e.repo.ReplaceCurrent(
		e.ctx,
		publicID,
		publicID,
		"hash-new",
		e.now.Add(2*time.Hour),
		e.now.Add(time.Minute),
		&e.adminID,
	)
	if !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("replacement collision error = %v, want ErrConflict", err)
	}
	var revoked sql.NullInt64
	if err := e.pool.Read.QueryRowContext(e.ctx,
		`SELECT revoked_at FROM invites WHERE public_id = ?`, publicID,
	).Scan(&revoked); err != nil {
		t.Fatal(err)
	}
	if revoked.Valid {
		t.Fatal("failed replacement committed the old generation's retirement")
	}
}

func TestInviteRepo_ReplacementRejectsArchivedLegacyOwner(t *testing.T) {
	e := setupInviteRepo(t)
	ben := e.member(t, "Ben")
	publicID := "public-ben-legacy-0000000000"
	if err := e.repo.Create(e.ctx, ben, publicID, "hash-old", e.now.Add(time.Hour), e.now, &e.adminID); err != nil {
		t.Fatal(err)
	}
	// Model a legacy/manual inconsistent row: the member was archived without
	// the normal lifecycle cleanup, leaving its current invite behind.
	markUserArchived(t, e.ctx, e.pool, ben)

	err := e.repo.ReplaceCurrent(
		e.ctx,
		publicID,
		"public-ben-replacement-0000000",
		"hash-new",
		e.now.Add(2*time.Hour),
		e.now.Add(time.Minute),
		&e.adminID,
	)
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("archived replacement error = %v, want ErrNotFound", err)
	}

	var revoked sql.NullInt64
	if err := e.pool.Read.QueryRowContext(e.ctx,
		`SELECT revoked_at FROM invites WHERE public_id = ?`, publicID,
	).Scan(&revoked); err != nil {
		t.Fatal(err)
	}
	if revoked.Valid {
		t.Fatal("failed archived replacement committed the old generation's retirement")
	}
	var replacements int
	if err := e.pool.Read.QueryRowContext(e.ctx,
		`SELECT COUNT(*) FROM invites WHERE token_hash = 'hash-new'`,
	).Scan(&replacements); err != nil {
		t.Fatal(err)
	}
	if replacements != 0 {
		t.Fatalf("replacement rows = %d, want 0", replacements)
	}
}

func TestInviteRepo_PublicHandlePreventsRowIDReuseABA(t *testing.T) {
	e := setupInviteRepo(t)
	ben := e.member(t, "Ben")
	oldPublicID := "public-ben-old-000000000000"
	e.create(t, ben, "ben-old", e.now.Add(time.Hour))
	var oldID int64
	if err := e.pool.Read.QueryRowContext(e.ctx,
		`SELECT id FROM invites WHERE public_id = ?`, oldPublicID,
	).Scan(&oldID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.pool.Write.ExecContext(e.ctx, `DELETE FROM invites WHERE id = ?`, oldID); err != nil {
		t.Fatal(err)
	}
	newPublicID := "public-ben-new-00000000000"
	if err := e.repo.Create(e.ctx, ben, newPublicID, "hash-new", e.now.Add(time.Hour), e.now, &e.adminID); err != nil {
		t.Fatal(err)
	}
	var newID int64
	if err := e.pool.Read.QueryRowContext(e.ctx,
		`SELECT id FROM invites WHERE public_id = ?`, newPublicID,
	).Scan(&newID); err != nil {
		t.Fatal(err)
	}
	if newID != oldID {
		t.Fatalf("fixture did not reuse row id: old=%d new=%d", oldID, newID)
	}
	if err := e.repo.RevokeOpen(e.ctx, oldPublicID, e.now, e.now); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("stale public handle error = %v, want ErrConflict", err)
	}
	if err := e.repo.RevokeOpen(e.ctx, newPublicID, e.now, e.now); err != nil {
		t.Fatalf("new generation was not independently addressable: %v", err)
	}
}

func TestInviteRepo_RevokeAndDismissEnforceExactStateBoundary(t *testing.T) {
	e := setupInviteRepo(t)
	ben := e.member(t, "Ben")
	openID := "public-ben-open-0000000000"
	if err := e.repo.Create(e.ctx, ben, openID, "hash-open", e.now.Add(time.Second), e.now, &e.adminID); err != nil {
		t.Fatal(err)
	}
	if err := e.repo.DismissExpired(e.ctx, openID, e.now, e.now); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("dismiss open error = %v, want ErrConflict", err)
	}
	if err := e.repo.RevokeOpen(e.ctx, openID, e.now.Add(time.Second), e.now); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("revoke at exact expiry error = %v, want ErrConflict", err)
	}
	if err := e.repo.DismissExpired(e.ctx, openID, e.now.Add(time.Second), e.now.Add(time.Second)); err != nil {
		t.Fatalf("dismiss at exact expiry: %v", err)
	}
}

func TestInviteRepo_ListCurrentKeepsResetInvitesManageable(t *testing.T) {
	e := setupInviteRepo(t)
	ben := e.member(t, "Ben")
	cleo := e.member(t, "Cleo")
	e.create(t, ben, "ben-1", e.now.Add(time.Hour))
	if err := e.accounts.Create(e.ctx, cleo, "cleo", "hash"); err != nil {
		t.Fatal(err)
	}
	if err := e.repo.Create(
		e.ctx,
		cleo,
		"public-cleo-1-000000000000",
		"hash-cleo-1",
		e.now.Add(-time.Hour),
		e.now,
		&e.adminID,
		true,
	); err != nil {
		t.Fatal(err)
	}

	rows, err := e.repo.ListCurrent(e.ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("rows = %+v, want onboarding and reset invites", rows)
	}
	byMember := make(map[int]domain.InviteOverview, len(rows))
	for _, row := range rows {
		byMember[row.UserID] = row
	}
	if byMember[ben].PublicID != "public-ben-1-000000000000" {
		t.Fatalf("Ben row = %+v", byMember[ben])
	}
	if byMember[cleo].PublicID != "public-cleo-1-000000000000" {
		t.Fatalf("Cleo reset row = %+v", byMember[cleo])
	}
	if byMember[ben].IssuedBy == nil || *byMember[ben].IssuedBy != "Ada" {
		t.Fatalf("issuer = %v, want Ada", byMember[ben].IssuedBy)
	}
}
