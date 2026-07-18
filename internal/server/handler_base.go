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
	"moviepickarr/internal/nextup"
	"moviepickarr/internal/settings"
	"moviepickarr/internal/user"

	"github.com/gofiber/fiber/v2"
	"github.com/rs/zerolog"
)

var imdbIDRegex = regexp.MustCompile(`tt\d{7,8}`)

type handler struct {
	broker          *eventBroker
	log             zerolog.Logger
	userService     *user.Service
	movieService    *movie.Service
	nextUpService   *nextup.Service
	settingsService *settings.Service
	movieMetadata   domain.MovieMetadataRepo
	movieCredits    domain.MovieCreditsRepo
	tmdb            *tmdbClient
	enrichRunner    *enrichRunner
	statsCacheMu    sync.RWMutex
	statsCache      map[string]statsCacheEntry
	statsCacheTTL   time.Duration

	// Filter options (genres/actors/crew/years/adders for the Stats filter bar)
	// are derived from the watched library's metadata+credits — the same data
	// the watched list used to ship inline. A single cached snapshot, invalidated
	// on the same triggers as the stats cache.
	filterOptionsMu     sync.RWMutex
	filterOptionsCache  *filterOptionsResponse
	filterOptionsExpiry time.Time
}

func (h *handler) Close() {
	if h == nil {
		return
	}
	if h.movieService != nil {
		h.movieService.Close()
	}
	if h.broker != nil {
		h.broker.Close()
	}
}

// revealBroadcaster is the movie.DrawConfig.OnRevealed adapter: it tells every
// client to close its reel and reveal the winner in lockstep. The Service
// invokes it exactly once per draw: manual confirm and the server-owned
// auto-reveal emit an identical frame.
func revealBroadcaster(broker *eventBroker) func(movie.ActiveDraw) {
	return func(ap movie.ActiveDraw) {
		broker.Broadcast(event{Type: "movie:revealed", Data: map[string]any{
			"movieID": ap.MovieID,
			"drawnAt": formatTime(&ap.DrawnAt),
		}})
	}
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
