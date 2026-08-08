package db

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
)

func TestMigration016BackfillsCurrentMovieAsRevealedAcquisition(t *testing.T) {
	ctx := context.Background()
	pool, err := OpenSQLite(filepath.Join(t.TempDir(), "radarr-backfill.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = pool.Close() }()

	applyThrough(t, ctx, pool.Write, 15)
	if _, err := pool.Write.ExecContext(ctx,
		`INSERT INTO users (id, name) VALUES (1, 'Alice')`); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	if _, err := pool.Write.ExecContext(ctx, `
		INSERT INTO movies (id, title, status, added_by_id, tmdb_id, imdb_id)
		VALUES (7, 'Heat', 'current', 1, 949, 'tt0113277')
	`); err != nil {
		t.Fatalf("insert current movie: %v", err)
	}

	applyOne(t, ctx, pool.Write, 16, "016_radarr.sql")

	var (
		movieID         int
		status          string
		actionReason    sql.NullString
		actionVersion   int
		actionStartedAt sql.NullInt64
		title           string
		tmdbID          sql.NullInt64
		imdbID          sql.NullString
		identitySource  sql.NullString
		drawnAt         int64
		revealAt        int64
		drawClientID    string
		revealedAt      sql.NullInt64
	)
	err = pool.Read.QueryRowContext(ctx, `
		SELECT movie_id, status, action_reason, action_version, action_started_at,
		       movie_title, tmdb_id, imdb_id, identity_source,
		       drawn_at, reveal_at, draw_client_id, revealed_at
		FROM radarr_acquisitions
	`).Scan(
		&movieID,
		&status,
		&actionReason,
		&actionVersion,
		&actionStartedAt,
		&title,
		&tmdbID,
		&imdbID,
		&identitySource,
		&drawnAt,
		&revealAt,
		&drawClientID,
		&revealedAt,
	)
	if err != nil {
		t.Fatalf("read backfilled acquisition: %v", err)
	}

	if movieID != 7 || status != "needs_preset" || title != "Heat" {
		t.Fatalf("backfill identity = movie=%d status=%q title=%q", movieID, status, title)
	}
	if !actionReason.Valid || actionReason.String != "preset_required" || actionVersion != 1 {
		t.Fatalf(
			"backfill action = reason=%v version=%d, want preset_required version 1",
			actionReason,
			actionVersion,
		)
	}
	if !tmdbID.Valid || tmdbID.Int64 != 949 || !imdbID.Valid || imdbID.String != "tt0113277" {
		t.Fatalf("backfill provider ids = tmdb=%v imdb=%v", tmdbID, imdbID)
	}
	if !identitySource.Valid || identitySource.String != "tmdb" {
		t.Fatalf("identity source = %v, want tmdb", identitySource)
	}
	if drawClientID != "" {
		t.Fatalf("legacy draw client id = %q, want empty", drawClientID)
	}
	if drawnAt < 1_000_000_000_000 || revealAt != drawnAt {
		t.Fatalf("draw times = drawn=%d reveal=%d, want equal Unix milliseconds", drawnAt, revealAt)
	}
	if !revealedAt.Valid || revealedAt.Int64 != drawnAt {
		t.Fatalf("revealed_at = %v, want %d", revealedAt, drawnAt)
	}
	if !actionStartedAt.Valid || actionStartedAt.Int64 != drawnAt {
		t.Fatalf("action_started_at = %v, want %d", actionStartedAt, drawnAt)
	}
}

func TestMigration016SchemaAndActiveNameConstraints(t *testing.T) {
	ctx := context.Background()
	pool, err := OpenSQLite(filepath.Join(t.TempDir(), "radarr-schema.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = pool.Close() }()
	if err := RunMigrations(ctx, pool.Write); err != nil {
		t.Fatalf("run migrations: %v", err)
	}

	mustExec := func(query string, args ...any) {
		t.Helper()
		if _, err := pool.Write.ExecContext(ctx, query, args...); err != nil {
			t.Fatalf("exec %q: %v", query, err)
		}
	}
	wantConstraint := func(name, query string, args ...any) {
		t.Helper()
		if _, err := pool.Write.ExecContext(ctx, query, args...); err == nil {
			t.Errorf("%s: expected constraint error", name)
		}
	}

	for _, table := range []string{
		"radarr_instances",
		"radarr_presets",
		"radarr_acquisitions",
		"radarr_webhook_destinations",
		"radarr_webhook_deliveries",
	} {
		var strict int
		if err := pool.Read.QueryRowContext(ctx,
			`SELECT strict FROM pragma_table_list WHERE name = ?`, table).Scan(&strict); err != nil {
			t.Fatalf("read strict flag for %s: %v", table, err)
		}
		if strict != 1 {
			t.Errorf("table %s strict = %d, want 1", table, strict)
		}
	}

	mustExec(`
		INSERT INTO radarr_instances
		    (id, name, base_url, encrypted_api_key, last_checked_at)
		VALUES (1, 'Movies', 'http://radarr.test', ?, 100)
	`, []byte{1})
	wantConstraint("active instance name is unique without case",
		`INSERT INTO radarr_instances
		    (name, base_url, encrypted_api_key, last_checked_at)
		 VALUES ('movies', 'http://other.test', ?, 100)`, []byte{2})
	mustExec(`UPDATE radarr_instances SET archived_at = created_at WHERE id = 1`)
	mustExec(`
		INSERT INTO radarr_instances
		    (id, name, base_url, encrypted_api_key, last_checked_at)
		VALUES (2, 'movies', 'http://other.test', ?, 100)
	`, []byte{2})

	mustExec(`
		INSERT INTO radarr_presets (
		    id, name, instance_id, root_folder_id, root_folder_path,
		    quality_profile_id, quality_profile_name, tags,
		    minimum_availability, acquisition_mode, valid, validated_at
		) VALUES (
		    1, '1080p', 2, 4, '/movies', 6, 'HD', '[2,3]',
		    'inCinemas', 'manual', 1, 100
		)
	`)
	wantConstraint("Radarr wire availability is exact", `
		INSERT INTO radarr_presets (
		    name, instance_id, root_folder_id, root_folder_path,
		    quality_profile_id, quality_profile_name, tags,
		    minimum_availability, acquisition_mode, valid, validated_at
		) VALUES (
		    'Invalid', 2, 4, '/movies', 6, 'HD', '[]',
		    'in_cinemas', 'manual', 1, 100
		)
	`)
	wantConstraint("preset tags must be a JSON array", `
		INSERT INTO radarr_presets (
		    name, instance_id, root_folder_id, root_folder_path,
		    quality_profile_id, quality_profile_name, tags,
		    minimum_availability, acquisition_mode, valid, validated_at
		) VALUES (
		    'Invalid JSON', 2, 4, '/movies', 6, 'HD', '{}',
		    'released', 'manual', 1, 100
		)
	`)

	mustExec(`
		INSERT INTO radarr_webhook_destinations (
		    id, name, kind, encrypted_url, reason_filters, enabled, verified_at
		) VALUES (1, 'Discord', 'discord', ?, '["preset_required"]', 1, 100)
	`, []byte{3})
	wantConstraint("enabled webhook must be verified", `
		INSERT INTO radarr_webhook_destinations
		    (name, kind, encrypted_url, enabled)
		VALUES ('Unverified', 'generic', ?, 1)
	`, []byte{4})
	wantConstraint("active webhook name is unique without case", `
		INSERT INTO radarr_webhook_destinations
		    (name, kind, encrypted_url)
		VALUES ('discord', 'generic', ?)
	`, []byte{5})
	mustExec(`UPDATE radarr_webhook_destinations SET enabled = 0, archived_at = created_at WHERE id = 1`)
	mustExec(`
		INSERT INTO radarr_webhook_destinations
		    (name, kind, encrypted_url)
		VALUES ('discord', 'generic', ?)
	`, []byte{5})
}
