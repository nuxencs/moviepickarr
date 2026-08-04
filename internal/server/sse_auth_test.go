package server

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"moviepickarr/internal/db"
	"moviepickarr/internal/repository"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/requestid"
	"github.com/rs/zerolog"
)

// setupSSEApp builds a handler over a temp DB with the real csrfGuard then
// requireSession chain in front of /events, plus a fast heartbeat so the
// per-heartbeat session revalidation is observable within a test.
func setupSSEApp(t *testing.T) (*handler, *fiber.App, *db.Pool) {
	t.Helper()

	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "sse-auth-test.db")
	dbConn, err := db.OpenSQLite(dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.RunMigrations(ctx, dbConn.Write); err != nil {
		t.Fatalf("run migrations: %v", err)
	}

	h := newHandler(dbConn, zerolog.Nop())
	h.sseHeartbeatInterval = 25 * time.Millisecond
	t.Cleanup(func() {
		h.Close()
		if err := dbConn.Close(); err != nil {
			t.Fatalf("close sqlite: %v", err)
		}
	})

	app := fiber.New()
	app.Use(requestid.New())
	v1 := app.Group("/api/v1")
	v1.Use(csrfGuard)
	v1.Use(h.requireSession)
	v1.Get("/events", h.handleSSE)

	return h, app, dbConn
}

// The SSE handshake is authed before the stream opens: an invalid/absent session
// gets 401 (not an opened stream that later errors).
func TestSSE_HandshakeRejectsUnauthenticated(t *testing.T) {
	t.Parallel()

	_, app, _ := setupSSEApp(t)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/events", nil)
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != fiber.StatusUnauthorized {
		t.Fatalf("expected 401 before stream, got %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); strings.Contains(ct, "text/event-stream") {
		t.Fatalf("stream opened for an unauthenticated request (content-type %q)", ct)
	}
}

// A session revoked after the handshake stops receiving updates: the next
// heartbeat revalidates it, fails, and closes the stream.
func TestSSE_RevokedMidStreamDropsOnHeartbeat(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	h, app, dbConn := setupSSEApp(t)
	var logs bytes.Buffer
	h.log = zerolog.New(&logs).Level(zerolog.DebugLevel)

	member, err := repository.NewSqliteUserRepository(dbConn).Create(ctx, "Alice")
	if err != nil {
		t.Fatalf("create member: %v", err)
	}
	rawToken, _, err := h.sessions.Mint(ctx, member.ID, nil)
	if err != nil {
		t.Fatalf("mint session: %v", err)
	}

	// Revoke the session shortly after the stream opens; the next heartbeat
	// (25ms cadence) must then close it.
	go func() {
		time.Sleep(80 * time.Millisecond)
		_ = h.sessions.RevokeAll(ctx, member.ID)
	}()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/events", nil)
	req.Header.Set("Cookie", sessionCookieName+"="+rawToken)
	req.Header.Set(fiber.HeaderXRequestID, "revoked-stream")

	// A generous timeout: if revalidation never dropped the stream, the writer
	// would loop until this fires and app.Test would return an error instead.
	resp, err := app.Test(req, 5000)
	if err != nil {
		t.Fatalf("stream did not close after revocation (app.Test: %v)", err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("expected 200 for the opened stream, got %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read stream body: %v", err)
	}
	if !strings.Contains(string(body), "event: connected") {
		t.Fatalf("stream never sent its handshake frame; body=%q", string(body))
	}
	logged := logs.String()
	for _, want := range []string{
		`"level":"debug"`,
		`"subsystem":"sse"`,
		`"member_id":` + strconv.Itoa(member.ID),
		`"request_id":"revoked-stream"`,
		`"method":"GET"`,
		`"route":"/api/v1/events"`,
		`"message":"session revoked or expired mid-stream, closing stream"`,
	} {
		if !strings.Contains(logged, want) {
			t.Errorf("SSE close log missing %q: %s", want, logged)
		}
	}
}

func TestSSE_RevalidationDatabaseFailureLogsError(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	h, app, dbConn := setupSSEApp(t)
	var logs bytes.Buffer
	h.log = zerolog.New(&logs).Level(zerolog.DebugLevel)

	member, err := repository.NewSqliteUserRepository(dbConn).Create(ctx, "Alice")
	if err != nil {
		t.Fatalf("create member: %v", err)
	}
	rawToken, _, err := h.sessions.Mint(ctx, member.ID, nil)
	if err != nil {
		t.Fatalf("mint session: %v", err)
	}

	go func() {
		time.Sleep(80 * time.Millisecond)
		_ = dbConn.Close()
	}()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/events", nil)
	req.Header.Set("Cookie", sessionCookieName+"="+rawToken)
	req.Header.Set(fiber.HeaderXRequestID, "failed-stream")
	resp, err := app.Test(req, 5000)
	if err != nil {
		t.Fatalf("stream did not close after database failure (app.Test: %v)", err)
	}
	defer resp.Body.Close()

	logged := logs.String()
	for _, want := range []string{
		`"level":"error"`,
		`"request_id":"failed-stream"`,
		`"message":"session revalidation failed, closing stream"`,
	} {
		if !strings.Contains(logged, want) {
			t.Errorf("SSE failure log missing %q: %s", want, logged)
		}
	}
	if strings.Contains(logged, "session revoked or expired") {
		t.Errorf("database failure logged as session expiry: %s", logged)
	}
}
