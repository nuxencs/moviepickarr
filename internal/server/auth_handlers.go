package server

import (
	"fmt"
	"strings"
	"time"

	"moviepickarr/internal/domain"

	"github.com/gofiber/fiber/v2"
)

const (
	authPrincipalContextKey = "auth_principal"
	defaultAuthCookieName   = "moviepickarr_session"
)

type authMeResponse struct {
	UserID    int    `json:"userID"`
	Name      string `json:"name"`
	Username  string `json:"username"`
	Role      string `json:"role"`
	LoggedIn  bool   `json:"loggedIn"`
	ExpiresAt string `json:"expiresAt,omitempty"`
}

func (h *handler) requireAuth(c *fiber.Ctx) error {
	token := strings.TrimSpace(c.Cookies(h.authCookieName))
	principal, err := h.authService.Authenticate(c.UserContext(), token)
	if err != nil {
		return writeError(c, domain.ErrUnauthenticated)
	}

	c.Locals(authPrincipalContextKey, principal)
	return c.Next()
}

func (h *handler) handleAuthLogin(c *fiber.Ctx) error {
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := c.BodyParser(&body); err != nil {
		return writeError(c, fmt.Errorf("%w: invalid request body", domain.ErrInvalidInput))
	}

	result, err := h.authService.Login(c.UserContext(), body.Username, body.Password, loginMetadataFromFiber(c))
	if err != nil {
		return writeError(c, err)
	}

	h.setSessionCookie(c, result.Token, result.ExpiresAt)

	return c.Status(fiber.StatusOK).JSON(authMeResponse{
		UserID:    result.Principal.UserID,
		Name:      result.Principal.Name,
		Username:  result.Principal.Username,
		Role:      string(result.Principal.Role),
		LoggedIn:  true,
		ExpiresAt: result.ExpiresAt.UTC().Format(time.RFC3339),
	})
}

func (h *handler) handleAuthMe(c *fiber.Ctx) error {
	principal, ok := h.currentPrincipal(c)
	if !ok {
		return writeError(c, domain.ErrUnauthenticated)
	}

	return c.Status(fiber.StatusOK).JSON(authMeResponse{
		UserID:   principal.UserID,
		Name:     principal.Name,
		Username: principal.Username,
		Role:     string(principal.Role),
		LoggedIn: true,
	})
}

func (h *handler) handleAuthLogout(c *fiber.Ctx) error {
	token := strings.TrimSpace(c.Cookies(h.authCookieName))
	if token != "" {
		_ = h.authService.Logout(c.UserContext(), token)
	}
	h.clearSessionCookie(c)
	return c.SendStatus(fiber.StatusNoContent)
}

func (h *handler) setSessionCookie(c *fiber.Ctx, token string, expiresAt time.Time) {
	c.Cookie(&fiber.Cookie{
		Name:     h.authCookieName,
		Value:    token,
		Path:     "/",
		HTTPOnly: true,
		Secure:   h.authCookieSecure,
		SameSite: "lax",
		Expires:  expiresAt,
	})
}

func (h *handler) clearSessionCookie(c *fiber.Ctx) {
	c.Cookie(&fiber.Cookie{
		Name:     h.authCookieName,
		Value:    "",
		Path:     "/",
		HTTPOnly: true,
		Secure:   h.authCookieSecure,
		SameSite: "lax",
		MaxAge:   -1,
		Expires:  time.Unix(0, 0),
	})
}
