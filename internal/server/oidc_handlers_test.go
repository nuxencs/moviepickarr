package server

import (
	"context"
	"encoding/base64"
	"encoding/json/v2"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"moviepickarr/internal/auth"
	"moviepickarr/internal/db"
	"moviepickarr/internal/domain"
	"moviepickarr/internal/repository"

	"github.com/gofiber/fiber/v2"
	"github.com/rs/zerolog"
)

const (
	testOIDCClientID = "test-client"
	testOIDCSecret   = "test-secret"
	testOIDCRedirect = "http://localhost/api/v1/auth/oidc/callback"
)

// oidcTestEnv wraps the shared auth test env with the fake provider and the
// identity store, so OIDC tests reuse the same seed/login/request helpers as the
// local-auth tests.
type oidcTestEnv struct {
	*authTestEnv
	idp        *fakeIdP
	identities *repository.SqliteOIDCIdentityRepository
}

// setupOIDCApp builds a handler over a temp DB with the fake provider wired in
// before registerRoutes, so the /oidc/* routes are mounted and the real route
// chain (csrfGuard → unauth OIDC → requireSession → authed OIDC) is exercised.
// The tx codec and auth stores share the same fake clock as sessions/invites so
// expiry and last-login timestamps advance deterministically.
func setupOIDCApp(t *testing.T) *oidcTestEnv {
	t.Helper()

	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "oidc-test.db")
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
	localAccounts := repository.NewSqliteLocalAccountRepository(dbConn)
	h.localAuth = auth.NewLocalAuth(localAccounts, auth.WithLocalClock(clk.now))
	h.invites = auth.NewInviteManager(
		repository.NewSqliteInviteRepository(dbConn),
		repository.NewSqliteAuthTransitionStore(dbConn),
		auth.WithInviteClock(clk.now),
	)

	idp := newFakeIdP(t)
	identities := repository.NewSqliteOIDCIdentityRepository(dbConn)
	rp, err := auth.NewRelyingParty(ctx, auth.OIDCConfig{
		Issuer:       idp.issuer(),
		ClientID:     testOIDCClientID,
		ClientSecret: testOIDCSecret,
		RedirectURL:  testOIDCRedirect,
	})
	if err != nil {
		t.Fatalf("new relying party: %v", err)
	}
	txCodec, err := auth.NewOIDCTxCodec("fixed-test-tx-secret", auth.WithTxClock(clk.now))
	if err != nil {
		t.Fatalf("new tx codec: %v", err)
	}
	h.oidc = rp
	h.oidcTx = txCodec
	h.oidcEnabled = true

	t.Cleanup(func() {
		h.Close()
		if err := dbConn.Close(); err != nil {
			t.Fatalf("close sqlite: %v", err)
		}
	})

	app := fiber.New()
	registerRoutes(app, h)

	return &oidcTestEnv{
		authTestEnv: &authTestEnv{
			h:        h,
			app:      app,
			users:    repository.NewSqliteUserRepository(dbConn),
			accounts: localAccounts,
			pool:     dbConn,
			clk:      clk,
		},
		idp:        idp,
		identities: identities,
	}
}

// linkIdentity seeds an oidc_identities row directly, standing in for a member
// who has already linked their SSO identity.
func (e *oidcTestEnv) linkIdentity(t *testing.T, userID int, subject, email string) {
	t.Helper()
	now := e.clk.t
	em := email
	if err := e.identities.Insert(context.Background(), domain.OIDCIdentity{
		UserID:      userID,
		Issuer:      e.idp.issuer(),
		Subject:     subject,
		Email:       &em,
		LastLoginAt: &now,
	}, now); err != nil {
		t.Fatalf("seed linked identity: %v", err)
	}
}

func (e *oidcTestEnv) countIdentities(t *testing.T) int {
	t.Helper()
	var n int
	if err := e.pool.Read.QueryRowContext(context.Background(),
		"SELECT COUNT(*) FROM oidc_identities").Scan(&n); err != nil {
		t.Fatalf("count identities: %v", err)
	}
	return n
}

