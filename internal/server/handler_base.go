package server

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"moviepickarr/internal/domain"
	"moviepickarr/internal/movie"
	"moviepickarr/internal/nextpicker"
	"moviepickarr/internal/settings"
	"moviepickarr/internal/user"

	"github.com/gofiber/fiber/v2"
)

var imdbIDRegex = regexp.MustCompile(`tt\d{7,8}`)

type handler struct {
	broker            *eventBroker
	userService       user.Service
	movieService      movie.Service
	nextPickerService nextpicker.Service
	settingsService   settings.Service
	tmdb              *tmdbClient
	enrichRunner      *enrichRunner
	statsCacheMu      sync.RWMutex
	statsCache        map[string]statsCacheEntry
	statsCacheTTL     time.Duration
}

func (h *handler) Close() {
	if h == nil || h.broker == nil {
		return
	}
	h.broker.Close()
}

func sanitizeInput(input string) string {
	return strings.TrimSpace(input)
}

func sanitizeLink(link string) string {
	link = strings.TrimSpace(link)

	if strings.Contains(link, "imdb.com") {
		match := imdbIDRegex.FindString(link)
		if match != "" {
			return "https://www.imdb.com/title/" + match + "/"
		}
	}

	return link
}

func parseInt(raw string) (int, bool) {
	if raw == "" {
		return 0, false
	}
	v, err := strconv.Atoi(raw)
	if err != nil || v <= 0 {
		return 0, false
	}
	return v, true
}

func (h *handler) resolveUserID(c *fiber.Ctx) (int, error) {
	if v, ok := parseInt(c.Params("userID")); ok {
		return v, nil
	}

	return 0, fmt.Errorf("%w: userID path parameter is required", domain.ErrInvalidInput)
}

func (h *handler) resolveUserAndMovieID(c *fiber.Ctx) (int, int, error) {
	userID, userOK := parseInt(c.Params("userID"))
	movieID, movieOK := parseInt(c.Params("movieID"))
	if userOK && movieOK {
		return userID, movieID, nil
	}

	return 0, 0, fmt.Errorf("%w: userID and movieID path parameters are required", domain.ErrInvalidInput)
}
