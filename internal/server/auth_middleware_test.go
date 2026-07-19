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

type sessionTestEnv struct {
	h     *handler
	app   *fiber.App
	users *repository.SqliteUserRepository
	pool  *db.Pool
	clk   *fakeClock
}

type fakeClock struct{ t time.Time }

func (c *fakeClock) now() time.Time { return c.t }

// setupSessionApp builds a handler over a temp DB with the real auth chain
// (csrfGuard → requireSession) mounted on /api/v1, mirroring registerRoutes.
// A test-only /test/login route mints a session (standing in for the later
// login handlers), and probe routes report the attached actor.
func setupSessionApp(t *testing.T) *sessionTestEnv {
	t.Helper()

	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "session-mw-test.db")
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

	t.Cleanup(func() {
		h.Close()
		if err := dbConn.Close(); err != nil {
			t.Fatalf("close sqlite: %v", err)
		}
	})

	app := fiber.New()
	app.Post("/test/login/:memberID", func(c *fiber.Ctx) error {
		id, _ := strconv.Atoi(c.Params("memberID"))
		if err := h.issueSession(c, id); err != nil {
			return err
		}
		return c.SendStatus(fiber.StatusNoContent)
	})

	v1 := app.Group("/api/v1")
	v1.Use(csrfGuard)
	v1.Use(h.requireSession)
	v1.Get("/whoami", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{
			"memberID": c.Locals(localsMemberID),
			"role":     c.Locals(localsRole),
		})
	})
	v1.Post("/probe", func(c *fiber.Ctx) error { return c.SendStatus(fiber.StatusNoContent) })

	return &sessionTestEnv{h: h, app: app, users: repository.NewSqliteUserRepository(dbConn), pool: dbConn, clk: clk}
}

// login mints a session for memberID and returns the raw session cookie value.
func (e *sessionTestEnv) login(t *testing.T, memberID int) string {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/test/login/"+strconv.Itoa(memberID), nil)
	resp, err := e.app.Test(req, -1)
	if err != nil {
		t.Fatalf("login request: %v", err)
	}
	if resp.StatusCode != fiber.StatusNoContent {
		t.Fatalf("login status = %d, want 204", resp.StatusCode)
	}
	for _, cookie := range resp.Cookies() {
		if cookie.Name == sessionCookieName {
			if cookie.Value == "" {
				t.Fatal("login set an empty session cookie")
			}
			return cookie.Value
		}
	}
	t.Fatal("login did not set a session cookie")
	return ""
}

func (e *sessionTestEnv) createUser(t *testing.T, name string) int {
	t.Helper()
	u, err := e.users.Create(context.Background(), name)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	return u.ID
}

type whoamiBody struct {
	MemberID int    `json:"memberID"`
	Role     string `json:"role"`
}

func decodeWhoami(t *testing.T, resp *http.Response) whoamiBody {
	t.Helper()
	var body whoamiBody
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode whoami: %v", err)
	}
	return body
}

// sessionClearedBy reports whether a response clears the session cookie.
func sessionClearedBy(resp *http.Response) bool {
	for _, cookie := range resp.Cookies() {
		if cookie.Name == sessionCookieName && cookie.Value == "" {
			return true
		}
	}
	return false
}