// getWithCookies issues a GET carrying the given cookies verbatim (the OIDC flow
// juggles the tx cookie and the session cookie together, which the shared
// request helper can't express).
func (e *oidcTestEnv) getWithCookies(t *testing.T, path string, cookies ...*http.Cookie) *http.Response {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if hdr := cookieHeader(cookies...); hdr != "" {
		req.Header.Set("Cookie", hdr)
	}
	resp, err := e.app.Test(req, -1)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	return resp
}

// begin runs an initiation endpoint and returns the state + nonce the provider
// redirect carried plus the tx cookie it set, so a test can arm the fake IdP and
// replay the callback.
func (e *oidcTestEnv) begin(t *testing.T, path string, cookies ...*http.Cookie) (state, nonce string, tx *http.Cookie) {
	t.Helper()
	resp := e.getWithCookies(t, path, cookies...)
	if resp.StatusCode != fiber.StatusFound {
		t.Fatalf("begin %s = %d, want 302", path, resp.StatusCode)
	}
	loc, err := url.Parse(resp.Header.Get("Location"))
	if err != nil {
		t.Fatalf("parse authorize location: %v", err)
	}
	tx = findCookie(resp, oidcTxCookieName)
	if tx == nil || tx.Value == "" {
		t.Fatal("initiation set no tx cookie")
	}
	return loc.Query().Get("state"), loc.Query().Get("nonce"), tx
}

// callback replays the provider redirect with the given query and cookies.
func (e *oidcTestEnv) callback(t *testing.T, query string, cookies ...*http.Cookie) *http.Response {
	t.Helper()
	return e.getWithCookies(t, "/api/v1/auth/oidc/callback?"+query, cookies...)
}

func cookieHeader(cookies ...*http.Cookie) string {
	parts := make([]string, 0, len(cookies))
	for _, c := range cookies {
		if c != nil && c.Value != "" {
			parts = append(parts, c.Name+"="+c.Value)
		}
	}
	return strings.Join(parts, "; ")
}

func findCookie(resp *http.Response, name string) *http.Cookie {
	for _, c := range resp.Cookies() {
		if c.Name == name {
			return c
		}
	}
	return nil
}

func sessionCookie(value string) *http.Cookie {
	return &http.Cookie{Name: sessionCookieName, Value: value}
}

// locationError parses the ?error= bucket off a 302 Location.
func locationError(t *testing.T, resp *http.Response) string {
	t.Helper()
	if resp.StatusCode != fiber.StatusFound {
		t.Fatalf("status = %d, want 302", resp.StatusCode)
	}
	loc, err := url.Parse(resp.Header.Get("Location"))
	if err != nil {
		t.Fatalf("parse location: %v", err)
	}
	return loc.Query().Get("error")
}

func TestOIDC_LoginLinkedMintsSession(t *testing.T) {
	e := setupOIDCApp(t)
	member := e.seedMember(t, "Alice", "member")
	e.linkIdentity(t, member, "alice-sub", "alice@example.com")

	state, nonce, tx := e.begin(t, "/api/v1/auth/oidc/login")
	e.idp.setIDToken(t, idTokenClaims{
		Sub: "alice-sub", Aud: testOIDCClientID, Nonce: nonce, Email: "alice-new@example.com",
	})

	resp := e.callback(t, "code=abc&state="+state, tx)
	if resp.StatusCode != fiber.StatusFound {
		t.Fatalf("callback = %d, want 302", resp.StatusCode)
	}
	if loc := resp.Header.Get("Location"); loc != "/" {
		t.Fatalf("login redirect = %q, want /", loc)
	}
	if sessionCookieValue(resp) == "" {
		t.Fatal("linked login minted no session")
	}
	// Snapshot refreshed on login.
	oi, err := e.identities.FindByIssuerSubject(context.Background(), e.idp.issuer(), "alice-sub")
	if err != nil {
		t.Fatalf("find identity: %v", err)
	}
	if oi.Email == nil || *oi.Email != "alice-new@example.com" {
		t.Fatalf("email snapshot = %v, want refreshed", oi.Email)
	}
}

