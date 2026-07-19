package repository

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"moviepickarr/internal/db"
	"moviepickarr/internal/domain"
)

// userRemoveEnv is the full set of repos the delete/archive/restore paths touch,
// so a test can wire a member with credentials/session/invite/identity and then
// assert exactly what each removal outcome leaves behind.
type userRemoveEnv struct {
	ctx      context.Context
	pool     *db.Pool
	users    *SqliteUserRepository
	movies   *SqliteMoviesRepository
	nextUp   *SqliteNextUpRepository
	sessions *SqliteSessionRepository
	accounts *SqliteLocalAccountRepository
	invites  *SqliteInviteRepository
	oidc     *SqliteOIDCIdentityRepository
}

func setupUserRemoveEnv(t *testing.T) *userRemoveEnv {
	t.Helper()

	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "user-remove-test.db")
	pool, err := db.OpenSQLite(dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.RunMigrations(ctx, pool.Write); err != nil {
		t.Fatalf("run migrations: %v", err)
	}
	t.Cleanup(func() {
		if err := pool.Close(); err != nil {
			t.Fatalf("close sqlite: %v", err)
		}
	})

	return &userRemoveEnv{
		ctx:      ctx,
		pool:     pool,
		users:    NewSqliteUserRepository(pool),
		movies:   NewSqliteMoviesRepository(pool),
		nextUp:   NewSqliteNextUpRepository(pool),
		sessions: NewSqliteSessionRepository(pool),
		accounts: NewSqliteLocalAccountRepository(pool),
		invites:  NewSqliteInviteRepository(pool),
		oidc:     NewSqliteOIDCIdentityRepository(pool),
	}
}

// seedLogin gives a member the full credential set (local login, linked identity,
// live session, valid invite), so a removal test can prove each one is gone.
func (e *userRemoveEnv) seedLogin(t *testing.T, userID int, username string) {
	t.Helper()
	now := time.Now().UTC().Truncate(time.Second)

	if err := e.accounts.Create(e.ctx, userID, username, "hash"); err != nil {
		t.Fatalf("seed local account: %v", err)
	}
	if err := e.oidc.Insert(e.ctx, domain.OIDCIdentity{
		UserID: userID, Issuer: "https://idp.test", Subject: username,
	}, now); err != nil {
		t.Fatalf("seed oidc identity: %v", err)
	}
	if err := e.sessions.Create(e.ctx, domain.Session{
		UserID: userID, TokenHash: "sess-" + username,
		ExpiresAt: now.Add(24 * time.Hour), LastSeenAt: now, CreatedAt: now,
	}); err != nil {
		t.Fatalf("seed session: %v", err)
	}
	if err := e.invites.Create(e.ctx, userID, "invite-"+username, now.Add(24*time.Hour), nil); err != nil {
		t.Fatalf("seed invite: %v", err)
	}
}

func (e *userRemoveEnv) archivedAt(t *testing.T, userID int) sql.NullInt64 {
	t.Helper()
	var archived sql.NullInt64
	if err := e.pool.Read.QueryRowContext(e.ctx,
		"SELECT archived_at FROM users WHERE id = ?", userID).Scan(&archived); err != nil {
		t.Fatalf("read archived_at: %v", err)
	}
	return archived
}

func (e *userRemoveEnv) countRow(t *testing.T, query string, args ...any) int {
	t.Helper()
	var n int
	if err := e.pool.Read.QueryRowContext(e.ctx, query, args...).Scan(&n); err != nil {
		t.Fatalf("count query %q: %v", query, err)
	}
	return n
}

