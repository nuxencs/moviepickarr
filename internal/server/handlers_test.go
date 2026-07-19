package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"moviepickarr/internal/domain"

	"github.com/gofiber/fiber/v2"
)

func TestResolveMemberID_FromPath(t *testing.T) {
	app := fiber.New()
	app.Get("/members/:memberID/pool", func(c *fiber.Ctx) error {
		id, err := resolveMemberID(c)
		if err != nil {
			return writeError(c, err)
		}
		return c.Status(fiber.StatusOK).JSON(fiber.Map{"id": id})
	})

	req := httptest.NewRequest(http.MethodGet, "/members/42/pool", nil)
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}
}

func TestResolveMovieID_RejectsMissingParam(t *testing.T) {
	app := fiber.New()
	app.Get("/movies", func(c *fiber.Ctx) error {
		_, err := resolveMovieID(c)
		if err != nil {
			return writeError(c, err)
		}
		return c.SendStatus(fiber.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/movies?movieID=7", nil)
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != fiber.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", resp.StatusCode)
	}
}

func TestActorMemberID_ReadsSessionLocal(t *testing.T) {
	app := fiber.New()
	app.Get("/whoami", func(c *fiber.Ctx) error {
		c.Locals(localsMemberID, 99)
		return c.JSON(fiber.Map{"id": actorMemberID(c)})
	})

	req := httptest.NewRequest(http.MethodGet, "/whoami", nil)
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
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
