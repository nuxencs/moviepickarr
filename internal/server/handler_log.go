package server

import (
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/requestid"
	"github.com/rs/zerolog"
)

// reqLog derives the per-request sub-logger every handler should log through.
// It carries the same request_id the access-log middleware emits, so an app
// line and its access line join on one key, plus the route and, once
// requireSession has run, the acting member.
//
// The key is "route", not "path", and it holds the route template rather than
// c.Path(). Two reasons, and they pull the same way: "/api/v1/movies/:movieID"
// groups where "/api/v1/movies/8213" does not, and the access-log middleware
// already owns "path" for the concrete URL. Reusing "path" for a different
// value would break the very correlation this helper exists to provide.
//
// Handlers use this; background work (sweeps, warm-up, startup) uses h.log
// directly since there is no request to scope to.
//
// Returns a pointer because zerolog's level methods take a pointer receiver,
// so a returned value could not be chained into .Error() at the call site.
func (h *handler) reqLog(c *fiber.Ctx) *zerolog.Logger {
	return h.reqLogWithRoute(c, c.Route().Path)
}

// reqLogBeforeRoute is the request logger for middleware that can return before
// c.Next advances Fiber to the endpoint route. In that position c.Route() is the
// middleware prefix, not the endpoint template, so emitting it as route would be
// misleading. The request_id still joins the line to the access log's concrete
// path.
func (h *handler) reqLogBeforeRoute(c *fiber.Ctx) *zerolog.Logger {
	return h.reqLogWithRoute(c, "")
}

func (h *handler) reqLogWithRoute(c *fiber.Ctx, route string) *zerolog.Logger {
	ctx := h.log.With().Str("method", c.Method())
	if route != "" {
		ctx = ctx.Str("route", route)
	}

	// Empty when this route is mounted ahead of the requestid middleware.
	// Omit rather than emit "". A blank key reads like a correlation id that
	// went missing in transit.
	if id, ok := c.Locals(requestid.ConfigDefault.ContextKey).(string); ok && id != "" {
		ctx = ctx.Str("request_id", id)
	}
	if memberID := actorMemberID(c); memberID != 0 {
		ctx = ctx.Int("member_id", memberID)
	}

	log := ctx.Logger()
	return &log
}
