package server

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"moviepickarr/internal/db"
	"moviepickarr/internal/domain"
	"moviepickarr/internal/repository"

	"github.com/gofiber/fiber/v2"
)

func setupAuthTestApp(t *testing.T) (*handler, *fiber.App, *repository.SqliteUserRepository, *repository.SqliteMoviesRepository) {
	t.Helper()

	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "moviepickarr-auth-test.db")
	dbConn, err := db.OpenSQLite(dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.RunMigrations(ctx, dbConn); err != nil {
		t.Fatalf("run migrations: %v", err)
	}

	h := newHandler(dbConn)
	t.Cleanup(func() {
		h.Close()
		if err := dbConn.Close(); err != nil {
			t.Fatalf("close sqlite: %v", err)
		}
	})

	app := fiber.New()
	registerV1Routes(app.Group("/api/v1"), h)

	return h, app, repository.NewSqliteUserRepository(dbConn), repository.NewSqliteMoviesRepository(dbConn)
}

func TestAuthBootstrapEndpointDisabled(t *testing.T) {
	t.Parallel()

	_, app, _, _ := setupAuthTestApp(t)

	bootstrapBody := `{"name":"Owner","username":"owner","password":"very-secure-password"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/bootstrap", strings.NewReader(bootstrapBody))
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	if resp.StatusCode != fiber.StatusUnauthorized {
		t.Fatalf("expected status 401, got %d", resp.StatusCode)
	}
}

func TestProtectedEndpointRequiresAuth(t *testing.T) {
	t.Parallel()

	_, app, _, _ := setupAuthTestApp(t)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/movies/pool", nil)
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	if resp.StatusCode != fiber.StatusUnauthorized {
		t.Fatalf("expected status 401, got %d", resp.StatusCode)
	}
}

func TestEnsureInitialAdminSeedsLoginFromConfig(t *testing.T) {
	t.Parallel()

	h, app, _, _ := setupAuthTestApp(t)
	h.authAdminName = "Owner"
	h.authAdminUsername = "owner"
	h.authAdminPassword = "very-secure-password"

	if err := h.ensureInitialAdmin(context.Background()); err != nil {
		t.Fatalf("ensureInitialAdmin: %v", err)
	}

	loginReq := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/auth/login",
		strings.NewReader(`{"username":"owner","password":"very-secure-password"}`),
	)
	loginReq.Header.Set("Content-Type", "application/json")
	loginResp, err := app.Test(loginReq, -1)
	if err != nil {
		t.Fatalf("login request: %v", err)
	}
	if loginResp.StatusCode != fiber.StatusOK {
		t.Fatalf("expected login status 200, got %d", loginResp.StatusCode)
	}
}

func TestNonAdminCannotMutateOtherUserMovie(t *testing.T) {
	t.Parallel()

	h, app, userRepo, movieRepo := setupAuthTestApp(t)
	ctx := context.Background()

	alice, err := userRepo.Create(ctx, "Alice")
	if err != nil {
		t.Fatalf("create user alice: %v", err)
	}
	bob, err := userRepo.Create(ctx, "Bob")
	if err != nil {
		t.Fatalf("create user bob: %v", err)
	}
	movie, err := movieRepo.Add(ctx, "Before", "https://example.com/before", "pool", bob.ID)
	if err != nil {
		t.Fatalf("add movie: %v", err)
	}

	if _, err := h.authService.UpsertAccount(ctx, alice.ID, "alice", "very-secure-password", domain.RoleMember); err != nil {
		t.Fatalf("upsert alice account: %v", err)
	}
	if _, err := h.authService.UpsertAccount(ctx, bob.ID, "bob", "very-secure-password", domain.RoleMember); err != nil {
		t.Fatalf("upsert bob account: %v", err)
	}

	loginReq := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/auth/login",
		strings.NewReader(`{"username":"alice","password":"very-secure-password"}`),
	)
	loginReq.Header.Set("Content-Type", "application/json")
	loginResp, err := app.Test(loginReq, -1)
	if err != nil {
		t.Fatalf("login request: %v", err)
	}
	if loginResp.StatusCode != fiber.StatusOK {
		t.Fatalf("expected login status 200, got %d", loginResp.StatusCode)
	}
	cookie := loginResp.Header.Get("Set-Cookie")
	if cookie == "" {
		t.Fatalf("missing auth cookie")
	}

	body := `{"title":"After","link":"https://example.com/after"}`
	updateReq := httptest.NewRequest(
		http.MethodPut,
		fmt.Sprintf("/api/v1/users/%d/movies/%d", bob.ID, movie.ID),
		strings.NewReader(body),
	)
	updateReq.Header.Set("Content-Type", "application/json")
	updateReq.Header.Set("Cookie", cookie)

	updateResp, err := app.Test(updateReq, -1)
	if err != nil {
		t.Fatalf("update request: %v", err)
	}
	if updateResp.StatusCode != fiber.StatusForbidden {
		t.Fatalf("expected status 403, got %d", updateResp.StatusCode)
	}
}
