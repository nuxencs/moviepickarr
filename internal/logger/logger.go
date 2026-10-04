// Package logger builds the root zerolog logger from environment
// configuration. Components derive sub-loggers with a "component" field.
package logger

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/mattn/go-isatty"
	"github.com/rs/zerolog"
)

// Config selects the logger's behaviour. The zero value is JSON at info level.
type Config struct {
	// Level is a zerolog level name. Empty or unknown means info.
	Level string
	// Format is "console" for the human-readable dev writer, else JSON.
	Format string
}

// FromEnv reads LOG_LEVEL and LOG_FORMAT.
func FromEnv() Config {
	return Config{
		Level:  os.Getenv("LOG_LEVEL"),
		Format: os.Getenv("LOG_FORMAT"),
	}
}

// New builds the root logger and sets zerolog's global level. Call it once.
func New(cfg Config) zerolog.Logger {
	zerolog.SetGlobalLevel(parseLevel(cfg.Level))

	if strings.EqualFold(cfg.Format, "console") {
		// Only console mode renders the caller, so this global affects dev output only.
		zerolog.CallerMarshalFunc = shortCaller
		cw := zerolog.ConsoleWriter{
			Out:        os.Stderr,
			TimeFormat: "15:04:05.000",
			// Keep escape codes out of piped or captured logs.
			NoColor: !isatty.IsTerminal(os.Stderr.Fd()),
		}
		return zerolog.New(cw).With().Timestamp().Caller().Logger()
	}

	// No caller in production: keeps lines lean and the hot path allocation-free.
	return zerolog.New(os.Stderr).With().Timestamp().Logger()
}

func shortCaller(_ uintptr, file string, line int) string {
	return filepath.Base(file) + ":" + strconv.Itoa(line)
}

func parseLevel(s string) zerolog.Level {
	s = strings.TrimSpace(strings.ToLower(s))
	if s == "" {
		return zerolog.InfoLevel
	}
	lvl, err := zerolog.ParseLevel(s)
	if err != nil {
		return zerolog.InfoLevel
	}
	return lvl
}
