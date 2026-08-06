package server

import (
	"errors"
	"strconv"
	"strings"

	"moviepickarr/internal/integration"
	integrationtmdb "moviepickarr/internal/integration/tmdb"

	"github.com/gofiber/fiber/v2"
)

type tmdbRunActionRequest struct {
	Operation string `json:"operation"`
	Confirm   bool   `json:"confirm"`
}

type tmdbNoWorkResponse struct {
	NoWork bool `json:"noWork"`
}

func (h *handler) handleStartTMDBRun(c *fiber.Ctx) error {
	if ok, err := h.requireAdmin(c); !ok {
		return err
	}
	var request tmdbRunActionRequest
	if err := c.BodyParser(&request); err != nil {
		return writeProblem(c, fiber.StatusBadRequest, "invalid_request", "invalid request body")
	}
	operation := integration.RunOperation(strings.TrimSpace(request.Operation))
	switch operation {
	case integration.RunOperationRefreshStale:
	case integration.RunOperationReEnrichAll:
		if !request.Confirm {
			return writeProblem(
				c,
				fiber.StatusConflict,
				"confirmation_required",
				"Re-enriching every movie requires confirmation",
			)
		}
	default:
		return writeProblem(c, fiber.StatusBadRequest, "invalid_request", "unsupported TMDB run operation")
	}
	if h.tmdbRuns == nil {
		return writeProblem(c, fiber.StatusServiceUnavailable, "integration_unavailable", "TMDB work is unavailable")
	}
	initiatedBy := actorMemberID(c)
	result, err := h.tmdbRuns.Start(c.UserContext(), tmdbRunStart{
		Operation: operation, Trigger: integration.RunTriggerManual, InitiatedBy: &initiatedBy,
	})
	if err != nil {
		return h.writeTMDBRunError(c, err)
	}
	if result.NoWork {
		if h.integrationConfigs != nil {
			if err := h.integrationConfigs.UpdateLastChecked(c.UserContext(), "tmdb", result.CheckedAt); err != nil {
				return h.writeInternal(c, err, "updating TMDB last checked time failed")
			}
		}
		return c.Status(fiber.StatusOK).JSON(tmdbNoWorkResponse{NoWork: true})
	}
	return c.Status(fiber.StatusAccepted).JSON(toIntegrationRunResponse(*result.Run))
}

func (h *handler) handleCancelTMDBRun(c *fiber.Ctx) error {
	if ok, err := h.requireAdmin(c); !ok {
		return err
	}
	runID, err := strconv.ParseInt(c.Params("runID"), 10, 64)
	if err != nil || runID <= 0 {
		return writeProblem(c, fiber.StatusBadRequest, "invalid_request", "runID must be a positive integer")
	}
	if h.tmdbRuns == nil {
		return writeProblem(c, fiber.StatusConflict, "run_not_active", "integration run is not active")
	}
	if err := h.tmdbRuns.Cancel(runID); err != nil {
		return h.writeTMDBRunError(c, err)
	}
	return c.SendStatus(fiber.StatusAccepted)
}

func (h *handler) writeTMDBRunError(c *fiber.Ctx, err error) error {
	switch {
	case errors.Is(err, ErrTMDBLibraryRunActive):
		return writeProblem(c, fiber.StatusConflict, "run_active", "a TMDB library run is already active")
	case errors.Is(err, ErrTMDBRunNotActive):
		return writeProblem(c, fiber.StatusConflict, "run_not_active", "integration run is not active")
	case errors.Is(err, integrationtmdb.ErrRuntimeDisabled),
		errors.Is(err, integrationtmdb.ErrAPIKeyRejected),
		errors.Is(err, integration.ErrCredentialUnavailable):
		return writeProblem(c, fiber.StatusConflict, "integration_unavailable", "TMDB is not available for new work")
	default:
		return h.writeInternal(c, err, "running TMDB integration action failed")
	}
}
