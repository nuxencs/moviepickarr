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
	// Fatal calls os.Exit, so keep it in main: deeper, it would skip graceful shutdown.
	if err := run(); err != nil {
		log.Fatal().Err(err).Msg("server exited with an error")
	}
}

func run() error {
	webRoot, err := fs.Sub(webFS, "web/dist")
	if err != nil {
		return fmt.Errorf("web dist embed: %w", err)
	}

	return server.Run(context.Background(), server.Config{
		Port: ":3030",
		// Empty DBFile: server.Run resolves DB_FILE.
		WebRoot: http.FS(webRoot),
		Version: version,
		Commit:  commit,
		Date:    date,
	})
}
