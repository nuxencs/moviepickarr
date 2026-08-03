package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"moviepickarr/internal/auth"
	"moviepickarr/internal/db"
	"moviepickarr/internal/repository"

	"github.com/gofiber/fiber/v2"
	"github.com/rs/zerolog"
)

type authTestEnv struct {
	h        *handler
	app      *fiber.App
	users    *repository.SqliteUserRepository
	accounts *repository.SqliteLocalAccountRepository
	pool     *db.Pool
	clk      *fakeClock
}

// setupAuthApp builds a handler over a temp DB with the real route chain from
// registerRoutes (csrfGuard → login → requireSession → the rest), so the auth
// handlers are exercised through the same middleware ordering production uses.
// Both time-driven managers share one fake clock so lockout and session windows
// advance deterministically.
func setupAuthApp(t *testing.T) *authTestEnv {
	t.Helper()

	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "auth-test.db")
	dbConn, err := db.OpenSQLite(dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.RunMigrations(ctx, dbConn.Write); err != nil {
		t.Fatalf("run migrations: %v", err)
	}

	h := newHandler(dbConn, zerolog.Nop())
	clk := &fakeClock{t: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)}
	h.sessions = auth.NewSessionManager(repository.NewSqliteSessionRepository(dbConn), auth.WithClock(clk.now))
	h.localAuth = auth.NewLocalAuth(repository.NewSqliteLocalAccountRepository(dbConn), auth.WithLocalClock(clk.now))
	// The invite manager and scoped transition store share the fake clock so
	// invite expiry and credential commits advance with the test.
	h.invites = auth.NewInviteManager(
		repository.NewSqliteInviteRepository(dbConn),
		repository.NewSqliteAuthTransitionStore(dbConn),
		auth.WithInviteClock(clk.now),
	)

	t.Cleanup(func() {
		h.Close()
		if err := dbConn.Close(); err != nil {
			t.Fatalf("close sqlite: %v", err)
		}
	})

	app := fiber.New()
	registerRoutes(app, h)

	return &authTestEnv{
		h:        h,
		app:      app,
		users:    repository.NewSqliteUserRepository(dbConn),
		accounts: repository.NewSqliteLocalAccountRepository(dbConn),
		pool:     dbConn,
		clk:      clk,
	}
}

func (e *authTestEnv) seedMember(t *testing.T, name, role string) int {
	t.Helper()
	u, err := e.users.Create(context.Background(), name)
	if err != nil {
		t.Fatalf("create member: %v", err)
	}
	if role != "member" {
		if _, err := e.pool.Write.ExecContext(context.Background(),
			"UPDATE users SET role = ? WHERE id = ?", role, u.ID); err != nil {
			t.Fatalf("set role: %v", err)
		}
	}
	return u.ID
}

func (e *authTestEnv) seedLocalLogin(t *testing.T, userID int, username, password string) {
	t.Helper()
	hash, err := auth.HashPassword(password)
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	if err := e.accounts.Create(context.Background(), userID, username, hash); err != nil {
		t.Fatalf("seed local login: %v", err)
	}
}

// request issues an HTTP request. It attaches the same-origin CSRF signal to
// unsafe methods and a JSON body when one is given.
func (e *authTestEnv) request(t *testing.T, method, path, cookie string, body any) *http.Response {
	t.Helper()
	var reader *strings.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		reader = strings.NewReader(string(b))
	} else {
		reader = strings.NewReader("")
	}
	req := httptest.NewRequest(method, path, reader)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if method != http.MethodGet {
		req.Header.Set("Sec-Fetch-Site", "same-origin")
	}
	if cookie != "" {
		req.Header.Set("Cookie", sessionCookieName+"="+cookie)
	}
	resp, err := e.app.Test(req, -1)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	return resp
}

func sessionCookieValue(resp *http.Response) string {
	for _, c := range resp.Cookies() {
		if c.Name == sessionCookieName && c.Value != "" {
			return c.Value
		}
	}
	return ""
}

