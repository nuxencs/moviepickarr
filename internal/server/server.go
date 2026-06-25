package server

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"moviepickarr/internal/db"
	"moviepickarr/internal/logger"
	"moviepickarr/internal/movie"
	"moviepickarr/internal/nextpicker"
	"moviepickarr/internal/repository"
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

func Run(ctx context.Context, cfg Config) error {
	_ = godotenv.Load()

	if cfg.Port == "" {
		cfg.Port = ":3030"
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

	dbConn, err := db.OpenSQLite(cfg.DBFile)
	if err != nil {
		return err
	}

	if err := db.RunMigrations(ctx, dbConn); err != nil {
		_ = dbConn.Close()
		return err
	}

	h := newHandler(dbConn, rootLog)
	if h.enrichRunner != nil {
		h.enrichRunner.Start(ctx)
	} else {
		rootLog.Info().Msg("enrichment disabled: TMDB_API_KEY not set")
	}

	app := fiber.New(fiber.Config{DisableStartupMessage: true})

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
			if err := dbConn.Close(); err != nil {
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

func newHandler(dbConn *sql.DB, rootLog zerolog.Logger) *handler {
	userRepo := repository.NewSqliteUserRepository(dbConn)
	movieRepo := repository.NewSqliteMoviesRepository(dbConn)
	nextPickerRepo := repository.NewSqliteNextPickerRepository(dbConn)
	settingsRepo := repository.NewSqliteSettingsRepository(dbConn)
	movieMetadataRepo := repository.NewSqliteMovieMetadataRepository(dbConn)
	movieCreditsRepo := repository.NewSqliteMovieCreditsRepository(dbConn)

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

	h := &handler{
		broker:            broker,
		log:               rootLog.With().Str("component", "http").Logger(),
		userService:       user.NewService(userRepo, nextPickerRepo),
		movieService:      movie.NewService(movieRepo),
		nextPickerService: nextpicker.NewService(nextPickerRepo, userRepo),
		settingsService:   settings.NewService(settingsRepo),
		movieMetadata:     movieMetadataRepo,
		movieCredits:      movieCreditsRepo,
		tmdb:              tmdbCli,
		enrichRunner:      runner,
		statsCache:        make(map[string]statsCacheEntry),
		statsCacheTTL:     time.Minute,
	}

	// Stats now aggregate enriched metadata/credits, so every successful
	// enrichment invalidates the cached stats payloads.
	if runner != nil {
		runner.onEnriched = h.invalidateStatsCache
	}

	return h
}

func registerRoutes(app *fiber.App, h *handler) {
	v1 := app.Group("/api/v1")
	registerV1Routes(v1, h)
}

func registerV1Routes(v1 fiber.Router, h *handler) {
	v1.Get("/events", h.handleSSE)

	v1.Get("/users", h.handleGetUsers)
	v1.Post("/users", h.handleCreateUser)
	v1.Delete("/users/:userID", h.handleDeleteUser)
	v1.Post("/users/:userID/movies", h.handleAddMovie)
	v1.Put("/users/:userID/movies/:movieID", h.handleEditMovie)
	v1.Delete("/users/:userID/movies/:movieID", h.handleDeleteMovie)
	v1.Post("/users/:userID/movies/:movieID/move", h.handleMove)
	v1.Get("/users/:userID/pool", h.handleGetPool)
	v1.Get("/users/:userID/stash", h.handleGetStash)

	v1.Get("/movies/pool", h.handleGetPooledMovies)
	v1.Post("/movies/random", h.handleGetRandomMovie)
	v1.Get("/movies/current", h.handleGetCurrentMovie)
	v1.Get("/movies/watched", h.handleGetWatchedMovies)
	v1.Post("/movies/current/watch", h.handleWatchMovie)
	v1.Get("/stats", h.handleGetStats)

	v1.Get("/settings/pool-lock", h.handleGetPoolLock)
	v1.Put("/settings/pool-lock", h.handleSetPoolLock)
	v1.Get("/settings/next-picker", h.handleGetNextPicker)

	v1.Get("/tmdb/search", h.handleTMDBSearch)
}