func TestSession_MintedCookieRoundTripsWithLiveRole(t *testing.T) {
	e := setupSessionApp(t)
	id := e.createUser(t, "Alice")
	token := e.login(t, id)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/whoami", nil)
	req.Header.Set("Cookie", sessionCookieName+"="+token)
	resp, err := e.app.Test(req, -1)
	if err != nil {
		t.Fatalf("whoami: %v", err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("whoami status = %d, want 200", resp.StatusCode)
	}
	body := decodeWhoami(t, resp)
	if body.MemberID != id {
		t.Fatalf("memberID = %d, want %d", body.MemberID, id)
	}
	if body.Role != "member" {
		t.Fatalf("role = %q, want member", body.Role)
	}

	// Role is read live: promote and the very next request reflects it.
	if _, err := e.pool.Write.ExecContext(context.Background(), "UPDATE users SET role = 'admin' WHERE id = ?", id); err != nil {
		t.Fatalf("promote: %v", err)
	}
	req = httptest.NewRequest(http.MethodGet, "/api/v1/whoami", nil)
	req.Header.Set("Cookie", sessionCookieName+"="+token)
	resp, _ = e.app.Test(req, -1)
	if decodeWhoami(t, resp).Role != "admin" {
		t.Fatal("role change not reflected live")
	}
}

func TestSession_MintCookieAttributes(t *testing.T) {
	e := setupSessionApp(t)
	id := e.createUser(t, "Alice")

	req := httptest.NewRequest(http.MethodPost, "/test/login/"+strconv.Itoa(id), nil)
	resp, _ := e.app.Test(req, -1)

	var raw string
	for _, sc := range resp.Header.Values("Set-Cookie") {
		if strings.HasPrefix(sc, sessionCookieName+"=") {
			raw = sc
		}
	}
	if raw == "" {
		t.Fatal("no session Set-Cookie")
	}
	low := strings.ToLower(raw)
	for _, want := range []string{"httponly", "samesite=lax", "path=/", "max-age=7776000"} {
		if !strings.Contains(low, want) {
			t.Fatalf("Set-Cookie %q missing %q", raw, want)
		}
	}
	// Plain-http request: Secure must be omitted so raw-http dev works.
	if strings.Contains(low, "secure") {
		t.Fatalf("Set-Cookie %q set Secure on http", raw)
	}
}

func TestSession_MissingCookieRejectedAndCleared(t *testing.T) {
	e := setupSessionApp(t)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/whoami", nil)
	resp, err := e.app.Test(req, -1)
	if err != nil {
		t.Fatalf("whoami: %v", err)
	}
	if resp.StatusCode != fiber.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
	if !sessionClearedBy(resp) {
		t.Fatal("401 did not clear the session cookie")
	}
}

func TestSession_ExpiredAndIdleRejected(t *testing.T) {
	e := setupSessionApp(t)
	id := e.createUser(t, "Alice")
	token := e.login(t, id)

	// Absolute cap: jump past 90 days.
	e.clk.t = e.clk.t.Add(auth.SessionAbsoluteTTL + time.Hour)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/whoami", nil)
	req.Header.Set("Cookie", sessionCookieName+"="+token)
	resp, _ := e.app.Test(req, -1)
	if resp.StatusCode != fiber.StatusUnauthorized {
		t.Fatalf("expired status = %d, want 401", resp.StatusCode)
	}
	if !sessionClearedBy(resp) {
		t.Fatal("expired 401 did not clear the cookie")
	}

	// Idle window: fresh login, then jump past 30 days of inactivity.
	token2 := e.login(t, id)
	e.clk.t = e.clk.t.Add(auth.SessionIdleTTL + time.Hour)
	req = httptest.NewRequest(http.MethodGet, "/api/v1/whoami", nil)
	req.Header.Set("Cookie", sessionCookieName+"="+token2)
	resp, _ = e.app.Test(req, -1)
	if resp.StatusCode != fiber.StatusUnauthorized {
		t.Fatalf("idle status = %d, want 401", resp.StatusCode)
	}
	if !sessionClearedBy(resp) {
		t.Fatal("idle 401 did not clear the cookie")
	}
}

func TestSession_LastSeenSlidesOnlyWhenStale(t *testing.T) {
	e := setupSessionApp(t)
	id := e.createUser(t, "Alice")
	base := e.clk.t

	// Under 1h: last_seen stays at mint time.
	token := e.login(t, id)
	e.clk.t = base.Add(30 * time.Minute)
	e.doWhoami(t, token, fiber.StatusOK)
	if got := e.lastSeen(t, token); !got.Equal(base) {
		t.Fatalf("last_seen slid to %v under threshold, want %v", got, base)
	}

	// Over 1h: last_seen slides to the current clock.
	token2 := e.login(t, id)
	e.clk.t = base.Add(2 * time.Hour)
	e.doWhoami(t, token2, fiber.StatusOK)
	if got := e.lastSeen(t, token2); !got.Equal(base.Add(2 * time.Hour)) {
		t.Fatalf("last_seen = %v, want %v", got, base.Add(2*time.Hour))
	}
}

func (e *sessionTestEnv) doWhoami(t *testing.T, token string, wantStatus int) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/whoami", nil)
	req.Header.Set("Cookie", sessionCookieName+"="+token)
	resp, err := e.app.Test(req, -1)
	if err != nil {
		t.Fatalf("whoami: %v", err)
	}
	if resp.StatusCode != wantStatus {
		t.Fatalf("whoami status = %d, want %d", resp.StatusCode, wantStatus)
	}
}