// A member who authored no movies is hard-deleted: the row goes, the whole
// credential set cascades away, and next_up (which pointed at them) nulls out.
func TestUserRepo_Remove_HardDeletesWhenNoMovies(t *testing.T) {
	e := setupUserRemoveEnv(t)
	alice, err := e.users.Create(e.ctx, "Alice")
	if err != nil {
		t.Fatalf("create alice: %v", err)
	}
	e.seedLogin(t, alice.ID, "alice")
	if err := e.nextUp.Set(e.ctx, alice.ID); err != nil {
		t.Fatalf("set next up: %v", err)
	}

	outcome, err := e.users.Remove(e.ctx, alice.ID)
	if err != nil {
		t.Fatalf("remove: %v", err)
	}
	if outcome != domain.OutcomeDeleted {
		t.Fatalf("outcome = %q, want deleted", outcome)
	}

	if n := e.countRow(t, "SELECT COUNT(*) FROM users WHERE id = ?", alice.ID); n != 0 {
		t.Fatalf("users row survived hard delete: %d", n)
	}
	if n := e.countRow(t, "SELECT COUNT(*) FROM local_accounts WHERE user_id = ?", alice.ID); n != 0 {
		t.Fatalf("local account survived: %d", n)
	}
	if n := e.countRow(t, "SELECT COUNT(*) FROM oidc_identities WHERE user_id = ?", alice.ID); n != 0 {
		t.Fatalf("oidc identity survived: %d", n)
	}
	if n := e.countRow(t, "SELECT COUNT(*) FROM sessions WHERE user_id = ?", alice.ID); n != 0 {
		t.Fatalf("session survived: %d", n)
	}
	if n := e.countRow(t, "SELECT COUNT(*) FROM invites WHERE user_id = ?", alice.ID); n != 0 {
		t.Fatalf("invite survived: %d", n)
	}

	// next_up SET NULL fired, so the rotation pointer no longer dangles.
	var nextUp sql.NullInt64
	if err := e.pool.Read.QueryRowContext(e.ctx,
		"SELECT user_id FROM next_up WHERE id = 1").Scan(&nextUp); err != nil {
		t.Fatalf("read next_up: %v", err)
	}
	if nextUp.Valid {
		t.Fatalf("next_up still points at deleted member: %d", nextUp.Int64)
	}

	// The name is freed: a fresh member can reuse it (users.name is UNIQUE).
	if _, err := e.users.Create(e.ctx, "Alice"); err != nil {
		t.Fatalf("name not freed after hard delete: %v", err)
	}
}

// A member who authored movies is archived, not deleted: the row and the movie's
// attribution survive, archived_at is set, and every login row is stripped so
// the member can no longer authenticate.
func TestUserRepo_Remove_ArchivesWhenAuthoredMovies(t *testing.T) {
	e := setupUserRemoveEnv(t)
	bob, err := e.users.Create(e.ctx, "Bob")
	if err != nil {
		t.Fatalf("create bob: %v", err)
	}
	e.seedLogin(t, bob.ID, "bob")
	movie, err := e.movies.Add(e.ctx, "Heat", "pool", bob.ID)
	if err != nil {
		t.Fatalf("add movie: %v", err)
	}

	outcome, err := e.users.Remove(e.ctx, bob.ID)
	if err != nil {
		t.Fatalf("remove: %v", err)
	}
	if outcome != domain.OutcomeArchived {
		t.Fatalf("outcome = %q, want archived", outcome)
	}

	// The row survives with archived_at stamped.
	if n := e.countRow(t, "SELECT COUNT(*) FROM users WHERE id = ?", bob.ID); n != 1 {
		t.Fatalf("archived users row missing: %d", n)
	}
	if !e.archivedAt(t, bob.ID).Valid {
		t.Fatal("archived_at not set on archive")
	}

	// Attribution is intact: the movie still resolves to Bob's name.
	got, err := e.movies.FindByID(e.ctx, movie.ID)
	if err != nil {
		t.Fatalf("find movie: %v", err)
	}
	if got.AddedByID != bob.ID || got.AddedByName != "Bob" {
		t.Fatalf("attribution lost: addedBy=%d name=%q", got.AddedByID, got.AddedByName)
	}

	// Every login row is gone, so login is dead.
	if n := e.countRow(t, "SELECT COUNT(*) FROM local_accounts WHERE user_id = ?", bob.ID); n != 0 {
		t.Fatalf("local account survived archive: %d", n)
	}
	if n := e.countRow(t, "SELECT COUNT(*) FROM oidc_identities WHERE user_id = ?", bob.ID); n != 0 {
		t.Fatalf("oidc identity survived archive: %d", n)
	}
	if n := e.countRow(t, "SELECT COUNT(*) FROM sessions WHERE user_id = ?", bob.ID); n != 0 {
		t.Fatalf("session survived archive: %d", n)
	}
	if n := e.countRow(t, "SELECT COUNT(*) FROM invites WHERE user_id = ?", bob.ID); n != 0 {
		t.Fatalf("invite survived archive: %d", n)
	}
}

