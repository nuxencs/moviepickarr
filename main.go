package main

import (
	"context"
	"embed"
	"io/fs"
	"log"
	"net/http"

	"moviepickarr/internal/server"
)

var (
	version = "dev"
	commit  = "dev"
	date    = "unknown"
)

//go:embed web/dist
var webFS embed.FS

func main() {
	webRoot, err := fs.Sub(webFS, "web/dist")
	if err != nil {
		log.Fatal(err)
	}

	if err := server.Run(context.Background(), server.Config{
		Port:    ":3030",
		DBFile:  "moviepickarr.db",
		WebRoot: http.FS(webRoot),
	}); err != nil {
		log.Fatal(err)
	}
}
