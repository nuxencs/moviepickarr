package server

import (
	"context"
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
	"moviepickarr/internal/domain"
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
		log.Warn().Str("DB_BACKUP_MAX", raw).Int("default", defaultMaxBackups).
			Msg("invalid DB_BACKUP_MAX, using default")
		return defaultMaxBackups
	}
	return n
}

func Run(ctx context.Context, cfg Config) error {
	_ = godotenv.Load()

	if cfg.Port == "" {
		cfg.Port = ":3030"
	}
	// Resolved after godotenv.Load() above so DB_FILE works from a .env file
	// too, not just the process environment.
	if cfg.DBFile == "" {
		cfg.DBFile = os.Getenv("DB_FILE")
	}
	if cfg.DBFile == "" {
		cfg.DBFile = "moviepickarr.db"
	}
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

	h := newHandler(pool, rootLog)
	h.startSessionSweeper(ctx)
	if h.enrichRunner != nil {
		h.enrichRunner.Start(ctx)
	} else {
		rootLog.Info().Msg("enrichment disabled: TMDB_API_KEY not set")
	}

	app := fiber.New(fiber.Config{
		DisableStartupMessage: true,
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

			ctxTimeout, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			if err := app.ShutdownWithContext(ctxTimeout); err != nil {
				rootLog.Error().Err(err).Msg("server shutdown error")
			}
			// Stop the worker after Fiber has drained (no handler can enqueue)
			// but before the DB closes (in-flight enrichment still reads it).
			if h.enrichRunner != nil {
				h.enrichRunner.Stop()
			}
			h.Close()
			if err := pool.Close(); err != nil {
				rootLog.Error().Err(err).Msg("db close error")
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
	userRepo := repository.NewSqliteUserRepository(pool)
	movieRepo := repository.NewSqliteMoviesRepository(pool)
	nextUpRepo := repository.NewSqliteNextUpRepository(pool)
	settingsRepo := repository.NewSqliteSettingsRepository(pool)
	movieMetadataRepo := repository.NewSqliteMovieMetadataRepository(pool)
	movieCreditsRepo := repository.NewSqliteMovieCreditsRepository(pool)

	enrichCfg := loadEnrichConfig()
	limiter := newRateLimiter(enrichCfg.MinInterval)
	tmdbCli := newTMDBClient(enrichCfg, limiter)
	broker := newEventBroker()

	// The enrichment runner is only created when TMDB is configured; otherwise
	// it stays nil and all enqueue/start/stop sites no-op (handlers guard on nil).
	var runner *enrichRunner
	if tmdbCli.apiKey != "" {
		enrichmentSvc := newEnrichmentService(movieRepo, movieMetadataRepo, movieCreditsRepo, tmdbCli, enrichCfg.CastLimit)
		enrichLog := rootLog.With().Str("component", "enrich").Logger()
		runner = newEnrichRunner(enrichmentSvc, broker, enrichCfg, enrichLog)
	}

	// localAuth is shared: the invite manager reuses its SetLocalLogin for the
	// password-claim upsert, so both hold the same instance. The local-account
	// repo is hoisted so the OIDC linker can read it for the unlink guard.
	localAccountRepo := repository.NewSqliteLocalAccountRepository(pool)
	localAuth := auth.NewLocalAuth(localAccountRepo)

	h := &handler{
		broker:      broker,
		log:         rootLog.With().Str("component", "http").Logger(),
		sessions:    auth.NewSessionManager(repository.NewSqliteSessionRepository(pool)),
		localAuth:   localAuth,
		invites:     auth.NewInviteManager(repository.NewSqliteInviteRepository(pool), localAuth),
		userService: user.NewService(userRepo, nextUpRepo),
		movieService: movie.NewService(movieRepo, movie.DrawConfig{
			OnRevealed: revealBroadcaster(broker),
		}),
		nextUpService:   nextup.NewService(nextUpRepo, userRepo, movieRepo),
		settingsService: settings.NewService(settingsRepo),
		movieMetadata:   movieMetadataRepo,
		movieCredits:    movieCreditsRepo,
		tmdb:            tmdbCli,
		enrichRunner:    runner,
		statsCache:      make(map[string]statsCacheEntry),
		statsCacheTTL:   time.Minute,

		sseHeartbeatInterval: sseHeartbeatInterval,
	}

	// Stats now aggregate enriched metadata/credits, so every successful
	// enrichment invalidates the cached stats payloads.
	if runner != nil {
		runner.onEnriched = h.invalidateStatsCache
	}

	// OIDC is presence-derived: wired only when the config quartet is set. A tx
	// codec or a discovery failure leaves SSO off (routes unmounted, claim page
	// hides the option) rather than failing boot, so the app still serves local
	// login.
	if oidcCfg, enabled := auth.OIDCConfigFromEnv(); enabled {
		wireOIDC(h, oidcCfg, repository.NewSqliteOIDCIdentityRepository(pool), localAccountRepo, rootLog)
	}

	return h
}

// wireOIDC builds the relying party (running discovery once), the tx-cookie AEAD
// codec, and the identity linker, attaching them to the handler and flipping
// oidcEnabled. Any failure (bad tx secret, unreachable provider) is logged and
// leaves OIDC off, so a misconfigured provider never takes the whole app down.
func wireOIDC(h *handler, cfg auth.OIDCConfig, identities domain.OIDCIdentityRepo, local domain.LocalAccountRepo, log zerolog.Logger) {
	txCodec, err := auth.NewOIDCTxCodec(os.Getenv("MPA_OIDC_TX_SECRET"))
	if err != nil {
		log.Error().Err(err).Msg("oidc tx codec init failed; SSO disabled")
		return
	}

	// Discovery is a network round trip; bound it so a slow or down provider
	// doesn't stall boot.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	rp, err := auth.NewRelyingParty(ctx, cfg)
	if err != nil {
		log.Error().Err(err).Str("issuer", cfg.Issuer).Msg("oidc discovery failed; SSO disabled")
		return
	}

	h.oidc = rp
	h.oidcTx = txCodec
	h.linker = auth.NewIdentityLinker(identities, local)
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
	// Self-serve credential completeness: an authed member with no local login
	// sets a first username + password (the session is the proof).
	v1.Post("/auth/local-login", h.handleSelfServeLocalLogin)
	v1.Put("/members/:memberID/local-login", h.handleSetLocalLogin)
	v1.Delete("/members/:memberID/local-login", h.handleDeleteLocalLogin)
	// Invite issuance/revocation (admin-gated inside the handlers). POST /members
	// itself (create + first invite) lives in registerV1Routes with the roster.
	v1.Post("/members/:memberID/invite", h.handleReissueInvite)
	v1.Delete("/members/:memberID/invite", h.handleRevokeInvite)

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

	// Roster: reads are any-authenticated; create/delete are admin-only (guarded
	// inside the handlers). The actor is always the session member, never a path id.
	v1.Get("/members", h.handleGetUsers)
	v1.Post("/members", h.handleCreateUser)
	v1.Delete("/members/:memberID", h.handleDeleteUser)
	v1.Get("/members/:memberID/pool", h.handleGetPool)
	v1.Get("/members/:memberID/stash", h.handleGetStash)

	// Movie mutations carry no actor id: the adder is the session member. Edit,
	// delete and move are adder-only (403 not_adder, no admin override), enforced
	// inside each handler.
	v1.Post("/movies", h.handleAddMovie)
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
