// Package logger builds the application's root zerolog logger from environment
// configuration. It is the single place that decides output format (JSON vs the
// human-readable console writer), the log level, and the per-event decorations
// (timestamp, and — in console mode only — the calling file:line). Components
// derive their own sub-loggers from the returned logger with
// logger.With().Str("component", ...).Logger().
package logger

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/mattn/go-isatty"
	"github.com/rs/zerolog"
)

// Config selects the logger's behaviour. The zero value (empty strings) yields
// the production default: JSON output at info level.
type Config struct {
	// Level is one of trace|debug|info|warn|error|fatal (case-insensitive).
	// Empty or unrecognised falls back to info.
	Level string
	// Format is "console" for the colourised, human-readable writer (local dev)
	// or "json" (default) for one structured object per line (production).
	Format string
}

// FromEnv reads LOG_LEVEL and LOG_FORMAT. godotenv has already populated the
// process environment from .env by the time Run calls this.
func FromEnv() Config {
	return Config{
		Level:  os.Getenv("LOG_LEVEL"),
		Format: os.Getenv("LOG_FORMAT"),
	}
}

// New builds the root logger and applies the parsed level as zerolog's global
// floor. Call it exactly once, at startup.
func New(cfg Config) zerolog.Logger {
	zerolog.SetGlobalLevel(parseLevel(cfg.Level))

	if strings.EqualFold(cfg.Format, "console") {
		// Short "file:line" instead of zerolog's full path. Only console mode
		// renders the caller, so this global affects dev output exclusively.
		zerolog.CallerMarshalFunc = shortCaller
		cw := zerolog.ConsoleWriter{
			Out:        os.Stderr,
			TimeFormat: "15:04:05.000",
			// Drop ANSI colour when stderr is not a terminal (piped to a file,
			// CI, journald) so captured logs stay free of escape codes.
			NoColor: !isatty.IsTerminal(os.Stderr.Fd()),
		}
		return zerolog.New(cw).With().Timestamp().Caller().Logger()
	}

	// Production: raw JSON to stderr — zero-allocation and machine-parseable.
	// No caller: keeps lines lean and the hot path allocation-free.
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
