package server

import (
	"context"
	"errors"
	"strings"
	"time"

	"moviepickarr/internal/auth"

	"github.com/gofiber/fiber/v2"
)

// sessionCookieName holds the raw session token, never anything derived from
// the member.
const sessionCookieName = "mpa_session"

// c.Locals keys for the actor requireSession attaches.
const (
	localsMemberID = "memberID"
	localsRole     = "role"
)

var cookieEpoch = time.Unix(0, 0)

// isHTTPS is the one scheme check for the cookie Secure flag and the CSRF
// origin check. Plain http gets no Secure flag, so raw-http dev still works.
func isHTTPS(c *fiber.Ctx) bool {
	if strings.EqualFold(c.Get(fiber.HeaderXForwardedProto), "https") {
		return true
	}
	return c.Protocol() == "https"
}

func setSessionCookie(c *fiber.Ctx, rawToken string) {
	c.Cookie(&fiber.Cookie{
		Name:     sessionCookieName,
		Value:    rawToken,
		Path:     "/",
		MaxAge:   int(auth.SessionAbsoluteTTL / time.Second),
		HTTPOnly: true,
		SameSite: fiber.CookieSameSiteLaxMode,
		Secure:   isHTTPS(c),
	})
}

// clearSessionCookie must mirror setSessionCookie's attributes, or the browser
// keeps the cookie.
func clearSessionCookie(c *fiber.Ctx) {
	c.Cookie(&fiber.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		Expires:  cookieEpoch,
		MaxAge:   -1,
		HTTPOnly: true,
		SameSite: fiber.CookieSameSiteLaxMode,
		Secure:   isHTTPS(c),
	})
}

// issueSession always mints a fresh session and never adopts an inbound cookie,
// which prevents session fixation.
func (h *handler) issueSession(c *fiber.Ctx, memberID int) error {
	rawToken, _, err := h.sessions.Mint(c.UserContext(), memberID, stringPtrOrNil(c.Get(fiber.HeaderUserAgent)))
	if err != nil {
		return err
	}
	setSessionCookie(c, rawToken)
	return nil
}

// sessionSweepInterval is housekeeping only: Authenticate already rejects
// expired rows.
const sessionSweepInterval = time.Hour

// startSessionSweeper sweeps expired sessions now and then hourly until ctx is
// cancelled.
func (h *handler) startSessionSweeper(ctx context.Context) {
	h.sweepSessions(ctx)

	go func() {
		ticker := time.NewTicker(sessionSweepInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				h.sweepSessions(ctx)
			}
		}
	}()
}

func (h *handler) sweepSessions(ctx context.Context) {
	removed, err := h.sessions.Sweep(ctx)
	if err != nil {
		// Warn, not Error: the next tick retries.
		h.log.Warn().Err(err).Msg("expired-session sweep failed, retrying next tick")
		return
	}
	if removed > 0 {
		h.log.Debug().Int64("count", removed).Msg("swept expired sessions")
	}
}

// requireSession attaches the live actor or rejects with 401 and clears the
// cookie. It runs after csrfGuard.
func (h *handler) requireSession(c *fiber.Ctx) error {
	as, err := h.sessions.Authenticate(c.UserContext(), c.Cookies(sessionCookieName))
	if err != nil {
		if errors.Is(err, auth.ErrSessionInvalid) {
			clearSessionCookie(c)
			return writeProblem(c, fiber.StatusUnauthorized, "unauthorized", "authentication required")
		}
		// No actor attached yet: this line is the only trace of this 500.
		h.reqLogBeforeRoute(c).Error().Err(err).Msg("session lookup failed")
		return writeProblem(c, fiber.StatusInternalServerError, "internal_error", "internal server error")
	}

	c.Locals(localsMemberID, as.UserID)
	c.Locals(localsRole, as.Role)
	return c.Next()
}

// csrfGuard rejects cross-origin state-changing requests (OWASP
// Sec-Fetch-Site, then Origin). It fails closed when both headers are absent.
func csrfGuard(c *fiber.Ctx) error {
	if isSafeMethod(c.Method()) {
		return c.Next()
	}

	switch c.Get("Sec-Fetch-Site") {
	case "same-origin", "none":
		return c.Next()
	}

	if origin := c.Get(fiber.HeaderOrigin); origin != "" && origin == requestOrigin(c) {
		return c.Next()
	}

	return writeProblem(c, fiber.StatusForbidden, "forbidden", "cross-origin request rejected")
}

// requestOrigin rebuilds scheme://host[:port] in the Origin header's format.
func requestOrigin(c *fiber.Ctx) string {
	scheme := "http"
	if isHTTPS(c) {
		scheme = "https"
	}
	return scheme + "://" + string(c.Request().Host())
}

func isSafeMethod(method string) bool {
	switch method {
	case fiber.MethodGet, fiber.MethodHead, fiber.MethodOptions:
		return true
	default:
		return false
	}
}

// stringPtrOrNil stores an absent User-Agent as SQL NULL, not "".
func stringPtrOrNil(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