func TestOIDC_LoginArchivedIdentityIsUnlinked(t *testing.T) {
	e := setupOIDCApp(t)
	member := e.seedMember(t, "Alice", "member")
	e.linkIdentity(t, member, "alice-sub", "alice@example.com")
	if _, err := e.pool.Write.ExecContext(context.Background(),
		"UPDATE users SET archived_at = unixepoch() WHERE id = ?", member); err != nil {
		t.Fatalf("archive member without cleanup: %v", err)
	}

	state, nonce, tx := e.begin(t, "/api/v1/auth/oidc/login")
	e.idp.setIDToken(t, idTokenClaims{
		Sub: "alice-sub", Aud: testOIDCClientID, Nonce: nonce, Email: "alice@example.com",
	})

	resp := e.callback(t, "code=abc&state="+state, tx)
	if got := locationError(t, resp); got != errOIDCUnlinked {
		t.Fatalf("archived identity error = %q, want %q", got, errOIDCUnlinked)
	}
	if findCookie(resp, sessionCookieName) != nil {
		t.Fatal("archived OIDC login minted a session cookie")
	}
}

func TestOIDC_LoginUnlinkedRejectsEphemerally(t *testing.T) {
	e := setupOIDCApp(t)

	state, nonce, tx := e.begin(t, "/api/v1/auth/oidc/login")
	e.idp.setIDToken(t, idTokenClaims{
		Sub: "stranger-sub", Aud: testOIDCClientID, Nonce: nonce, Email: "stranger@example.com",
	})

	resp := e.callback(t, "code=abc&state="+state, tx)
	if got := locationError(t, resp); got != errOIDCUnlinked {
		t.Fatalf("error bucket = %q, want %q", got, errOIDCUnlinked)
	}
	if sessionCookieValue(resp) != "" {
		t.Fatal("unlinked login minted a session")
	}
	if n := e.countIdentities(t); n != 0 {
		t.Fatalf("unlinked login persisted %d identities, want 0", n)
	}
}

func TestOIDC_CallbackProviderErrorDenied(t *testing.T) {
	e := setupOIDCApp(t)
	_, _, tx := e.begin(t, "/api/v1/auth/oidc/login")

	resp := e.callback(t, "error=access_denied", tx)
	if got := locationError(t, resp); got != errOIDCDenied {
		t.Fatalf("error bucket = %q, want %q", got, errOIDCDenied)
	}
}

func TestOIDC_CallbackTamperedTxExpired(t *testing.T) {
	e := setupOIDCApp(t)
	state, _, tx := e.begin(t, "/api/v1/auth/oidc/login")

	// Corrupt the AEAD ciphertext: the auth tag no longer verifies, so Open
	// rejects it as invalid.
	tampered := &http.Cookie{Name: tx.Name, Value: corruptSealed(t, tx.Value)}
	resp := e.callback(t, "code=abc&state="+state, tampered)
	if got := locationError(t, resp); got != errOIDCExpired {
		t.Fatalf("error bucket = %q, want %q", got, errOIDCExpired)
	}
}

func TestOIDC_CallbackStateMismatchFailed(t *testing.T) {
	e := setupOIDCApp(t)
	_, nonce, tx := e.begin(t, "/api/v1/auth/oidc/login")
	e.idp.setIDToken(t, idTokenClaims{Sub: "x", Aud: testOIDCClientID, Nonce: nonce})

	resp := e.callback(t, "code=abc&state=not-the-real-state", tx)
	if got := locationError(t, resp); got != errOIDCFailed {
		t.Fatalf("error bucket = %q, want %q", got, errOIDCFailed)
	}
}

