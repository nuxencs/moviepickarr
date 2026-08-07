package server

import (
	"errors"
	"fmt"
	"strings"

	"moviepickarr/internal/domain"
	"moviepickarr/internal/integration"
	integrationtmdb "moviepickarr/internal/integration/tmdb"

	"github.com/gofiber/fiber/v2"
)

func (h *handler) handleTMDBSearch(c *fiber.Ctx) error {
	query := c.Query("query")
	if strings.TrimSpace(query) == "" {
		return writeError(c, fmt.Errorf("%w: query parameter is required", domain.ErrInvalidInput))
	}

	movies, err := h.tmdb.Search(c.UserContext(), query)
	if err != nil {
		var queueFull *tmdbRequestQueueFullError
		if errors.As(err, &queueFull) ||
			errors.Is(err, errTMDBNotConfigured) ||
			errors.Is(err, integration.ErrCredentialUnavailable) ||
			errors.Is(err, integrationtmdb.ErrRuntimeDisabled) ||
			errors.Is(err, integrationtmdb.ErrAPIKeyRejected) {
			return writeProblem(
				c,
				fiber.StatusServiceUnavailable,
				"temporarily_unavailable",
				"Movie search is temporarily unavailable",
			)
		}
		return writeError(c, err)
	}

	return c.Status(fiber.StatusOK).JSON(movies)
}
