package server

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"time"

	"moviepickarr/internal/domain"

	"github.com/gofiber/fiber/v2"
)

func (h *handler) getPooledMovies(ctx context.Context) ([]movieResponse, error) {
	movies, err := h.movieService.Pooled(ctx)
	if err != nil {
		return nil, err
	}
	return toAPIMovies(movies), nil
}

func (h *handler) advanceNextPicker(ctx context.Context) error {
	users, err := h.userService.List(ctx)
	if err != nil {
		return err
	}
	if len(users) <= 1 {
		return nil
	}

	pooled, err := h.movieService.Pooled(ctx)
	if err != nil {
		return err
	}
	if len(pooled) == 0 {
		return nil
	}

	current, err := h.nextPickerService.Get(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			if err := h.initNextPicker(ctx); err != nil {
				return err
			}
			current, err = h.nextPickerService.Get(ctx)
			if errors.Is(err, sql.ErrNoRows) {
				return nil
			}
		}
		if err != nil {
			return err
		}
	}

	currentIndex := -1
	for i := range users {
		if current != nil && users[i].ID == current.ID {
			currentIndex = i
			break
		}
	}

	nextIndex := 0
	if currentIndex >= 0 {
		nextIndex = (currentIndex + 1) % len(users)
	}

	if err := h.nextPickerService.Set(ctx, users[nextIndex].ID); err != nil {
		return err
	}

	h.broker.Broadcast(event{
		Type: "settings:next-picker-changed",
		Data: map[string]any{
			"id":   users[nextIndex].ID,
			"name": users[nextIndex].Name,
		},
	})

	return nil
}

func (h *handler) handleAddMovie(c *fiber.Ctx) error {
	userID, err := h.resolveUserID(c)
	if err != nil {
		return writeError(c, err)
	}

	var body struct {
		Title string `json:"title"`
		Link  string `json:"link"`
	}
	if err := c.BodyParser(&body); err != nil {
		return writeError(c, fmt.Errorf("%w: invalid request body", domain.ErrInvalidInput))
	}

	title := sanitizeInput(body.Title)
	link := sanitizeLink(body.Link)
	if title == "" || link == "" {
		return writeError(c, fmt.Errorf("%w: title and link are required", domain.ErrInvalidInput))
	}

	ctx := c.UserContext()
	if _, err := h.userService.Get(ctx, userID); err != nil {
		return writeError(c, err)
	}

	movieRecord, err := h.movieService.AddToPool(ctx, title, link, userID)
	if err != nil && (errors.Is(err, domain.ErrPoolLimitReached) || errors.Is(err, domain.ErrPoolLocked)) {
		movieRecord, err = h.movieService.AddToStash(ctx, title, link, userID)
	}
	if err != nil {
		return writeError(c, err)
	}

	payload := toAPIMovie(movieRecord)
	h.broker.Broadcast(event{Type: "movie:added", Data: payload})

	return c.Status(fiber.StatusCreated).JSON(payload)
}

func (h *handler) handleGetPool(c *fiber.Ctx) error {
	userID, err := h.resolveUserID(c)
	if err != nil {
		return writeError(c, err)
	}

	ctx := c.UserContext()
	if _, err := h.userService.Get(ctx, userID); err != nil {
		return writeError(c, err)
	}

	movies, err := h.movieService.PooledByUserID(ctx, userID)
	if err != nil {
		return writeError(c, err)
	}

	return c.Status(fiber.StatusOK).JSON(toAPIMovies(movies))
}

func (h *handler) handleEditMovie(c *fiber.Ctx) error {
	userID, movieID, err := h.resolveUserAndMovieID(c)
	if err != nil {
		return writeError(c, err)
	}

	var body struct {
		Title     string  `json:"title"`
		Link      string  `json:"link"`
		WatchedAt *string `json:"watchedAt"`
	}
	if err := c.BodyParser(&body); err != nil {
		return writeError(c, fmt.Errorf("%w: invalid request body", domain.ErrInvalidInput))
	}

	title := sanitizeInput(body.Title)
	link := sanitizeLink(body.Link)
	if title == "" || link == "" {
		return writeError(c, fmt.Errorf("%w: title and link are required", domain.ErrInvalidInput))
	}

	var watchedAt *time.Time
	if body.WatchedAt != nil {
		raw := sanitizeInput(*body.WatchedAt)
		if raw == "" {
			return writeError(c, fmt.Errorf("%w: watchedAt must be a valid RFC3339 timestamp", domain.ErrInvalidInput))
		}

		parsed, err := time.Parse(timeFormat, raw)
		if err != nil {
			return writeError(c, fmt.Errorf("%w: watchedAt must be a valid RFC3339 timestamp", domain.ErrInvalidInput))
		}

		parsedUTC := parsed.UTC()
		watchedAt = &parsedUTC
	}

	ctx := c.UserContext()
	movieRecord, err := h.movieService.Get(ctx, movieID)
	if err != nil {
		return writeError(c, err)
	}
	if movieRecord.AddedByID != userID {
		return writeError(c, domain.ErrNotFound)
	}

	updatedMovie, err := h.movieService.Update(ctx, movieID, title, link, watchedAt)
	if err != nil {
		return writeError(c, err)
	}

	if watchedAt != nil {
		h.invalidateStatsCache()
	}

	payload := toAPIMovie(updatedMovie)
	h.broker.Broadcast(event{Type: "movie:updated", Data: payload})

	return c.Status(fiber.StatusOK).JSON(payload)
}

