package server

import (
	"fmt"
	"strconv"

	"moviepickarr/internal/domain"

	"github.com/gofiber/fiber/v2"
)

func (h *handler) handleGetUsers(c *fiber.Ctx) error {
	ctx := c.UserContext()

	users, err := h.userService.List(ctx)
	if err != nil {
		return writeError(c, err)
	}

	// Fetch only pool and stash rows, not the whole table with its watched history.
	pooled, err := h.movieService.Pooled(ctx)
	if err != nil {
		return writeError(c, err)
	}
	stashed, err := h.movieService.Stashed(ctx)
	if err != nil {
		return writeError(c, err)
	}
	visible := make([]*domain.Movie, 0, len(pooled)+len(stashed))
	visible = append(visible, pooled...)
	visible = append(visible, stashed...)

	// Boards render tiles only, so skip the credits batch-load.
	meta := h.metaFor(c, visible)
	poolByUser := make(map[int]map[string]leanMovieTile)
	stashByUser := make(map[int]map[string]leanMovieTile)

	for i := range visible {
		tile := toLeanTile(visible[i], meta[visible[i].ID])
		key := strconv.Itoa(visible[i].ID)

		if visible[i].Status == "pool" {
			if poolByUser[visible[i].AddedByID] == nil {
				poolByUser[visible[i].AddedByID] = map[string]leanMovieTile{}
			}
			poolByUser[visible[i].AddedByID][key] = tile
			continue
		}

		if stashByUser[visible[i].AddedByID] == nil {
			stashByUser[visible[i].AddedByID] = map[string]leanMovieTile{}
		}
		stashByUser[visible[i].AddedByID][key] = tile
	}

	response := make([]userResponse, 0, len(users))
	for i := range users {
		currentPool := poolByUser[users[i].ID]
		if currentPool == nil {
			currentPool = map[string]leanMovieTile{}
		}
		stash := stashByUser[users[i].ID]
		if stash == nil {
			stash = map[string]leanMovieTile{}
		}

		response = append(response, userResponse{
			ID:          users[i].ID,
			Name:        users[i].Name,
			CurrentPool: currentPool,
			Stash:       stash,
			CreatedAt:   formatTime(users[i].CreatedAt),
		})
	}

	return c.Status(fiber.StatusOK).JSON(response)
}

func (h *handler) handleCreateUser(c *fiber.Ctx) error {
	if ok, err := h.requireAdmin(c); !ok {
		return err
	}

	var body struct {
		Name string `json:"name"`
		Role string `json:"role"`
	}
	if err := c.BodyParser(&body); err != nil {
		return writeError(c, fmt.Errorf("%w: invalid request body", domain.ErrInvalidInput))
	}

	name := sanitizeInput(body.Name)
	if name == "" {
		return writeError(c, fmt.Errorf("%w: name is required", domain.ErrInvalidInput))
	}
	role := sanitizeInput(body.Role)
	if role == "" {
		role = string(domain.RoleMember)
	}
	parsedRole, valid := domain.ParseRole(role)
	if !valid {
		return writeError(c, fmt.Errorf("%w: invalid member role", domain.ErrInvalidInput))
	}

	ctx := c.UserContext()
	// Placeholder, initial next-up and first invite commit together. The raw
	// claim token is never persisted and only returned here.
	createdUser, rawToken, err := h.invites.CreateMemberWithInvite(ctx, name, parsedRole, actorMemberID(c))
	if err != nil {
		return writeError(c, err)
	}

	// Stats list every member, so do not wait for the cache TTL.
	h.invalidateStatsCache()

	payload := userResponse{
		ID:          createdUser.ID,
		Name:        createdUser.Name,
		CurrentPool: map[string]leanMovieTile{},
		Stash:       map[string]leanMovieTile{},
		CreatedAt:   formatTime(createdUser.CreatedAt),
	}

	// Roster row only: the claim URL is a one-time secret for the issuing admin.
	h.broker.Broadcast(event{Type: "user:created", Data: payload})

	return c.Status(fiber.StatusCreated).JSON(createMemberResponse{
		userResponse: payload,
		ClaimURL:     claimURL(rawToken),
	})
}

// createMemberResponse is the POST /members payload. The claim URL is never
// broadcast.
type createMemberResponse struct {
	userResponse
	ClaimURL string `json:"claimUrl"`
}

// removeMemberResponse reports whether removal deleted or archived the member.
type removeMemberResponse struct {
	Outcome domain.RemoveOutcome `json:"outcome"`
}

// handleDeleteUser hard-deletes a member who authored nothing, else archives
// them to keep watch-history attribution. Both broadcast user:deleted.
func (h *handler) handleDeleteUser(c *fiber.Ctx) error {
	if ok, err := h.requireAdmin(c); !ok {
		return err
	}

	memberID, err := resolveMemberID(c)
	if err != nil {
		return writeError(c, err)
	}

	ctx := c.UserContext()
	outcome, err := h.userService.Remove(ctx, memberID)
	if err != nil {
		return writeError(c, err)
	}

	h.invalidateStatsCache()

	h.broker.Broadcast(event{Type: "user:deleted", Data: fiber.Map{"userID": memberID}})

	return c.Status(fiber.StatusOK).JSON(removeMemberResponse{Outcome: outcome})
}

// handleRestoreUser reactivates an archived member with a fresh invite, since
// archiving stripped their credentials.
func (h *handler) handleRestoreUser(c *fiber.Ctx) error {
	if ok, err := h.requireAdmin(c); !ok {
		return err
	}

	memberID, err := resolveMemberID(c)
	if err != nil {
		return writeError(c, err)
	}

	ctx := c.UserContext()
	restoredUser, rawToken, err := h.invites.RestoreMemberWithInvite(ctx, memberID, actorMemberID(c))
	if err != nil {
		return writeError(c, err)
	}

	// Built from the pre-commit read, so no fallible read after the commit can
	// lose the one-time claim URL.
	payload := userResponse{
		ID:          restoredUser.ID,
		Name:        restoredUser.Name,
		CurrentPool: map[string]leanMovieTile{},
		Stash:       map[string]leanMovieTile{},
		CreatedAt:   formatTime(restoredUser.CreatedAt),
	}

	h.invalidateStatsCache()

	// Roster row only: the claim URL is a one-time secret.
	h.broker.Broadcast(event{Type: "user:created", Data: payload})

	return c.Status(fiber.StatusOK).JSON(createMemberResponse{
		userResponse: payload,
		ClaimURL:     claimURL(rawToken),
	})
}
