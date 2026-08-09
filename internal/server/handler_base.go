package server

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"moviepickarr/internal/auth"
	"moviepickarr/internal/domain"
	"moviepickarr/internal/integration"
	integrationtmdb "moviepickarr/internal/integration/tmdb"
	"moviepickarr/internal/movie"
	"moviepickarr/internal/nextup"
	"moviepickarr/internal/settings"
	"moviepickarr/internal/user"

	"github.com/gofiber/fiber/v2"
	"github.com/rs/zerolog"
)

var imdbIDRegex = regexp.MustCompile(`(?i)tt\d{7,8}`)

type tmdbSearcher interface {
	Search(context.Context, string) ([]tmdbMovie, error)
}

type tmdbScheduleLifecycle interface {
	Start() error
	Reconfigure() error
	AuthenticationRejected(int64) error
	Close()
}

type handler struct {
	broker    *eventBroker
	log       zerolog.Logger
	sessions  *auth.SessionManager
	localAuth *auth.LocalAuth
	invites   *auth.InviteManager
	// OIDC relying-party surface. These fields are set together (or not at all):
	// oidcEnabled gates route registration and the claim-page OIDC option, so
	// when provider configuration is incomplete or discovery fails, oidc and
	// oidcTx stay nil and /oidc/* is never mounted.
	oidc            *auth.RelyingParty
	oidcTx          *auth.OIDCTxCodec
	oidcEnabled     bool
	userService     *user.Service
	movieService    *movie.Service
	nextUpService   *nextup.Service
	settingsService *settings.Service
	// drawCommandMu keeps next-up authorization attached to the lifecycle command
	// and synchronous event publication it admitted. In particular, watch owns
	// the turn through its rotation and movie:watched broadcast.
	drawCommandMu sync.Mutex
	// poolStateMu orders pool-lock changes with every admitted pool membership
	// mutation and its synchronous event. A successful lock response therefore
	// cannot be followed by a move or delete that observed the prior lock value.
	poolStateMu        sync.Mutex
	movieMetadata      domain.MovieMetadataRepo
	movieCredits       domain.MovieCreditsRepo
	tmdb               tmdbSearcher
	enrichRunner       *enrichRunner
	tmdbIntegration    *integrationtmdb.Service
	integrationConfigs integration.ConfigStore
	integrationRuns    integration.RunLedger
	runRetention       *integrationRunRetention
	tmdbRuns           *tmdbRunController
	tmdbScheduler      tmdbScheduleLifecycle
	radarr             *radarrService
	radarrAcquisitions *radarrAcquisitionWorker
	radarrWebhooks     *radarrWebhookWorker
	radarrWorkersOnce  sync.Once
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
	if h.radarrAcquisitions != nil {
		h.radarrAcquisitions.Close()
	}
	if h.radarrWebhooks != nil {
		h.radarrWebhooks.Close()
	}
	if h.movieService != nil {
		h.movieService.Close()
	}
	if h.runRetention != nil {
		h.runRetention.Close()
	}
	if h.tmdbScheduler != nil {
		h.tmdbScheduler.Close()
	}
	if h.tmdbRuns != nil {
		h.tmdbRuns.Close()
	}
	if h.broker != nil {
		h.broker.Close()
	}
}

// startRadarrWorkers starts process-owned Acquisition reconciliation and
// webhook delivery exactly once. Tests can construct a handler without
// background work; Run attaches both workers to its cancellation context.
func (h *handler) startRadarrWorkers(ctx context.Context) {
	if h == nil || h.radarr == nil {
		return
	}
	h.radarrWorkersOnce.Do(func() {
		h.radarrAcquisitions = newRadarrAcquisitionWorker(
			ctx,
			h.radarr,
			func(err error) {
				h.log.Error().Err(err).Msg("reconciling Radarr acquisitions failed")
			},
			func(processed int) {
				h.log.Debug().Int("count", processed).Msg("reconciled Radarr acquisitions")
			},
		)
		h.radarrWebhooks = newRadarrWebhookWorker(ctx, h.radarr, func(err error) {
			h.log.Error().Err(err).Msg("processing Radarr webhook deliveries failed")
		})
	})
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
