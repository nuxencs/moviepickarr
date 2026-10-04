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

// handleSkipNextUp is the admin's explicit way past a stuck turn: it passes
// Next up to the following Turn participant without a draw. The body names the
// holder the admin saw, so a stale client cannot skip a member it never showed.
// It shares the draw-command lock so it cannot interleave with a draw, Reveal,
// or watch authorization.
func (h *handler) handleSkipNextUp(c *fiber.Ctx) error {
	if ok, err := h.requireAdmin(c); !ok {
		return err
	}

	var body struct {
		MemberID int `json:"memberId"`
	}
	if err := c.BodyParser(&body); err != nil || body.MemberID <= 0 {
		return writeProblem(c, fiber.StatusBadRequest, "invalid_request", "memberId is required")
	}

	h.drawCommandMu.Lock()
	defer h.drawCommandMu.Unlock()

	next, err := h.nextUpService.Skip(c.UserContext(), body.MemberID)
	if err != nil {
		return writeError(c, err)
	}

	payload := fiber.Map{"id": next.ID, "name": next.Name}
	h.broker.Broadcast(event{Type: "settings:next-up-changed", Data: payload})
	return c.Status(fiber.StatusOK).JSON(payload)
}
