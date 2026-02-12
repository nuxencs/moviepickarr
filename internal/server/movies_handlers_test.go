package server

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"moviepickarr/internal/db"
	"moviepickarr/internal/repository"

	"github.com/gofiber/fiber/v2"
)

func setupEditMovieTest(t *testing.T) (*handler, *fiber.App, *repository.SqliteUserRepository, *repository.SqliteMoviesRepository) {
	t.Helper()

	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "moviepickarr-test.db")
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

func TestHandleEditMovie_RejectsWatchedAtForNonWatchedMovie(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	_, app, userRepo, movieRepo := setupEditMovieTest(t)

	user, err := userRepo.Create(ctx, "Alice")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	movie, err := movieRepo.Add(ctx, "Before", "https://example.com/before", "pool", user.ID)
	if err != nil {
		t.Fatalf("create movie: %v", err)
	}

	body := `{"title":"After","link":"https://example.com/after","watchedAt":"2026-02-08T18:30:00Z"}`
	req := httptest.NewRequest(
		http.MethodPut,
		fmt.Sprintf("/api/v1/users/%d/movies/%d", user.ID, movie.ID),
		strings.NewReader(body),
	)
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != fiber.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", resp.StatusCode)
	}

	unchanged, err := movieRepo.FindByID(ctx, movie.ID)
	if err != nil {
		t.Fatalf("fetch movie: %v", err)
	}
	if unchanged.Title != "Before" {
		t.Fatalf("expected title unchanged, got %q", unchanged.Title)
	}
	if unchanged.Link != "https://example.com/before" {
		t.Fatalf("expected link unchanged, got %q", unchanged.Link)
	}
}

func TestHandleEditMovie_UpdatesWatchedMovieWithWatchedAt(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	_, app, userRepo, movieRepo := setupEditMovieTest(t)

	user, err := userRepo.Create(ctx, "Bob")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	movie, err := movieRepo.Add(ctx, "Before", "https://example.com/before", "pool", user.ID)
	if err != nil {
		t.Fatalf("create movie: %v", err)
	}

	initialWatchedAt := time.Date(2026, 2, 7, 13, 0, 0, 0, time.UTC)
	if err := movieRepo.MarkAsWatched(ctx, movie.ID, initialWatchedAt); err != nil {
		t.Fatalf("mark watched: %v", err)
	}

	body := `{"title":"After","link":"https://example.com/after","watchedAt":"2026-02-08T16:45:00Z"}`
	req := httptest.NewRequest(
		http.MethodPut,
		fmt.Sprintf("/api/v1/users/%d/movies/%d", user.ID, movie.ID),
		strings.NewReader(body),
	)
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}

	updated, err := movieRepo.FindByID(ctx, movie.ID)
	if err != nil {
		t.Fatalf("fetch updated movie: %v", err)
	}
	if updated.Title != "After" {
		t.Fatalf("expected title updated, got %q", updated.Title)
	}
	if updated.Link != "https://example.com/after" {
		t.Fatalf("expected link updated, got %q", updated.Link)
	}
	if updated.WatchedAt == nil {
		t.Fatalf("expected watchedAt to be set")
	}

	expectedWatchedAt := time.Date(2026, 2, 8, 16, 45, 0, 0, time.UTC)
	if !updated.WatchedAt.Equal(expectedWatchedAt) {
		t.Fatalf("expected watchedAt %v, got %v", expectedWatchedAt, updated.WatchedAt)
	}
}