// login runs the real login endpoint and returns the session cookie.
func (e *authTestEnv) login(t *testing.T, username, password string) string {
	t.Helper()
	resp := e.request(t, http.MethodPost, "/api/v1/auth/login", "", map[string]string{
		"username": username, "password": password,
	})
	if resp.StatusCode != fiber.StatusNoContent {
		t.Fatalf("login status = %d, want 204", resp.StatusCode)
	}
	cookie := sessionCookieValue(resp)
	if cookie == "" {
		t.Fatal("login set no session cookie")
	}
	return cookie
}

func bodyString(t *testing.T, resp *http.Response) string {
	t.Helper()
	b := make([]byte, resp.ContentLength)
	if resp.ContentLength > 0 {
		_, _ = resp.Body.Read(b)
	}
	return string(b)
}

func TestLogin_ValidRoundTripsToMe(t *testing.T) {
	e := setupAuthApp(t)
	id := e.seedMember(t, "Alice", "admin")
	e.seedLocalLogin(t, id, "alice", "correct horse battery")

	cookie := e.login(t, "  ALICE ", "correct horse battery") // trimmed + case-folded

	resp := e.request(t, http.MethodGet, "/api/v1/auth/me", cookie, nil)
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("me status = %d, want 200", resp.StatusCode)
	}
	var me meResponse
	if err := json.NewDecoder(resp.Body).Decode(&me); err != nil {
		t.Fatalf("decode me: %v", err)
	}
	if me.ID != id || me.DisplayName != "Alice" || me.Role != "admin" {
		t.Fatalf("me = %+v", me)
	}
	if me.Username == nil || *me.Username != "alice" {
		t.Fatalf("me.Username = %v, want alice", me.Username)
	}
	if !me.HasLocalLogin || me.HasLinkedIdentity {
		t.Fatalf("flags: hasLocalLogin=%v hasLinkedIdentity=%v", me.HasLocalLogin, me.HasLinkedIdentity)
	}
}

func TestLogin_ArchivedCredentialIsUniformlyRejected(t *testing.T) {
	e := setupAuthApp(t)
	id := e.seedMember(t, "Alice", "admin")
	e.seedLocalLogin(t, id, "alice", "correct horse battery")
	if _, err := e.pool.Write.ExecContext(context.Background(),
		"UPDATE users SET archived_at = unixepoch() WHERE id = ?", id); err != nil {
		t.Fatalf("archive member without cleanup: %v", err)
	}

	archived := e.request(t, http.MethodPost, "/api/v1/auth/login", "", map[string]string{
		"username": "alice", "password": "correct horse battery",
	})
	unknown := e.request(t, http.MethodPost, "/api/v1/auth/login", "", map[string]string{
		"username": "unknown", "password": "correct horse battery",
	})
	if archived.StatusCode != fiber.StatusUnauthorized {
		t.Fatalf("archived login = %d, want 401", archived.StatusCode)
	}
	archivedBody := bodyString(t, archived)
	unknownBody := bodyString(t, unknown)
	if archivedBody != unknownBody {
		t.Fatalf("archived and unknown responses differ: archived=%q unknown=%q", archivedBody, unknownBody)
	}
	if sessionCookieValue(archived) != "" {
		t.Fatal("archived login minted a session cookie")
	}
}

func TestMe_RequiresSession(t *testing.T) {
	e := setupAuthApp(t)
	resp := e.request(t, http.MethodGet, "/api/v1/auth/me", "", nil)
	if resp.StatusCode != fiber.StatusUnauthorized {
		t.Fatalf("me without session = %d, want 401", resp.StatusCode)
	}
}

