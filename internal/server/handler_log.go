package server

import (
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/requestid"
	"github.com/rs/zerolog"
)

// reqLog derives the per-request sub-logger every handler should log through.
// It carries the same request_id the access-log middleware emits, so an app
// line and its access line join on one key, plus the route and — once
// requireSession has run — the acting member.
//
// The route template is logged rather than c.Path(): "/api/v1/movies/:movieID"
// groups, "/api/v1/movies/8213" does not.
//
// Handlers use this; background work (sweeps, warm-up, startup) uses h.log
// directly since there is no request to scope to.
//
// Returns a pointer because zerolog's level methods take a pointer receiver,
// so a returned value could not be chained into .Error() at the call site.
func (h *handler) reqLog(c *fiber.Ctx) *zerolog.Logger {
	ctx := h.log.With().
		Str("method", c.Method()).
		Str("path", c.Route().Path)

	// Empty when this route is mounted ahead of the requestid middleware.
	// Omit rather than emit "" — a blank key reads like a correlation id that
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
