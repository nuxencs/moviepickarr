package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
)

const (
	chromeMacUA   = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36"
	safariPhoneUA = "Mozilla/5.0 (iPhone; CPU iPhone OS 17_5 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.5 Mobile/15E148 Safari/604.1"
)

// loginFrom logs in carrying a user agent, so the session row records the device
// the list is supposed to describe. The shared request helper sends none.
func (e *authTestEnv) loginFrom(t *testing.T, username, password, userAgent string) string {
	t.Helper()
	body, err := json.Marshal(map[string]string{"username": username, "password": password})
	if err != nil {
		t.Fatalf("marshal login: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	req.Header.Set(fiber.HeaderUserAgent, userAgent)

	resp, err := e.app.Test(req, -1)
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	if resp.StatusCode != fiber.StatusNoContent {
		t.Fatalf("login status = %d, want 204", resp.StatusCode)
	}
	cookie := sessionCookieValue(resp)
	if cookie == "" {
		t.Fatal("login set no session cookie")
	}
	return cookie
}

func (e *authTestEnv) sessions(t *testing.T, cookie string) []sessionResponse {
	t.Helper()
	resp := e.request(t, http.MethodGet, "/api/v1/auth/sessions", cookie, nil)
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("list sessions status = %d, want 200", resp.StatusCode)
	}
	var rows []sessionResponse
	if err := json.NewDecoder(resp.Body).Decode(&rows); err != nil {
		t.Fatalf("decode sessions: %v", err)
	}
	return rows
}

// sessionID finds the public handle of the caller's current device
// (current=true) or another one (current=false). The list is the only place a
// client can learn one.
func (e *authTestEnv) sessionID(t *testing.T, cookie string, current bool) string {
	t.Helper()
	for _, r := range e.sessions(t, cookie) {
		if r.Current == current {
			return r.ID
		}
	}
	t.Fatalf("no session with current=%v in the list", current)
	return ""
}

func TestListSessions_DescribesOwnDevicesOnly(t *testing.T) {
	e := setupAuthApp(t)
	id := e.seedMember(t, "Gwen", "member")
	e.seedLocalLogin(t, id, "gwen", "correct horse battery")
	other := e.seedMember(t, "Hal", "member")
	e.seedLocalLogin(t, other, "hal", "correct horse battery")

	laptop := e.loginFrom(t, "gwen", "correct horse battery", chromeMacUA)
	phone := e.loginFrom(t, "gwen", "correct horse battery", safariPhoneUA)
	_ = e.loginFrom(t, "hal", "correct horse battery", chromeMacUA)

	rows := e.sessions(t, laptop)
	if len(rows) != 2 {
		t.Fatalf("listed %d sessions, want 2 (Hal's excluded)", len(rows))
	}

	// Both logins land on the same fake-clock instant, so the id tiebreak puts
	// the newest first.
	if rows[0].Device != "Safari on iPhone" {
		t.Errorf("newest device = %q, want Safari on iPhone", rows[0].Device)
	}
	if rows[1].Device != "Chrome on macOS" {
		t.Errorf("older device = %q, want Chrome on macOS", rows[1].Device)
	}
	if rows[0].Current {
		t.Error("phone session flagged current on a laptop request")
	}
	if !rows[1].Current {
		t.Error("laptop session not flagged current on its own request")
	}
	if rows[0].LastSeenAt == "" {
		t.Error("session row is missing its last-active time")
	}

	// The same two sessions from the phone, with the current flag moved.
	fromPhone := e.sessions(t, phone)
	if len(fromPhone) != 2 || !fromPhone[0].Current || fromPhone[1].Current {
		t.Fatal("current flag does not follow the requesting device")
	}
}

func TestRevokeSession_EndsThatDeviceOnly(t *testing.T) {
	e := setupAuthApp(t)
	id := e.seedMember(t, "Gwen", "member")
	e.seedLocalLogin(t, id, "gwen", "correct horse battery")

	laptop := e.loginFrom(t, "gwen", "correct horse battery", chromeMacUA)
	phone := e.loginFrom(t, "gwen", "correct horse battery", safariPhoneUA)

	phoneID := e.sessionID(t, laptop, false)

	resp := e.request(t, http.MethodDelete, "/api/v1/auth/sessions/"+phoneID, laptop, nil)
	if resp.StatusCode != fiber.StatusNoContent {
		t.Fatalf("revoke status = %d, want 204", resp.StatusCode)
	}
	// Revoking another device must not clear the caller's own cookie.
	if sessionCookieValue(resp) != "" {
		t.Error("revoking another device rotated the caller's cookie")
	}

	if me := e.request(t, http.MethodGet, "/api/v1/auth/me", phone, nil); me.StatusCode != fiber.StatusUnauthorized {
		t.Fatalf("revoked device me = %d, want 401", me.StatusCode)
	}
	if me := e.request(t, http.MethodGet, "/api/v1/auth/me", laptop, nil); me.StatusCode != fiber.StatusOK {
		t.Fatalf("caller me after revoke = %d, want 200", me.StatusCode)
	}
	if rows := e.sessions(t, laptop); len(rows) != 1 {
		t.Fatalf("listed %d sessions after revoke, want 1", len(rows))
	}

	// The list the member acted on can be stale; a second revoke says so rather
	// than reporting a success that revoked nothing.
	again := e.request(t, http.MethodDelete, "/api/v1/auth/sessions/"+phoneID, laptop, nil)
	if again.StatusCode != fiber.StatusNotFound {
		t.Fatalf("re-revoke status = %d, want 404", again.StatusCode)
	}
}

func TestRevokeSession_StaleHandleCannotReachANewerLogin(t *testing.T) {
	e := setupAuthApp(t)
	id := e.seedMember(t, "Gwen", "member")
	e.seedLocalLogin(t, id, "gwen", "correct horse battery")

	laptop := e.loginFrom(t, "gwen", "correct horse battery", chromeMacUA)
	oldPhone := e.loginFrom(t, "gwen", "correct horse battery", safariPhoneUA)
	staleID := e.sessionID(t, laptop, false)

	first := e.request(t, http.MethodDelete, "/api/v1/auth/sessions/"+staleID, laptop, nil)
	if first.StatusCode != fiber.StatusNoContent {
		t.Fatalf("first revoke status = %d, want 204", first.StatusCode)
	}
	if me := e.request(t, http.MethodGet, "/api/v1/auth/me", oldPhone, nil); me.StatusCode != fiber.StatusUnauthorized {
		t.Fatalf("old phone me = %d, want 401", me.StatusCode)
	}

	newPhone := e.loginFrom(t, "gwen", "correct horse battery", safariPhoneUA)
	newID := e.sessionID(t, laptop, false)
	if newID == staleID {
		t.Fatal("new login reused a public session handle")
	}

	stale := e.request(t, http.MethodDelete, "/api/v1/auth/sessions/"+staleID, laptop, nil)
	if stale.StatusCode != fiber.StatusNotFound {
		t.Fatalf("stale revoke status = %d, want 404", stale.StatusCode)
	}
	if me := e.request(t, http.MethodGet, "/api/v1/auth/me", newPhone, nil); me.StatusCode != fiber.StatusOK {
		t.Fatalf("new phone me after stale revoke = %d, want 200", me.StatusCode)
	}
}

func TestRevokeSession_AnotherMembersSessionIsUnreachable(t *testing.T) {
	e := setupAuthApp(t)
	gwen := e.seedMember(t, "Gwen", "member")
	e.seedLocalLogin(t, gwen, "gwen", "correct horse battery")
	// An admin, to prove the refusal is ownership and not privilege.
	hal := e.seedMember(t, "Hal", "admin")
	e.seedLocalLogin(t, hal, "hal", "correct horse battery")

	gwenCookie := e.loginFrom(t, "gwen", "correct horse battery", chromeMacUA)
	halCookie := e.loginFrom(t, "hal", "correct horse battery", chromeMacUA)

	gwenSessionID := e.sessionID(t, gwenCookie, true)

	resp := e.request(t, http.MethodDelete, "/api/v1/auth/sessions/"+gwenSessionID, halCookie, nil)
	if resp.StatusCode != fiber.StatusNotFound {
		t.Fatalf("revoking another member's session = %d, want 404", resp.StatusCode)
	}
	if me := e.request(t, http.MethodGet, "/api/v1/auth/me", gwenCookie, nil); me.StatusCode != fiber.StatusOK {
		t.Fatalf("target session me = %d, want 200 (it must survive)", me.StatusCode)
	}
}

func TestRevokeSession_CurrentDeviceClearsTheCookie(t *testing.T) {
	e := setupAuthApp(t)
	id := e.seedMember(t, "Gwen", "member")
	e.seedLocalLogin(t, id, "gwen", "correct horse battery")

	laptop := e.loginFrom(t, "gwen", "correct horse battery", chromeMacUA)
	currentID := e.sessionID(t, laptop, true)

	resp := e.request(t, http.MethodDelete, "/api/v1/auth/sessions/"+currentID, laptop, nil)
	if resp.StatusCode != fiber.StatusNoContent {
		t.Fatalf("revoke current status = %d, want 204", resp.StatusCode)
	}
	var cleared bool
	for _, c := range resp.Cookies() {
		if c.Name == sessionCookieName && c.Value == "" {
			cleared = true
		}
	}
	if !cleared {
		t.Error("revoking the current session left the cookie set")
	}
	if me := e.request(t, http.MethodGet, "/api/v1/auth/me", laptop, nil); me.StatusCode != fiber.StatusUnauthorized {
		t.Fatalf("me after revoking own session = %d, want 401", me.StatusCode)
	}
}

func TestSessions_RequireASession(t *testing.T) {
	e := setupAuthApp(t)

	if resp := e.request(t, http.MethodGet, "/api/v1/auth/sessions", "", nil); resp.StatusCode != fiber.StatusUnauthorized {
		t.Fatalf("anonymous list = %d, want 401", resp.StatusCode)
	}
	if resp := e.request(t, http.MethodDelete, "/api/v1/auth/sessions/1", "", nil); resp.StatusCode != fiber.StatusUnauthorized {
		t.Fatalf("anonymous revoke = %d, want 401", resp.StatusCode)
	}
}

func TestRevokeSession_RejectsAnUnusableID(t *testing.T) {
	e := setupAuthApp(t)
	id := e.seedMember(t, "Gwen", "member")
	e.seedLocalLogin(t, id, "gwen", "correct horse battery")
	cookie := e.loginFrom(t, "gwen", "correct horse battery", chromeMacUA)

	for _, raw := range []string{"abc", "0", "-3"} {
		resp := e.request(t, http.MethodDelete, "/api/v1/auth/sessions/"+raw, cookie, nil)
		if resp.StatusCode != fiber.StatusBadRequest {
			t.Errorf("revoke %q = %d, want 400", raw, resp.StatusCode)
		}
	}
}