func TestAuthConfig_ReportsOIDCPresenceUnauthenticated(t *testing.T) {
	e := setupAuthApp(t)

	// No provider configured (the setup wires no OIDC): the endpoint answers
	// without a session and reports SSO off, so the login page hides the button.
	off := e.request(t, http.MethodGet, "/api/v1/auth/config", "", nil)
	if off.StatusCode != fiber.StatusOK {
		t.Fatalf("config status = %d, want 200", off.StatusCode)
	}
	var cfg authConfigResponse
	if err := json.NewDecoder(off.Body).Decode(&cfg); err != nil {
		t.Fatalf("decode config: %v", err)
	}
	if cfg.OIDC {
		t.Fatalf("config.OIDC = true, want false with no provider")
	}

	// Flip the presence-derived flag the same way registerRoutes would once a
	// provider is configured; the endpoint now advertises the SSO button.
	e.h.oidcEnabled = true
	on := e.request(t, http.MethodGet, "/api/v1/auth/config", "", nil)
	if err := json.NewDecoder(on.Body).Decode(&cfg); err != nil {
		t.Fatalf("decode config: %v", err)
	}
	if !cfg.OIDC {
		t.Fatalf("config.OIDC = false, want true when a provider is configured")
	}
}

func TestPosterWall_PublicAndEmptyWhenUnwarmed(t *testing.T) {
	e := setupAuthApp(t)

	// No TMDB key in the test env → posterWall is nil (keyless boot). The route
	// sits ahead of requireSession, so it answers without a session and returns a
	// clean JSON [] the client can fall back from.
	resp := e.request(t, http.MethodGet, "/api/v1/auth/poster-wall", "", nil)
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("poster-wall status = %d, want 200", resp.StatusCode)
	}
	var paths []string
	if err := json.NewDecoder(resp.Body).Decode(&paths); err != nil {
		t.Fatalf("decode poster-wall: %v", err)
	}
	if paths == nil {
		t.Fatal("poster-wall body decoded to null, want a JSON array")
	}
	if len(paths) != 0 {
		t.Fatalf("poster-wall unwarmed = %v, want []", paths)
	}
}

func TestLogin_FailuresAreUniform(t *testing.T) {
	e := setupAuthApp(t)
	id := e.seedMember(t, "Bob", "member")
	e.seedLocalLogin(t, id, "bob", "the right password")

	// Wrong password, unknown username, and (below) a locked account must be
	// identical in status and body.
	wrong := e.request(t, http.MethodPost, "/api/v1/auth/login", "", map[string]string{"username": "bob", "password": "nope"})
	unknown := e.request(t, http.MethodPost, "/api/v1/auth/login", "", map[string]string{"username": "ghost", "password": "nope"})

	wrongBody := bodyString(t, wrong)
	unknownBody := bodyString(t, unknown)
	if wrong.StatusCode != fiber.StatusUnauthorized || unknown.StatusCode != fiber.StatusUnauthorized {
		t.Fatalf("statuses: wrong=%d unknown=%d, want 401", wrong.StatusCode, unknown.StatusCode)
	}
	if wrongBody != unknownBody {
		t.Fatalf("bodies differ: wrong=%q unknown=%q", wrongBody, unknownBody)
	}
	if !strings.Contains(wrongBody, `"invalid credentials"`) {
		t.Fatalf("body = %q, want invalid credentials", wrongBody)
	}
	if sessionCookieValue(wrong) != "" {
		t.Fatal("failed login set a session cookie")
	}
}

