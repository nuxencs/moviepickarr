package repository

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"moviepickarr/internal/db"
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

// overviewEnv is the shared fixture for the outstanding-invites read: a repo, an
// admin to issue as, and a base time the tests hang expiries off.
type overviewEnv struct {
	ctx      context.Context
	invites  *SqliteInviteRepository
	accounts *SqliteLocalAccountRepository
	users    *SqliteUserRepository
	pool     *db.Pool
	admin    int
	now      time.Time
}

func setupOverview(t *testing.T) *overviewEnv {
	t.Helper()
	ctx, accounts, users, pool := setupLocalAccountRepo(t)
	admin, err := users.Create(ctx, "Ada")
	if err != nil {
		t.Fatalf("create admin: %v", err)
	}
	return &overviewEnv{
		ctx:      ctx,
		invites:  NewSqliteInviteRepository(pool),
		accounts: accounts,
		users:    users,
		pool:     pool,
		admin:    admin.ID,
		now:      time.Now().UTC().Truncate(time.Second),
	}
}

// member creates an active member and returns its id.
func (e *overviewEnv) member(t *testing.T, name string) int {
	t.Helper()
	u, err := e.users.Create(e.ctx, name)
	if err != nil {
		t.Fatalf("create member %q: %v", name, err)
	}
	return u.ID
}

// invite issues one invite from the admin, expiring at now+ttl.
func (e *overviewEnv) invite(t *testing.T, userID int, hash string, ttl time.Duration) {
	t.Helper()
	if err := e.invites.Create(e.ctx, userID, hash, e.now.Add(ttl), &e.admin); err != nil {
		t.Fatalf("create invite %q: %v", hash, err)
	}
}

// createdAt backdates an invite's created_at, so newest-per-member can be tested
// without leaning on insertion order alone.
func (e *overviewEnv) createdAt(t *testing.T, hash string, at time.Time) {
	t.Helper()
	if _, err := e.pool.Write.ExecContext(e.ctx,
		"UPDATE invites SET created_at = ? WHERE token_hash = ?", db.ToUnix(at), hash); err != nil {
		t.Fatalf("backdate invite %q: %v", hash, err)
	}
}

// idOf reads an invite's row id by token hash, for the by-id revoke tests.
func (e *overviewEnv) idOf(t *testing.T, hash string) int64 {
	t.Helper()
	var id int64
	if err := e.pool.Read.QueryRowContext(e.ctx,
		"SELECT id FROM invites WHERE token_hash = ?", hash).Scan(&id); err != nil {
		t.Fatalf("read invite id %q: %v", hash, err)
	}
	return id
}

func (e *overviewEnv) list(t *testing.T) []domain.InviteOverview {
	t.Helper()
	rows, err := e.invites.ListOutstanding(e.ctx)
	if err != nil {
		t.Fatalf("list outstanding: %v", err)
	}
	return rows
}

// TestInviteRepo_ListOutstandingKeepsNewestPerMember covers the rule the surface
// depends on: Issue only revokes *valid* invites, so a member re-invited after
// each lapse accumulates unrevoked expired rows. The overview is one row per
// member regardless, the newest by created_at.
func TestInviteRepo_ListOutstandingKeepsNewestPerMember(t *testing.T) {
	e := setupOverview(t)
	ben := e.member(t, "Ben")

	e.invite(t, ben, "ben-1", time.Hour)
	e.invite(t, ben, "ben-2", 2*time.Hour)
	e.invite(t, ben, "ben-3", 3*time.Hour)
	// Backdate so the intended newest is unambiguous rather than merely last-inserted.
	e.createdAt(t, "ben-1", e.now.Add(-72*time.Hour))
	e.createdAt(t, "ben-2", e.now.Add(-24*time.Hour))
	e.createdAt(t, "ben-3", e.now.Add(-time.Hour))

	rows := e.list(t)
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1 (newest invite only)", len(rows))
	}
	if want := e.now.Add(3 * time.Hour); !rows[0].ExpiresAt.Equal(want) {
		t.Fatalf("expiresAt = %v, want %v (the newest invite)", rows[0].ExpiresAt, want)
	}
	if rows[0].UserID != ben || rows[0].MemberName != "Ben" {
		t.Fatalf("row = %+v, want member %d/Ben", rows[0], ben)
	}
	if rows[0].IssuedBy == nil || *rows[0].IssuedBy != "Ada" {
		t.Fatalf("issuedBy = %v, want Ada", rows[0].IssuedBy)
	}
}

