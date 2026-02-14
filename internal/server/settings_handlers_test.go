package server

import (
	"context"
	"encoding/json"
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

func TestHandleGetNextPicker_ReinitializesWhenNextPickerIsNull(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "moviepickarr-settings-test.db")
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

	userRepo := repository.NewSqliteUserRepository(dbConn)
	userRecord, err := userRepo.Create(ctx, "Alice")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	if _, err := h.authService.UpsertAccount(ctx, userRecord.ID, "alice", "very-secure-password", domain.RoleAdmin); err != nil {
		t.Fatalf("upsert account: %v", err)
	}

	if _, err := dbConn.ExecContext(ctx, "UPDATE next_picker SET user_id = NULL WHERE id = 1"); err != nil {
		t.Fatalf("clear next picker user: %v", err)
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

	nextReq := httptest.NewRequest(http.MethodGet, "/api/v1/settings/next-picker", nil)
	nextReq.Header.Set("Cookie", cookie)
	nextResp, err := app.Test(nextReq, -1)
	if err != nil {
		t.Fatalf("next-picker request: %v", err)
	}
	if nextResp.StatusCode != fiber.StatusOK {
		t.Fatalf("expected status 200, got %d", nextResp.StatusCode)
	}

	var body struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
	}
	if err := readJSON(nextResp, &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}

	if body.ID != userRecord.ID || body.Name != userRecord.Name {
		t.Fatalf("expected next picker %d/%q, got %d/%q", userRecord.ID, userRecord.Name, body.ID, body.Name)
	}
}

func readJSON(resp *http.Response, dest any) error {
	decoder := json.NewDecoder(resp.Body)
	defer resp.Body.Close()
	if err := decoder.Decode(dest); err != nil {
		return fmt.Errorf("decode json: %w", err)
	}
	return nil
}
