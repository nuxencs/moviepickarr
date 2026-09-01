package server

import (
	"errors"

	"moviepickarr/internal/domain"

	"github.com/gofiber/fiber/v2"
)

// rosterMemberResponse is one row of the admin roster: identity plus the
// presence-derived login state the surface renders as chips. Link-state is never
// a stored flag: the three booleans are the existence of a credential / invite /
// archive row. moviesAuthored lets the surface preview whether a remove will
// hard-delete (frees the name) or archive (keeps attribution) before committing.
// lastSeenAt is the newest session touch, omitted for members who never had one.
type rosterMemberResponse struct {
	ID                int         `json:"id"`
	Name              string      `json:"name"`
	Username          string      `json:"username,omitempty"`
	Role              domain.Role `json:"role"`
	Archived          bool        `json:"archived"`
	HasLocalLogin     bool        `json:"hasLocalLogin"`
	HasLinkedIdentity bool        `json:"hasLinkedIdentity"`
	InvitePending     bool        `json:"invitePending"`
	MoviesAuthored    int         `json:"moviesAuthored"`
	LastSeenAt        string      `json:"lastSeenAt,omitempty"`
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
// enum {member, guest, admin}. Demoting the Next up holder to Guest requires an
// explicit retry because the same transaction also hands off the turn. The repo
// refuses demoting the last admin. Sessions stay valid and read the live role.
func (h *handler) handleSetRole(c *fiber.Ctx) error {
	if ok, err := h.requireAdmin(c); !ok {
		return err
	}

	memberID, err := resolveMemberID(c)
	if err != nil {
		return writeError(c, err)
	}

	var body struct {
		Role               string `json:"role"`
		ConfirmTurnHandoff bool   `json:"confirmTurnHandoff"`
	}
	if err := c.BodyParser(&body); err != nil {
		return writeProblem(c, fiber.StatusBadRequest, "invalid_request", "invalid request body")
	}

	role, valid := domain.ParseRole(body.Role)
	if !valid {
		return writeProblem(c, fiber.StatusBadRequest, "invalid_request", "role must be member, guest, or admin")
	}
	result, err := h.userService.SetRole(c.UserContext(), domain.RoleChange{
		MemberID:           memberID,
		Role:               role,
		ConfirmTurnHandoff: body.ConfirmTurnHandoff,
	})
	if errors.Is(err, domain.ErrTurnHandoffConfirmationRequired) {
		return writeProblem(c, fiber.StatusConflict, "turn_handoff_confirmation_required", "making this member a Guest will hand Next up to the next eligible member")
	}
	if err != nil {
		return writeError(c, err)
	}
	if result.Changed {
		h.broker.Broadcast(event{Type: "user:role-changed", Data: fiber.Map{
			"userID": memberID,
			"role":   role,
		}})
	}
	if result.TurnChanged {
		payload := fiber.Map{"id": 0, "name": ""}
		if result.NextUp != nil {
			payload = fiber.Map{"id": result.NextUp.ID, "name": result.NextUp.Name}
		}
		h.broker.Broadcast(event{Type: "settings:next-up-changed", Data: payload})
	}

	return c.SendStatus(fiber.StatusNoContent)
}
