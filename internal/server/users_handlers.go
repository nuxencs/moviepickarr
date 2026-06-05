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

	meta := h.metaFor(ctx, movies)
	poolByUser := make(map[int]map[string]movieResponse)
	stashByUser := make(map[int]map[string]movieResponse)

	for i := range movies {
		if movies[i].Status != "pool" && movies[i].Status != "stash" {
			continue
		}

		apiMovie := toAPIMovieMeta(movies[i], meta[movies[i].ID])
		key := strconv.Itoa(movies[i].ID)

		if movies[i].Status == "pool" {
			if poolByUser[movies[i].AddedByID] == nil {
				poolByUser[movies[i].AddedByID] = map[string]movieResponse{}
			}
			poolByUser[movies[i].AddedByID][key] = apiMovie
			continue
		}

		if stashByUser[movies[i].AddedByID] == nil {
			stashByUser[movies[i].AddedByID] = map[string]movieResponse{}
		}
		stashByUser[movies[i].AddedByID][key] = apiMovie
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
