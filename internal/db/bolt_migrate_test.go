package db

import (
	"context"
	"encoding/json"
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
			PoolLocked:   true,
			NextPickerID: "user-1",
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
	if err := sqliteDB.QueryRow("SELECT COUNT(*) FROM users").Scan(&userCount); err != nil {
		t.Fatalf("user count: %v", err)
	}
	if userCount != 2 {
		t.Fatalf("expected 2 users, got %d", userCount)
	}

	var movieCount int
	if err := sqliteDB.QueryRow("SELECT COUNT(*) FROM movies").Scan(&movieCount); err != nil {
		t.Fatalf("movie count: %v", err)
	}
	if movieCount != 4 {
		t.Fatalf("expected 4 movies, got %d", movieCount)
	}

	var poolLocked string
	if err := sqliteDB.QueryRow("SELECT value FROM settings WHERE key = 'pool_locked'").Scan(&poolLocked); err != nil {
		t.Fatalf("pool_locked: %v", err)
	}
	if poolLocked != "true" {
		t.Fatalf("expected pool_locked true, got %q", poolLocked)
	}

	var nextPickerID int
	if err := sqliteDB.QueryRow("SELECT user_id FROM next_picker WHERE id = 1").Scan(&nextPickerID); err != nil {
		t.Fatalf("next_picker: %v", err)
	}
	if nextPickerID != 1 {
		t.Fatalf("expected next_picker user_id 1, got %d", nextPickerID)
	}
}
