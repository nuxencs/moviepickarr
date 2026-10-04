package server

import (
	"database/sql"
	"errors"
	"fmt"

	"moviepickarr/internal/auth"
	"moviepickarr/internal/domain"

	"github.com/gofiber/fiber/v2"
)

// writeInvalidCredentials writes the one uniform login failure, so every
// credential failure looks the same. Plain {error}, not problem+json: the login
// form expects it.
func writeInvalidCredentials(c *fiber.Ctx) error {
	return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "invalid credentials"})
}

// writeAuthError maps auth sentinels to HTTP and defers the rest to writeError.
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

// writeInternal logs an infrastructure fault and returns an opaque 500.
func (h *handler) writeInternal(c *fiber.Ctx, err error, msg string) error {
	h.reqLog(c).Error().Err(err).Msg(msg)
	return writeProblem(c, fiber.StatusInternalServerError, "internal_error", "internal server error")
}

func (h *handler) isAdmin(c *fiber.Ctx) bool {
	return roleFromLocals(c) == domain.RoleAdmin
}

func roleFromLocals(c *fiber.Ctx) domain.Role {
	switch value := c.Locals(localsRole).(type) {
	case domain.Role:
		return value
	case string:
		role, _ := domain.ParseRole(value)
		return role
	default:
		return ""
	}
}

// requireAdmin writes a 403 for a non-admin and reports whether to proceed.
func (h *handler) requireAdmin(c *fiber.Ctx) (bool, error) {
	if h.isAdmin(c) {
		return true, nil
	}
	return false, writeProblem(c, fiber.StatusForbidden, "admin_required", "admin role required")
}

// requireTurnParticipant refuses group-decision commands for Guests, so a stale
// or crafted request cannot bypass a disabled control.
func (h *handler) requireTurnParticipant(c *fiber.Ctx) (bool, error) {
	if roleFromLocals(c).IsTurnParticipant() {
		return true, nil
	}
	return false, writeProblem(c, fiber.StatusForbidden, "guest_restricted", "guest role cannot perform this action")
}

// requireNextUp lets only the next-up member run the watch, draw, reveal turn,
// even admins. Admins move a stuck turn with handleSkipNextUp.
func (h *handler) requireNextUp(c *fiber.Ctx) (bool, error) {
	if ok, err := h.requireTurnParticipant(c); !ok {
		return false, err
	}

	nextUp, err := h.nextUpService.Get(c.UserContext())
	if err == nil && nextUp.ID == actorMemberID(c) {
		return true, nil
	}
	// sql.ErrNoRows (empty roster) is a plain not-your-turn refusal.
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return false, writeError(c, err)
	}
	return false, writeProblem(c, fiber.StatusForbidden, "not_next_up", "it is not your turn")
}

