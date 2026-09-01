package server

import (
	"database/sql"
	"errors"
	"fmt"

	"moviepickarr/internal/domain"

	"github.com/gofiber/fiber/v2"
)

func (h *handler) handleSetPoolLock(c *fiber.Ctx) error {
	if ok, err := h.requireAdmin(c); !ok {
		return err
	}

	var body struct {
		PoolLocked *bool `json:"poolLocked"`
	}
	if err := c.BodyParser(&body); err != nil || body.PoolLocked == nil {
		return writeError(c, fmt.Errorf("%w: poolLocked is required", domain.ErrInvalidInput))
	}

	ctx := c.UserContext()
	var payload settingsResponse
	err := h.runPoolStateCommand(func() error {
		if err := h.settingsService.SetPoolLock(ctx, *body.PoolLocked); err != nil {
			return err
		}

		payload = settingsResponse{
			PoolLocked:     *body.PoolLocked,
			DrawInProgress: h.movieService.DrawInProgress(),
		}
		h.broker.Broadcast(event{Type: "settings:pool-lock-changed", Data: payload})
		return nil
	})
	if err != nil {
		return writeError(c, err)
	}

	return c.Status(fiber.StatusOK).JSON(payload)
}

func (h *handler) handleGetPoolLock(c *fiber.Ctx) error {
	poolLocked, err := h.settingsService.GetPoolLock(c.UserContext())
	if err != nil {
		return writeError(c, err)
	}

	return c.Status(fiber.StatusOK).JSON(settingsResponse{
		PoolLocked:     poolLocked,
		DrawInProgress: h.movieService.DrawInProgress(),
	})
}

func (h *handler) handleGetNextUp(c *fiber.Ctx) error {
	ctx := c.UserContext()

	// Get self-seeds a fresh install. No rows means no active Turn participant
	// exists, which includes a Guest-only roster.
	nextUp, err := h.nextUpService.Get(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return c.Status(fiber.StatusOK).JSON(fiber.Map{"id": 0, "name": ""})
		}
		return writeError(c, err)
	}

	return c.Status(fiber.StatusOK).JSON(fiber.Map{
		"id":   nextUp.ID,
		"name": nextUp.Name,
	})
}
