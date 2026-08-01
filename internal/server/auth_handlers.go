package server

import (
	"database/sql"
	"errors"
	"fmt"

	"moviepickarr/internal/auth"
	"moviepickarr/internal/domain"

	"github.com/gofiber/fiber/v2"
)

// writeInvalidCredentials writes the one uniform login failure: 401 with the
// exact body {"error":"invalid credentials"}. Unknown username, wrong password,
// no local login, and a locked account all land here so status and body stay
// indistinguishable (the timing match is the service's dummy-verify job). It is
// deliberately the plain {error} shape the login form expects, not the
// problem+json the rest of the API uses.
func writeInvalidCredentials(c *fiber.Ctx) error {
	return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "invalid credentials"})
}

// writeAuthError maps the auth package's sentinels to their HTTP shapes and
// defers everything else (validation, conflict, not-found, infra) to writeError.
func (h *handler) writeAuthError(c *fiber.Ctx, err error) error {
	switch {
	case errors.Is(err, auth.ErrInvalidCredentials):
		return writeInvalidCredentials(c)
	case errors.Is(err, auth.ErrNoLocalLogin):
		return writeProblem(c, fiber.StatusConflict, "conflict", "no local login to change")
	default:
		return writeError(c, err)
	}
}

// writeInternal logs an infrastructure fault and returns the opaque 500 every
// auth handler shares, so the log-and-mask pair lives in one place.
func (h *handler) writeInternal(c *fiber.Ctx, err error, msg string) error {
	h.log.Error().Err(err).Msg(msg)
	return writeProblem(c, fiber.StatusInternalServerError, "internal_error", "internal server error")
}

// isAdmin reports whether the session actor holds the admin role. The admin
// local-login routes gate on this inline until the per-route authz reshape
// lands a shared admin middleware.
func (h *handler) isAdmin(c *fiber.Ctx) bool {
	role, _ := c.Locals(localsRole).(string)
	return role == "admin"
}

// requireAdmin writes the 403 an admin-only route returns to a non-admin and
// reports whether the caller may proceed.
func (h *handler) requireAdmin(c *fiber.Ctx) (bool, error) {
	if h.isAdmin(c) {
		return true, nil
	}
	return false, writeProblem(c, fiber.StatusForbidden, "admin_required", "admin role required")
}

// requireNextUpOrAdmin gates the draw → reveal → watch cycle: only the member
// whose turn it is, or an admin, may run movie night. Anyone else gets 403
// not_next_up. Because the rotation advances only on watch, the same member
// holds the turn across the whole cycle.
func (h *handler) requireNextUpOrAdmin(c *fiber.Ctx) (bool, error) {
	if h.isAdmin(c) {
		return true, nil
	}

	nextUp, err := h.nextUpService.Get(c.UserContext())
	if err == nil && nextUp.ID == actorMemberID(c) {
		return true, nil
	}
	// A real infra error surfaces as-is; sql.ErrNoRows (empty roster, no one up)
	// falls through to the same not-your-turn refusal as a plain mismatch.
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return false, writeError(c, err)
	}
	return false, writeProblem(c, fiber.StatusForbidden, "not_next_up", "it is not your turn")
}

// runMovieNightCommand keeps the authorization snapshot, lifecycle command, and
// synchronous event publication in one process-local critical section. Watch
// holds the section until next up advances and movie:watched is published, so
// the outgoing member cannot authorize another command in the gap.
func (h *handler) runMovieNightCommand(c *fiber.Ctx, command func() error) (ran bool, err error) {
	h.movieNightMu.Lock()
	defer h.movieNightMu.Unlock()

	if ok, err := h.requireNextUpOrAdmin(c); !ok {
		return false, err
	}
	return true, command()
}

// resolveMemberID reads the :memberID path parameter as a positive int.
func resolveMemberID(c *fiber.Ctx) (int, error) {
	if v, ok := parseInt(c.Params("memberID")); ok {
		return v, nil
	}
	return 0, fmt.Errorf("%w: memberID path parameter is required", domain.ErrInvalidInput)
}