func TestUserRepo_Remove_NotFound(t *testing.T) {
	e := setupUserRemoveEnv(t)
	if _, err := e.users.Remove(e.ctx, 4242); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("remove missing member: got %v, want ErrNotFound", err)
	}
}

// Active reads exclude archived members: List and FindByID skip them, but the
// row is still there (attribution) and Restore brings them back.
func TestUserRepo_ArchivedFilteredFromActiveReads(t *testing.T) {
	e := setupUserRemoveEnv(t)
	carol, err := e.users.Create(e.ctx, "Carol")
	if err != nil {
		t.Fatalf("create carol: %v", err)
	}
	if _, err := e.movies.Add(e.ctx, "Collateral", "pool", carol.ID); err != nil {
		t.Fatalf("add movie: %v", err)
	}
	if _, err := e.users.Remove(e.ctx, carol.ID); err != nil {
		t.Fatalf("remove: %v", err)
	}

	list, err := e.users.List(e.ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	for _, u := range list {
		if u.ID == carol.ID {
			t.Fatal("archived member showed up in List")
		}
	}

	if _, err := e.users.FindByID(e.ctx, carol.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("FindByID on archived member: got %v, want ErrNotFound", err)
	}
}

// next_up pointing at an archived member reads as no-one-up (active-read filter),
// so the rotation self-heals rather than surfacing a member who has left.
func TestNextUpRepo_ArchivedMemberReadsAsEmpty(t *testing.T) {
	e := setupUserRemoveEnv(t)
	dave, err := e.users.Create(e.ctx, "Dave")
	if err != nil {
		t.Fatalf("create dave: %v", err)
	}
	if _, err := e.movies.Add(e.ctx, "Dune", "pool", dave.ID); err != nil {
		t.Fatalf("add movie: %v", err)
	}
	if err := e.nextUp.Set(e.ctx, dave.ID); err != nil {
		t.Fatalf("set next up: %v", err)
	}
	if _, err := e.users.Remove(e.ctx, dave.ID); err != nil {
		t.Fatalf("remove: %v", err)
	}

	if _, err := e.nextUp.Get(e.ctx); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("next_up on archived member: got %v, want sql.ErrNoRows", err)
	}
}

func TestUserRepo_Restore_ReactivatesArchivedMember(t *testing.T) {
	e := setupUserRemoveEnv(t)
	erin, err := e.users.Create(e.ctx, "Erin")
	if err != nil {
		t.Fatalf("create erin: %v", err)
	}
	if _, err := e.movies.Add(e.ctx, "Empire", "pool", erin.ID); err != nil {
		t.Fatalf("add movie: %v", err)
	}
	if _, err := e.users.Remove(e.ctx, erin.ID); err != nil {
		t.Fatalf("remove: %v", err)
	}

	if err := e.users.Restore(e.ctx, erin.ID); err != nil {
		t.Fatalf("restore: %v", err)
	}
	if e.archivedAt(t, erin.ID).Valid {
		t.Fatal("archived_at not cleared on restore")
	}

	// Back on the active roster.
	got, err := e.users.FindByID(e.ctx, erin.ID)
	if err != nil {
		t.Fatalf("find restored member: %v", err)
	}
	if got.Name != "Erin" {
		t.Fatalf("restored member name = %q, want Erin", got.Name)
	}
}

// Restoring a member who is not archived (active or missing) is a no-op that
// reports ErrNotFound: there is nothing to restore.
func TestUserRepo_Restore_NotArchivedIsNotFound(t *testing.T) {
	e := setupUserRemoveEnv(t)
	frank, err := e.users.Create(e.ctx, "Frank")
	if err != nil {
		t.Fatalf("create frank: %v", err)
	}

	if err := e.users.Restore(e.ctx, frank.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("restore active member: got %v, want ErrNotFound", err)
	}
	if err := e.users.Restore(e.ctx, 9999); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("restore missing member: got %v, want ErrNotFound", err)
	}
}
