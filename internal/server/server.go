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

// shutdownTimeout bounds how long Fiber gets to drain in-flight requests. Named
// so the log line that fires when it expires can report the budget it blew.
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

// dbMaxBackups resolves DB_BACKUP_MAX: how many pre-migration snapshots to
// keep next to the DB file. 0 disables backups; invalid values fall back to
// the default with a warning rather than failing startup.
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

// ResolveDBFile picks the SQLite path the app opens: an explicit value wins,
// then DB_FILE from the environment (populate it from .env with godotenv.Load
// first), then the "moviepickarr.db" default in the working directory. Both the
// server (Run) and the dev-fixtures command resolve the DB the same way so they
// never disagree about which file is "the dev DB".
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
	// Resolved after godotenv.Load() above so DB_FILE works from a .env file
	// too, not just the process environment.
	cfg.DBFile = ResolveDBFile(cfg.DBFile)
	if cfg.WebRoot == nil {
		return fmt.Errorf("web root is required")
	}

	// Build the root logger first so everything below is observable. Mirror it
	// to the zerolog global (zlog) so package-level call sites — e.g. the env
	// parsers in enrich_worker — log through the same configured writer.
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

	// Boot ordering is migrate → seed → serve: the break-glass admin seed runs
	// on the freshly migrated schema, before any request can be served. A
	// misconfigured seed fails boot loudly rather than leaving a login-less
	// deploy.
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
	// Warm the public poster wall in the background: nil (no key) means the wall
	// never warms and the endpoint serves []. Async, so boot is never blocked by
	// the TMDB round trip.
	h.posterWall.Start(ctx)

	app := fiber.New(fiber.Config{
		DisableStartupMessage: true,
		JSONEncoder: func(value any) ([]byte, error) {
			return json.Marshal(value)
		},
		JSONDecoder: func(data []byte, value any) error {
			return json.Unmarshal(data, value)
		},
		// Cheap hardening: cap how long a (possibly half-open) client can hold a
		// connection while sending its request or sitting idle between keep-alive
		// requests. Deliberately NO WriteTimeout — it would sever the long-lived
		// /api/v1/events SSE stream mid-response.
		ReadTimeout: 15 * time.Second,
		IdleTimeout: 120 * time.Second,
	})

	// Middleware order is deliberate: requestid sets the X-Request-ID response
	// header on the way in; fiberzerolog reads it on the way out, so requestid
	// must precede it. fiberzerolog also sits ahead of recover so a recovered
	// panic still yields one access-log line carrying the error. It reuses the
	// handler's component=http logger so access and app logs share one derivation.
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
		// Emit request_id/bytes_sent rather than fiberzerolog's default
		// requestId/bytesSent, so an access line and the app lines from the same
		// request join on one key. See docs/LOGGING.md.
		FieldsSnakeCase: true,
		// The SSE stream is long-lived: its "latency" would span the whole
		// session and it logs one line per open — noise, so skip it.
		SkipURIs: []string{"/api/v1/events"},
	}))
	app.Use(recover.New())
	// Gzip JSON responses and the embedded SPA assets. The SSE stream is excluded:
	// compression buffers the response body, which would break the per-event flush
	// that keeps the event stream real-time.
	app.Use(compress.New(compress.Config{
		Next: func(c *fiber.Ctx) bool { return c.Path() == "/api/v1/events" },
	}))
	app.Use(cors.New())

	registerRoutes(app, h)

	// Unmatched API routes must 404 as JSON rather than fall through to the SPA
	// fallback below — otherwise a mistyped endpoint would return index.html.
	// Real /api routes are registered above and terminate the chain, so this
	// only fires for paths no handler matched.
	app.Use("/api", func(c *fiber.Ctx) error {
		return writeProblem(c, fiber.StatusNotFound, "not_found", "unknown API endpoint")
	})

	// Freshness headers for the embedded SPA. Vite emits content-hashed files
	// under /assets/ (e.g. index-DfysZQP7.css) whose name changes on every
	// content change, so they're safe to cache forever; everything else
	// (index.html and the SPA fallback) must stay uncached so a new deploy's
	// asset URLs are always loaded. This matters because WebRoot is a
	// go:embed FS, which reports a zero ModTime — the filesystem middleware
	// below then emits NO freshness signal at all (no Cache-Control, no
	// Last-Modified, no ETag), so without this every load re-pulls and
	// re-compresses the whole bundle. The middleware leaves Cache-Control alone
	// (its MaxAge defaults to 0), so the header set here survives.
	app.Use("/", func(c *fiber.Ctx) error {
		if strings.HasPrefix(c.Path(), "/assets/") {
			c.Set(fiber.HeaderCacheControl, "public, max-age=31536000, immutable")
		} else {
			c.Set(fiber.HeaderCacheControl, "no-cache")
		}
		return c.Next()
	})

	// Serve the embedded SPA. NotFoundFile makes unknown paths fall back to
	// index.html so client-side routes (e.g. /stats, /users) resolve on a hard
	// refresh or shared deep-link instead of 404ing. Non-API paths only — the
	// /api catch-all above keeps API 404s as JSON.
	app.Use("/", filesystem.New(filesystem.Config{Root: cfg.WebRoot, NotFoundFile: "index.html"}))

	shutdownCh := make(chan os.Signal, 1)
	signal.Notify(shutdownCh, syscall.SIGHUP, syscall.SIGINT, syscall.SIGQUIT, syscall.SIGTERM)

	var shutdownOnce sync.Once
	shutdown := func() {
		shutdownOnce.Do(func() {
			rootLog.Info().Msg("gracefully shutting down")
			// Close the event broker FIRST: every SSE stream blocks in a select
			// on its event channel, and only the broker closing those channels
			// unwinds them. Closing here lets each stream return so Fiber can
			// drain immediately — otherwise ShutdownWithContext burns its full
			// 10s timeout waiting on idle-but-open SSE goroutines (a real tax on
			// every dev restart). Close is idempotent and marks the broker closed
			// so a late Subscribe can't re-stall.
			h.Close()

			ctxTimeout, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
			defer cancel()

			if err := app.ShutdownWithContext(ctxTimeout); err != nil {
				logHTTPShutdownError(rootLog, err)
			}
			// Stop the worker after Fiber has drained (no handler can enqueue)
			// but before the DB closes (in-flight enrichment still reads it).
			if h.enrichRunner != nil {
				h.enrichRunner.Stop()
			}
			// Stop the poster-wall refresh loop too; nil-safe when keyless.
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

	// The public wall stays empty while TMDB is unavailable. It shares the same
	// revision-scoped client and pacing as search and enrichment.
	posterLog := rootLog.With().Str("component", "poster-wall").Logger()
	posterWall := newPosterWallCache(tmdbGateway.DiscoverPopularPosters, posterWallRefreshInterval, posterLog)

	// The local-account repo backs LocalAuth reads and password verification.
	// InviteManager gets the scoped store that commits credential and invite
	// transitions together.
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
		nextUpService:      nextup.NewService(nextUpRepo, userRepo),
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

	// Stats now aggregate enriched metadata/credits, so every successful
	// enrichment invalidates the cached stats payloads.
	runner.onEnriched = h.invalidateStatsCache
	wireTMDBRunEnriched(tmdbRuns, runner)

	// OIDC is presence-derived: wired only when the config quartet is set. A tx
	// codec or a discovery failure leaves SSO off (routes unmounted, claim page
	// hides the option) rather than failing boot, so the app still serves local
	// login.
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

// wireOIDC builds the relying party (running discovery once) and tx-cookie AEAD
// codec, attaching them to the handler and flipping oidcEnabled. Any failure
// leaves OIDC off, so a bad provider never takes the whole app down.
func wireOIDC(h *handler, cfg auth.OIDCConfig, log zerolog.Logger) {
	txCodec, err := auth.NewOIDCTxCodec(os.Getenv("MPA_OIDC_TX_SECRET"))
	if err != nil {
		log.Error().Err(err).Msg("oidc tx codec init failed, SSO stays disabled")
		return
	}

	// Discovery is a network round trip; bound it so a slow or down provider
	// doesn't stall boot.
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
	// csrfGuard runs first on the whole group so a forged cross-origin
	// state-changing call never reaches a session lookup or a login verify.
	v1.Use(csrfGuard)

	// Auth-establishing routes sit ahead of requireSession: login and claim have
	// no session yet, so they declare their own (absent) auth per-route. All are
	// still behind csrfGuard above (the claim-validate GET is exempt as a safe
	// method; the password claim POST carries the same-origin CSRF signal).
	// Public auth capabilities for the unauthenticated login page (presence of an
	// SSO provider today). GET + no secrets, so it sits ahead of requireSession
	// alongside the other pre-auth routes.
	v1.Get("/auth/config", h.handleAuthConfig)
	// Poster wall for the unauthenticated login/claim panel. GET + no secrets
	// (poster paths are public artwork), so it sits ahead of requireSession beside
	// /auth/config and rides the csrfGuard safe-method exemption.
	v1.Get("/auth/poster-wall", h.handlePosterWall)
	v1.Post("/auth/login", h.handleLogin)
	v1.Get("/auth/claim/:token", h.handleValidateClaim)
	v1.Post("/auth/claim/:token/password", h.handleClaimPassword)

	// OIDC initiation + callback are unauthenticated (login and claim carry no
	// session; the callback re-authenticates itself for the link intent) and
	// mounted only when a provider is configured. All are GETs: csrfGuard exempts
	// them, and the callback's CSRF defense is its own state/PKCE. link + unlink
	// are authed, registered below.
	if h.oidcEnabled {
		v1.Get("/auth/oidc/login", h.handleOIDCLogin)
		v1.Get("/auth/oidc/callback", h.handleOIDCCallback)
		v1.Get("/auth/claim/:token/oidc", h.handleClaimOIDC)
	} else {
		// SSO off: the whole OIDC surface is a clean 404, registered ahead of
		// requireSession so a probe sees "no such feature" instead of the blanket
		// 401 the session gate would otherwise return for an unmatched path.
		v1.All("/auth/oidc/*", ssoDisabled)
		v1.Get("/auth/claim/:token/oidc", ssoDisabled)
		v1.All("/auth/linked-identity", ssoDisabled)
		v1.All("/members/:memberID/linked-identity", ssoDisabled)
	}

	// requireSession turns the session cookie into a live actor (401 +
	// cookie-clear when it can't); everything registered after this point is
	// authenticated. The shared registerV1Routes carries no auth of its own, so
	// the per-route authz reshape can layer on later without moving this wiring.
	v1.Use(h.requireSession)

	// Self-serve identity routes. The admin local-login routes sit under a
	// temporary in-handler admin guard until the per-route authz reshape lands;
	// they use the /members surface the reshape will settle on.
	v1.Get("/auth/me", h.handleMe)
	v1.Post("/auth/password", h.handleChangePassword)
	// Session logout: empty/{} ends this device, {"all":true} ends every session
	// for the member. Always clears the cookie, 204, idempotent.
	v1.Post("/auth/logout", h.handleLogout)
	// The member's own device list and per-device sign-out. Self-only by
	// construction: both read the member id off the session, so there is no
	// target id to authorize and no path to anyone else's sessions.
	v1.Get("/auth/sessions", h.handleListSessions)
	v1.Delete("/auth/sessions/:sessionID", h.handleRevokeSession)
	// Self-serve credential completeness: an authed member with no local login
	// sets a first username + password (the session is the proof).
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

	// Authed OIDC surface: link (start the link intent for the session member) and
	// unlink (self + admin), mounted only when a provider is configured. When off,
	// these paths are handled by the pre-session 404 stubs above.
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

	// Roster: reads are any-authenticated; create/delete are admin-only (guarded
	// inside the handlers). The actor is always the session member, never a path id.
	v1.Get("/members", h.handleGetUsers)
	// The admin roster is a distinct read: every member (active + archived) with
	// presence-derived login state, admin-gated inside the handler. It sits beside
	// the lean movie-board GET /members rather than widening it.
	v1.Get("/members/roster", h.handleGetRoster)
	v1.Post("/members", h.handleCreateUser)
	v1.Patch("/members/:memberID/role", h.handleSetRole)
	v1.Delete("/members/:memberID", h.handleDeleteUser)
	v1.Post("/members/:memberID/restore", h.handleRestoreUser)
	v1.Get("/members/:memberID/pool", h.handleGetPool)
	v1.Get("/members/:memberID/stash", h.handleGetStash)

	// Movie mutations carry no actor id: the adder is the session member. Edit,
	// delete and move are adder-only (403 not_adder, no admin override), enforced
	// inside each handler.
	v1.Post("/movies", h.handleAddMovie)
	// Literal wildcard routes must precede the :movieID routes. In particular,
	// DELETE /movies/wildcard would otherwise be parsed as a movie id.
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
	// Literal /movies/* GETs are registered before the :movieID param route so
	// they take precedence; the param route serves the lazy detail modal.
	v1.Get("/movies/filter-options", h.handleGetFilterOptions)
	v1.Get("/movies/:movieID", h.handleGetMovie)
	v1.Post("/movies/current/watch", h.handleWatchMovie)
	v1.Get("/stats", h.handleGetStats)

	v1.Get("/settings/pool-lock", h.handleGetPoolLock)
	v1.Put("/settings/pool-lock", h.handleSetPoolLock)
	v1.Get("/settings/next-up", h.handleGetNextUp)

	v1.Get("/tmdb/search", h.handleTMDBSearch)
}