func TestLogin_Lockout(t *testing.T) {
	e := setupAuthApp(t)
	id := e.seedMember(t, "Carol", "member")
	e.seedLocalLogin(t, id, "carol", "the right password")

	for i := range 10 {
		resp := e.request(t, http.MethodPost, "/api/v1/auth/login", "", map[string]string{"username": "carol", "password": "wrong"})
		if resp.StatusCode != fiber.StatusUnauthorized {
			t.Fatalf("attempt %d status = %d, want 401", i, resp.StatusCode)
		}
	}

	// Now locked: even the correct password is refused with the same uniform 401.
	locked := e.request(t, http.MethodPost, "/api/v1/auth/login", "", map[string]string{"username": "carol", "password": "the right password"})
	if locked.StatusCode != fiber.StatusUnauthorized {
		t.Fatalf("locked status = %d, want 401", locked.StatusCode)
	}
	if sessionCookieValue(locked) != "" {
		t.Fatal("locked login minted a session")
	}

	// Lock auto-expires after 15 minutes; the correct password then works and
	// resets the counters.
	e.clk.t = e.clk.t.Add(16 * time.Minute)
	cookie := e.login(t, "carol", "the right password")
	if cookie == "" {
		t.Fatal("no cookie after lock expiry")
	}
	acct, _ := e.accounts.FindByUserID(context.Background(), id)
	if acct.FailedAttempts != 0 || acct.LockedUntil != nil {
		t.Fatalf("success did not reset lockout: %+v", acct)
	}
}

func TestChangePassword_RotatesAndRevokes(t *testing.T) {
	e := setupAuthApp(t)
	adminID := e.seedMember(t, "Admin", "admin")
	e.seedLocalLogin(t, adminID, "admin", "admin password ok")
	adminCookie := e.login(t, "admin", "admin password ok")
	id := e.seedMember(t, "Dana", "member")
	e.seedLocalLogin(t, id, "dana", "old password here")
	issue := e.request(
		t,
		http.MethodPost,
		"/api/v1/members/"+strconv.Itoa(id)+"/invite",
		adminCookie,
		map[string]string{"purpose": "password_reset"},
	)
	if issue.StatusCode != fiber.StatusCreated {
		t.Fatalf("reset invite status = %d, want 201", issue.StatusCode)
	}
	var resetInvite inviteResponse
	if err := json.NewDecoder(issue.Body).Decode(&resetInvite); err != nil {
		t.Fatalf("decode reset invite: %v", err)
	}
	resetToken := tokenFromClaimURL(t, resetInvite.ClaimURL)

	deviceA := e.login(t, "dana", "old password here")
	deviceB := e.login(t, "dana", "old password here")

	resp := e.request(t, http.MethodPost, "/api/v1/auth/password", deviceA, map[string]string{
		"currentPassword": "old password here",
		"newPassword":     "brand new password",
	})
	if resp.StatusCode != fiber.StatusNoContent {
		t.Fatalf("change status = %d, want 204", resp.StatusCode)
	}
	rotated := sessionCookieValue(resp)
	if rotated == "" || rotated == deviceA {
		t.Fatalf("current token not rotated (got %q)", rotated)
	}

	// The rotated cookie still authenticates.
	if me := e.request(t, http.MethodGet, "/api/v1/auth/me", rotated, nil); me.StatusCode != fiber.StatusOK {
		t.Fatalf("rotated cookie me = %d, want 200", me.StatusCode)
	}
	// The old device-A token and device-B are both revoked.
	if old := e.request(t, http.MethodGet, "/api/v1/auth/me", deviceA, nil); old.StatusCode != fiber.StatusUnauthorized {
		t.Fatalf("old current token me = %d, want 401", old.StatusCode)
	}
	if other := e.request(t, http.MethodGet, "/api/v1/auth/me", deviceB, nil); other.StatusCode != fiber.StatusUnauthorized {
		t.Fatalf("other device me = %d, want 401", other.StatusCode)
	}

	// The new password logs in; the old one does not.
	_ = e.login(t, "dana", "brand new password")
	bad := e.request(t, http.MethodPost, "/api/v1/auth/login", "", map[string]string{"username": "dana", "password": "old password here"})
	if bad.StatusCode != fiber.StatusUnauthorized {
		t.Fatalf("old password login = %d, want 401", bad.StatusCode)
	}
	if claim := e.request(t, http.MethodGet, "/api/v1/auth/claim/"+resetToken, "", nil); claim.StatusCode != fiber.StatusNotFound {
		t.Fatalf("reset invite after password change = %d, want 404", claim.StatusCode)
	}
}

