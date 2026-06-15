package server

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"moviepickarr/internal/db"
	"moviepickarr/internal/movie"
	"moviepickarr/internal/nextpicker"
	"moviepickarr/internal/repository"
	"moviepickarr/internal/settings"
	"moviepickarr/internal/user"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/filesystem"
	"github.com/gofiber/fiber/v2/middleware/logger"
	"github.com/gofiber/fiber/v2/middleware/recover"
	"github.com/gofiber/fiber/v2/middleware/requestid"
	"github.com/joho/godotenv"
)

type Config struct {
	Port    string
	DBFile  string
	WebRoot http.FileSystem
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

	h := newHandler(dbConn)
	if h.enrichRunner != nil {
		h.enrichRunner.Start(ctx)
	} else {
		log.Println("enrichment disabled: TMDB_API_KEY not set")
	}

	app := fiber.New(fiber.Config{DisableStartupMessage: true})

	app.Use(logger.New(logger.Config{
		TimeFormat: "2006-01-02 15:04:05",
		TimeZone:   "Local",
	}))
	app.Use(recover.New())
	app.Use(requestid.New())
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
			log.Println("gracefully shutting down")
			ctxTimeout, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			if err := app.ShutdownWithContext(ctxTimeout); err != nil {
				log.Printf("shutdown error: %v", err)
			}
			// Stop the worker after Fiber has drained (no handler can enqueue)
			// but before the DB closes (in-flight enrichment still reads it).
			if h.enrichRunner != nil {
				h.enrichRunner.Stop()
			}
			h.Close()
			if err := dbConn.Close(); err != nil {
				log.Printf("db close error: %v", err)
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

func newHandler(dbConn *sql.DB) *handler {
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
		runner = newEnrichRunner(enrichmentSvc, broker, enrichCfg)
	}

	h := &handler{
		broker:            broker,
		userService:       user.NewService(userRepo, nextPickerRepo),
		movieService:      movie.NewService(movieRepo, settingsRepo),
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