// TestInviteRepo_ListOutstandingRanksOverSpentInvitesToo guards the trap in the
// newest-per-member rule: rank over only the *outstanding* rows and revoking a
// member's live invite promotes their previous dead one, so an admin who just
// clicked Revoke watches the member reappear under Expired, from a link that
// stopped working weeks ago. Ranking over every invite and rejecting a spent
// winner is what makes Revoke and Dismiss actually clear the row.
func TestInviteRepo_ListOutstandingRanksOverSpentInvitesToo(t *testing.T) {
	e := setupOverview(t)
	ben := e.member(t, "Ben")

	// The shape Issue leaves behind: a lapsed invite it never revoked (it only
	// revokes valid ones), plus the live replacement.
	e.invite(t, ben, "ben-lapsed", -48*time.Hour)
	e.invite(t, ben, "ben-live", time.Hour)
	e.createdAt(t, "ben-lapsed", e.now.Add(-9*24*time.Hour))
	e.createdAt(t, "ben-live", e.now.Add(-time.Hour))

	if rows := e.list(t); len(rows) != 1 || !rows[0].ExpiresAt.Equal(e.now.Add(time.Hour)) {
		t.Fatalf("rows = %+v, want just the live invite", rows)
	}

	if _, err := e.invites.RevokeValidByUserID(e.ctx, ben, e.now, e.now); err != nil {
		t.Fatalf("revoke valid: %v", err)
	}

	if rows := e.list(t); len(rows) != 0 {
		t.Fatalf("rows after revoke = %+v, want none (the lapsed invite must not resurface)", rows)
	}
}

// TestInviteRepo_ListOutstandingSkipsCredentialedMembers is the self-clearing
// rule: a member who has since gained a login no longer needs an invite, so
// their dead row leaves the surface without anyone dismissing it. Both
// credential kinds count, since either one means they can get in.
func TestInviteRepo_ListOutstandingSkipsCredentialedMembers(t *testing.T) {
	e := setupOverview(t)
	withPassword := e.member(t, "Cleo")
	withSSO := e.member(t, "Dev")
	waiting := e.member(t, "Erin")

	e.invite(t, withPassword, "cleo-1", time.Hour)
	e.invite(t, withSSO, "dev-1", time.Hour)
	e.invite(t, waiting, "erin-1", time.Hour)

	if err := e.accounts.Create(e.ctx, withPassword, "cleo", "hash"); err != nil {
		t.Fatalf("seed local login: %v", err)
	}
	identities := NewSqliteOIDCIdentityRepository(e.pool)
	if err := identities.Insert(e.ctx, domain.OIDCIdentity{
		UserID:  withSSO,
		Issuer:  "https://idp.example",
		Subject: "dev-sub",
	}, e.now); err != nil {
		t.Fatalf("seed linked identity: %v", err)
	}

	rows := e.list(t)
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1 (only the credential-less member)", len(rows))
	}
	if rows[0].UserID != waiting {
		t.Fatalf("row member = %d, want %d (Erin)", rows[0].UserID, waiting)
	}
}

