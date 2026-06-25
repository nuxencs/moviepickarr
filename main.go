package main

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"net/http"

	"moviepickarr/internal/server"

	"github.com/rs/zerolog/log"
)

var (
	version = "dev"
	commit  = "dev"
	date    = "unknown"
)

//go:embed web/dist
var webFS embed.FS

func main() {
	// run() owns all fallible startup; main only reports a fatal exit. zerolog's
	// Fatal logs then calls os.Exit(1), so it must stay at this outermost frame —
	// never deep in a handler or worker, where it would skip graceful shutdown.
	if err := run(); err != nil {
		log.Fatal().Err(err).Msg("server exited")
	}
}

func run() error {
	webRoot, err := fs.Sub(webFS, "web/dist")
	if err != nil {
		return fmt.Errorf("web dist embed: %w", err)
	}

	return server.Run(context.Background(), server.Config{
		Port:    ":3030",
		DBFile:  "moviepickarr.db",
		WebRoot: http.FS(webRoot),
		Version: version,
		Commit:  commit,
		Date:    date,
	})
}
