package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"regexp"
	"slices"
	"time"

	bolt "go.etcd.io/bbolt"
)

// boltIMDbIDRegex extracts the IMDb id from a legacy Bolt link. Duplicated here
// (rather than importing the server package) to respect the db→server layering.
var boltIMDbIDRegex = regexp.MustCompile(`tt\d{7,8}`)

type boltUser struct {
	ID          string               `json:"userID"`
	Name        string               `json:"name"`
	CurrentPool map[string]boltMovie `json:"currentPool"`
	Stash       map[string]boltMovie `json:"stash"`
	CreatedAt   string               `json:"createdAt"`
	Metadata    map[string]any       `json:"-"`
}

type boltMovie struct {
	ID          string `json:"movieID"`
	Title       string `json:"title"`
	Link        string `json:"link"`
	AddedAt     string `json:"addedAt"`
	AddedByID   string `json:"addedByID"`
	AddedByName string `json:"addedByName"`
	WatchedAt   string `json:"watchedAt"`
}

type boltSettings struct {
	PoolLocked bool `json:"poolLocked"`
	// JSON tags stay on the legacy Bolt keys (nextPickerID/nextPickerName): they
	// mirror what the pre-rename app wrote to disk, so the one-time import must
	// still read them. Only the Go-side names move to the next-up vocabulary.
	NextUpID   string `json:"nextPickerID"`
	NextUpName string `json:"nextPickerName"`
}

const (
	boltUsersBucket       = "users"
	boltWatchedBucket     = "watched_movies"
	boltNextToWatchBucket = "next_to_watch"
	boltSettingsBucket    = "settings"
)

func MigrateBoltToSQLite(ctx context.Context, boltPath, sqlitePath string) (bool, error) {
	exists, err := fileExists(boltPath)
	if err != nil || !exists {
		return false, err
	}

	isSQLite, err := isSQLiteFile(boltPath)
	if err != nil {
		return false, err
	}
	if isSQLite {
		return false, nil
	}

	tmpPath := sqlitePath + ".tmp"
	if exists, _ := fileExists(tmpPath); exists {
		tmpPath = fmt.Sprintf("%s.tmp-%d", sqlitePath, time.Now().Unix())
	}

	pool, err := OpenSQLite(tmpPath)
	if err != nil {
		return false, err
	}
	sqliteDB := pool.Write

	if err := RunMigrations(ctx, sqliteDB); err != nil {
		_ = pool.Close()
		return false, err
	}

	boltDB, err := bolt.Open(boltPath, 0o600, &bolt.Options{ReadOnly: true, Timeout: 1 * time.Second})
	if err != nil {
		_ = pool.Close()
		return false, err
	}

	migrationErr := boltDB.View(func(tx *bolt.Tx) error {
		usersBucket := tx.Bucket([]byte(boltUsersBucket))
		if usersBucket == nil {
			return nil
		}

		users, err := readBoltUsers(usersBucket)
		if err != nil {
			return err
		}

		slices.SortFunc(users, func(a, b boltUser) int {
			switch {
			case a.CreatedAt < b.CreatedAt:
				return -1
			case a.CreatedAt > b.CreatedAt:
				return 1
			default:
				return 0
			}
		})

		userIDMap := make(map[string]int, len(users))
		movieIDMap := make(map[string]int)
		defaultUserID := 0

		for _, user := range users {
			createdAt := parseTimeOrNow(user.CreatedAt)
			result, err := sqliteDB.ExecContext(
				ctx,
				"INSERT INTO users (name, created_at, updated_at) VALUES (?, ?, ?)",
				user.Name,
				ToUnix(createdAt),
				ToUnix(createdAt),
			)
			if err != nil {
				return err
			}

			id, err := result.LastInsertId()
			if err != nil {
				return err
			}

			newID := int(id)
			userIDMap[user.ID] = newID
			if defaultUserID == 0 {
				defaultUserID = newID
			}
		}

		for _, user := range users {
			newUserID := userIDMap[user.ID]
			if err := insertBoltMovies(ctx, sqliteDB, movieIDMap, userIDMap, defaultUserID, newUserID, user.CurrentPool, "pool"); err != nil {
				return err
			}
			if err := insertBoltMovies(ctx, sqliteDB, movieIDMap, userIDMap, defaultUserID, newUserID, user.Stash, "stash"); err != nil {
				return err
			}
		}

		if watchedBucket := tx.Bucket([]byte(boltWatchedBucket)); watchedBucket != nil {
			if err := watchedBucket.ForEach(func(_, v []byte) error {
				var movie boltMovie
				if err := json.Unmarshal(v, &movie); err != nil {
					return err
				}
				return upsertBoltMovie(ctx, sqliteDB, movieIDMap, userIDMap, defaultUserID, movie, "watched")
			}); err != nil {
				return err
			}
		}

		if currentBucket := tx.Bucket([]byte(boltNextToWatchBucket)); currentBucket != nil {
			data := currentBucket.Get([]byte("current"))
			if data != nil {
				var movie boltMovie
				if err := json.Unmarshal(data, &movie); err != nil {
					return err
				}
				if err := upsertBoltMovie(ctx, sqliteDB, movieIDMap, userIDMap, defaultUserID, movie, "current"); err != nil {
					return err
				}
			}
		}

		settings := boltSettings{}
		if settingsBucket := tx.Bucket([]byte(boltSettingsBucket)); settingsBucket != nil {
			if data := settingsBucket.Get([]byte("global")); data != nil {
				if err := json.Unmarshal(data, &settings); err != nil {
					return err
				}
			}
		}

		if _, err := sqliteDB.ExecContext(
			ctx,
			"INSERT INTO settings (key, value) VALUES ('pool_locked', ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value",
			fmt.Sprint(settings.PoolLocked),
		); err != nil {
			return err
		}

		if settings.NextUpID != "" {
			if nextID := userIDMap[settings.NextUpID]; nextID != 0 {
				if _, err := sqliteDB.ExecContext(ctx, "UPDATE next_up SET user_id = ? WHERE id = 1", nextID); err != nil {
					return err
				}
			}
		}

		return nil
	})

	_ = boltDB.Close()
	if migrationErr != nil {
		_ = pool.Close()
		return false, migrationErr
	}

	if err := pool.Close(); err != nil {
		return false, err
	}

	backupPath := boltPath + ".bak"
	if exists, _ := fileExists(backupPath); exists {
		backupPath = fmt.Sprintf("%s.%d.bak", boltPath, time.Now().Unix())
	}

	if err := os.Rename(boltPath, backupPath); err != nil {
		return false, err
	}

	if err := os.Rename(tmpPath, sqlitePath); err != nil {
		return false, err
	}

	return true, nil
}

