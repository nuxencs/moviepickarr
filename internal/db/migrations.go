package db

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

type migration struct {
	version int
	name    string
}

func RunMigrations(ctx context.Context, db *sql.DB) error {
	return RunMigrationsWithBackup(ctx, db, BackupConfig{})
}

// RunMigrationsWithBackup is RunMigrations plus a pre-migration safety net:
// when migrations are pending against a previously migrated database, it
// integrity-checks the file and snapshots it next to the DB before touching
// the schema. A fresh database (nothing applied yet) is never backed up —
// there is nothing to lose.
func RunMigrationsWithBackup(ctx context.Context, db *sql.DB, backup BackupConfig) error {
	if _, err := db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version INTEGER PRIMARY KEY
		)
	`); err != nil {
		return err
	}

	entries, err := fs.ReadDir(migrationsFS, "migrations")
	if err != nil {
		return err
	}

	migrations := make([]migration, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}

		version, ok := parseMigrationVersion(entry.Name())
		if !ok {
			return fmt.Errorf("invalid migration filename: %s", entry.Name())
		}

		migrations = append(migrations, migration{
			version: version,
			name:    entry.Name(),
		})
	}

	sort.Slice(migrations, func(i, j int) bool {
		return migrations[i].version < migrations[j].version
	})

	pending := make([]migration, 0, len(migrations))
	lastApplied := 0
	for _, m := range migrations {
		var applied int
		err := db.QueryRowContext(ctx, "SELECT 1 FROM schema_migrations WHERE version = ?", m.version).Scan(&applied)
		if err == nil {
			if m.version > lastApplied {
				lastApplied = m.version
			}
			continue
		}
		if err != sql.ErrNoRows {
			return err
		}
		pending = append(pending, m)
	}

	if len(pending) == 0 {
		return nil
	}

	if backup.MaxBackups > 0 && lastApplied > 0 {
		if err := backupBeforeMigrations(ctx, db, backup, lastApplied); err != nil {
			return err
		}
	}

	for _, m := range pending {
		content, err := fs.ReadFile(migrationsFS, filepath.Join("migrations", m.name))
		if err != nil {
			return err
		}

		// Tolerate a BOM or leading blank lines — missing the marker on a
		// rebuild migration would run its DROP TABLE with FKs on and cascade
		// into child tables, so detection must not hinge on exact first bytes.
		if strings.HasPrefix(strings.TrimLeft(string(content), "\uFEFF \t\r\n"), fkOffMarker) {
			err = applyMigrationFKOff(ctx, db, m, string(content))
		} else {
			err = applyMigration(ctx, db, m, string(content))
		}
		if err != nil {
			return err
		}
	}

	return nil
}

func applyMigration(ctx context.Context, db *sql.DB, m migration, content string) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}

	if _, err := tx.ExecContext(ctx, content); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("apply migration %s: %w", m.name, err)
	}

	if _, err := tx.ExecContext(ctx, "INSERT INTO schema_migrations (version) VALUES (?)", m.version); err != nil {
		_ = tx.Rollback()
		return err
	}

	return tx.Commit()
}

// fkOffMarker on a migration's first line means it rebuilds a table that other
// tables reference: DROP TABLE with foreign_keys=ON would cascade-delete the
// referencing rows. PRAGMA foreign_keys is a silent no-op inside a
// transaction, so the pragma must run on the pinned connection around the tx
// (SQLite's documented rebuild procedure). Before committing, a full
// foreign_key_check guards against the rebuild leaving dangling references.
const fkOffMarker = "-- migrate:fk_off"

func applyMigrationFKOff(ctx context.Context, db *sql.DB, m migration, content string) error {
	conn, err := db.Conn(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()

	if _, err := conn.ExecContext(ctx, "PRAGMA foreign_keys = OFF"); err != nil {
		return err
	}
	// Re-enable on every exit path; the connection returns to the pool.
	defer func() { _, _ = conn.ExecContext(ctx, "PRAGMA foreign_keys = ON") }()

	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}

	if _, err := tx.ExecContext(ctx, content); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("apply migration %s: %w", m.name, err)
	}

	var fkViolation string
	err = tx.QueryRowContext(ctx, "SELECT \"table\" FROM pragma_foreign_key_check LIMIT 1").Scan(&fkViolation)
	if err == nil {
		_ = tx.Rollback()
		return fmt.Errorf("apply migration %s: foreign key violation in table %s", m.name, fkViolation)
	}
	if err != sql.ErrNoRows {
		_ = tx.Rollback()
		return err
	}

	if _, err := tx.ExecContext(ctx, "INSERT INTO schema_migrations (version) VALUES (?)", m.version); err != nil {
		_ = tx.Rollback()
		return err
	}

	return tx.Commit()
}

func parseMigrationVersion(name string) (int, bool) {
	base := filepath.Base(name)
	end := 0
	for end < len(base) && base[end] >= '0' && base[end] <= '9' {
		end++
	}
	if end == 0 {
		return 0, false
	}

	version, err := strconv.Atoi(base[:end])
	if err != nil {
		return 0, false
	}

	return version, true
}