func (h *handler) handleDeleteMovie(c *fiber.Ctx) error {
	userID, movieID, err := h.resolveUserAndMovieID(c)
	if err != nil {
		return writeError(c, err)
	}

	ctx := c.UserContext()
	movieRecord, err := h.movieService.Get(ctx, movieID)
	if err != nil {
		return writeError(c, err)
	}

	if movieRecord.AddedByID != userID {
		return writeError(c, domain.ErrNotFound)
	}
	if movieRecord.Status != "pool" && movieRecord.Status != "stash" {
		return writeError(c, domain.ErrInvalidState)
	}

	if err := h.movieService.Delete(ctx, movieID); err != nil {
		return writeError(c, err)
	}

	h.broker.Broadcast(event{
		Type: "movie:deleted",
		Data: fiber.Map{"userID": userID, "movieID": movieID},
	})

	return c.SendStatus(fiber.StatusNoContent)
}

func (h *handler) handleGetStash(c *fiber.Ctx) error {
	userID, err := h.resolveUserID(c)
	if err != nil {
		return writeError(c, err)
	}

	ctx := c.UserContext()
	if _, err := h.userService.Get(ctx, userID); err != nil {
		return writeError(c, err)
	}

	movies, err := h.movieService.StashedByUserID(ctx, userID)
	if err != nil {
		return writeError(c, err)
	}

	return c.Status(fiber.StatusOK).JSON(toAPIMovies(movies))
}

func (h *handler) handleMove(c *fiber.Ctx) error {
	userID, movieID, err := h.resolveUserAndMovieID(c)
	if err != nil {
		return writeError(c, err)
	}

	ctx := c.UserContext()

	poolLocked, err := h.settingsService.GetPoolLock(ctx)
	if err != nil {
		return writeError(c, err)
	}
	if poolLocked {
		return writeError(c, domain.ErrPoolLocked)
	}

	userRecord, err := h.userService.Get(ctx, userID)
	if err != nil {
		return writeError(c, err)
	}

	movieRecord, err := h.movieService.Get(ctx, movieID)
	if err != nil {
		return writeError(c, err)
	}

	if movieRecord.AddedByID != userID {
		return writeError(c, domain.ErrNotFound)
	}

	switch movieRecord.Status {
	case "stash":
		pooled, err := h.movieService.PooledByUserID(ctx, userID)
		if err != nil {
			return writeError(c, err)
		}
		if len(pooled) >= maxPoolSize {
			return writeError(c, domain.ErrPoolLimitReached)
		}
		if err := h.movieService.MoveToPool(ctx, movieID); err != nil {
			return writeError(c, err)
		}
	case "pool":
		if err := h.movieService.MoveToStash(ctx, movieID); err != nil {
			return writeError(c, err)
		}
	default:
		return writeError(c, domain.ErrInvalidState)
	}

	updatedPool, err := h.movieService.PooledByUserID(ctx, userID)
	if err != nil {
		return writeError(c, err)
	}
	updatedStash, err := h.movieService.StashedByUserID(ctx, userID)
	if err != nil {
		return writeError(c, err)
	}

	updatedUser := toAPIUser(userRecord, updatedPool, updatedStash)
	h.broker.Broadcast(event{Type: "movie:moved", Data: updatedUser})

	return c.Status(fiber.StatusOK).JSON(updatedUser)
}

func (h *handler) handleGetPooledMovies(c *fiber.Ctx) error {
	movies, err := h.getPooledMovies(c.UserContext())
	if err != nil {
		return writeError(c, err)
	}

	return c.Status(fiber.StatusOK).JSON(movies)
}

func (h *handler) handleGetRandomMovie(c *fiber.Ctx) error {
	ctx := c.UserContext()

	selectedMovie, err := h.movieService.PickRandom(ctx)
	if err != nil {
		return writeError(c, err)
	}

	if err := h.advanceNextPicker(ctx); err != nil {
		log.Printf("failed to advance next picker: %v", err)
	}

	payload := toAPIMovie(selectedMovie)
	h.broker.Broadcast(event{Type: "movie:picked", Data: payload})

	return c.Status(fiber.StatusOK).JSON(payload)
}

func (h *handler) handleGetCurrentMovie(c *fiber.Ctx) error {
	movieRecord, err := h.movieService.Current(c.UserContext())
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return c.Status(fiber.StatusOK).JSON(nil)
		}
		return writeError(c, err)
	}

	return c.Status(fiber.StatusOK).JSON(toAPIMovie(movieRecord))
}

func (h *handler) handleWatchMovie(c *fiber.Ctx) error {
	watched, err := h.movieService.MarkCurrentAsWatched(c.UserContext())
	if err != nil {
		return writeError(c, err)
	}

	h.invalidateStatsCache()

	payload := toAPIMovie(watched)
	h.broker.Broadcast(event{Type: "movie:watched", Data: payload})

	return c.Status(fiber.StatusOK).JSON(payload)
}

func (h *handler) handleGetWatchedMovies(c *fiber.Ctx) error {
	movies, err := h.movieService.Watched(c.UserContext())
	if err != nil {
		return writeError(c, err)
	}

	return c.Status(fiber.StatusOK).JSON(toAPIMovies(movies))
}
