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

// meResponse is the GET /auth/me projection. Username serializes as null (not
// omitted) when the member has no local login, matching the spec's username|null.
type meResponse struct {
	ID                int     `json:"id"`
	DisplayName       string  `json:"displayName"`
	Username          *string `json:"username"`
	Role              string  `json:"role"`
	HasLocalLogin     bool    `json:"hasLocalLogin"`
	HasLinkedIdentity bool    `json:"hasLinkedIdentity"`
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

	return c.Status(fiber.StatusOK).JSON(meResponse{
		ID:                id.ID,
		DisplayName:       id.DisplayName,
		Username:          id.Username,
		Role:              id.Role,
		HasLocalLogin:     id.HasLocalLogin,
		HasLinkedIdentity: id.HasLinkedIdentity,
	})
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
