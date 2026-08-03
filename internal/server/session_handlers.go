package server

import (
	"errors"
	"fmt"

	"moviepickarr/internal/domain"

	"github.com/gofiber/fiber/v2"
)

// sessionResponse is one row of a member's own device list. The token hash never
// leaves the store, so a session is addressed by row id; the id is only ever
// usable by its owner, since every revoke scopes the delete by member. Device is
// derived from the stored user agent rather than shipping the raw string: the
// member wants to recognize a device, not read a UA. Current marks the session
// making this request, which is why that row offers no sign-out.
type sessionResponse struct {
	ID         int64  `json:"id"`
	Device     string `json:"device"`
	IP         string `json:"ip,omitempty"`
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
			ID:         s.ID,
			Device:     deviceLabel(s.UserAgent),
			LastSeenAt: formatTime(&s.LastSeenAt),
			Current:    s.Current,
		}
		if s.IP != nil {
			row.IP = *s.IP
		}
		rows = append(rows, row)
	}

	return c.Status(fiber.StatusOK).JSON(rows)
}

// handleRevokeSession signs one of the actor's own devices out. The delete is
// scoped to the actor, so a session id belonging to another member matches
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

	wasCurrent, err := h.sessions.RevokeByID(c.UserContext(), memberID, sessionID, c.Cookies(sessionCookieName))
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

// resolveSessionID reads the :sessionID path parameter as a positive int64.
func resolveSessionID(c *fiber.Ctx) (int64, error) {
	if v, ok := parseInt(c.Params("sessionID")); ok {
		return int64(v), nil
	}
	return 0, fmt.Errorf("%w: sessionID path parameter is required", domain.ErrInvalidInput)
}
