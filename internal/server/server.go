package server

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"moviepickarr/internal/auth"
	"moviepickarr/internal/db"
	"moviepickarr/internal/integration"
	integrationtmdb "moviepickarr/internal/integration/tmdb"
	"moviepickarr/internal/logger"
	"moviepickarr/internal/movie"
	"moviepickarr/internal/nextup"
	"moviepickarr/internal/repository"
	"moviepickarr/internal/seed"
	"moviepickarr/internal/settings"
	"moviepickarr/internal/user"

	"github.com/gofiber/contrib/fiberzerolog"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/compress"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/filesystem"
	"github.com/gofiber/fiber/v2/middleware/recover"
	"github.com/gofiber/fiber/v2/middleware/requestid"
	"github.com/joho/godotenv"
	"github.com/rs/zerolog"
	zlog "github.com/rs/zerolog/log"
)

type Config struct {
	Port    string
	DBFile  string
	WebRoot http.FileSystem

	// Build metadata, surfaced in the startup banner.
	Version string
	Commit  string
	Date    string
}

// shutdownTimeout bounds how long Fiber gets to drain in-flight requests.
const shutdownTimeout = 10 * time.Second

func logHTTPShutdownError(log zerolog.Logger, err error) {
	event := log.Error().Err(err)
	if errors.Is(err, context.DeadlineExceeded) {
		event.Dur("timeout", shutdownTimeout).
			Msg("http server did not drain before the shutdown timeout")
		return
	}
	event.Msg("shutting down the http server failed")
}

func logTMDBEnvironmentIssues(rootLog zerolog.Logger, issues []integrationtmdb.EnvironmentIssue) {
	log := rootLog.With().
		Str("component", "integration").
		Str("integration", "tmdb").
		Logger()
	for _, issue := range issues {
		log.Warn().
			Str("environment_key", issue.Field).
			Str("reason", issue.Message).
			Msg("invalid integration environment value; using lower-precedence setting")
	}
}