func TestLogout_CurrentDeviceOnly(t *testing.T) {
	e := setupAuthApp(t)
	id := e.seedMember(t, "Hank", "member")
	e.seedLocalLogin(t, id, "hank", "correct horse battery")

	deviceA := e.login(t, "hank", "correct horse battery")
	deviceB := e.login(t, "hank", "correct horse battery")

	// Empty body → log out just this device. Cookie cleared, 204.
	resp := e.request(t, http.MethodPost, "/api/v1/auth/logout", deviceA, nil)
	if resp.StatusCode != fiber.StatusNoContent {
		t.Fatalf("logout status = %d, want 204", resp.StatusCode)
	}
	// The cleared cookie is sent as an empty, already-expired value.
	if sessionCookieValue(resp) != "" {
		t.Fatal("logout did not clear the session cookie")
	}

	// Device A is revoked; device B still authenticates.
	if a := e.request(t, http.MethodGet, "/api/v1/auth/me", deviceA, nil); a.StatusCode != fiber.StatusUnauthorized {
		t.Fatalf("logged-out device me = %d, want 401", a.StatusCode)
	}
	if b := e.request(t, http.MethodGet, "/api/v1/auth/me", deviceB, nil); b.StatusCode != fiber.StatusOK {
		t.Fatalf("other device me = %d, want 200", b.StatusCode)
	}

	// A second logout with the now-revoked cookie is rejected by requireSession
	// before the handler runs: the session gate sits ahead of logout, so a dead
	// cookie is a 401, not another 204. The client is already at the login screen.
	again := e.request(t, http.MethodPost, "/api/v1/auth/logout", deviceA, nil)
	if again.StatusCode != fiber.StatusUnauthorized {
		t.Fatalf("repeat logout with revoked cookie = %d, want 401", again.StatusCode)
	}
}

func TestLogout_Everywhere(t *testing.T) {
	e := setupAuthApp(t)
	id := e.seedMember(t, "Iris", "member")
	e.seedLocalLogin(t, id, "iris", "correct horse battery")

	deviceA := e.login(t, "iris", "correct horse battery")
	deviceB := e.login(t, "iris", "correct horse battery")

	// {"all":true} → every session for the member is revoked, this one included.
	resp := e.request(t, http.MethodPost, "/api/v1/auth/logout", deviceA, map[string]bool{"all": true})
	if resp.StatusCode != fiber.StatusNoContent {
		t.Fatalf("logout-all status = %d, want 204", resp.StatusCode)
	}
	for name, cookie := range map[string]string{"A": deviceA, "B": deviceB} {
		if r := e.request(t, http.MethodGet, "/api/v1/auth/me", cookie, nil); r.StatusCode != fiber.StatusUnauthorized {
			t.Fatalf("device %s me after logout-all = %d, want 401", name, r.StatusCode)
		}
	}
}

func TestLogout_RequiresSession(t *testing.T) {
	e := setupAuthApp(t)
	resp := e.request(t, http.MethodPost, "/api/v1/auth/logout", "", nil)
	if resp.StatusCode != fiber.StatusUnauthorized {
		t.Fatalf("logout without session = %d, want 401", resp.StatusCode)
	}
}

