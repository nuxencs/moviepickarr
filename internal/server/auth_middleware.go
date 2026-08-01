package server

import (
	"context"
	"errors"
	"strings"
	"time"

	"moviepickarr/internal/auth"

	"github.com/gofiber/fiber/v2"
)

// sessionCookieName is the one opaque cookie every login path sets and
// requireSession reads. HttpOnly + SameSite=Lax + Path=/ with a scheme-derived
// Secure flag; the value is the raw session token, never anything derived from
// the member.
const sessionCookieName = "mpa_session"

// Request-scoped keys for the actor requireSession attaches. Unexported so only
// this package's handlers read them, via c.Locals.
const (
	localsMemberID = "memberID"
	localsRole     = "role"
)

// cookieEpoch is a fixed past instant used to expire the session cookie on a
// clear. It is a constant, not the session clock, so it stays deterministic.
var cookieEpoch = time.Unix(0, 0)

// isHTTPS reports whether the request reached us over TLS, so the session
// cookie's Secure flag and the CSRF origin check derive the scheme the same
// way. Honors X-Forwarded-Proto for a TLS-terminating proxy; omitted on plain
// http so raw-http dev still works (documented residual: no Secure on http).
func isHTTPS(c *fiber.Ctx) bool {
	if strings.EqualFold(c.Get(fiber.HeaderXForwardedProto), "https") {
		return true
	}
	return c.Protocol() == "https"
}

// setSessionCookie writes the session cookie with a persistent 90-day Max-Age,
// set once at mint. Secure tracks the request scheme.
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

// clearSessionCookie expires the session cookie. It mirrors the set attributes
// (Path, HttpOnly, SameSite, Secure) so the browser matches and drops the right
// cookie.
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

// issueSession mints a fresh session for a member and sets its cookie. Every
// login path calls this after establishing identity; it never adopts an inbound
// cookie, so a fixed token can't be promoted into an authenticated one.
func (h *handler) issueSession(c *fiber.Ctx, memberID int) error {
	rawToken, _, err := h.sessions.Mint(c.UserContext(), memberID, stringPtrOrNil(c.Get(fiber.HeaderUserAgent)), stringPtrOrNil(c.IP()))
	if err != nil {
		return err
	}
	setSessionCookie(c, rawToken)
	return nil
}

// sessionSweepInterval is how often expired sessions are swept in the
// background. Lazy rejection in Authenticate already keeps expired rows
// harmless, so this cadence is housekeeping, not a security boundary.
const sessionSweepInterval = time.Hour

// startSessionSweeper sweeps expired sessions once now and then hourly until ctx
// is cancelled. The startup sweep clears anything that expired while the process
// was down; the ticker keeps the table from accumulating dead rows over a long
// uptime. A sweep failure is logged and the loop continues.
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
		// The sweep is a periodic background tick: a failure costs nothing but
		// some stale rows, and the next tick retries. Recoverable, so warn.
		h.log.Warn().Err(err).Msg("expired-session sweep failed, retrying next tick")
		return
	}
	if removed > 0 {
		h.log.Debug().Int64("count", removed).Msg("swept expired sessions")
	}
}

// requireSession is the gate: cookie → validate → attach live actor, or reject
// with 401 and a cookie-clear. It runs after csrfGuard in the chain.
func (h *handler) requireSession(c *fiber.Ctx) error {
	as, err := h.sessions.Authenticate(c.UserContext(), c.Cookies(sessionCookieName))
	if err != nil {
		if errors.Is(err, auth.ErrSessionInvalid) {
			clearSessionCookie(c)
			return writeProblem(c, fiber.StatusUnauthorized, "unauthorized", "authentication required")
		}
		// requireSession has not attached an actor yet, so this line carries the
		// request only. That is the point: it is the one 500 whose cause is
		// invisible from the access log alone.
		h.reqLogBeforeRoute(c).Error().Err(err).Msg("session lookup failed")
		return writeProblem(c, fiber.StatusInternalServerError, "internal_error", "internal server error")
	}

	c.Locals(localsMemberID, as.UserID)
	c.Locals(localsRole, as.Role)
	return c.Next()
}

// csrfGuard rejects cross-origin state-changing requests before requireSession
// runs. Rule (OWASP): safe methods pass; otherwise allow when Sec-Fetch-Site is
// same-origin/none, else allow when the Origin header equals our origin, else
// 403 (fail closed) when both signals are absent. Safe methods and the
// read-only GETs (OIDC callback, claim) fall through here because they are GETs.
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

// requestOrigin reconstructs this request's origin (scheme://host[:port]) so the
// Origin header can be compared against it. Host carries any non-default port,
// matching the Origin header's own format.
func requestOrigin(c *fiber.Ctx) string {
	scheme := "http"
	if isHTTPS(c) {
		scheme = "https"
	}
	return scheme + "://" + string(c.Request().Host())
}

// isSafeMethod reports whether an HTTP method is read-only and thus exempt from
// the CSRF origin check.
func isSafeMethod(method string) bool {
	switch method {
	case fiber.MethodGet, fiber.MethodHead, fiber.MethodOptions:
		return true
	default:
		return false
	}
}

// stringPtrOrNil returns nil for an empty string so an absent User-Agent or IP
// lands as SQL NULL rather than an empty-string row.
func stringPtrOrNil(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