func TestOIDC_LinkSuccessAndIdempotent(t *testing.T) {
	e := setupOIDCApp(t)
	id := e.seedMember(t, "Bob", "member")
	e.seedLocalLogin(t, id, "bob", "bob password here")
	cookie := sessionCookie(e.login(t, "bob", "bob password here"))

	// First link: writes the identity, lands on settings?linked=1.
	state, nonce, tx := e.begin(t, "/api/v1/auth/oidc/link", cookie)
	e.idp.setIDToken(t, idTokenClaims{Sub: "bob-sub", Aud: testOIDCClientID, Nonce: nonce, Email: "bob@example.com"})
	resp := e.callback(t, "code=abc&state="+state, tx, cookie)
	loc, _ := url.Parse(resp.Header.Get("Location"))
	if resp.StatusCode != fiber.StatusFound || loc.Path != oidcLinkRedirect || loc.Query().Get("linked") != "1" {
		t.Fatalf("link redirect = %q (status %d), want %s?linked=1", resp.Header.Get("Location"), resp.StatusCode, oidcLinkRedirect)
	}
	if n := e.countIdentities(t); n != 1 {
		t.Fatalf("after link identities = %d, want 1", n)
	}

	// Re-linking the same identity is idempotent success, not a conflict.
	state2, nonce2, tx2 := e.begin(t, "/api/v1/auth/oidc/link", cookie)
	e.idp.setIDToken(t, idTokenClaims{Sub: "bob-sub", Aud: testOIDCClientID, Nonce: nonce2, Email: "bob@example.com"})
	resp2 := e.callback(t, "code=abc&state="+state2, tx2, cookie)
	loc2, _ := url.Parse(resp2.Header.Get("Location"))
	if resp2.StatusCode != fiber.StatusFound || loc2.Query().Get("linked") != "1" {
		t.Fatalf("idempotent re-link = %q (status %d), want linked=1", resp2.Header.Get("Location"), resp2.StatusCode)
	}
	if n := e.countIdentities(t); n != 1 {
		t.Fatalf("after re-link identities = %d, want 1", n)
	}
}

func TestOIDC_LinkConflictWritesNothing(t *testing.T) {
	e := setupOIDCApp(t)
	// Carol already owns the identity.
	carol := e.seedMember(t, "Carol", "member")
	e.linkIdentity(t, carol, "shared-sub", "carol@example.com")

	// Dave, logged in, tries to link the same (issuer, subject).
	dave := e.seedMember(t, "Dave", "member")
	e.seedLocalLogin(t, dave, "dave", "dave password here")
	cookie := sessionCookie(e.login(t, "dave", "dave password here"))

	state, nonce, tx := e.begin(t, "/api/v1/auth/oidc/link", cookie)
	e.idp.setIDToken(t, idTokenClaims{Sub: "shared-sub", Aud: testOIDCClientID, Nonce: nonce})
	resp := e.callback(t, "code=abc&state="+state, tx, cookie)
	if got := locationError(t, resp); got != errOIDCLinkConflict {
		t.Fatalf("error bucket = %q, want %q", got, errOIDCLinkConflict)
	}
	// Still exactly one identity, still Carol's.
	if n := e.countIdentities(t); n != 1 {
		t.Fatalf("after conflict identities = %d, want 1", n)
	}
	oi, _ := e.identities.FindByIssuerSubject(context.Background(), e.idp.issuer(), "shared-sub")
	if oi.UserID != carol {
		t.Fatalf("identity owner = %d, want carol (%d)", oi.UserID, carol)
	}
}