// handleLogin is the username/password login. On success it mints a fresh
// session (never adopts an inbound cookie) and returns 204 with the cookie set;
// the client hydrates via /me. Every credential failure is the uniform 401.
func (h *handler) handleLogin(c *fiber.Ctx) error {
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := c.BodyParser(&body); err != nil {
		return writeProblem(c, fiber.StatusBadRequest, "invalid_request", "invalid request body")
	}

	memberID, err := h.localAuth.Login(c.UserContext(), body.Username, body.Password)
	if err != nil {
		if errors.Is(err, auth.ErrInvalidCredentials) {
			return writeInvalidCredentials(c)
		}
		return h.writeInternal(c, err, "login failed")
	}

	if err := h.issueSession(c, memberID); err != nil {
		return h.writeInternal(c, err, "minting session on login failed")
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// authConfigResponse is the public GET /auth/config projection: the handful of
// presence-derived facts an unauthenticated login page needs to render itself.
// Today that is only whether an SSO provider is configured, so the login page
// can render the SSO button (present, not disabled) exactly when it can work.
type authConfigResponse struct {
	OIDC bool `json:"oidc"`
}

// handleAuthConfig reports the auth capabilities visible to a caller with no
// session yet. It is deliberately unauthenticated and carries no secrets: OIDC
// enablement is already public (the SSO button either appears or it doesn't).
func (h *handler) handleAuthConfig(c *fiber.Ctx) error {
	return c.Status(fiber.StatusOK).JSON(authConfigResponse{OIDC: h.oidcEnabled})
}

// meResponse is the GET /auth/me projection. Username serializes as null (not
// omitted) when the member has no local login, matching the spec's username|null.
type meResponse struct {
	ID                int     `json:"id"`
	DisplayName       string  `json:"displayName"`
	Username          *string `json:"username"`
	Role              string  `json:"role"`
	HasLocalLogin     bool    `json:"hasLocalLogin"`
	HasLinkedIdentity bool    `json:"hasLinkedIdentity"`
	// OtherSessions is how many other devices the actor is signed in on, so the
	// account page can show the count before a log-out-everywhere. It excludes
	// this session and counts only live rows.
	OtherSessions int `json:"otherSessions"`
}

// handleMe returns the current session actor's identity plus its
// presence-derived link-state flags. requireSession has already rejected an
// invalid session with 401.
func (h *handler) handleMe(c *fiber.Ctx) error {
	memberID, _ := c.Locals(localsMemberID).(int)

	id, err := h.localAuth.Identity(c.UserContext(), memberID)
	if err != nil {
		return writeError(c, err)
	}

	// The other-device count is best-effort: a failed count must not break the
	// identity read the whole app depends on, so it falls back to 0 (the account
	// page then just reads "no other devices" until the next /me succeeds).
	others, err := h.sessions.CountOtherSessions(c.UserContext(), memberID, c.Cookies(sessionCookieName))
	if err != nil {
		h.log.Error().Err(err).Msg("counting other sessions failed")
		others = 0
	}

	return c.Status(fiber.StatusOK).JSON(meResponse{
		ID:                id.ID,
		DisplayName:       id.DisplayName,
		Username:          id.Username,
		Role:              id.Role,
		HasLocalLogin:     id.HasLocalLogin,
		HasLinkedIdentity: id.HasLinkedIdentity,
		OtherSessions:     others,
	})
}

// handleLogout revokes the actor's session(s) and clears the cookie. An empty
// body or {} logs out just this device; {"all":true} revokes every session for
// the member (the compromise-recovery path). It always clears the cookie and
// returns 204, and revoking an already-gone session is a no-op, so logout is
// idempotent.
func (h *handler) handleLogout(c *fiber.Ctx) error {
	memberID, _ := c.Locals(localsMemberID).(int)

	var body struct {
		All bool `json:"all"`
	}
	// The body is optional: only parse when one was sent, so an empty POST
	// (current-device logout) isn't rejected as a malformed body.
	if len(c.Body()) > 0 {
		if err := c.BodyParser(&body); err != nil {
			return writeProblem(c, fiber.StatusBadRequest, "invalid_request", "invalid request body")
		}
	}

	if body.All {
		if err := h.sessions.RevokeAll(c.UserContext(), memberID); err != nil {
			return h.writeInternal(c, err, "revoking all sessions on logout failed")
		}
	} else {
		if err := h.sessions.RevokeCurrent(c.UserContext(), c.Cookies(sessionCookieName)); err != nil {
			return h.writeInternal(c, err, "revoking current session on logout failed")
		}
	}

	clearSessionCookie(c)
	return c.SendStatus(fiber.StatusNoContent)
}

// handleChangePassword verifies the actor's current password and rewrites it,
// then closes the exposure: every other session is revoked and the current
// token is rotated (revoke-all + fresh mint), so the rotation actually kicks
// other devices without logging this one out.
func (h *handler) handleChangePassword(c *fiber.Ctx) error {
	memberID, _ := c.Locals(localsMemberID).(int)

	var body struct {
		CurrentPassword string `json:"currentPassword"`
		NewPassword     string `json:"newPassword"`
	}
	if err := c.BodyParser(&body); err != nil {
		return writeProblem(c, fiber.StatusBadRequest, "invalid_request", "invalid request body")
	}

	if err := h.localAuth.ChangePassword(c.UserContext(), memberID, body.CurrentPassword, body.NewPassword); err != nil {
		return h.writeAuthError(c, err)
	}

	// Revoke every session (including this one) then mint fresh for this device:
	// the net state is exactly one live session with a new token, so other
	// devices are closed and the current token is rotated.
	if err := h.sessions.RevokeAll(c.UserContext(), memberID); err != nil {
		return h.writeInternal(c, err, "revoking sessions on password change failed")
	}
	if err := h.issueSession(c, memberID); err != nil {
		return h.writeInternal(c, err, "rotating session on password change failed")
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// handleSetLocalLogin is the admin upsert of a member's local login. Creating a
// first login needs {username, password}; an existing row is an admin reset
// (password only, username immutable) that revokes all of the target's sessions
// and clears any lockout.
func (h *handler) handleSetLocalLogin(c *fiber.Ctx) error {
	if ok, err := h.requireAdmin(c); !ok {
		return err
	}
	targetID, err := resolveMemberID(c)
	if err != nil {
		return writeError(c, err)
	}

	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := c.BodyParser(&body); err != nil {
		return writeProblem(c, fiber.StatusBadRequest, "invalid_request", "invalid request body")
	}

	res, err := h.localAuth.SetLocalLogin(c.UserContext(), targetID, body.Username, body.Password)
	if err != nil {
		return writeError(c, err)
	}

	if res.WasReset {
		if err := h.sessions.RevokeAll(c.UserContext(), targetID); err != nil {
			return h.writeInternal(c, err, "revoking sessions on admin reset failed")
		}
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// handleDeleteLocalLogin is the admin removal of a member's local login. The
// self-last-credential guard (in the service) refuses an admin's own only
// credential with a 409.
func (h *handler) handleDeleteLocalLogin(c *fiber.Ctx) error {
	if ok, err := h.requireAdmin(c); !ok {
		return err
	}
	targetID, err := resolveMemberID(c)
	if err != nil {
		return writeError(c, err)
	}
	actorID, _ := c.Locals(localsMemberID).(int)

	if err := h.localAuth.DeleteLocalLogin(c.UserContext(), targetID, actorID); err != nil {
		return writeError(c, err)
	}
	return c.SendStatus(fiber.StatusNoContent)
}
