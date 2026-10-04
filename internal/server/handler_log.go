package server

import (
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/requestid"
	"github.com/rs/zerolog"
)

// reqLog is the per-request logger for handlers (see docs/LOGGING.md). The key
// is "route" with the template, because the access log already owns "path".
func (h *handler) reqLog(c *fiber.Ctx) *zerolog.Logger {
	return h.reqLogWithRoute(c, c.Route().Path)
}

// reqLogBeforeRoute omits route for middleware that returns before c.Next:
// there c.Route() is the middleware prefix, not the endpoint.
func (h *handler) reqLogBeforeRoute(c *fiber.Ctx) *zerolog.Logger {
	return h.reqLogWithRoute(c, "")
}

func (h *handler) reqLogWithRoute(c *fiber.Ctx, route string) *zerolog.Logger {
	ctx := h.log.With().Str("method", c.Method())
	if route != "" {
		ctx = ctx.Str("route", route)
	}

	// Omit an empty id: a blank key reads like a lost correlation id.
	if id, ok := c.Locals(requestid.ConfigDefault.ContextKey).(string); ok && id != "" {
		ctx = ctx.Str("request_id", id)
	}
	if memberID := actorMemberID(c); memberID != 0 {
		ctx = ctx.Int("member_id", memberID)
	}

	log := ctx.Logger()
	return &log
}
