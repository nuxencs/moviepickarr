package server

import (
	"database/sql"
	"encoding/json"
	"errors"

	"moviepickarr/internal/domain"

	"github.com/gofiber/fiber/v2"
)

type problemDetails struct {
	Type   string `json:"type"`
	Title  string `json:"title"`
	Status int    `json:"status"`
	Detail string `json:"detail,omitempty"`
}

func writeProblem(c *fiber.Ctx, status int, title, detail string) error {
	payload := problemDetails{
		Type:   "about:blank",
		Title:  title,
		Status: status,
		Detail: detail,
	}

	b, err := json.Marshal(payload)
	if err != nil {
		return c.SendStatus(fiber.StatusInternalServerError)
	}

	c.Set("Content-Type", "application/problem+json")
	return c.Status(status).Send(b)
}

func writeError(c *fiber.Ctx, err error) error {
	switch {
	case errors.Is(err, domain.ErrInvalidInput):
		return writeProblem(c, fiber.StatusBadRequest, "invalid_request", err.Error())
	case errors.Is(err, domain.ErrForbidden), errors.Is(err, domain.ErrPoolLocked):
		return writeProblem(c, fiber.StatusForbidden, "forbidden", err.Error())
	case errors.Is(err, domain.ErrNotFound), errors.Is(err, sql.ErrNoRows):
		return writeProblem(c, fiber.StatusNotFound, "not_found", err.Error())
	case errors.Is(err, domain.ErrPoolLimitReached):
		return writeProblem(c, fiber.StatusConflict, "pool_limit_reached", err.Error())
	case errors.Is(err, domain.ErrNoCurrentDraw):
		return writeProblem(c, fiber.StatusBadRequest, "no_current_draw", err.Error())
	case errors.Is(err, domain.ErrCurrentDrawExists):
		return writeProblem(c, fiber.StatusConflict, "current_draw_exists", err.Error())
	case errors.Is(err, domain.ErrInvalidState):
		return writeProblem(c, fiber.StatusBadRequest, "invalid_state", err.Error())
	case errors.Is(err, domain.ErrConflict):
		return writeProblem(c, fiber.StatusConflict, "conflict", err.Error())
	default:
		return writeProblem(c, fiber.StatusInternalServerError, "internal_error", "internal server error")
	}
}