// runDrawCommand runs the turn check, the command, and its event publication
// under one lock, so the outgoing member cannot authorize another command
// before next up advances.
func (h *handler) runDrawCommand(c *fiber.Ctx, command func() error) (ran bool, err error) {
	h.drawCommandMu.Lock()
	defer h.drawCommandMu.Unlock()

	if ok, err := h.requireNextUp(c); !ok {
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

// handleLogin verifies outside SQLite, then commits the session under an
// expected-password-hash guard, so a recovery that commits during verification
// wins over the old password.
func (h *handler) handleLogin(c *fiber.Ctx) error {
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := c.BodyParser(&body); err != nil {
		return writeProblem(c, fiber.StatusBadRequest, "invalid_request", "invalid request body")
	}

	login, err := h.localAuth.PrepareLogin(c.UserContext(), body.Username, body.Password)
	if err != nil {
		if errors.Is(err, auth.ErrInvalidCredentials) {
			return writeInvalidCredentials(c)
		}
		return h.writeInternal(c, err, "verifying local login failed")
	}

	rawToken, session, err := h.sessions.PrepareMint(
		login.UserID,
		stringPtrOrNil(c.Get(fiber.HeaderUserAgent)),
	)
	if err != nil {
		return h.writeInternal(c, err, "preparing session on login failed")
	}
	if err := h.invites.CompleteLocalLogin(c.UserContext(), login, session); err != nil {
		if errors.Is(err, auth.ErrInvalidCredentials) {
			return writeInvalidCredentials(c)
		}
		return h.writeInternal(c, err, "committing local login failed")
	}
	setSessionCookie(c, rawToken)
	return c.SendStatus(fiber.StatusNoContent)
}

// authConfigResponse is what the unauthenticated login page needs to render.
type authConfigResponse struct {
	OIDC bool `json:"oidc"`
}

// handleAuthConfig is unauthenticated on purpose: OIDC enablement is already
// public through the SSO button.
func (h *handler) handleAuthConfig(c *fiber.Ctx) error {
	return c.Status(fiber.StatusOK).JSON(authConfigResponse{OIDC: h.oidcEnabled})
}

// meResponse is the GET /auth/me body. Username is null, not omitted, without
// a local login.
type meResponse struct {
	ID                int         `json:"id"`
	DisplayName       string      `json:"displayName"`
	Username          *string     `json:"username"`
	Role              domain.Role `json:"role"`
	HasLocalLogin     bool        `json:"hasLocalLogin"`
	HasLinkedIdentity bool        `json:"hasLinkedIdentity"`
}

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

// handleLogout revokes this device's session, or every session with
// {"all":true}. It is idempotent.
func (h *handler) handleLogout(c *fiber.Ctx) error {
	memberID, _ := c.Locals(localsMemberID).(int)

	var body struct {
		All bool `json:"all"`
	}
	// An empty POST is a valid current-device logout, not a malformed body.
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

// handleChangePassword verifies and hashes outside SQLite, then atomically
// rewrites the credential, revokes old sessions, and mints a fresh one.
func (h *handler) handleChangePassword(c *fiber.Ctx) error {
	memberID, _ := c.Locals(localsMemberID).(int)

	var body struct {
		CurrentPassword string `json:"currentPassword"`
		NewPassword     string `json:"newPassword"`
	}
	if err := c.BodyParser(&body); err != nil {
		return writeProblem(c, fiber.StatusBadRequest, "invalid_request", "invalid request body")
	}

	change, err := h.localAuth.PreparePasswordChange(
		c.UserContext(),
		memberID,
		body.CurrentPassword,
		body.NewPassword,
	)
	if err != nil {
		return h.writeAuthError(c, err)
	}
	rawSession, session, err := h.sessions.PrepareMint(
		memberID,
		stringPtrOrNil(c.Get(fiber.HeaderUserAgent)),
	)
	if err != nil {
		return h.writeInternal(c, err, "preparing rotated session on password change failed")
	}
	change.Session = &session
	if err := h.invites.ChangePassword(c.UserContext(), change); err != nil {
		return h.writeAuthError(c, err)
	}
	setSessionCookie(c, rawSession)
	return c.SendStatus(fiber.StatusNoContent)
}

// handleSetLocalLogin is the admin upsert of a member's local login. A reset
// keeps the username, revokes the target's sessions, and clears any lockout.
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

	_, err = h.invites.SetLocalLogin(c.UserContext(), targetID, body.Username, body.Password)
	if err != nil {
		return writeError(c, err)
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// handleDeleteLocalLogin is the admin removal of a member's local login. The
// service refuses an admin's own last credential with a 409.
func (h *handler) handleDeleteLocalLogin(c *fiber.Ctx) error {
	if ok, err := h.requireAdmin(c); !ok {
		return err
	}
	targetID, err := resolveMemberID(c)
	if err != nil {
		return writeError(c, err)
	}
	actorID, _ := c.Locals(localsMemberID).(int)

	if err := h.invites.DeleteLocalLogin(c.UserContext(), targetID, actorID); err != nil {
		return writeError(c, err)
	}
	return c.SendStatus(fiber.StatusNoContent)
}
