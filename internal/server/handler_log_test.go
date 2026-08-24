package server

import (
	"bytes"
	"encoding/json/v2"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/requestid"
	"github.com/rs/zerolog"
)

// captureReqLog drives one request through a throwaway app whose only handler
// logs via h.reqLog, and returns the decoded JSON line.
func captureReqLog(t *testing.T, setup func(app *fiber.App), method, path string) map[string]any {
	t.Helper()

	var buf bytes.Buffer
	h := &handler{log: zerolog.New(&buf)}

	app := fiber.New()
	if setup != nil {
		setup(app)
	}
	app.Add(method, "/req/:movieID?", func(c *fiber.Ctx) error {
		h.reqLog(c).Info().Msg("probe")
		return c.SendStatus(fiber.StatusNoContent)
	})

	resp, err := app.Test(httptest.NewRequest(method, path, nil))
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	defer resp.Body.Close()

	var line map[string]any
	if err := json.Unmarshal(buf.Bytes(), &line); err != nil {
		t.Fatalf("decode log line %q: %v", buf.String(), err)
	}
	return line
}

func TestReqLogCarriesRequestScope(t *testing.T) {
	line := captureReqLog(t, func(app *fiber.App) {
		app.Use(requestid.New())
	}, fiber.MethodGet, "/req/42")

	if got := line["method"]; got != fiber.MethodGet {
		t.Errorf("method = %v, want %s", got, fiber.MethodGet)
	}
	// The route template under "route", not "path": a per-request path would
	// make every movie its own log stream, and "path" belongs to the access-log
	// middleware, which puts the concrete URL there.
	if got := line["route"]; got != "/req/:movieID?" {
		t.Errorf("route = %v, want the route template", got)
	}
	if _, ok := line["path"]; ok {
		t.Errorf("reqLog claimed the access log's path key: %v", line)
	}
	if got, _ := line["request_id"].(string); got == "" {
		t.Errorf("request_id missing from %v", line)
	}
	if _, ok := line["member_id"]; ok {
		t.Errorf("member_id present without a session: %v", line)
	}
}

func TestReqLogCarriesMemberIDWhenSessionAttached(t *testing.T) {
	line := captureReqLog(t, func(app *fiber.App) {
		app.Use(requestid.New())
		app.Use(func(c *fiber.Ctx) error {
			c.Locals(localsMemberID, 7)
			return c.Next()
		})
	}, fiber.MethodGet, "/req/42")

	if got, ok := line["member_id"].(float64); !ok || int(got) != 7 {
		t.Errorf("member_id = %v, want 7", line["member_id"])
	}
}

func TestReqLogOmitsRequestIDWhenMiddlewareAbsent(t *testing.T) {
	// The SSE stream and any future route mounted ahead of requestid must not
	// emit an empty request_id key that looks like a lost correlation id.
	line := captureReqLog(t, nil, fiber.MethodGet, "/req/42")

	if _, ok := line["request_id"]; ok {
		t.Errorf("request_id present without the middleware: %v", line)
	}
}

func TestReqLogBeforeRouteOmitsMiddlewarePrefix(t *testing.T) {
	var buf bytes.Buffer
	h := &handler{log: zerolog.New(&buf)}

	app := fiber.New()
	app.Use(requestid.New())
	app.Use("/req", func(c *fiber.Ctx) error {
		h.reqLogBeforeRoute(c).Error().Msg("probe")
		return c.SendStatus(fiber.StatusInternalServerError)
	})
	app.Get("/req/:movieID", func(c *fiber.Ctx) error {
		return c.SendStatus(fiber.StatusNoContent)
	})

	resp, err := app.Test(httptest.NewRequest(fiber.MethodGet, "/req/42", nil))
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	defer resp.Body.Close()

	var line map[string]any
	if err := json.Unmarshal(buf.Bytes(), &line); err != nil {
		t.Fatalf("decode log line %q: %v", buf.String(), err)
	}
	if _, ok := line["route"]; ok {
		t.Errorf("middleware prefix emitted as endpoint route: %v", line)
	}
	if got, _ := line["request_id"].(string); got == "" {
		t.Errorf("request_id missing from %v", line)
	}
}
