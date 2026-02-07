package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"moviepickarr/internal/domain"

	"github.com/gofiber/fiber/v2"
)

func TestResolveUserID_FromPath(t *testing.T) {
	h := &handler{}
	app := fiber.New()
	app.Get("/users/:userID/pool", func(c *fiber.Ctx) error {
		id, err := h.resolveUserID(c)
		if err != nil {
			return err
		}
		return c.Status(fiber.StatusOK).JSON(fiber.Map{"id": id})
	})

	req := httptest.NewRequest(http.MethodGet, "/users/42/pool", nil)
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}

	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}
}

func TestResolveUserID_RejectsQueryAndBodyFallbacks(t *testing.T) {
	h := &handler{}
	app := fiber.New()
	app.Get("/users/pool", func(c *fiber.Ctx) error {
		_, err := h.resolveUserID(c)
		if err != nil {
			return writeError(c, err)
		}
		return c.SendStatus(fiber.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/users/pool?userID=7", nil)
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}

	if resp.StatusCode != fiber.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", resp.StatusCode)
	}
}

func TestWriteError_PoolLocked(t *testing.T) {
	app := fiber.New()
	app.Get("/err", func(c *fiber.Ctx) error {
		return writeError(c, domain.ErrPoolLocked)
	})

	req := httptest.NewRequest(http.MethodGet, "/err", nil)
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}

	if resp.StatusCode != fiber.StatusForbidden {
		t.Fatalf("expected status 403, got %d", resp.StatusCode)
	}
	if got := resp.Header.Get("Content-Type"); !strings.Contains(got, "application/problem+json") {
		t.Fatalf("expected problem+json content type, got %q", got)
	}
}

func TestResolveUserAndMovieID_RequiresPathParams(t *testing.T) {
	h := &handler{}
	app := fiber.New()
	app.Post("/users/movie/move", func(c *fiber.Ctx) error {
		_, _, err := h.resolveUserAndMovieID(c)
		if err != nil {
			return writeError(c, err)
		}
		return c.SendStatus(fiber.StatusOK)
	})

	req := httptest.NewRequest(http.MethodPost, "/users/movie/move", nil)
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}

	if resp.StatusCode != fiber.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", resp.StatusCode)
	}
}
