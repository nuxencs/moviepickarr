package db

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/rs/zerolog"
	zlog "github.com/rs/zerolog/log"
)

// BackupConfig controls the pre-migration snapshot taken by
// RunMigrationsWithBackup.
type BackupConfig struct {
	// Path is the SQLite database file; backups are written alongside it as
	// <Path>.v<version>-<utc timestamp>.backup. Must be set when MaxBackups > 0.
	Path string
	// MaxBackups is how many backup files to retain (oldest pruned first).
	// 0 disables backups entirely.
	MaxBackups int
}

const backupSuffix = ".backup"

// backupLog tags this file's lines with component=db. Backups run during boot,
// before any component sub-logger exists, so they derive from the zerolog global
// rather than an injected logger. Derived at the top of a function, never inside
// a loop: the return is a fresh Logger each call, not a shared one.
func backupLog() zerolog.Logger {
	return zlog.With().Str("component", "db").Logger()
}

// backupTimeFormat sorts lexicographically, so retention can order backups by
// filename alone.
const backupTimeFormat = "20060102T150405Z"

func backupBeforeMigrations(ctx context.Context, db *sql.DB, cfg BackupConfig, lastApplied int) error {
	if cfg.Path == "" {
		return fmt.Errorf("db backup: path required when MaxBackups > 0")
	}

	// Never snapshot a corrupt file: a backup that cannot be restored is worse
	// than none, because it looks like a safety net.
	if err := checkIntegrity(ctx, db); err != nil {
		return err
	}

	target, err := createBackup(ctx, db, cfg.Path, lastApplied)
	if err != nil {
		return err
	}
	log := backupLog()
	log.Info().Str("file", target).Int("schema_version", lastApplied).
		Msg("database backed up before migrations")

	return cleanupBackups(cfg.Path, cfg.MaxBackups)
}

func checkIntegrity(ctx context.Context, db *sql.DB) error {
	rows, err := db.QueryContext(ctx, "PRAGMA integrity_check")
	if err != nil {
		return fmt.Errorf("db integrity check: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var problems []string
	for rows.Next() {
		var result string
		if err := rows.Scan(&result); err != nil {
			return fmt.Errorf("db integrity check: %w", err)
		}
		if result != "ok" {
			problems = append(problems, result)
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("db integrity check: %w", err)
	}

	if len(problems) > 0 {
		return fmt.Errorf("db integrity check failed: %s", strings.Join(problems, "; "))
	}
	return nil
}

func createBackup(ctx context.Context, db *sql.DB, path string, version int) (string, error) {
	target := fmt.Sprintf("%s.v%03d-%s%s",
		path, version, time.Now().UTC().Format(backupTimeFormat), backupSuffix)

	// VACUUM INTO writes a compacted, self-consistent copy in one statement and
	// refuses to overwrite an existing file — no partial backups on error.
	if _, err := db.ExecContext(ctx, "VACUUM INTO ?", target); err != nil {
		return "", fmt.Errorf("db backup to %s: %w", target, err)
	}
	return target, nil
}

func cleanupBackups(path string, maxBackups int) error {
	dir := filepath.Dir(path)
	prefix := filepath.Base(path) + ".v"

	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("db backup cleanup: %w", err)
	}

	var backups []string
	for _, entry := range entries {
		name := entry.Name()
		if !entry.IsDir() && strings.HasPrefix(name, prefix) && strings.HasSuffix(name, backupSuffix) {
			backups = append(backups, name)
		}
	}

	// Version is zero-padded and the timestamp is lexicographically ordered, so
	// a plain ascending name sort is oldest-first.
	slices.Sort(backups)

	log := backupLog()
	for _, name := range backups[:max(len(backups)-maxBackups, 0)] {
		if err := os.Remove(filepath.Join(dir, name)); err != nil {
			return fmt.Errorf("db backup cleanup: %w", err)
		}
		log.Info().Str("file", filepath.Join(dir, name)).
			Int("keep", maxBackups).
			Msg("pruned old database backup")
	}
	return nil
}
