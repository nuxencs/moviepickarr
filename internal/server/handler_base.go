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
	// The OIDC fields are set together or not at all; without them /oidc/* is
	// never mounted.
	oidc            *auth.RelyingParty
	oidcTx          *auth.OIDCTxCodec
	oidcEnabled     bool
	userService     *user.Service
	movieService    *movie.Service
	nextUpService   *nextup.Service
	settingsService *settings.Service
	// drawCommandMu holds next-up authorization through the command and its
	// events (see runDrawCommand).
	drawCommandMu sync.Mutex
	// poolStateMu orders pool-lock changes with pool mutations, so no move or
	// delete can act on a stale lock value after a lock succeeds.
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
	// posterWall is nil without a TMDB key; the endpoint then serves [].
	posterWall    *posterWallCache
	statsCacheMu  sync.RWMutex
	statsCache    map[string]statsCacheEntry
	statsCacheTTL time.Duration

	// sseHeartbeatInterval is a field so tests can shorten it.
	sseHeartbeatInterval time.Duration

	// Cached filter options, invalidated with the stats cache.
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

// startRadarrWorkers starts Acquisition reconciliation and webhook delivery
// once. Run calls it, so tests can build a handler without background work.
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

// revealBroadcaster is the movie.DrawConfig.OnRevealed adapter. The Service
// calls it once per draw, whatever path revealed it.
func revealBroadcaster(broker *eventBroker) func(movie.Reveal) {
	return func(r movie.Reveal) {
		broker.Broadcast(event{Type: "movie:revealed", Data: map[string]any{
			"movieID": r.MovieID,
			"drawnAt": formatTime(&r.DrawnAt),
		}})
		if r.NextUp != nil {
			broker.Broadcast(event{Type: "settings:next-up-changed", Data: map[string]any{
				"id":   r.NextUp.ID,
				"name": r.NextUp.Name,
			}})
		}
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

// actorMemberID is the one source of the acting member. Never take the actor
// from a path parameter, or a URL edit could act as someone else.
func actorMemberID(c *fiber.Ctx) int {
	id, _ := c.Locals(localsMemberID).(int)
	return id
}

func resolveMovieID(c *fiber.Ctx) (int, error) {
	if v, ok := parseInt(c.Params("movieID")); ok {
		return v, nil
	}

	return 0, fmt.Errorf("%w: movieID path parameter is required", domain.ErrInvalidInput)
}
