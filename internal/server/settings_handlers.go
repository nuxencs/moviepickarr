package server

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"moviepickarr/internal/domain"

	"github.com/gofiber/fiber/v2"
)

func (h *handler) initNextPicker(ctx context.Context) error {
	users, err := h.userService.List(ctx)
	if err != nil {
		return err
	}
	if len(users) == 0 {
		return nil
	}

	return h.nextPickerService.Set(ctx, users[0].ID)
}

func (h *handler) handleSetPoolLock(c *fiber.Ctx) error {
	if !h.isCurrentUserAdmin(c) {
		return writeError(c, domain.ErrForbidden)
	}

	var body struct {
		PoolLocked *bool `json:"poolLocked"`
	}
	if err := c.BodyParser(&body); err != nil || body.PoolLocked == nil {
		return writeError(c, fmt.Errorf("%w: poolLocked is required", domain.ErrInvalidInput))
	}

	ctx := c.UserContext()
	if err := h.settingsService.SetPoolLock(ctx, *body.PoolLocked); err != nil {
		return writeError(c, err)
	}

	payload := settingsResponse{PoolLocked: *body.PoolLocked}
	h.broker.Broadcast(event{Type: "settings:pool-lock-changed", Data: payload})

	return c.Status(fiber.StatusOK).JSON(payload)
}

func (h *handler) handleGetPoolLock(c *fiber.Ctx) error {
	poolLocked, err := h.settingsService.GetPoolLock(c.UserContext())
	if err != nil {
		return writeError(c, err)
	}

	return c.Status(fiber.StatusOK).JSON(poolLocked)
}

func (h *handler) handleGetNextPicker(c *fiber.Ctx) error {
	ctx := c.UserContext()

	nextPicker, err := h.nextPickerService.Get(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			if err := h.initNextPicker(ctx); err != nil {
				return writeError(c, err)
			}
			nextPicker, err = h.nextPickerService.Get(ctx)
			if errors.Is(err, sql.ErrNoRows) {
				return c.Status(fiber.StatusOK).JSON(fiber.Map{"id": 0, "name": ""})
			}
		}
		if err != nil {
			return writeError(c, err)
		}
	}
	if nextPicker == nil {
		return c.Status(fiber.StatusOK).JSON(fiber.Map{"id": 0, "name": ""})
	}

	return c.Status(fiber.StatusOK).JSON(fiber.Map{
		"id":   nextPicker.ID,
		"name": nextPicker.Name,
	})
}