func TestChangePassword_WrongCurrentAndNoLocalLogin(t *testing.T) {
	e := setupAuthApp(t)
	id := e.seedMember(t, "Erin", "member")
	e.seedLocalLogin(t, id, "erin", "old password here")
	cookie := e.login(t, "erin", "old password here")

	// Wrong current password → generic 401.
	wrong := e.request(t, http.MethodPost, "/api/v1/auth/password", cookie, map[string]string{
		"currentPassword": "not it", "newPassword": "a fresh password",
	})
	if wrong.StatusCode != fiber.StatusUnauthorized {
		t.Fatalf("wrong current = %d, want 401", wrong.StatusCode)
	}

	// A member with a valid session but no local login → 409. Mint a session for
	// a placeholder member directly (they cannot obtain one through login).
	placeholder := e.seedMember(t, "Frank", "member")
	raw, _, err := e.h.sessions.Mint(context.Background(), placeholder, nil)
	if err != nil {
		t.Fatalf("mint placeholder session: %v", err)
	}
	noLocal := e.request(t, http.MethodPost, "/api/v1/auth/password", raw, map[string]string{
		"currentPassword": "whatever", "newPassword": "a fresh password",
	})
	if noLocal.StatusCode != fiber.StatusConflict {
		t.Fatalf("no-local-login change = %d, want 409", noLocal.StatusCode)
	}
}

func TestAdminSetLocalLogin(t *testing.T) {
	e := setupAuthApp(t)
	adminID := e.seedMember(t, "Admin", "admin")
	e.seedLocalLogin(t, adminID, "admin", "admin password ok")
	adminCookie := e.login(t, "admin", "admin password ok")

	placeholder := e.seedMember(t, "Gwen", "member")

	// Create a first local login for the placeholder.
	create := e.request(t, http.MethodPut, "/api/v1/members/"+strconv.Itoa(placeholder)+"/local-login", adminCookie,
		map[string]string{"username": "gwen", "password": "gwen password ok"})
	if create.StatusCode != fiber.StatusNoContent {
		t.Fatalf("create status = %d, want 204", create.StatusCode)
	}
	// Gwen can now log in.
	gwenCookie := e.login(t, "gwen", "gwen password ok")

	// Reset (existing row) revokes the target's sessions.
	reset := e.request(t, http.MethodPut, "/api/v1/members/"+strconv.Itoa(placeholder)+"/local-login", adminCookie,
		map[string]string{"password": "reset password ok"})
	if reset.StatusCode != fiber.StatusNoContent {
		t.Fatalf("reset status = %d, want 204", reset.StatusCode)
	}
	if me := e.request(t, http.MethodGet, "/api/v1/auth/me", gwenCookie, nil); me.StatusCode != fiber.StatusUnauthorized {
		t.Fatalf("gwen session after reset = %d, want 401 (revoked)", me.StatusCode)
	}
	_ = e.login(t, "gwen", "reset password ok") // the reset password works

	// NOCASE username collision → 409.
	other := e.seedMember(t, "Hank", "member")
	collision := e.request(t, http.MethodPut, "/api/v1/members/"+strconv.Itoa(other)+"/local-login", adminCookie,
		map[string]string{"username": "GWEN", "password": "hank password ok"})
	if collision.StatusCode != fiber.StatusConflict {
		t.Fatalf("collision status = %d, want 409", collision.StatusCode)
	}

	// A non-admin is forbidden.
	nonAdmin := e.seedMember(t, "Ivy", "member")
	e.seedLocalLogin(t, nonAdmin, "ivy", "ivy password ok")
	ivyCookie := e.login(t, "ivy", "ivy password ok")
	forbidden := e.request(t, http.MethodPut, "/api/v1/members/"+strconv.Itoa(other)+"/local-login", ivyCookie,
		map[string]string{"username": "newname", "password": "some password ok"})
	if forbidden.StatusCode != fiber.StatusForbidden {
		t.Fatalf("non-admin PUT = %d, want 403", forbidden.StatusCode)
	}
}

