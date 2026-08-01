package server

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"moviepickarr/internal/auth"
	"moviepickarr/internal/domain"
	"moviepickarr/internal/movie"
	"moviepickarr/internal/nextup"
	"moviepickarr/internal/settings"
	"moviepickarr/internal/user"

	"github.com/gofiber/fiber/v2"
	"github.com/rs/zerolog"
)

var imdbIDRegex = regexp.MustCompile(`(?i)tt\d{7,8}`)

type handler struct {
	broker    *eventBroker
	log       zerolog.Logger
	sessions  *auth.SessionManager
	localAuth *auth.LocalAuth
	invites   *auth.InviteManager
	// OIDC relying-party surface. All four are set together (or none): oidcEnabled
	// gates route registration and the claim-page OIDC option, so when the
	// provider quartet is unset or discovery fails, oidc/oidcTx/linker stay nil and
	// /oidc/* is never mounted.
	oidc            *auth.RelyingParty
	oidcTx          *auth.OIDCTxCodec
	linker          *auth.IdentityLinker
	oidcEnabled     bool
	userService     *user.Service
	movieService    *movie.Service
	nextUpService   *nextup.Service
	settingsService *settings.Service
	// movieNightMu keeps next-up authorization attached to the lifecycle command
	// and synchronous event publication it admitted. In particular, watch owns
	// the turn through its rotation and movie:watched broadcast.
	movieNightMu sync.Mutex
	// poolStateMu orders pool-lock changes with every admitted pool membership
	// mutation and its synchronous event. A successful lock response therefore
	// cannot be followed by a move or delete that observed the prior lock value.
	poolStateMu   sync.Mutex
	movieMetadata domain.MovieMetadataRepo
	movieCredits  domain.MovieCreditsRepo
	tmdb          *tmdbClient
	enrichRunner  *enrichRunner
	// posterWall backs the public GET /auth/poster-wall endpoint. Like
	// enrichRunner it is nil when no TMDB key is set, and the handler then serves
	// an empty array.
	posterWall    *posterWallCache
	statsCacheMu  sync.RWMutex
	statsCache    map[string]statsCacheEntry
	statsCacheTTL time.Duration

	// sseHeartbeatInterval is how often an open SSE stream writes a heartbeat and
	// revalidates the session. A field (not the const directly) so tests can drive
	// the revalidation without waiting the full production interval.
	sseHeartbeatInterval time.Duration

	// Filter options (genres/actors/crew/years/adders for the Stats filter bar)
	// are derived from the watched library's metadata+credits — the same data
	// the watched list used to ship inline. A single cached snapshot, invalidated
	// on the same triggers as the stats cache.
	filterOptionsMu     sync.RWMutex
	filterOptionsCache  *filterOptionsResponse
	filterOptionsExpiry time.Time
}

func (h *handler) runPoolStateCommand(command func() error) error {
	h.poolStateMu.Lock()
	defer h.poolStateMu.Unlock()
	return command()
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
// invokes it exactly once per draw: manual confirm, server-owned auto-reveal,
// and an early watch emit an identical frame.
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

	if strings.Contains(strings.ToLower(link), "imdb.com") {
		if imdbID := extractIMDbID(link); imdbID != "" {
			return "https://www.imdb.com/title/" + imdbID + "/"
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

// actorMemberID returns the session member id requireSession attached. It is the
// single source of "who is acting": every mutation derives the actor from here,
// never from a path parameter, so no one can act as someone else by editing a URL.
func actorMemberID(c *fiber.Ctx) int {
	id, _ := c.Locals(localsMemberID).(int)
	return id
}

// resolveMovieID reads the :movieID path parameter as a positive int.
func resolveMovieID(c *fiber.Ctx) (int, error) {
	if v, ok := parseInt(c.Params("movieID")); ok {
		return v, nil
	}

	return 0, fmt.Errorf("%w: movieID path parameter is required", domain.ErrInvalidInput)
}