func TestOIDC_LinkSessionMismatchExpired(t *testing.T) {
	e := setupOIDCApp(t)
	id := e.seedMember(t, "Erin", "member")
	e.seedLocalLogin(t, id, "erin", "erin password here")
	cookie := sessionCookie(e.login(t, "erin", "erin password here"))

	state, nonce, tx := e.begin(t, "/api/v1/auth/oidc/link", cookie)
	e.idp.setIDToken(t, idTokenClaims{Sub: "erin-sub", Aud: testOIDCClientID, Nonce: nonce})

	// Replay the callback with NO session cookie: the tx member no longer matches
	// a live session.
	resp := e.callback(t, "code=abc&state="+state, tx)
	if got := locationError(t, resp); got != errOIDCSessionExpired {
		t.Fatalf("error bucket = %q, want %q", got, errOIDCSessionExpired)
	}
	if n := e.countIdentities(t); n != 0 {
		t.Fatalf("session-mismatch link wrote %d identities, want 0", n)
	}
}

func TestOIDC_ClaimLinksConsumesMints(t *testing.T) {
	e := setupOIDCApp(t)
	admin := e.seedMember(t, "Admin", "admin")
	placeholder := e.seedMember(t, "Frank", "member")
	raw, err := e.h.invites.Issue(context.Background(), placeholder, admin)
	if err != nil {
		t.Fatalf("issue invite: %v", err)
	}

	state, nonce, tx := e.begin(t, "/api/v1/auth/claim/"+raw+"/oidc")
	e.idp.setIDToken(t, idTokenClaims{Sub: "frank-sub", Aud: testOIDCClientID, Nonce: nonce, Email: "frank@example.com"})

	resp := e.callback(t, "code=abc&state="+state, tx)
	if resp.StatusCode != fiber.StatusFound || resp.Header.Get("Location") != "/" {
		t.Fatalf("claim redirect = %q (status %d), want /", resp.Header.Get("Location"), resp.StatusCode)
	}
	if sessionCookieValue(resp) == "" {
		t.Fatal("claim minted no session")
	}
	// Identity linked to the placeholder.
	oi, err := e.identities.FindByUserID(context.Background(), placeholder)
	if err != nil {
		t.Fatalf("find claimed identity: %v", err)
	}
	if oi.Subject != "frank-sub" {
		t.Fatalf("linked subject = %q, want frank-sub", oi.Subject)
	}
	// Invite consumed → now reads as already-used.
	if _, err := e.h.invites.Validate(context.Background(), raw); err != auth.ErrInviteUsed {
		t.Fatalf("invite after claim = %v, want ErrInviteUsed", err)
	}
}

func TestOIDC_PasswordResetClaimDoesNotOfferOrStartSSO(t *testing.T) {
	e := setupOIDCApp(t)
	admin := e.seedMember(t, "Admin", "admin")
	member := e.seedMember(t, "Inez", "member")
	e.seedLocalLogin(t, member, "inez", "inez password here")
	raw, err := e.h.invites.IssuePasswordReset(context.Background(), member, admin)
	if err != nil {
		t.Fatalf("issue password reset invite: %v", err)
	}

	validate := e.request(t, http.MethodGet, "/api/v1/auth/claim/"+raw, "", nil)
	if validate.StatusCode != fiber.StatusOK {
		t.Fatalf("validate password reset claim = %d, want 200", validate.StatusCode)
	}
	var claim claimResponse
	if err := json.UnmarshalRead(validate.Body, &claim); err != nil {
		t.Fatalf("decode password reset claim: %v", err)
	}
	if claim.Mode != "reset" || claim.Options.OIDC {
		t.Fatalf("password reset claim = %+v, want reset with OIDC off", claim)
	}

	start := e.getWithCookies(t, "/api/v1/auth/claim/"+raw+"/oidc")
	if start.StatusCode != fiber.StatusFound || start.Header.Get("Location") != "/claim/"+raw {
		t.Fatalf("reset OIDC start = %d %q, want claim-page redirect", start.StatusCode, start.Header.Get("Location"))
	}
	if tx := findCookie(start, oidcTxCookieName); tx != nil && tx.Value != "" {
		t.Fatal("password reset OIDC start minted a transaction cookie")
	}
	if n := e.countIdentities(t); n != 0 {
		t.Fatalf("password reset OIDC start linked %d identities, want 0", n)
	}
}

