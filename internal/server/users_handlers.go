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

	// Only pool/stash movies are rendered on the users board, so fetch exactly
	// those (both status-indexed) instead of loading the entire movies table —
	// including the ever-growing watched history — only to filter it back down
	// in Go. The bucketing loop below relies on each row's Status, which the two
	// status-scoped queries set correctly.
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

	// Boards render tile-level data only, so build lean tiles and skip the
	// credits batch-load (GetCreditsByMovieIDs over every board movie) — a
	// read-path saving on its own, on top of the smaller wire payload.
	meta := h.metaFor(ctx, visible)
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
	}
	if err := c.BodyParser(&body); err != nil {
		return writeError(c, fmt.Errorf("%w: invalid request body", domain.ErrInvalidInput))
	}

	name := sanitizeInput(body.Name)
	if name == "" {
		return writeError(c, fmt.Errorf("%w: name is required", domain.ErrInvalidInput))
	}

	ctx := c.UserContext()
	createdUser, err := h.userService.Create(ctx, name)
	if err != nil {
		return writeError(c, err)
	}

	// Stats list every roster member (zero rows included), so a new member
	// must show up there immediately, not after the cache TTL.
	h.invalidateStatsCache()

	payload := userResponse{
		ID:          createdUser.ID,
		Name:        createdUser.Name,
		CurrentPool: map[string]leanMovieTile{},
		Stash:       map[string]leanMovieTile{},
		CreatedAt:   formatTime(createdUser.CreatedAt),
	}

	h.broker.Broadcast(event{Type: "user:created", Data: payload})

	return c.Status(fiber.StatusCreated).JSON(payload)
}

func (h *handler) handleDeleteUser(c *fiber.Ctx) error {
	if ok, err := h.requireAdmin(c); !ok {
		return err
	}

	memberID, err := resolveMemberID(c)
	if err != nil {
		return writeError(c, err)
	}

	ctx := c.UserContext()
	if err := h.userService.Delete(ctx, memberID); err != nil {
		return writeError(c, err)
	}

	h.invalidateStatsCache()

	h.broker.Broadcast(event{Type: "user:deleted", Data: fiber.Map{"userID": memberID}})

	return c.SendStatus(fiber.StatusNoContent)
}