// TestInviteRepo_ListOutstandingSkipsSpentAndArchived proves the three states
// that never render: a claimed invite, a revoked one (which is what Dismiss
// writes), and any invite belonging to an archived member.
func TestInviteRepo_ListOutstandingSkipsSpentAndArchived(t *testing.T) {
	e := setupOverview(t)
	used := e.member(t, "Ben")
	revoked := e.member(t, "Cleo")
	archived := e.member(t, "Dev")

	e.invite(t, used, "ben-1", time.Hour)
	e.invite(t, revoked, "cleo-1", time.Hour)
	e.invite(t, archived, "dev-1", time.Hour)

	if err := e.invites.MarkUsed(e.ctx, e.idOf(t, "ben-1"), e.now); err != nil {
		t.Fatalf("mark used: %v", err)
	}
	if _, err := e.invites.RevokeValidByUserID(e.ctx, revoked, e.now, e.now); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	markUserArchived(t, e.ctx, e.pool, archived)

	if rows := e.list(t); len(rows) != 0 {
		t.Fatalf("rows = %+v, want none", rows)
	}
}

// TestInviteRepo_ListOutstandingKeepsExpiredAndIssuerlessRows guards the two
// things the overview must NOT drop: an expired invite (it is the whole Expired
// group, so filtering on expiry here would empty it), and an invite with no
// issuer (created_by is nullable and the row still belongs on the surface).
func TestInviteRepo_ListOutstandingKeepsExpiredAndIssuerlessRows(t *testing.T) {
	e := setupOverview(t)
	lapsed := e.member(t, "Ben")

	if err := e.invites.Create(e.ctx, lapsed, "ben-1", e.now.Add(-48*time.Hour), nil); err != nil {
		t.Fatalf("create expired invite: %v", err)
	}

	rows := e.list(t)
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1 (an expired invite is still outstanding)", len(rows))
	}
	if rows[0].IssuedBy != nil {
		t.Fatalf("issuedBy = %v, want nil for a null created_by", *rows[0].IssuedBy)
	}
	if !rows[0].ExpiresAt.Equal(e.now.Add(-48 * time.Hour)) {
		t.Fatalf("expiresAt = %v, want the past expiry", rows[0].ExpiresAt)
	}
}

// TestInviteRepo_RevokeByIDDismissesOneRow covers Dismiss: it clears exactly the
// addressed row, reports the affected count so a repeat reads as a miss, and
// leaves other members' invites alone.
func TestInviteRepo_RevokeByIDDismissesOneRow(t *testing.T) {
	e := setupOverview(t)
	ben := e.member(t, "Ben")
	cleo := e.member(t, "Cleo")
	e.invite(t, ben, "ben-1", -time.Hour)
	e.invite(t, cleo, "cleo-1", time.Hour)

	id := e.idOf(t, "ben-1")
	n, err := e.invites.RevokeByID(e.ctx, id, e.now)
	if err != nil {
		t.Fatalf("revoke by id: %v", err)
	}
	if n != 1 {
		t.Fatalf("affected = %d, want 1", n)
	}

	rows := e.list(t)
	if len(rows) != 1 || rows[0].UserID != cleo {
		t.Fatalf("rows = %+v, want only Cleo's invite left", rows)
	}

	// A second dismiss of the same row affects nothing: the caller turns that
	// into a 404 rather than reporting it cleared something.
	again, err := e.invites.RevokeByID(e.ctx, id, e.now)
	if err != nil {
		t.Fatalf("second revoke by id: %v", err)
	}
	if again != 0 {
		t.Fatalf("second affected = %d, want 0", again)
	}
}

// TestInviteRepo_RevokeByIDLeavesUsedInvitesAlone: a claimed invite is spent,
// not outstanding. Stamping revoked_at on it would rewrite history for a login
// that already happened, so the by-id revoke skips it.
func TestInviteRepo_RevokeByIDLeavesUsedInvitesAlone(t *testing.T) {
	e := setupOverview(t)
	ben := e.member(t, "Ben")
	e.invite(t, ben, "ben-1", time.Hour)
	id := e.idOf(t, "ben-1")
	if err := e.invites.MarkUsed(e.ctx, id, e.now); err != nil {
		t.Fatalf("mark used: %v", err)
	}

	n, err := e.invites.RevokeByID(e.ctx, id, e.now)
	if err != nil {
		t.Fatalf("revoke by id: %v", err)
	}
	if n != 0 {
		t.Fatalf("affected = %d, want 0 for a used invite", n)
	}
}