func TestOIDC_ClaimConflictDoesNotConsume(t *testing.T) {
	e := setupOIDCApp(t)
	admin := e.seedMember(t, "Admin", "admin")
	// The identity is already linked to someone else.
	other := e.seedMember(t, "Gwen", "member")
	e.linkIdentity(t, other, "taken-sub", "gwen@example.com")

	placeholder := e.seedMember(t, "Hank", "member")
	raw, err := e.h.invites.Issue(context.Background(), placeholder, admin)
	if err != nil {
		t.Fatalf("issue invite: %v", err)
	}

	state, nonce, tx := e.begin(t, "/api/v1/auth/claim/"+raw+"/oidc")
	e.idp.setIDToken(t, idTokenClaims{Sub: "taken-sub", Aud: testOIDCClientID, Nonce: nonce})

	resp := e.callback(t, "code=abc&state="+state, tx)
	if got := locationError(t, resp); got != errOIDCLinkConflict {
		t.Fatalf("error bucket = %q, want %q", got, errOIDCLinkConflict)
	}
	if sessionCookieValue(resp) != "" {
		t.Fatal("conflicting claim minted a session")
	}
	// Invite NOT consumed: still validatable (placeholder mode).
	if _, err := e.h.invites.Validate(context.Background(), raw); err != nil {
		t.Fatalf("invite after conflict = %v, want still valid", err)
	}
	// Placeholder gained no identity.
	if _, err := e.identities.FindByUserID(context.Background(), placeholder); err == nil {
		t.Fatal("conflicting claim linked the placeholder")
	}
}

func TestOIDC_UnlinkSelfLastCredentialGuard(t *testing.T) {
	e := setupOIDCApp(t)
	member := e.seedMember(t, "Zoe", "member")
	e.linkIdentity(t, member, "zoe-sub", "zoe@example.com")
	raw, _, err := e.h.sessions.Mint(context.Background(), member, nil)
	if err != nil {
		t.Fatalf("mint session: %v", err)
	}

	// Only credential is the identity: self-unlink is refused with 409.
	resp := e.request(t, http.MethodDelete, "/api/v1/auth/linked-identity", raw, nil)
	if resp.StatusCode != fiber.StatusConflict {
		t.Fatalf("last-credential unlink = %d, want 409", resp.StatusCode)
	}
	if n := e.countIdentities(t); n != 1 {
		t.Fatalf("refused unlink still removed the identity (count %d)", n)
	}

	// Add a local login, and now the unlink succeeds.
	e.seedLocalLogin(t, member, "zoe", "zoe password here")
	resetToken, err := e.h.invites.IssuePasswordReset(context.Background(), member, member)
	if err != nil {
		t.Fatalf("issue reset invite: %v", err)
	}
	ok := e.request(t, http.MethodDelete, "/api/v1/auth/linked-identity", raw, nil)
	if ok.StatusCode != fiber.StatusNoContent {
		t.Fatalf("unlink with fallback = %d, want 204", ok.StatusCode)
	}
	if n := e.countIdentities(t); n != 0 {
		t.Fatalf("after unlink identities = %d, want 0", n)
	}
	if _, err := e.h.invites.Validate(context.Background(), resetToken); !errors.Is(err, auth.ErrInviteInvalid) {
		t.Fatalf("reset invite after unlink = %v, want ErrInviteInvalid", err)
	}
}

