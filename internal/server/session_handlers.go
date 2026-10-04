package server

import (
	"errors"
	"fmt"
	"regexp"

	"moviepickarr/internal/domain"

	"github.com/gofiber/fiber/v2"
)

// sessionResponse is one row of a member's device list. The token hash and row
// id never leave the store; a random handle addresses a revoke.
type sessionResponse struct {
	ID         string `json:"id"`
	Device     string `json:"device"`
	LastSeenAt string `json:"lastSeenAt"`
	Current    bool   `json:"current"`
}

// handleListSessions returns the actor's live sessions, most recent first. The
// member id comes from the session, never the request.
func (h *handler) handleListSessions(c *fiber.Ctx) error {
	memberID := actorMemberID(c)

	sessions, err := h.sessions.List(c.UserContext(), memberID, c.Cookies(sessionCookieName))
	if err != nil {
		return h.writeInternal(c, err, "listing sessions failed")
	}

	rows := make([]sessionResponse, 0, len(sessions))
	for _, s := range sessions {
		row := sessionResponse{
			ID:         s.PublicID,
			Device:     deviceLabel(s.UserAgent),
			LastSeenAt: formatTime(&s.LastSeenAt),
			Current:    s.Current,
		}
		rows = append(rows, row)
	}

	return c.Status(fiber.StatusOK).JSON(rows)
}

// handleRevokeSession signs out one of the actor's devices. The delete scopes
// by actor, so another member's handle is a 404. A gone row is also 404, to
// tell the client its list was stale.
func (h *handler) handleRevokeSession(c *fiber.Ctx) error {
	memberID := actorMemberID(c)

	sessionID, err := resolveSessionID(c)
	if err != nil {
		return writeError(c, err)
	}

	wasCurrent, err := h.sessions.RevokeByPublicID(c.UserContext(), memberID, sessionID, c.Cookies(sessionCookieName))
	if errors.Is(err, domain.ErrNotFound) {
		return writeError(c, err)
	}
	if err != nil {
		// A store fault: log it, not a bare 500.
		return h.writeInternal(c, err, "revoking session failed")
	}

	if wasCurrent {
		clearSessionCookie(c)
	}
	return c.SendStatus(fiber.StatusNoContent)
}

var sessionPublicIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{20,64}$`)

// resolveSessionID reads the immutable public handle carried in :sessionID.
// Both migrated hex ids and newly minted base64url ids fit this alphabet.
func resolveSessionID(c *fiber.Ctx) (string, error) {
	value := c.Params("sessionID")
	if sessionPublicIDPattern.MatchString(value) {
		return value, nil
	}
	return "", fmt.Errorf("%w: sessionID path parameter is invalid", domain.ErrInvalidInput)
}
