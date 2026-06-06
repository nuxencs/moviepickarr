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

	movies, err := h.movieService.List(ctx)
	if err != nil {
		return writeError(c, err)
	}

	// Only pool/stash movies are rendered on the users board. Scope metadata
	// loading to just those so we don't batch-load (and discard) enriched data
	// for the ever-growing watched history.
	visible := make([]*domain.Movie, 0, len(movies))
	for i := range movies {
		if movies[i].Status == "pool" || movies[i].Status == "stash" {
			visible = append(visible, movies[i])
		}
	}

	meta := h.metaFor(ctx, visible)
	poolByUser := make(map[int]map[string]movieResponse)
	stashByUser := make(map[int]map[string]movieResponse)

	for i := range visible {
		apiMovie := toAPIMovieMeta(visible[i], meta[visible[i].ID])
		key := strconv.Itoa(visible[i].ID)

		if visible[i].Status == "pool" {
			if poolByUser[visible[i].AddedByID] == nil {
				poolByUser[visible[i].AddedByID] = map[string]movieResponse{}
			}
			poolByUser[visible[i].AddedByID][key] = apiMovie
			continue
		}

		if stashByUser[visible[i].AddedByID] == nil {
			stashByUser[visible[i].AddedByID] = map[string]movieResponse{}
		}
		stashByUser[visible[i].AddedByID][key] = apiMovie
	}

	response := make([]userResponse, 0, len(users))
	for i := range users {
		currentPool := poolByUser[users[i].ID]
		if currentPool == nil {
			currentPool = map[string]movieResponse{}
		}
		stash := stashByUser[users[i].ID]
		if stash == nil {
			stash = map[string]movieResponse{}
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

	payload := userResponse{
		ID:          createdUser.ID,
		Name:        createdUser.Name,
		CurrentPool: map[string]movieResponse{},
		Stash:       map[string]movieResponse{},
		CreatedAt:   formatTime(createdUser.CreatedAt),
	}

	h.broker.Broadcast(event{Type: "user:created", Data: payload})

	return c.Status(fiber.StatusCreated).JSON(payload)
}

func (h *handler) handleDeleteUser(c *fiber.Ctx) error {
	userID, err := h.resolveUserID(c)
	if err != nil {
		return writeError(c, err)
	}

	ctx := c.UserContext()
	if err := h.userService.Delete(ctx, userID); err != nil {
		return writeError(c, err)
	}

	h.invalidateStatsCache()

	h.broker.Broadcast(event{Type: "user:deleted", Data: fiber.Map{"userID": userID}})

	return c.SendStatus(fiber.StatusNoContent)
}