func (e *sessionTestEnv) lastSeen(t *testing.T, token string) time.Time {
	t.Helper()
	var epoch int64
	err := e.pool.Read.QueryRowContext(context.Background(),
		"SELECT last_seen_at FROM sessions WHERE token_hash = ?", auth.HashToken(token)).Scan(&epoch)
	if err != nil {
		t.Fatalf("read last_seen: %v", err)
	}
	return db.FromUnix(epoch)
}

func TestCSRF_FailsClosedWhenNoSignals(t *testing.T) {
	e := setupSessionApp(t)
	id := e.createUser(t, "Alice")
	token := e.login(t, id)

	// Unsafe method, valid session, but neither Sec-Fetch-Site nor Origin: 403.
	req := httptest.NewRequest(http.MethodPost, "/api/v1/probe", nil)
	req.Header.Set("Cookie", sessionCookieName+"="+token)
	resp, _ := e.app.Test(req, -1)
	if resp.StatusCode != fiber.StatusForbidden {
		t.Fatalf("status = %d, want 403", resp.StatusCode)
	}
}

func TestCSRF_AllowsSameOriginAndMatchingOrigin(t *testing.T) {
	e := setupSessionApp(t)
	id := e.createUser(t, "Alice")
	token := e.login(t, id)

	// Sec-Fetch-Site: same-origin passes.
	req := httptest.NewRequest(http.MethodPost, "/api/v1/probe", nil)
	req.Header.Set("Cookie", sessionCookieName+"="+token)
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	resp, _ := e.app.Test(req, -1)
	if resp.StatusCode != fiber.StatusNoContent {
		t.Fatalf("same-origin status = %d, want 204", resp.StatusCode)
	}

	// Matching Origin (no Sec-Fetch-Site) passes. httptest host is example.com.
	req = httptest.NewRequest(http.MethodPost, "/api/v1/probe", nil)
	req.Header.Set("Cookie", sessionCookieName+"="+token)
	req.Header.Set("Origin", "http://example.com")
	resp, _ = e.app.Test(req, -1)
	if resp.StatusCode != fiber.StatusNoContent {
		t.Fatalf("matching-origin status = %d, want 204", resp.StatusCode)
	}
}

func TestCSRF_RejectsForeignOrigin(t *testing.T) {
	e := setupSessionApp(t)
	id := e.createUser(t, "Alice")
	token := e.login(t, id)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/probe", nil)
	req.Header.Set("Cookie", sessionCookieName+"="+token)
	req.Header.Set("Origin", "http://evil.example")
	resp, _ := e.app.Test(req, -1)
	if resp.StatusCode != fiber.StatusForbidden {
		t.Fatalf("foreign-origin status = %d, want 403", resp.StatusCode)
	}
}

func TestCSRF_RunsBeforeRequireSession(t *testing.T) {
	e := setupSessionApp(t)

	// No session and no origin: CSRF must reject with 403 before the session
	// check would return 401.
	req := httptest.NewRequest(http.MethodPost, "/api/v1/probe", nil)
	resp, _ := e.app.Test(req, -1)
	if resp.StatusCode != fiber.StatusForbidden {
		t.Fatalf("status = %d, want 403 (CSRF before session)", resp.StatusCode)
	}

	// Passes CSRF (same-origin) but has no session: now 401 from requireSession.
	req = httptest.NewRequest(http.MethodPost, "/api/v1/probe", nil)
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	resp, _ = e.app.Test(req, -1)
	if resp.StatusCode != fiber.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
}

func TestCSRF_ExemptsSafeMethods(t *testing.T) {
	e := setupSessionApp(t)
	id := e.createUser(t, "Alice")
	token := e.login(t, id)

	// GET with a valid session and no origin signals passes (safe method).
	e.doWhoami(t, token, fiber.StatusOK)
}
