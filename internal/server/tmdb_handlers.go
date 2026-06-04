package server

import (
	"fmt"
	"strings"

	"moviepickarr/internal/domain"

	"github.com/gofiber/fiber/v2"
)

func (h *handler) handleTMDBSearch(c *fiber.Ctx) error {
	query := c.Query("query")
	if strings.TrimSpace(query) == "" {
		return writeError(c, fmt.Errorf("%w: query parameter is required", domain.ErrInvalidInput))
	}

	movies, err := h.tmdb.Search(c.UserContext(), query)
	if err != nil {
		return writeError(c, err)
	}

	return c.Status(fiber.StatusOK).JSON(movies)
}
