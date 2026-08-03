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
		PublicID: "public-" + username, UserID: userID, TokenHash: "sess-" + username,
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

func (e *userRemoveEnv) seedResidualAuthRows(t *testing.T, userID int, suffix string) {
	t.Helper()
	now := time.Now().UTC().Truncate(time.Second)
	stmts := []struct {
		query string
		args  []any
	}{
		{"INSERT INTO local_accounts (user_id, username, password_hash) VALUES (?, ?, 'hash')", []any{userID, "login-" + suffix}},
		{"INSERT INTO oidc_identities (user_id, issuer, subject) VALUES (?, 'https://idp.test', ?)", []any{userID, "subject-" + suffix}},
		{"INSERT INTO sessions (public_id, user_id, token_hash, expires_at, last_seen_at) VALUES (?, ?, ?, ?, ?)", []any{"public-" + suffix, userID, "session-" + suffix, db.ToUnix(now.Add(time.Hour)), db.ToUnix(now)}},
		{"INSERT INTO invites (user_id, token_hash, expires_at) VALUES (?, ?, ?)", []any{userID, "invite-" + suffix, db.ToUnix(now.Add(time.Hour))}},
	}
	for _, stmt := range stmts {
		if _, err := e.pool.Write.ExecContext(e.ctx, stmt.query, stmt.args...); err != nil {
			t.Fatalf("seed residual auth row (%s): %v", stmt.query, err)
		}
	}
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

func TestUserRepo_Restore_StripsResidualAuthenticationRows(t *testing.T) {
	e := setupUserRemoveEnv(t)
	erin, err := e.users.Create(e.ctx, "Erin")
	if err != nil {
		t.Fatalf("create erin: %v", err)
	}
	if _, err := e.movies.Add(e.ctx, "Empire", "pool", erin.ID); err != nil {
		t.Fatalf("add movie: %v", err)
	}
	if _, err := e.users.Remove(e.ctx, erin.ID); err != nil {
		t.Fatalf("archive: %v", err)
	}

	e.seedResidualAuthRows(t, erin.ID, "erin")

	if err := e.users.Restore(e.ctx, erin.ID); err != nil {
		t.Fatalf("restore: %v", err)
	}
	for _, table := range []string{"local_accounts", "oidc_identities", "sessions", "invites"} {
		if got := e.countRow(t, "SELECT COUNT(*) FROM "+table+" WHERE user_id = ?", erin.ID); got != 0 {
			t.Fatalf("restore left %d %s rows, want 0", got, table)
		}
	}
	if e.archivedAt(t, erin.ID).Valid {
		t.Fatal("restore did not clear archived_at")
	}
}

func TestUserRepo_Restore_RollsBackAuthCleanupOnFailure(t *testing.T) {
	e := setupUserRemoveEnv(t)
	erin, err := e.users.Create(e.ctx, "Erin")
	if err != nil {
		t.Fatalf("create erin: %v", err)
	}
	if _, err := e.movies.Add(e.ctx, "Empire", "pool", erin.ID); err != nil {
		t.Fatalf("add movie: %v", err)
	}
	if _, err := e.users.Remove(e.ctx, erin.ID); err != nil {
		t.Fatalf("archive: %v", err)
	}
	e.seedResidualAuthRows(t, erin.ID, "rollback")

	if _, err := e.pool.Write.ExecContext(e.ctx, `
		CREATE TRIGGER fail_restore_session_delete
		BEFORE DELETE ON sessions
		BEGIN
			SELECT RAISE(ABORT, 'forced session delete failure');
		END`); err != nil {
		t.Fatalf("install failure trigger: %v", err)
	}

	if err := e.users.Restore(e.ctx, erin.ID); err == nil {
		t.Fatal("restore succeeded despite forced session delete failure")
	}
	if !e.archivedAt(t, erin.ID).Valid {
		t.Fatal("failed restore cleared archived_at")
	}
	for _, table := range []string{"local_accounts", "oidc_identities", "sessions", "invites"} {
		if got := e.countRow(t, "SELECT COUNT(*) FROM "+table+" WHERE user_id = ?", erin.ID); got != 1 {
			t.Fatalf("failed restore left %d %s rows, want rollback to 1", got, table)
		}
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

// rosterByID indexes a roster read by member id, so an assertion can pull the
// row it cares about without depending on the (active-then-oldest) ordering.
func rosterByID(t *testing.T, members []*domain.RosterMember) map[int]*domain.RosterMember {
	t.Helper()
	byID := make(map[int]*domain.RosterMember, len(members))
	for _, m := range members {
		byID[m.ID] = m
	}
	return byID
}

// The roster derives login state from credential/invite/archive presence, not a
// stored flag: a placeholder shows nothing, an invited placeholder shows a
// pending invite, a fully-credentialed member shows both link-states, and an
// archived member surfaces on the roster (unlike the active-only List) with its
// login rows stripped.
func TestUserRepo_Roster_DerivesLoginState(t *testing.T) {
	e := setupUserRemoveEnv(t)

	// Placeholder: no credentials, no invite.
	noor, err := e.users.Create(e.ctx, "Noor")
	if err != nil {
		t.Fatalf("create noor: %v", err)
	}
	// Invited placeholder: a valid unredeemed invite, still no credentials.
	jamie, err := e.users.Create(e.ctx, "Jamie")
	if err != nil {
		t.Fatalf("create jamie: %v", err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	if err := e.invites.Create(e.ctx, jamie.ID, "invite-jamie", now.Add(24*time.Hour), nil); err != nil {
		t.Fatalf("seed jamie invite: %v", err)
	}
	// Fully credentialed member with authored movies (so a remove would archive).
	// Seed the credentials directly (not seedLogin) so no leftover invite makes
	// this claimed member read as still-pending.
	alex, err := e.users.Create(e.ctx, "Alex")
	if err != nil {
		t.Fatalf("create alex: %v", err)
	}
	if err := e.accounts.Create(e.ctx, alex.ID, "alex", "hash"); err != nil {
		t.Fatalf("seed alex local account: %v", err)
	}
	if err := e.oidc.Insert(e.ctx, domain.OIDCIdentity{
		UserID: alex.ID, Issuer: "https://idp.test", Subject: "alex",
	}, now); err != nil {
		t.Fatalf("seed alex oidc identity: %v", err)
	}
	if _, err := e.movies.Add(e.ctx, "Heat", "pool", alex.ID); err != nil {
		t.Fatalf("add alex movie: %v", err)
	}
	// Archived member: kept for attribution, off the active roster but on this one.
	dana, err := e.users.Create(e.ctx, "Dana")
	if err != nil {
		t.Fatalf("create dana: %v", err)
	}
	if _, err := e.movies.Add(e.ctx, "Ronin", "pool", dana.ID); err != nil {
		t.Fatalf("add dana movie: %v", err)
	}
	if _, err := e.users.Remove(e.ctx, dana.ID); err != nil {
		t.Fatalf("archive dana: %v", err)
	}

	roster, err := e.users.Roster(e.ctx)
	if err != nil {
		t.Fatalf("roster: %v", err)
	}
	byID := rosterByID(t, roster)

	if got := byID[noor.ID]; got.HasLocalLogin || got.HasLinkedIdentity || got.InvitePending || got.Archived {
		t.Fatalf("placeholder Noor has unexpected login state: %+v", got)
	}
	if got := byID[jamie.ID]; !got.InvitePending || got.HasLocalLogin || got.HasLinkedIdentity {
		t.Fatalf("invited placeholder Jamie: want invitePending only, got %+v", got)
	}
	if got := byID[alex.ID]; !got.HasLocalLogin || !got.HasLinkedIdentity || got.InvitePending {
		t.Fatalf("credentialed Alex: want both link-states, no pending invite, got %+v", got)
	}
	if got := byID[alex.ID]; got.MoviesAuthored != 1 {
		t.Fatalf("Alex moviesAuthored = %d, want 1", got.MoviesAuthored)
	}
	dRow, ok := byID[dana.ID]
	if !ok {
		t.Fatal("archived Dana missing from roster (should surface, unlike active List)")
	}
	if !dRow.Archived {
		t.Fatal("Dana row not marked archived")
	}
	if dRow.HasLocalLogin || dRow.HasLinkedIdentity || dRow.InvitePending {
		t.Fatalf("archived Dana still carries login state: %+v", dRow)
	}
	if dRow.MoviesAuthored != 1 {
		t.Fatalf("archived Dana moviesAuthored = %d, want 1 (attribution kept)", dRow.MoviesAuthored)
	}

	// Active members sort before the archived one.
	if roster[len(roster)-1].ID != dana.ID {
		t.Fatalf("archived member not ordered last: got id %d", roster[len(roster)-1].ID)
	}
}

// Promote then demote round-trips the role. Promotion is unconditional; demotion
// is allowed here because a second admin remains after the change.
func TestUserRepo_SetRole_PromoteAndDemote(t *testing.T) {
	e := setupUserRemoveEnv(t)
	root, err := e.users.Create(e.ctx, "Root")
	if err != nil {
		t.Fatalf("create root: %v", err)
	}
	if err := e.users.SetRole(e.ctx, root.ID, domain.RoleAdmin); err != nil {
		t.Fatalf("promote root: %v", err)
	}
	priya, err := e.users.Create(e.ctx, "Priya")
	if err != nil {
		t.Fatalf("create priya: %v", err)
	}
	if err := e.users.SetRole(e.ctx, priya.ID, domain.RoleAdmin); err != nil {
		t.Fatalf("promote priya: %v", err)
	}

	byID := rosterByID(t, mustRoster(t, e))
	if byID[priya.ID].Role != domain.RoleAdmin {
		t.Fatalf("priya role = %q, want admin", byID[priya.ID].Role)
	}

	// Two admins remain, so demoting one is allowed.
	if err := e.users.SetRole(e.ctx, priya.ID, domain.RoleMember); err != nil {
		t.Fatalf("demote priya: %v", err)
	}
	byID = rosterByID(t, mustRoster(t, e))
	if byID[priya.ID].Role != domain.RoleMember {
		t.Fatalf("priya role = %q, want member after demote", byID[priya.ID].Role)
	}
}

// Demoting the only admin is refused so the roster can never be left with no one
// able to run admin actions.
func TestUserRepo_SetRole_RefusesLastAdmin(t *testing.T) {
	e := setupUserRemoveEnv(t)
	root, err := e.users.Create(e.ctx, "Root")
	if err != nil {
		t.Fatalf("create root: %v", err)
	}
	if err := e.users.SetRole(e.ctx, root.ID, domain.RoleAdmin); err != nil {
		t.Fatalf("promote root: %v", err)
	}

	if err := e.users.SetRole(e.ctx, root.ID, domain.RoleMember); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("demote last admin: got %v, want ErrConflict", err)
	}
	// The role is unchanged after the refused demotion.
	byID := rosterByID(t, mustRoster(t, e))
	if byID[root.ID].Role != domain.RoleAdmin {
		t.Fatalf("last admin role changed despite refusal: %q", byID[root.ID].Role)
	}
}

// A role change against a missing or archived member is ErrNotFound: an archived
// member's role is frozen (they are off the active roster).
func TestUserRepo_SetRole_MissingOrArchived(t *testing.T) {
	e := setupUserRemoveEnv(t)
	if err := e.users.SetRole(e.ctx, 4242, domain.RoleAdmin); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("set role on missing member: got %v, want ErrNotFound", err)
	}

	dana, err := e.users.Create(e.ctx, "Dana")
	if err != nil {
		t.Fatalf("create dana: %v", err)
	}
	if _, err := e.movies.Add(e.ctx, "Ronin", "pool", dana.ID); err != nil {
		t.Fatalf("add dana movie: %v", err)
	}
	if _, err := e.users.Remove(e.ctx, dana.ID); err != nil {
		t.Fatalf("archive dana: %v", err)
	}
	if err := e.users.SetRole(e.ctx, dana.ID, domain.RoleAdmin); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("set role on archived member: got %v, want ErrNotFound", err)
	}
}

func mustRoster(t *testing.T, e *userRemoveEnv) []*domain.RosterMember {
	t.Helper()
	roster, err := e.users.Roster(e.ctx)
	if err != nil {
		t.Fatalf("roster: %v", err)
	}
	return roster
}
