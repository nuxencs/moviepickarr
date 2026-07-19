package server

import (
	"github.com/gofiber/fiber/v2"
)

// rosterMemberResponse is one row of the admin roster: identity plus the
// presence-derived login state the surface renders as chips. Link-state is never
// a stored flag: the three booleans are the existence of a credential / invite /
// archive row. moviesAuthored lets the surface preview whether a remove will
// hard-delete (frees the name) or archive (keeps attribution) before committing.
// lastSeenAt is the newest session touch, omitted for members who never had one.
type rosterMemberResponse struct {
	ID                int    `json:"id"`
	Name              string `json:"name"`
	Username          string `json:"username,omitempty"`
	Role              string `json:"role"`
	Archived          bool   `json:"archived"`
	HasLocalLogin     bool   `json:"hasLocalLogin"`
	HasLinkedIdentity bool   `json:"hasLinkedIdentity"`
	InvitePending     bool   `json:"invitePending"`
	MoviesAuthored    int    `json:"moviesAuthored"`
	LastSeenAt        string `json:"lastSeenAt,omitempty"`
}

// handleGetRoster returns the admin roster (admin only): every member, active and
// archived, with presence-derived login state. Ordering is active-before-archived
// then oldest-first, so the surface splits the sections without re-sorting.
func (h *handler) handleGetRoster(c *fiber.Ctx) error {
	if ok, err := h.requireAdmin(c); !ok {
		return err
	}

	members, err := h.userService.Roster(c.UserContext())
	if err != nil {
		return writeError(c, err)
	}

	rows := make([]rosterMemberResponse, 0, len(members))
	for _, m := range members {
		rows = append(rows, rosterMemberResponse{
			ID:                m.ID,
			Name:              m.Name,
			Username:          m.Username,
			Role:              m.Role,
			Archived:          m.Archived,
			HasLocalLogin:     m.HasLocalLogin,
			HasLinkedIdentity: m.HasLinkedIdentity,
			InvitePending:     m.InvitePending,
			MoviesAuthored:    m.MoviesAuthored,
			LastSeenAt:        formatTime(m.LastSeenAt),
		})
	}

	return c.Status(fiber.StatusOK).JSON(rows)
}

// handleSetRole promotes or demotes a member (admin only). Role is the app-owned
// enum {member, admin}; the service validates it and the repo refuses demoting the
// last admin (409) so the roster can never be left unmanageable. Role is read live
// per request, so the change takes effect on the member's next call without
// disturbing their sessions: nothing here revokes or rotates a token.
func (h *handler) handleSetRole(c *fiber.Ctx) error {
	if ok, err := h.requireAdmin(c); !ok {
		return err
	}

	memberID, err := resolveMemberID(c)
	if err != nil {
		return writeError(c, err)
	}

	var body struct {
		Role string `json:"role"`
	}
	if err := c.BodyParser(&body); err != nil {
		return writeProblem(c, fiber.StatusBadRequest, "invalid_request", "invalid request body")
	}

	if err := h.userService.SetRole(c.UserContext(), memberID, body.Role); err != nil {
		return writeError(c, err)
	}

	return c.SendStatus(fiber.StatusNoContent)
}