func TestAdminSetLocalLogin_ArchivedMemberIsNotFound(t *testing.T) {
	e := setupAuthApp(t)
	adminID := e.seedMember(t, "Admin", "admin")
	e.seedLocalLogin(t, adminID, "admin", "admin password ok")
	adminCookie := e.login(t, "admin", "admin password ok")

	target := e.seedMember(t, "Archived", "member")
	if _, err := e.pool.Write.ExecContext(context.Background(),
		"UPDATE users SET archived_at = unixepoch() WHERE id = ?", target); err != nil {
		t.Fatalf("archive target: %v", err)
	}

	resp := e.request(t, http.MethodPut, "/api/v1/members/"+strconv.Itoa(target)+"/local-login", adminCookie,
		map[string]string{"username": "archived", "password": "archived password"})
	if resp.StatusCode != fiber.StatusNotFound {
		t.Fatalf("set archived login = %d, want 404", resp.StatusCode)
	}
	var n int
	if err := e.pool.Read.QueryRowContext(context.Background(),
		"SELECT COUNT(*) FROM local_accounts WHERE user_id = ?", target).Scan(&n); err != nil {
		t.Fatalf("count target credentials: %v", err)
	}
	if n != 0 {
		t.Fatalf("archived target gained %d local credentials", n)
	}
}

func TestAdminDeleteLocalLogin(t *testing.T) {
	e := setupAuthApp(t)
	adminID := e.seedMember(t, "Admin", "admin")
	e.seedLocalLogin(t, adminID, "admin", "admin password ok")
	adminCookie := e.login(t, "admin", "admin password ok")

	target := e.seedMember(t, "Jack", "member")
	e.seedLocalLogin(t, target, "jack", "jack password ok")
	targetCookie := e.login(t, "jack", "jack password ok")
	issue := e.request(
		t,
		http.MethodPost,
		"/api/v1/members/"+strconv.Itoa(target)+"/invite",
		adminCookie,
		map[string]string{"purpose": "password_reset"},
	)
	if issue.StatusCode != fiber.StatusCreated {
		t.Fatalf("reset invite status = %d, want 201", issue.StatusCode)
	}
	var resetInvite inviteResponse
	if err := json.NewDecoder(issue.Body).Decode(&resetInvite); err != nil {
		t.Fatalf("decode reset invite: %v", err)
	}
	resetToken := tokenFromClaimURL(t, resetInvite.ClaimURL)

	// Delete another member's login → 204, then it's gone (login fails).
	del := e.request(t, http.MethodDelete, "/api/v1/members/"+strconv.Itoa(target)+"/local-login", adminCookie, nil)
	if del.StatusCode != fiber.StatusNoContent {
		t.Fatalf("delete status = %d, want 204", del.StatusCode)
	}
	gone := e.request(t, http.MethodPost, "/api/v1/auth/login", "", map[string]string{"username": "jack", "password": "jack password ok"})
	if gone.StatusCode != fiber.StatusUnauthorized {
		t.Fatalf("deleted login = %d, want 401", gone.StatusCode)
	}
	if claim := e.request(t, http.MethodGet, "/api/v1/auth/claim/"+resetToken, "", nil); claim.StatusCode != fiber.StatusNotFound {
		t.Fatalf("reset invite after password removal = %d, want 404", claim.StatusCode)
	}
	if old := e.request(t, http.MethodGet, "/api/v1/auth/me", targetCookie, nil); old.StatusCode != fiber.StatusUnauthorized {
		t.Fatalf("session after last password removal = %d, want 401", old.StatusCode)
	}

	// Deleting a member with no local login → 404.
	missing := e.request(t, http.MethodDelete, "/api/v1/members/"+strconv.Itoa(target)+"/local-login", adminCookie, nil)
	if missing.StatusCode != fiber.StatusNotFound {
		t.Fatalf("missing delete = %d, want 404", missing.StatusCode)
	}

	// The admin cannot delete their own last credential.
	self := e.request(t, http.MethodDelete, "/api/v1/members/"+strconv.Itoa(adminID)+"/local-login", adminCookie, nil)
	if self.StatusCode != fiber.StatusConflict {
		t.Fatalf("self last-credential delete = %d, want 409", self.StatusCode)
	}
}