func TestOIDC_UnlinkAdminAndForbidden(t *testing.T) {
	e := setupOIDCApp(t)
	adminID := e.seedMember(t, "Admin", "admin")
	e.seedLocalLogin(t, adminID, "admin", "admin password ok")
	adminCookie := e.login(t, "admin", "admin password ok")

	target := e.seedMember(t, "Ivy", "member")
	e.linkIdentity(t, target, "ivy-sub", "ivy@example.com")
	targetCookie, _, err := e.h.sessions.Mint(context.Background(), target, nil)
	if err != nil {
		t.Fatalf("mint target session: %v", err)
	}

	// Admin removes another member's identity even though it's their last
	// credential (they fall back to a placeholder): 204.
	del := e.request(t, http.MethodDelete, "/api/v1/members/"+strconv.Itoa(target)+"/linked-identity", adminCookie, nil)
	if del.StatusCode != fiber.StatusNoContent {
		t.Fatalf("admin unlink = %d, want 204", del.StatusCode)
	}
	if n := e.countIdentities(t); n != 0 {
		t.Fatalf("after admin unlink identities = %d, want 0", n)
	}
	if old := e.request(t, http.MethodGet, "/api/v1/auth/me", targetCookie, nil); old.StatusCode != fiber.StatusUnauthorized {
		t.Fatalf("session after last identity removal = %d, want 401", old.StatusCode)
	}

	// A non-admin is forbidden.
	nonAdmin := e.seedMember(t, "Jack", "member")
	e.seedLocalLogin(t, nonAdmin, "jack", "jack password ok")
	jackCookie := e.login(t, "jack", "jack password ok")
	forbidden := e.request(t, http.MethodDelete, "/api/v1/members/"+strconv.Itoa(adminID)+"/linked-identity", jackCookie, nil)
	if forbidden.StatusCode != fiber.StatusForbidden {
		t.Fatalf("non-admin unlink = %d, want 403", forbidden.StatusCode)
	}
}

func TestOIDC_RoutesAbsentWhenDisabled(t *testing.T) {
	e := setupAuthApp(t) // no OIDC wired

	login := e.request(t, http.MethodGet, "/api/v1/auth/oidc/login", "", nil)
	if login.StatusCode != fiber.StatusNotFound {
		t.Fatalf("oidc login when disabled = %d, want 404", login.StatusCode)
	}
	callback := e.request(t, http.MethodGet, "/api/v1/auth/oidc/callback", "", nil)
	if callback.StatusCode != fiber.StatusNotFound {
		t.Fatalf("oidc callback when disabled = %d, want 404", callback.StatusCode)
	}

	// The claim page reports SSO off when no provider is configured.
	admin := e.seedMember(t, "Admin", "admin")
	placeholder := e.seedMember(t, "Kim", "member")
	raw, err := e.h.invites.Issue(context.Background(), placeholder, admin)
	if err != nil {
		t.Fatalf("issue invite: %v", err)
	}
	resp := e.request(t, http.MethodGet, "/api/v1/auth/claim/"+raw, "", nil)
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("validate claim = %d, want 200", resp.StatusCode)
	}
	var cr claimResponse
	if err := json.UnmarshalRead(resp.Body, &cr); err != nil {
		t.Fatalf("decode claim response: %v", err)
	}
	if cr.Options.OIDC {
		t.Fatal("claim options report OIDC on with no provider configured")
	}
	if !cr.Options.Password {
		t.Fatal("claim options report password off")
	}
}

// corruptSealed decodes a base64url-encoded AEAD cookie, flips a byte inside the
// decoded sealed bytes, and re-encodes it. Mutating a decoded byte (here the
// first, part of the GCM nonce) guarantees Open fails its auth check, unlike
// flipping the last base64 character: that final char carries only the trailing
// 2-4 significant bits of the payload, and the rest are discarded on decode, so
// on some random keys/nonces it decodes to the same bytes and Open still
// succeeds. That base64-boundary luck is what made the test flaky.
func corruptSealed(t *testing.T, s string) string {
	t.Helper()
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		t.Fatalf("decode sealed cookie: %v", err)
	}
	if len(raw) == 0 {
		t.Fatal("sealed cookie decoded to zero bytes")
	}
	raw[0] ^= 0xFF
	return base64.RawURLEncoding.EncodeToString(raw)
}
