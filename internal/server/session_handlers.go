package server

import (
	"errors"
	"fmt"
	"regexp"

	"moviepickarr/internal/domain"

	"github.com/gofiber/fiber/v2"
)

// sessionResponse is one row of a member's own device list. The token hash and
// internal row id never leave the store; an immutable random handle addresses a
// revoke and is usable only by its owner because every delete scopes by member.
// Device is derived from the stored user agent rather than shipping the raw
// string: the member wants to recognize a device, not read a UA.
type sessionResponse struct {
	ID         string `json:"id"`
	Device     string `json:"device"`
	LastSeenAt string `json:"lastSeenAt"`
	Current    bool   `json:"current"`
}

// handleListSessions returns the actor's own live sessions, most recently active
// first. Self-only by construction: the member id comes from the session, never
// from the request, so there is no id to authorize and no way to read someone
// else's devices.
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

// handleRevokeSession signs one of the actor's own devices out. The delete is
// scoped to the actor, so a session handle belonging to another member matches
// nothing and comes back 404 rather than revoking anything. Revoking a row
// that's already gone is also 404: the list the member acted on was stale, and
// saying so is more use than a silent 204. Ending the current session is
// allowed and clears the cookie, though the UI routes that through Log out.
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
		// Anything else is the store faulting, which must be logged rather than
		// masked behind a bare 500.
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