func readBoltUsers(bucket *bolt.Bucket) ([]boltUser, error) {
	users := make([]boltUser, 0)
	err := bucket.ForEach(func(_, v []byte) error {
		var user boltUser
		if err := json.Unmarshal(v, &user); err != nil {
			return err
		}
		if user.CurrentPool == nil {
			user.CurrentPool = map[string]boltMovie{}
		}
		if user.Stash == nil {
			user.Stash = map[string]boltMovie{}
		}
		users = append(users, user)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return users, nil
}

func insertBoltMovies(
	ctx context.Context,
	db *sql.DB,
	movieIDMap map[string]int,
	userIDMap map[string]int,
	defaultUserID int,
	fallbackUserID int,
	movies map[string]boltMovie,
	status string,
) error {
	for _, movie := range movies {
		if err := upsertBoltMovie(ctx, db, movieIDMap, userIDMap, defaultUserID, movie, status); err != nil {
			return err
		}
	}
	return nil
}

func upsertBoltMovie(
	ctx context.Context,
	db *sql.DB,
	movieIDMap map[string]int,
	userIDMap map[string]int,
	defaultUserID int,
	movie boltMovie,
	status string,
) error {
	if existingID, ok := movieIDMap[movie.ID]; ok {
		_, err := db.ExecContext(ctx, "UPDATE movies SET status = ?, watched_at = ? WHERE id = ?", status, watchedAtFor(movie, status), existingID)
		return err
	}

	addedByID := userIDMap[movie.AddedByID]
	if addedByID == 0 {
		addedByID = defaultUserID
	}
	if addedByID == 0 {
		return fmt.Errorf("missing added_by_id for movie %s", movie.ID)
	}

	addedAt := parseTimeOrNow(movie.AddedAt)
	watchedAt := watchedAtFor(movie, status)

	// The link column was dropped (migration 005); identity lives in imdb_id,
	// derived from the legacy Bolt link. Enrichment fills the rest later.
	var imdbID *string
	if id := boltIMDbIDRegex.FindString(movie.Link); id != "" {
		imdbID = &id
	}

	result, err := db.ExecContext(
		ctx,
		"INSERT INTO movies (title, status, added_at, added_by_id, watched_at, imdb_id) VALUES (?, ?, ?, ?, ?, ?)",
		movie.Title,
		status,
		ToUnix(addedAt),
		addedByID,
		watchedAt,
		imdbID,
	)
	if err != nil {
		return err
	}

	id, err := result.LastInsertId()
	if err != nil {
		return err
	}

	movieIDMap[movie.ID] = int(id)
	return nil
}

// watchedAtFor yields a watched_at binding that satisfies the schema's
// status <-> watched_at CHECK: watched rows always get a time (the recorded
// one, else the added time, else now), everything else stores NULL.
func watchedAtFor(movie boltMovie, status string) *int64 {
	if status != "watched" {
		return nil
	}
	if t := parseTimePtr(movie.WatchedAt); t != nil {
		return ToUnixPtr(t)
	}
	t := parseTimeOrNow(movie.AddedAt)
	return ToUnixPtr(&t)
}

func parseTimePtr(value string) *time.Time {
	if value == "" {
		return nil
	}
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return nil
	}
	return &parsed
}

func parseTimeOrNow(value string) time.Time {
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return time.Now().UTC()
	}
	return parsed
}

func isSQLiteFile(path string) (bool, error) {
	file, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer file.Close()

	header := make([]byte, 16)
	n, err := io.ReadFull(file, header)
	if err != nil && err != io.ErrUnexpectedEOF {
		return false, err
	}

	if n < len(header) {
		return false, nil
	}

	return string(header) == "SQLite format 3\x00", nil
}

func fileExists(path string) (bool, error) {
	_, err := os.Stat(path)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}

// keep filepath import? ensure unused
