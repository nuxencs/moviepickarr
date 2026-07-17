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

// defaultAutoRevealDelay is how long after a draw the server waits before
// revealing it itself, if no client confirms. It mirrors the client reel timing —
// --dur-spin (the reel scroll) + --dur-confirm (the settled OK-button countdown)
// in web/src/index.css (6.5s + 10s) — so the server fires exactly as the drawer's
// OK-fill completes. Keep the two in sync.
const defaultAutoRevealDelay = 16500 * time.Millisecond

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

	// Server-owned auto-reveal. When a draw goes unconfirmed, the server reveals
	// it at autoRevealDelay and broadcasts movie:revealed ONCE, so every client
	// closes its reel off that single broadcast — even a backgrounded, timer-
	// throttled tab — instead of each running its own countdown (whose independent
	// timers desynced the run-out reveal). autoRevealTimer is the pending fire;
	// autoRevealDelay is configurable so tests can use a short window.
	autoRevealMu    sync.Mutex
	autoRevealTimer *time.Timer
	autoRevealDelay time.Duration

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
	h.cancelAutoReveal()
	if h.broker != nil {
		h.broker.Close()
	}
}

// scheduleAutoReveal arms (or re-arms) the server-owned auto-reveal for the active
// draw — see the autoReveal* fields on handler for why the server owns this. A
// prior pending timer is stopped first; there is only ever one active draw.
func (h *handler) scheduleAutoReveal() {
	delay := h.autoRevealDelay
	if delay <= 0 {
		delay = defaultAutoRevealDelay
	}
	h.autoRevealMu.Lock()
	defer h.autoRevealMu.Unlock()
	if h.autoRevealTimer != nil {
		h.autoRevealTimer.Stop()
	}
	h.autoRevealTimer = time.AfterFunc(delay, h.autoReveal)
}

// cancelAutoReveal stops a pending auto-reveal — a manual confirm won the race, the
// draw was watched/cleared, or the server is shutting down.
func (h *handler) cancelAutoReveal() {
	h.autoRevealMu.Lock()
	defer h.autoRevealMu.Unlock()
	if h.autoRevealTimer != nil {
		h.autoRevealTimer.Stop()
		h.autoRevealTimer = nil
	}
}

// autoReveal fires when the confirm window elapses with no client confirmation. It
// is idempotent via RevealCurrentDraw (a no-op once revealed or cleared), so it
// races harmlessly with a late manual confirm or a watch.
func (h *handler) autoReveal() {
	if ap, flipped := h.movieService.RevealCurrentDraw(); flipped {
		h.broadcastRevealed(ap)
	}
}

// broadcastRevealed tells every client to close its reel and reveal the winner in
// lockstep. Shared by the manual confirm (handleRevealCurrentMovie) and the
// server-owned auto-reveal so both emit an identical frame.
func (h *handler) broadcastRevealed(ap movie.ActiveDraw) {
	h.broker.Broadcast(event{Type: "movie:revealed", Data: map[string]any{
		"movieID": ap.MovieID,
		"drawnAt": formatTime(&ap.DrawnAt),
	}})
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
