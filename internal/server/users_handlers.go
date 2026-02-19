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

	poolByUser := make(map[int]map[string]movieResponse)
	stashByUser := make(map[int]map[string]movieResponse)

	for _, m := range movies {
		if m.Status != "pool" && m.Status != "stash" {
			continue
		}

		apiMovie := toAPIMovie(m)
		key := strconv.Itoa(m.ID)

		if m.Status == "pool" {
			if poolByUser[m.AddedByID] == nil {
				poolByUser[m.AddedByID] = map[string]movieResponse{}
			}
			poolByUser[m.AddedByID][key] = apiMovie
			continue
		}

		if stashByUser[m.AddedByID] == nil {
			stashByUser[m.AddedByID] = map[string]movieResponse{}
		}
		stashByUser[m.AddedByID][key] = apiMovie
	}

	response := make([]userResponse, 0, len(users))
	for _, u := range users {
		currentPool := poolByUser[u.ID]
		if currentPool == nil {
			currentPool = map[string]movieResponse{}
		}
		stash := stashByUser[u.ID]
		if stash == nil {
			stash = map[string]movieResponse{}
		}

		response = append(response, userResponse{
			ID:          u.ID,
			Name:        u.Name,
			Username:    u.Username,
			Role:        string(u.Role),
			HasAccount:  u.HasAccount,
			CurrentPool: currentPool,
			Stash:       stash,
			CreatedAt:   formatTime(u.CreatedAt),
		})
	}

	return c.Status(fiber.StatusOK).JSON(response)
}

func (h *handler) handleCreateUser(c *fiber.Ctx) error {
	if !h.isCurrentUserAdmin(c) {
		return writeError(c, domain.ErrForbidden)
	}

	var body struct {
		Name     string `json:"name"`
		Username string `json:"username"`
		Password string `json:"password"`
		Role     string `json:"role"`
	}
	if err := c.BodyParser(&body); err != nil {
		return writeError(c, fmt.Errorf("%w: invalid request body", domain.ErrInvalidInput))
	}

	name := sanitizeInput(body.Name)
	if name == "" {
		return writeError(c, fmt.Errorf("%w: name is required", domain.ErrInvalidInput))
	}

	username := sanitizeInput(body.Username)
	if username == "" {
		return writeError(c, fmt.Errorf("%w: username is required", domain.ErrInvalidInput))
	}
	password := body.Password
	if sanitizeInput(password) == "" {
		return writeError(c, fmt.Errorf("%w: password is required", domain.ErrInvalidInput))
	}

	role := domain.RoleMember
	if sanitizeInput(body.Role) == string(domain.RoleAdmin) {
		role = domain.RoleAdmin
	}

	ctx := c.UserContext()
	createdUser, err := h.userService.Create(ctx, name)
	if err != nil {
		return writeError(c, err)
	}
	if _, err := h.authService.UpsertAccount(ctx, createdUser.ID, username, password, role); err != nil {
		_ = h.userService.Delete(ctx, createdUser.ID)
		return writeError(c, err)
	}
	createdUser, err = h.userService.Get(ctx, createdUser.ID)
	if err != nil {
		return writeError(c, err)
	}

	payload := userResponse{
		ID:          createdUser.ID,
		Name:        createdUser.Name,
		Username:    createdUser.Username,
		Role:        string(createdUser.Role),
		HasAccount:  createdUser.HasAccount,
		CurrentPool: map[string]movieResponse{},
		Stash:       map[string]movieResponse{},
		CreatedAt:   formatTime(createdUser.CreatedAt),
	}

	h.broker.Broadcast(event{Type: "user:created", Data: payload})

	return c.Status(fiber.StatusCreated).JSON(payload)
}

func (h *handler) handleDeleteUser(c *fiber.Ctx) error {
	if !h.isCurrentUserAdmin(c) {
		return writeError(c, domain.ErrForbidden)
	}

	userID, err := h.resolveUserID(c)
	if err != nil {
		return writeError(c, err)
	}

	ctx := c.UserContext()
	if err := h.authService.CanDeleteAccount(ctx, userID); err != nil {
		return writeError(c, err)
	}
	if err := h.userService.Delete(ctx, userID); err != nil {
		return writeError(c, err)
	}

	h.invalidateStatsCache()

	h.broker.Broadcast(event{Type: "user:deleted", Data: fiber.Map{"userID": userID}})

	return c.SendStatus(fiber.StatusNoContent)
}

func (h *handler) handleUpsertUserAccount(c *fiber.Ctx) error {
	if !h.isCurrentUserAdmin(c) {
		return writeError(c, domain.ErrForbidden)
	}

	userID, err := h.resolveUserID(c)
	if err != nil {
		return writeError(c, err)
	}

	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
		Role     string `json:"role"`
	}
	if err := c.BodyParser(&body); err != nil {
		return writeError(c, fmt.Errorf("%w: invalid request body", domain.ErrInvalidInput))
	}

	username := sanitizeInput(body.Username)
	password := body.Password
	if username == "" || sanitizeInput(password) == "" {
		return writeError(c, fmt.Errorf("%w: username and password are required", domain.ErrInvalidInput))
	}

	role := domain.RoleMember
	if sanitizeInput(body.Role) == string(domain.RoleAdmin) {
		role = domain.RoleAdmin
	}

	ctx := c.UserContext()
	if _, err := h.userService.Get(ctx, userID); err != nil {
		return writeError(c, err)
	}
	if _, err := h.authService.UpsertAccount(ctx, userID, username, password, role); err != nil {
		return writeError(c, err)
	}

	updatedUser, err := h.userService.Get(ctx, userID)
	if err != nil {
		return writeError(c, err)
	}

	return c.Status(fiber.StatusOK).JSON(userResponse{
		ID:          updatedUser.ID,
		Name:        updatedUser.Name,
		Username:    updatedUser.Username,
		Role:        string(updatedUser.Role),
		HasAccount:  updatedUser.HasAccount,
		CurrentPool: map[string]movieResponse{},
		Stash:       map[string]movieResponse{},
		CreatedAt:   formatTime(updatedUser.CreatedAt),
	})
}