// dbMaxBackups resolves DB_BACKUP_MAX, the number of pre-migration snapshots to
// keep. 0 disables backups; invalid values fall back to the default.
func dbMaxBackups(log zerolog.Logger) int {
	const defaultMaxBackups = 3
	raw := os.Getenv("DB_BACKUP_MAX")
	if raw == "" {
		return defaultMaxBackups
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 0 {
		log.Warn().Str("key", "DB_BACKUP_MAX").Str("value", raw).Int("default", defaultMaxBackups).
			Msg("env value is not a non-negative integer, using default")
		return defaultMaxBackups
	}
	return n
}

// ResolveDBFile picks the SQLite path: explicit value, then DB_FILE, then
// "moviepickarr.db". The server and the dev-fixtures command share it so they
// open the same file.
func ResolveDBFile(explicit string) string {
	if explicit != "" {
		return explicit
	}
	if v := os.Getenv("DB_FILE"); v != "" {
		return v
	}
	return "moviepickarr.db"
}

func Run(ctx context.Context, cfg Config) error {
	_ = godotenv.Load()

	if cfg.Port == "" {
		cfg.Port = ":3030"
	}
	// After godotenv.Load so DB_FILE also works from .env.
	cfg.DBFile = ResolveDBFile(cfg.DBFile)
	if cfg.WebRoot == nil {
		return fmt.Errorf("web root is required")
	}

	// Mirror to the zerolog global so package-level call sites (enrich_worker
	// env parsers) use the same writer.
	rootLog := logger.New(logger.FromEnv())
	zlog.Logger = rootLog
	rootLog.Info().
		Str("version", cfg.Version).
		Str("commit", cfg.Commit).
		Str("built", cfg.Date).
		Msg("moviepickarr starting")

	if _, err := db.MigrateBoltToSQLite(ctx, cfg.DBFile, cfg.DBFile); err != nil {
		return err
	}

	pool, err := db.OpenSQLite(cfg.DBFile)
	if err != nil {
		return err
	}

	if err := db.RunMigrationsWithBackup(ctx, pool.Write, db.BackupConfig{
		Path:       cfg.DBFile,
		MaxBackups: dbMaxBackups(rootLog),
	}); err != nil {
		_ = pool.Close()
		return err
	}

	// Seed before serving; a misconfigured seed fails boot instead of leaving a
	// deploy with no login.
	adminCfg, adminConfigured := seed.AdminConfigFromEnv(rootLog)
	if err := seed.BreakGlassAdmin(ctx, repository.NewSqliteAdminSeedRepository(pool), adminCfg, adminConfigured, rootLog); err != nil {
		_ = pool.Close()
		return err
	}

	h, err := newHandlerChecked(pool, rootLog)
	if err != nil {
		_ = pool.Close()
		return err
	}
	h.startRadarrWorkers(ctx)
	h.startSessionSweeper(ctx)
	if h.enrichRunner != nil {
		h.enrichRunner.Start(ctx)
	}
	if h.tmdbScheduler != nil {
		if err := h.tmdbScheduler.Start(); err != nil {
			rootLog.Error().Err(err).Msg("starting TMDB refresh scheduler failed")
		}
	}
	if h.tmdbRuns != nil {
		result, err := h.tmdbRuns.Start(ctx, tmdbRunStart{
			Operation: integration.RunOperationRefreshStale,
			Trigger:   integration.RunTriggerStartup,
		})
		if err == nil && result.NoWork && h.integrationConfigs != nil {
			if updateErr := h.integrationConfigs.UpdateLastChecked(ctx, "tmdb", result.CheckedAt); updateErr != nil {
				rootLog.Error().Err(updateErr).Msg("updating TMDB startup check time failed")
			}
		} else if err != nil &&
			!errors.Is(err, integrationtmdb.ErrRuntimeDisabled) &&
			!errors.Is(err, integrationtmdb.ErrAPIKeyRejected) &&
			!errors.Is(err, integration.ErrCredentialUnavailable) {
			rootLog.Error().Err(err).Msg("starting TMDB startup refresh failed")
		}
	}
	// Async so boot never waits on TMDB; with no key the wall serves [].
	h.posterWall.Start(ctx)

	app := fiber.New(fiber.Config{
		DisableStartupMessage: true,
		JSONEncoder: func(value any) ([]byte, error) {
			return json.Marshal(value)
		},
		JSONDecoder: func(data []byte, value any) error {
			return json.Unmarshal(data, value)
		},
		// No WriteTimeout: it would cut the long-lived /api/v1/events SSE stream.
		ReadTimeout: 15 * time.Second,
		IdleTimeout: 120 * time.Second,
	})

	// Order matters: requestid before fiberzerolog (which reads the ID), and
	// fiberzerolog before recover so a recovered panic still gets an access line.
	app.Use(requestid.New())
	app.Use(fiberzerolog.New(fiberzerolog.Config{
		Logger: &h.log,
		Fields: []string{
			fiberzerolog.FieldRequestID,
			fiberzerolog.FieldIP,
			fiberzerolog.FieldMethod,
			fiberzerolog.FieldPath,
			fiberzerolog.FieldStatus,
			fiberzerolog.FieldLatency,
			fiberzerolog.FieldBytesSent,
			fiberzerolog.FieldError,
		},
		Messages: []string{"http server error", "http client error", "http request"},
		Levels:   []zerolog.Level{zerolog.ErrorLevel, zerolog.WarnLevel, zerolog.InfoLevel},
		// request_id matches the app log key so lines join. See docs/LOGGING.md.
		FieldsSnakeCase: true,
		// The SSE stream's latency spans the whole session: noise.
		SkipURIs: []string{"/api/v1/events"},
	}))
	app.Use(recover.New())
	// Not the SSE stream: compression buffers the body and breaks per-event flush.
	app.Use(compress.New(compress.Config{
		Next: func(c *fiber.Ctx) bool { return c.Path() == "/api/v1/events" },
	}))
	app.Use(cors.New())

	registerRoutes(app, h)

	// Unmatched API routes 404 as JSON instead of falling through to index.html.
	app.Use("/api", func(c *fiber.Ctx) error {
		return writeProblem(c, fiber.StatusNotFound, "not_found", "unknown API endpoint")
	})

	// Vite content-hashes /assets/ names, so cache them forever; keep index.html
	// uncached. The go:embed FS has zero ModTime, so the filesystem middleware
	// sends no freshness headers of its own.
	app.Use("/", func(c *fiber.Ctx) error {
		if strings.HasPrefix(c.Path(), "/assets/") {
			c.Set(fiber.HeaderCacheControl, "public, max-age=31536000, immutable")
		} else {
			c.Set(fiber.HeaderCacheControl, "no-cache")
		}
		return c.Next()
	})

	// NotFoundFile lets client-side routes resolve on a hard refresh.
	app.Use("/", filesystem.New(filesystem.Config{Root: cfg.WebRoot, NotFoundFile: "index.html"}))

	shutdownCh := make(chan os.Signal, 1)
	signal.Notify(shutdownCh, syscall.SIGHUP, syscall.SIGINT, syscall.SIGQUIT, syscall.SIGTERM)

	var shutdownOnce sync.Once
	shutdown := func() {
		shutdownOnce.Do(func() {
			rootLog.Info().Msg("gracefully shutting down")
			// Close the broker first: only that unblocks open SSE streams, else
			// ShutdownWithContext waits the full timeout. Close is idempotent.
			h.Close()

			ctxTimeout, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
			defer cancel()

			if err := app.ShutdownWithContext(ctxTimeout); err != nil {
				logHTTPShutdownError(rootLog, err)
			}
			// After Fiber drains (no new enqueues), before the DB closes.
			if h.enrichRunner != nil {
				h.enrichRunner.Stop()
			}
			h.posterWall.Stop()
			h.Close()
			if err := pool.Close(); err != nil {
				rootLog.Error().Err(err).Msg("closing the database on shutdown failed")
			}
		})
	}

	go func() {
		<-shutdownCh
		shutdown()
	}()

	if err := app.Listen(cfg.Port); err != nil {
		shutdown()
		return err
	}

	shutdown()
	return nil
}

func newHandler(pool *db.Pool, rootLog zerolog.Logger) *handler {
	h, err := newHandlerChecked(pool, rootLog)
	if err != nil {
		panic(err)
	}
	return h
}

func newHandlerChecked(pool *db.Pool, rootLog zerolog.Logger) (*handler, error) {
	userRepo := repository.NewSqliteUserRepository(pool)
	movieRepo := repository.NewSqliteMoviesRepository(pool)
	nextUpRepo := repository.NewSqliteNextUpRepository(pool)
	settingsRepo := repository.NewSqliteSettingsRepository(pool)
	movieMetadataRepo := repository.NewSqliteMovieMetadataRepository(pool)
	movieCreditsRepo := repository.NewSqliteMovieCreditsRepository(pool)
	integrationConfigRepo := repository.NewSqliteIntegrationConfigRepository(pool)
	integrationRunRepo := repository.NewSqliteIntegrationRunRepository(pool)
	broker := newEventBroker()
	movieService, err := movie.NewServiceChecked(movieRepo, movie.DrawConfig{
		OnRevealed: revealBroadcaster(broker),
		OnRevealError: func(err error) {
			rootLog.Error().Err(err).Msg("restoring or persisting movie Reveal failed")
		},
	})
	if err != nil {
		return nil, fmt.Errorf("restore concealed draw: %w", err)
	}
	startupAt := time.Now().UTC()
	if interrupted, err := integrationRunRepo.InterruptRunning(context.Background(), startupAt); err != nil {
		rootLog.Error().Err(err).Msg("interrupting abandoned integration runs failed")
	} else if interrupted > 0 {
		rootLog.Warn().Int64("count", interrupted).Msg("abandoned integration runs marked interrupted")
	}
	if removed, err := integrationRunRepo.Prune(context.Background(), startupAt); err != nil {
		rootLog.Error().Err(err).Msg("pruning integration run history failed")
	} else if removed > 0 {
		rootLog.Info().Int64("count", removed).Msg("old integration runs pruned")
	}
	runRetention := newIntegrationRunRetention(
		context.Background(),
		integrationRunRepo,
		nil,
		nil,
		func(err error) {
			rootLog.Error().Err(err).Msg("pruning integration run history failed")
		},
		func(removed int64) {
			rootLog.Info().Int64("count", removed).Msg("old integration runs pruned")
		},
	)

	tmdbEnvironment, environmentIssues := integrationtmdb.LoadEnvironmentConfig(os.LookupEnv)
	logTMDBEnvironmentIssues(rootLog, environmentIssues)
	keyPath := os.Getenv("MPA_INTEGRATION_KEY_FILE")
	if keyPath == "" {
		keyPath = integrationKeyFilePath(pool)
	}
	secretStore := integration.NewSecretStore(integration.NewFileKeySource(keyPath))
	radarrService := newRadarrService(
		repository.NewSqliteRadarrRepository(pool),
		secretStore,
		nil,
		os.Getenv("MPA_PUBLIC_URL"),
	)
	tmdbRuntime := integrationtmdb.NewRuntime(integrationtmdb.RuntimeConfig{}, 0)
	tmdbIntegration := integrationtmdb.NewService(
		integrationConfigRepo,
		secretStore,
		tmdbEnvironment,
		newTMDBConnectionTester("https://api.themoviedb.org/3", &http.Client{Timeout: 8 * time.Second}),
		tmdbRuntime,
	)
	if _, err := tmdbIntegration.Get(context.Background()); err != nil {
		rootLog.Error().Err(err).Msg("loading TMDB runtime failed, integration stays unavailable")
	}
	var tmdbScheduler *tmdbRunScheduler
	pauseScheduleIfRejected := func(revision int64) {
		if tmdbScheduler == nil {
			return
		}
		if err := tmdbScheduler.AuthenticationRejected(revision); err != nil {
			rootLog.Error().Err(err).Msg("pausing TMDB refresh schedule failed")
		}
	}
	tmdbGateway := newTMDBRuntimeGateway(
		tmdbIntegration,
		defaultTMDBOperationsFactory,
		func(_ context.Context, snapshot integrationtmdb.RuntimeSnapshot, err error) {
			if err != nil {
				rootLog.Error().Err(err).Msg("recording rejected TMDB credential failed")
			}
			pauseScheduleIfRejected(snapshot.Revision)
		},
	)
	runEnricher := &tmdbSnapshotRunEnricher{
		gateway: tmdbGateway, movies: movieRepo, candidates: movieMetadataRepo,
	}
	authenticationRejected := func(snapshot integrationtmdb.RuntimeSnapshot) {
		applied, err := tmdbIntegration.AuthenticationRejected(context.Background(), snapshot)
		if err != nil {
			rootLog.Error().Err(err).Msg("recording rejected TMDB credential failed")
		}
		if applied {
			pauseScheduleIfRejected(snapshot.Revision)
		}
	}
	tmdbRuns := newTMDBRunController(
		context.Background(),
		&tmdbRepositoryRunCandidates{candidates: movieMetadataRepo},
		runEnricher,
		tmdbIntegration,
		integrationRunRepo,
		nil,
		authenticationRejected,
		withTMDBRunCompletion(func(completion tmdbRunCompletion) {
			ctx := context.Background()
			if err := integrationConfigRepo.UpdateLastChecked(ctx, "tmdb", completion.FinishedAt); err != nil {
				rootLog.Error().Err(err).Msg("updating TMDB last checked time failed")
			}
			if tmdbRunStatusIsSuccessful(completion.Status) {
				if err := integrationConfigRepo.UpdateSuccessfulRun(ctx, "tmdb", completion.FinishedAt); err != nil {
					rootLog.Error().Err(err).Msg("updating TMDB successful refresh time failed")
				}
			}
		}),
		withTMDBRunError(func(err error) {
			rootLog.Error().Err(err).Msg("persisting TMDB run state failed")
		}),
	)
	tmdbScheduler = newTMDBRunScheduler(
		context.Background(),
		tmdbIntegration,
		tmdbRuns,
		integrationConfigRepo,
		nil,
		func(err error) {
			rootLog.Error().Err(err).Msg("running scheduled TMDB refresh failed")
		},
	)
	singleMovieEnricher := newTMDBSingleMovieLedgerEnricher(
		movieMetadataRepo,
		runEnricher,
		tmdbIntegration,
		integrationRunRepo,
		nil,
		authenticationRejected,
	)
	enrichCfg := loadEnrichConfig()
	enrichCfg.RefreshInterval = 0
	enrichLog := rootLog.With().Str("component", "enrich").Logger()
	runner := newEnrichRunner(singleMovieEnricher, broker, enrichCfg, enrichLog)
	runner.initialDrain = false

	// Shares the revision-scoped TMDB client and pacing with search and enrichment.
	posterLog := rootLog.With().Str("component", "poster-wall").Logger()
	posterWall := newPosterWallCache(tmdbGateway.DiscoverPopularPosters, posterWallRefreshInterval, posterLog)

	localAccountRepo := repository.NewSqliteLocalAccountRepository(pool)
	localAuth := auth.NewLocalAuth(localAccountRepo)

	h := &handler{
		broker:    broker,
		log:       rootLog.With().Str("component", "http").Logger(),
		sessions:  auth.NewSessionManager(repository.NewSqliteSessionRepository(pool)),
		localAuth: localAuth,
		invites: auth.NewInviteManager(
			repository.NewSqliteInviteRepository(pool),
			repository.NewSqliteAuthTransitionStore(pool),
		),
		userService:        user.NewService(userRepo, nextUpRepo),
		movieService:       movieService,
		nextUpService:      nextup.NewService(nextUpRepo),
		settingsService:    settings.NewService(settingsRepo),
		movieMetadata:      movieMetadataRepo,
		movieCredits:       movieCreditsRepo,
		tmdb:               tmdbGateway,
		enrichRunner:       runner,
		tmdbIntegration:    tmdbIntegration,
		integrationConfigs: integrationConfigRepo,
		integrationRuns:    integrationRunRepo,
		runRetention:       runRetention,
		tmdbRuns:           tmdbRuns,
		tmdbScheduler:      tmdbScheduler,
		radarr:             radarrService,
		posterWall:         posterWall,
		statsCache:         make(map[string]statsCacheEntry),
		statsCacheTTL:      time.Minute,

		sseHeartbeatInterval: sseHeartbeatInterval,
	}

	// Stats aggregate enriched metadata, so each enrichment invalidates them.
	runner.onEnriched = h.invalidateStatsCache
	wireTMDBRunEnriched(tmdbRuns, runner)

	if oidcCfg, enabled := auth.OIDCConfigFromEnv(); enabled {
		wireOIDC(h, oidcCfg, rootLog)
	}

	return h, nil
}

func tmdbRunStatusIsSuccessful(status integration.RunStatus) bool {
	return status == integration.RunStatusCompleted
}

func wireTMDBRunEnriched(controller *tmdbRunController, runner *enrichRunner) {
	if controller == nil {
		return
	}
	if runner == nil {
		controller.setEnrichedCallback(nil)
		return
	}
	controller.setEnrichedCallback(runner.recordEnriched)
}

func integrationKeyFilePath(pool *db.Pool) string {
	var path string
	if err := pool.Write.QueryRow(`SELECT file FROM pragma_database_list WHERE name = 'main'`).Scan(&path); err != nil || path == "" {
		return "moviepickarr.integration.key"
	}
	return path + ".integration.key"
}

// wireOIDC enables OIDC on h. Any failure leaves SSO off instead of failing boot,
// so local login still works.
func wireOIDC(h *handler, cfg auth.OIDCConfig, log zerolog.Logger) {
	txCodec, err := auth.NewOIDCTxCodec(os.Getenv("MPA_OIDC_TX_SECRET"))
	if err != nil {
		log.Error().Err(err).Msg("oidc tx codec init failed, SSO stays disabled")
		return
	}

	// Bound discovery so a slow provider cannot stall boot.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	rp, err := auth.NewRelyingParty(ctx, cfg)
	if err != nil {
		log.Error().Err(err).Str("issuer", cfg.Issuer).Msg("oidc discovery failed, SSO stays disabled")
		return
	}

	h.oidc = rp
	h.oidcTx = txCodec
	h.oidcEnabled = true
	log.Info().Str("issuer", cfg.Issuer).Msg("oidc relying-party enabled")
}

func registerRoutes(app *fiber.App, h *handler) {
	v1 := app.Group("/api/v1")
	// First, so a forged cross-origin call never reaches a session lookup or login.
	v1.Use(csrfGuard)

	// Pre-session routes: login and claim have no session yet.
	v1.Get("/auth/config", h.handleAuthConfig)
	v1.Get("/auth/poster-wall", h.handlePosterWall)
	v1.Post("/auth/login", h.handleLogin)
	v1.Get("/auth/claim/:token", h.handleValidateClaim)
	v1.Post("/auth/claim/:token/password", h.handleClaimPassword)

	// Pre-session GETs: csrfGuard exempts them, so the callback's CSRF defense is
	// its own state and PKCE.
	if h.oidcEnabled {
		v1.Get("/auth/oidc/login", h.handleOIDCLogin)
		v1.Get("/auth/oidc/callback", h.handleOIDCCallback)
		v1.Get("/auth/claim/:token/oidc", h.handleClaimOIDC)
	} else {
		// Before requireSession, so a probe gets 404 instead of the session 401.
		v1.All("/auth/oidc/*", ssoDisabled)
		v1.Get("/auth/claim/:token/oidc", ssoDisabled)
		v1.All("/auth/linked-identity", ssoDisabled)
		v1.All("/members/:memberID/linked-identity", ssoDisabled)
	}

	// Everything registered after this point is authenticated.
	v1.Use(h.requireSession)

	// The admin local-login routes check the role inside the handler.
	v1.Get("/auth/me", h.handleMe)
	v1.Post("/auth/password", h.handleChangePassword)
	// Empty body ends this device, {"all":true} ends every session. Idempotent.
	v1.Post("/auth/logout", h.handleLogout)
	// Self-only: the member id comes from the session, not the path.
	v1.Get("/auth/sessions", h.handleListSessions)
	v1.Delete("/auth/sessions/:sessionID", h.handleRevokeSession)
	// First local login for an authed member; the session is the proof.
	v1.Post("/auth/local-login", h.handleSelfServeLocalLogin)
	v1.Put("/members/:memberID/local-login", h.handleSetLocalLogin)
	v1.Delete("/members/:memberID/local-login", h.handleDeleteLocalLogin)
	// Invite creation is member-addressed only when no generation exists. Every
	// action on an existing generation is a compare-and-swap on its public id.
	v1.Post("/members/:memberID/invite", h.handleCreateInvite)
	v1.Get("/invites", h.handleListInvites)
	v1.Post("/invites/:inviteID/replacement", h.handleReplaceInvite)
	v1.Delete("/invites/:inviteID", h.handleRevokeInvite)
	v1.Post("/invites/:inviteID/dismiss", h.handleDismissInvite)

	// When SSO is off, the pre-session stubs above 404 these paths.
	if h.oidcEnabled {
		v1.Get("/auth/oidc/link", h.handleOIDCLink)
		v1.Delete("/auth/linked-identity", h.handleUnlinkSelf)
		v1.Delete("/members/:memberID/linked-identity", h.handleUnlinkMember)
	}

	registerV1Routes(v1, h)
}

func registerV1Routes(v1 fiber.Router, h *handler) {
	v1.Get("/events", h.handleSSE)
	v1.Get("/integrations", h.handleListIntegrations)
	v1.Get("/integrations/tmdb", h.handleGetTMDBIntegration)
	v1.Put("/integrations/tmdb", h.handleSaveTMDBIntegration)
	v1.Post("/integrations/tmdb/test", h.handleTestTMDBConnection)
	v1.Post("/integrations/tmdb/runs", h.handleStartTMDBRun)
	v1.Get("/integrations/radarr/attention", h.handleGetRadarrAttention)
	v1.Get("/integrations/radarr/acquisitions", h.handleListRadarrAcquisitions)
	v1.Get("/integrations/radarr/acquisitions/:id", h.handleGetRadarrAcquisition)
	v1.Put("/integrations/radarr/acquisitions/:id/preset", h.handleSelectRadarrPreset)
	v1.Post("/integrations/radarr/acquisitions/:id/confirm", h.handleConfirmRadarrTarget)
	v1.Post("/integrations/radarr/acquisitions/:id/identity-search", h.handleSearchRadarrIdentity)
	v1.Put("/integrations/radarr/acquisitions/:id/identity", h.handleSelectRadarrIdentity)
	v1.Post("/integrations/radarr/acquisitions/:id/releases/search", h.handleSearchRadarrReleases)
	v1.Post("/integrations/radarr/acquisitions/:id/releases/:resultId/grab", h.handleGrabRadarrRelease)
	v1.Post("/integrations/radarr/acquisitions/:id/retry", h.handleRetryRadarrAcquisition)
	v1.Post("/integrations/radarr/acquisitions/:id/abandon/review", h.handleReviewRadarrAbandonment)
	v1.Post("/integrations/radarr/acquisitions/:id/abandon", h.handleAbandonRadarrAcquisition)
	v1.Get("/integrations/radarr/instances", h.handleListRadarrInstances)
	v1.Post("/integrations/radarr/instances", h.handleCreateRadarrInstance)
	v1.Put("/integrations/radarr/instances/:id", h.handleUpdateRadarrInstance)
	v1.Delete("/integrations/radarr/instances/:id", h.handleRemoveRadarrInstance)
	v1.Get("/integrations/radarr/instances/:id/options", h.handleGetRadarrInstanceOptions)
	v1.Get("/integrations/radarr/presets", h.handleListRadarrPresets)
	v1.Post("/integrations/radarr/presets", h.handleCreateRadarrPreset)
	v1.Put("/integrations/radarr/presets/:id", h.handleUpdateRadarrPreset)
	v1.Delete("/integrations/radarr/presets/:id", h.handleRemoveRadarrPreset)
	v1.Get("/integrations/radarr/webhooks", h.handleListRadarrWebhooks)
	v1.Post("/integrations/radarr/webhooks", h.handleCreateRadarrWebhook)
	v1.Put("/integrations/radarr/webhooks/:id", h.handleUpdateRadarrWebhook)
	v1.Delete("/integrations/radarr/webhooks/:id", h.handleArchiveRadarrWebhook)
	v1.Post("/integrations/radarr/webhooks/:id/test", h.handleTestRadarrWebhook)
	v1.Post("/integrations/radarr/webhooks/test", h.handleTestRadarrWebhookDraft)
	v1.Get("/integration-runs", h.handleListIntegrationRuns)
	v1.Delete("/integration-runs/:runID", h.handleCancelTMDBRun)

	// Writes are admin-only, checked in the handlers. The actor is always the
	// session member, never a path id.
	v1.Get("/members", h.handleGetUsers)
	// Admin-only, with archived members and login state; kept apart from the lean
	// GET /members.
	v1.Get("/members/roster", h.handleGetRoster)
	v1.Post("/members", h.handleCreateUser)
	v1.Patch("/members/:memberID/role", h.handleSetRole)
	v1.Delete("/members/:memberID", h.handleDeleteUser)
	v1.Post("/members/:memberID/restore", h.handleRestoreUser)
	v1.Get("/members/:memberID/pool", h.handleGetPool)
	v1.Get("/members/:memberID/stash", h.handleGetStash)

	// The adder is the session member. Edit, delete and move are adder-only (403
	// not_adder, no admin override).
	v1.Post("/movies", h.handleAddMovie)
	// Before the :movieID routes, else DELETE /movies/wildcard parses as an id.
	v1.Get("/movies/wildcard", h.handleGetActiveWildcard)
	v1.Post("/movies/wildcard", h.handleSelectWildcard)
	v1.Delete("/movies/wildcard", h.handleCancelWildcard)
	v1.Post("/movies/wildcard/watch", h.handleWatchWildcard)
	v1.Put("/movies/:movieID", h.handleEditMovie)
	v1.Delete("/movies/:movieID", h.handleDeleteMovie)
	v1.Post("/movies/:movieID/move", h.handleMove)

	v1.Get("/movies/pool", h.handleGetPooledMovies)
	v1.Post("/movies/random", h.handleGetRandomMovie)
	v1.Get("/movies/current", h.handleGetCurrentMovie)
	v1.Post("/movies/current/reveal", h.handleRevealCurrentMovie)
	v1.Get("/movies/watched", h.handleGetWatchedMovies)
	// Literal GETs before the :movieID route so they take precedence.
	v1.Get("/movies/filter-options", h.handleGetFilterOptions)
	v1.Get("/movies/:movieID", h.handleGetMovie)
	v1.Post("/movies/current/watch", h.handleWatchMovie)
	v1.Get("/stats", h.handleGetStats)

	v1.Get("/settings/pool-lock", h.handleGetPoolLock)
	v1.Put("/settings/pool-lock", h.handleSetPoolLock)
	v1.Get("/settings/next-up", h.handleGetNextUp)
	v1.Post("/settings/next-up/skip", h.handleSkipNextUp)

	v1.Get("/tmdb/search", h.handleTMDBSearch)
}
