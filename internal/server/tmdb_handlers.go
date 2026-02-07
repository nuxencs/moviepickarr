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

func (h *handler) handleTMDBExternalIDs(c *fiber.Ctx) error {
	movieID := c.Query("movieId")
	if strings.TrimSpace(movieID) == "" {
		return writeError(c, fmt.Errorf("%w: movieId parameter is required", domain.ErrInvalidInput))
	}

	link, err := h.tmdb.ExternalLink(c.UserContext(), movieID)
	if err != nil {
		return writeError(c, err)
	}

	return c.Status(fiber.StatusOK).JSON(fiber.Map{"link": link})
}
