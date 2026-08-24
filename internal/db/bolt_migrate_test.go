package db

import (
	"context"
	"database/sql"
	"encoding/json/v2"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	bolt "go.etcd.io/bbolt"
	_ "modernc.org/sqlite"
)

func TestMigrateBoltToSQLite(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	boltPath := filepath.Join(dir, "moviepickarr.db")

	boltDB, err := bolt.Open(boltPath, 0o600, nil)
	if err != nil {
		t.Fatalf("open bolt: %v", err)
	}

	err = boltDB.Update(func(tx *bolt.Tx) error {
		usersBucket, err := tx.CreateBucketIfNotExists([]byte(boltUsersBucket))
		if err != nil {
			return err
		}
		watchedBucket, err := tx.CreateBucketIfNotExists([]byte(boltWatchedBucket))
		if err != nil {
			return err
		}
		currentBucket, err := tx.CreateBucketIfNotExists([]byte(boltNextToWatchBucket))
		if err != nil {
			return err
		}
		settingsBucket, err := tx.CreateBucketIfNotExists([]byte(boltSettingsBucket))
		if err != nil {
			return err
		}

		user1 := boltUser{
			ID:        "user-1",
			Name:      "Alpha",
			CreatedAt: time.Now().Add(-2 * time.Hour).UTC().Format(time.RFC3339),
			CurrentPool: map[string]boltMovie{
				"movie-1": {
					ID:          "movie-1",
					Title:       "Pool Movie",
					Link:        "https://example.com/pool",
					AddedAt:     time.Now().Add(-90 * time.Minute).UTC().Format(time.RFC3339),
					AddedByID:   "user-1",
					AddedByName: "Alpha",
				},
			},
			Stash: map[string]boltMovie{},
		}

		user2 := boltUser{
			ID:          "user-2",
			Name:        "Beta",
			CreatedAt:   time.Now().Add(-1 * time.Hour).UTC().Format(time.RFC3339),
			CurrentPool: map[string]boltMovie{},
			Stash: map[string]boltMovie{
				"movie-2": {
					ID:          "movie-2",
					Title:       "Stash Movie",
					Link:        "https://example.com/stash",
					AddedAt:     time.Now().Add(-50 * time.Minute).UTC().Format(time.RFC3339),
					AddedByID:   "user-2",
					AddedByName: "Beta",
				},
			},
		}

		encodedUser1, err := json.Marshal(user1)
		if err != nil {
			return err
		}
		encodedUser2, err := json.Marshal(user2)
		if err != nil {
			return err
		}

		if err := usersBucket.Put([]byte(user1.ID), encodedUser1); err != nil {
			return err
		}
		if err := usersBucket.Put([]byte(user2.ID), encodedUser2); err != nil {
			return err
		}

		watchedMovie := boltMovie{
			ID:          "movie-3",
			Title:       "Watched Movie",
			Link:        "https://example.com/watched",
			AddedAt:     time.Now().Add(-3 * time.Hour).UTC().Format(time.RFC3339),
			AddedByID:   "user-1",
			AddedByName: "Alpha",
			WatchedAt:   time.Now().Add(-30 * time.Minute).UTC().Format(time.RFC3339),
		}
		encodedWatched, err := json.Marshal(watchedMovie)
		if err != nil {
			return err
		}
		if err := watchedBucket.Put([]byte(watchedMovie.ID), encodedWatched); err != nil {
			return err
		}

		currentMovie := boltMovie{
			ID:          "movie-4",
			Title:       "Current Movie",
			Link:        "https://example.com/current",
			AddedAt:     time.Now().Add(-20 * time.Minute).UTC().Format(time.RFC3339),
			AddedByID:   "user-2",
			AddedByName: "Beta",
		}
		encodedCurrent, err := json.Marshal(currentMovie)
		if err != nil {
			return err
		}
		if err := currentBucket.Put([]byte("current"), encodedCurrent); err != nil {
			return err
		}

		settings := boltSettings{
			PoolLocked: true,
			NextUpID:   "user-1",
		}
		encodedSettings, err := json.Marshal(settings)
		if err != nil {
			return err
		}
		if err := settingsBucket.Put([]byte("global"), encodedSettings); err != nil {
			return err
		}

		return nil
	})
	if err != nil {
		_ = boltDB.Close()
		t.Fatalf("seed bolt: %v", err)
	}

	if err := boltDB.Close(); err != nil {
		t.Fatalf("close bolt: %v", err)
	}

	migrated, err := MigrateBoltToSQLite(ctx, boltPath, boltPath)
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if !migrated {
		t.Fatalf("expected migration to run")
	}

	backups, _ := filepath.Glob(boltPath + "*.bak")
	if len(backups) == 0 {
		t.Fatalf("expected bolt backup")
	}

	if _, err := os.Stat(boltPath); err != nil {
		t.Fatalf("sqlite missing: %v", err)
	}

	sqliteDB, err := OpenSQLite(boltPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer sqliteDB.Close()

	var userCount int
	if err := sqliteDB.Read.QueryRow("SELECT COUNT(*) FROM users").Scan(&userCount); err != nil {
		t.Fatalf("user count: %v", err)
	}
	if userCount != 2 {
		t.Fatalf("expected 2 users, got %d", userCount)
	}

	var movieCount int
	if err := sqliteDB.Read.QueryRow("SELECT COUNT(*) FROM movies").Scan(&movieCount); err != nil {
		t.Fatalf("movie count: %v", err)
	}
	if movieCount != 4 {
		t.Fatalf("expected 4 movies, got %d", movieCount)
	}

	var poolLocked string
	if err := sqliteDB.Read.QueryRow("SELECT value FROM settings WHERE key = 'pool_locked'").Scan(&poolLocked); err != nil {
		t.Fatalf("pool_locked: %v", err)
	}
	if poolLocked != "true" {
		t.Fatalf("expected pool_locked true, got %q", poolLocked)
	}

	var nextUpID int
	if err := sqliteDB.Read.QueryRow("SELECT user_id FROM next_up WHERE id = 1").Scan(&nextUpID); err != nil {
		t.Fatalf("next_up: %v", err)
	}
	if nextUpID != 1 {
		t.Fatalf("expected next_up user_id 1, got %d", nextUpID)
	}
}

func TestMigrateBoltToSQLite_DeterministicOrderAndDuplicateIMDbAudit(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	boltPath := filepath.Join(dir, "moviepickarr.db")

	boltDB, err := bolt.Open(boltPath, 0o600, nil)
	if err != nil {
		t.Fatalf("open bolt: %v", err)
	}
	err = boltDB.Update(func(tx *bolt.Tx) error {
		usersBucket, err := tx.CreateBucketIfNotExists([]byte(boltUsersBucket))
		if err != nil {
			return err
		}
		watchedBucket, err := tx.CreateBucketIfNotExists([]byte(boltWatchedBucket))
		if err != nil {
			return err
		}
		currentPool := make(map[string]boltMovie, 23)
		for i := 23; i >= 2; i-- {
			key := fmt.Sprintf("movie-%02d", i)
			currentPool[key] = boltMovie{
				ID:        key,
				Title:     fmt.Sprintf("Movie %02d", i),
				Link:      fmt.Sprintf("https://www.imdb.com/title/tt8%06d/", i),
				AddedAt:   "2026-01-01T03:00:00Z",
				AddedByID: "user-a",
			}
		}
		currentPool["movie-00"] = boltMovie{
			ID:        "movie-00",
			Title:     "Alpha",
			Link:      "https://www.imdb.com/title/TT7654321/",
			AddedAt:   "2026-01-01T01:00:00Z",
			AddedByID: "user-a",
		}
		userA := boltUser{
			ID:          "user-a",
			Name:        "First importer",
			CreatedAt:   "2026-01-01T00:00:00Z",
			CurrentPool: currentPool,
			Stash:       map[string]boltMovie{},
		}
		userB := boltUser{
			ID:        "user-b",
			Name:      "Second importer",
			CreatedAt: "2026-01-01T00:00:00Z",
			CurrentPool: map[string]boltMovie{
				"movie-01": {
					ID:        "movie-01",
					Title:     "Beta",
					Link:      "https://www.imdb.com/title/tt7654321/",
					AddedAt:   "2026-01-01T02:00:00Z",
					AddedByID: "user-b",
				},
			},
			Stash: map[string]boltMovie{},
		}
		encodedA, err := json.Marshal(userA)
		if err != nil {
			return err
		}
		encodedB, err := json.Marshal(userB)
		if err != nil {
			return err
		}
		// Bucket order opposes logical user id order. Equal CreatedAt values
		// must fall back to user id, not the Bolt record key.
		if err := usersBucket.Put([]byte("a-record"), encodedB); err != nil {
			return err
		}
		if err := usersBucket.Put([]byte("z-record"), encodedA); err != nil {
			return err
		}

		// The same legacy movie can appear again in a later phase. It must update
		// the first row's status, not create or audit another identity conflict.
		repeatedAlpha := currentPool["movie-00"]
		repeatedAlpha.WatchedAt = "2026-01-02T00:00:00Z"
		encodedWatched, err := json.Marshal(repeatedAlpha)
		if err != nil {
			return err
		}
		return watchedBucket.Put([]byte(repeatedAlpha.ID), encodedWatched)
	})
	if err != nil {
		_ = boltDB.Close()
		t.Fatalf("seed bolt: %v", err)
	}
	if err := boltDB.Close(); err != nil {
		t.Fatalf("close bolt: %v", err)
	}

	migrated, err := MigrateBoltToSQLite(ctx, boltPath, boltPath)
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if !migrated {
		t.Fatal("expected migration to run")
	}

	sqliteDB, err := OpenSQLite(boltPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer func() { _ = sqliteDB.Close() }()

	rows, err := sqliteDB.Read.QueryContext(ctx,
		`SELECT title, imdb_id FROM movies ORDER BY id`,
	)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	type importedMovie struct {
		title  string
		imdbID sql.NullString
	}
	var got []importedMovie
	for rows.Next() {
		var movie importedMovie
		if err := rows.Scan(&movie.title, &movie.imdbID); err != nil {
			t.Fatal(err)
		}
		got = append(got, movie)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if len(got) != 24 {
		t.Fatalf("imported movies = %+v, want 24", got)
	}
	if got[0].title != "Alpha" || !got[0].imdbID.Valid || got[0].imdbID.String != "tt7654321" {
		t.Fatalf("first imported movie = %+v, want Alpha with canonical IMDb id", got[0])
	}
	for i := 2; i < 24; i++ {
		gotIndex := i - 1
		wantTitle := fmt.Sprintf("Movie %02d", i)
		wantIMDb := fmt.Sprintf("tt8%06d", i)
		if got[gotIndex].title != wantTitle || !got[gotIndex].imdbID.Valid || got[gotIndex].imdbID.String != wantIMDb {
			t.Fatalf("imported movie %d = %+v, want %s/%s", gotIndex, got[gotIndex], wantTitle, wantIMDb)
		}
	}
	if got[23].title != "Beta" || got[23].imdbID.Valid {
		t.Fatalf("last imported movie = %+v, want Beta with NULL IMDb id", got[23])
	}

	var duplicateTitle, canonicalTitle, duplicateIMDb string
	if err := sqliteDB.Read.QueryRowContext(ctx, `
		SELECT duplicate.title, canonical.title, conflict.imdb_id
		FROM movie_imdb_conflicts conflict
		JOIN movies duplicate ON duplicate.id = conflict.movie_id
		JOIN movies canonical ON canonical.id = conflict.canonical_movie_id
	`).Scan(&duplicateTitle, &canonicalTitle, &duplicateIMDb); err != nil {
		t.Fatalf("read IMDb conflict audit: %v", err)
	}
	if duplicateTitle != "Beta" || canonicalTitle != "Alpha" || duplicateIMDb != "tt7654321" {
		t.Fatalf("IMDb conflict audit = %q/%q/%q, want Beta/Alpha/tt7654321",
			duplicateTitle, canonicalTitle, duplicateIMDb)
	}
	var alphaStatus string
	if err := sqliteDB.Read.QueryRowContext(ctx,
		`SELECT status FROM movies WHERE title = 'Alpha'`,
	).Scan(&alphaStatus); err != nil {
		t.Fatalf("read repeated movie status: %v", err)
	}
	if alphaStatus != "watched" {
		t.Fatalf("repeated legacy movie status = %q, want watched", alphaStatus)
	}
	var conflictCount int
	if err := sqliteDB.Read.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM movie_imdb_conflicts`,
	).Scan(&conflictCount); err != nil {
		t.Fatalf("count IMDb conflicts: %v", err)
	}
	if conflictCount != 1 {
		t.Fatalf("IMDb conflict audit rows = %d, want 1", conflictCount)
	}
}
