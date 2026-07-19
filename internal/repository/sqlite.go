package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"moviepickarr/internal/db"
	"moviepickarr/internal/domain"
)

type rowScanner interface {
	Scan(dest ...any) error
}

// unixTimePtr converts a scanned epoch-seconds column to *time.Time (UTC).
func unixTimePtr(value sql.NullInt64) *time.Time {
	if !value.Valid {
		return nil
	}
	t := db.FromUnix(value.Int64)
	return &t
}

func scanUser(scanner rowScanner) (*domain.User, error) {
	user := &domain.User{}
	var createdAt sql.NullInt64
	var updatedAt sql.NullInt64

	if err := scanner.Scan(&user.ID, &user.Name, &createdAt, &updatedAt); err != nil {
		return nil, err
	}

	user.CreatedAt = unixTimePtr(createdAt)
	user.UpdatedAt = unixTimePtr(updatedAt)

	return user, nil
}

func scanMovie(scanner rowScanner) (*domain.Movie, error) {
	movie := &domain.Movie{}
	var addedAt sql.NullInt64
	var watchedAt sql.NullInt64
	var tmdbID sql.NullInt64
	var imdbID sql.NullString

	if err := scanner.Scan(
		&movie.ID,
		&movie.Title,
		&movie.Status,
		&addedAt,
		&movie.AddedByID,
		&movie.AddedByName,
		&watchedAt,
		&tmdbID,
		&imdbID,
	); err != nil {
		return nil, err
	}

	movie.AddedAt = unixTimePtr(addedAt)
	movie.WatchedAt = unixTimePtr(watchedAt)
	if tmdbID.Valid {
		v := int(tmdbID.Int64)
		movie.TMDBID = &v
	}
	if imdbID.Valid {
		movie.IMDbID = &imdbID.String
	}

	return movie, nil
}

type SqliteUserRepository struct {
	pool *db.Pool
}

func NewSqliteUserRepository(pool *db.Pool) *SqliteUserRepository {
	return &SqliteUserRepository{pool: pool}
}

func (d *SqliteUserRepository) FindByID(ctx context.Context, id int) (*domain.User, error) {
	// Active read: archived members are off the roster, so a lookup by id skips
	// them too (they resurface only via Restore, which reads the row directly).
	query := "SELECT id, name, created_at, updated_at FROM users WHERE id = ? AND archived_at IS NULL"

	user, err := scanUser(d.pool.Read.QueryRowContext(ctx, query, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("%w: user id %d", domain.ErrNotFound, id)
	}
	if err != nil {
		return nil, err
	}

	return user, nil
}

func (d *SqliteUserRepository) List(ctx context.Context) ([]*domain.User, error) {
	// The roster is active members only: archived members keep their row for
	// watch-history attribution but never show up in a live read (this backs the
	// board, stats, and the rotation candidate list alike).
	query := "SELECT id, name, created_at, updated_at FROM users WHERE archived_at IS NULL ORDER BY created_at ASC"

	rows, err := d.pool.Read.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	users := make([]*domain.User, 0)
	for rows.Next() {
		user, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		users = append(users, user)
	}

	return users, nil
}

func (d *SqliteUserRepository) Create(ctx context.Context, name string) (*domain.User, error) {
	query := "INSERT INTO users (name) VALUES (?)"

	result, err := d.pool.Write.ExecContext(ctx, query, name)
	if err != nil {
		return nil, err
	}

	id, err := result.LastInsertId()
	if err != nil {
		return nil, err
	}

	return d.FindByID(ctx, int(id))
}

// Remove deletes or archives a member as one action, chosen inside a single
// write transaction by whether they authored any movies. The whole decision runs
// under one tx so the movie count and the delete/archive it drives can't race a
// concurrent movie add against the ON DELETE RESTRICT constraint.
func (d *SqliteUserRepository) Remove(ctx context.Context, id int) (domain.RemoveOutcome, error) {
	tx, err := d.pool.Write.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer func() { _ = tx.Rollback() }()

	// Existence is the movies-join's problem otherwise, so check it up front: a
	// missing member is a 404, not a silent no-op.
	var exists int
	if err := tx.QueryRowContext(ctx, "SELECT 1 FROM users WHERE id = ?", id).Scan(&exists); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", fmt.Errorf("%w: user id %d", domain.ErrNotFound, id)
		}
		return "", err
	}

	var authored int
	if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM movies WHERE added_by_id = ?", id).Scan(&authored); err != nil {
		return "", err
	}

	outcome, err := removeMember(ctx, tx, id, authored)
	if err != nil {
		return "", err
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	return outcome, nil
}

// removeMember runs the chosen removal path against an open tx. Zero authored
// movies hard-deletes the row (FK cascade clears credentials/sessions/invites,
// next_up nulls, name freed); one or more archives it (archived_at set, then the
// login rows explicitly deleted so login dies) while the users row and its movie
// attribution survive.
func removeMember(ctx context.Context, tx *sql.Tx, id, authored int) (domain.RemoveOutcome, error) {
	if authored == 0 {
		if _, err := tx.ExecContext(ctx, "DELETE FROM users WHERE id = ?", id); err != nil {
			return "", err
		}
		return domain.OutcomeDeleted, nil
	}

	if _, err := tx.ExecContext(ctx,
		"UPDATE users SET archived_at = unixepoch(), updated_at = unixepoch() WHERE id = ?", id,
	); err != nil {
		return "", err
	}
	// The users row stays for attribution, so nothing cascades: strip every login
	// row by hand so the archived member cannot authenticate.
	for _, stmt := range []string{
		"DELETE FROM local_accounts WHERE user_id = ?",
		"DELETE FROM oidc_identities WHERE user_id = ?",
		"DELETE FROM sessions WHERE user_id = ?",
		"DELETE FROM invites WHERE user_id = ?",
	} {
		if _, err := tx.ExecContext(ctx, stmt, id); err != nil {
			return "", err
		}
	}
	return domain.OutcomeArchived, nil
}

// Restore reactivates an archived member by clearing archived_at. The
// archived_at IS NOT NULL guard makes a restore of an active or non-existent
// member a no-op that surfaces as ErrNotFound: there is nothing to restore.
func (d *SqliteUserRepository) Restore(ctx context.Context, id int) error {
	query := "UPDATE users SET archived_at = NULL, updated_at = unixepoch() WHERE id = ? AND archived_at IS NOT NULL"

	result, err := d.pool.Write.ExecContext(ctx, query, id)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return fmt.Errorf("%w: no archived member %d", domain.ErrNotFound, id)
	}
	return nil
}

type SqliteMoviesRepository struct {
	pool *db.Pool
}

func NewSqliteMoviesRepository(pool *db.Pool) *SqliteMoviesRepository {
	return &SqliteMoviesRepository{pool: pool}
}

// movieSelect is THE movies projection: every movie read starts from this
// exact select (movie columns + the adder's name) and scans via scanMovie.
// Adding a movie column is one edit here plus one in scanMovie; the query
// methods below only append their WHERE/ORDER BY tails.
const movieSelect = `
	SELECT
		m.id,
		m.title,
		m.status,
		m.added_at,
		m.added_by_id,
		u.name,
		m.watched_at,
		m.tmdb_id,
		m.imdb_id
	FROM movies m
	JOIN users u ON m.added_by_id = u.id`

// queryMovies runs a movieSelect-based query and scans the full result set.
func (d *SqliteMoviesRepository) queryMovies(ctx context.Context, query string, args ...any) ([]*domain.Movie, error) {
	rows, err := d.pool.Read.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	movies := make([]*domain.Movie, 0)
	for rows.Next() {
		movie, err := scanMovie(rows)
		if err != nil {
			return nil, err
		}
		movies = append(movies, movie)
	}

	return movies, rows.Err()
}

func (d *SqliteMoviesRepository) FindByID(ctx context.Context, id int) (*domain.Movie, error) {
	movie, err := scanMovie(d.pool.Read.QueryRowContext(ctx, movieSelect+" WHERE m.id = ?", id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("%w: movie id %d", domain.ErrNotFound, id)
	}
	if err != nil {
		return nil, err
	}

	return movie, nil
}

func (d *SqliteMoviesRepository) List(ctx context.Context) ([]*domain.Movie, error) {
	return d.queryMovies(ctx, movieSelect+" ORDER BY title DESC")
}

func (d *SqliteMoviesRepository) FindByUserID(ctx context.Context, userID int) ([]*domain.Movie, error) {
	return d.queryMovies(ctx, movieSelect+" WHERE m.added_by_id = ? ORDER BY title DESC", userID)
}

func (d *SqliteMoviesRepository) FindByStatus(ctx context.Context, status string) ([]*domain.Movie, error) {
	// The watched library reads in watch-recency order; everything else is a
	// plain title sort.
	order := " ORDER BY title"
	if status == "watched" {
		order = " ORDER BY m.watched_at DESC, m.title"
	}
	return d.queryMovies(ctx, movieSelect+" WHERE m.status = ?"+order, status)
}

func (d *SqliteMoviesRepository) FindByUserIDAndStatus(ctx context.Context, userID int, status string) ([]*domain.Movie, error) {
	return d.queryMovies(ctx, movieSelect+" WHERE m.added_by_id = ? AND m.status = ? ORDER BY title", userID, status)
}

func (d *SqliteMoviesRepository) CountByStatus(ctx context.Context, status string) (int, error) {
	query := "SELECT COUNT(*) FROM movies WHERE status = ?"

	var count int
	err := d.pool.Read.QueryRowContext(ctx, query, status).Scan(&count)
	if err != nil {
		return 0, err
	}

	return count, nil
}

func (d *SqliteMoviesRepository) CountByUserIDAndStatus(ctx context.Context, userID int, status string) (int, error) {
	query := "SELECT COUNT(*) FROM movies WHERE status = ? AND added_by_id = ?"

	var count int
	err := d.pool.Read.QueryRowContext(ctx, query, status, userID).Scan(&count)
	if err != nil {
		return 0, err
	}

	return count, nil
}

func (d *SqliteMoviesRepository) GetRandomPooled(ctx context.Context) (*domain.Movie, error) {
	row := d.pool.Read.QueryRowContext(ctx, movieSelect+" WHERE m.status = 'pool' ORDER BY RANDOM() LIMIT 1")
	movie, err := scanMovie(row)
	if err != nil {
		return nil, err
	}

	return movie, nil
}

func (d *SqliteMoviesRepository) GetCurrent(ctx context.Context) (*domain.Movie, error) {
	row := d.pool.Read.QueryRowContext(ctx, movieSelect+" WHERE m.status = 'current' LIMIT 1")
	movie, err := scanMovie(row)
	if err != nil {
		return nil, err
	}

	return movie, nil
}

func (d *SqliteMoviesRepository) Add(ctx context.Context, title, status string, userID int) (*domain.Movie, error) {
	query := `
		INSERT INTO movies (title, status, added_by_id)
		VALUES (?, ?, ?)
	`

	result, err := d.pool.Write.ExecContext(ctx, query, title, status, userID)
	if err != nil {
		return nil, err
	}

	id, err := result.LastInsertId()
	if err != nil {
		return nil, err
	}

	return d.FindByID(ctx, int(id))
}

func (d *SqliteMoviesRepository) SetExternalIDs(ctx context.Context, id int, tmdbID *int, imdbID *string) error {
	query := "UPDATE movies SET tmdb_id = ?, imdb_id = ? WHERE id = ?"

	result, err := d.pool.Write.ExecContext(ctx, query, tmdbID, imdbID, id)
	if err != nil {
		// movies_tmdb_id_unique: another row already carries this TMDB id.
		if db.IsUniqueViolation(err) {
			return fmt.Errorf("%w: another movie already has this tmdb id", domain.ErrConflict)
		}
		return err
	}

	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return sql.ErrNoRows
	}

	return nil
}

func (d *SqliteMoviesRepository) UpdateTitle(ctx context.Context, id int, title string) error {
	query := "UPDATE movies SET title = ? WHERE id = ?"

	result, err := d.pool.Write.ExecContext(ctx, query, title, id)
	if err != nil {
		return err
	}

	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return sql.ErrNoRows
	}

	return nil
}

func (d *SqliteMoviesRepository) UpdateWatchedAt(ctx context.Context, id int, watchedAt time.Time) error {
	query := "UPDATE movies SET watched_at = ? WHERE id = ?"

	result, err := d.pool.Write.ExecContext(ctx, query, db.ToUnix(watchedAt), id)
	if err != nil {
		return err
	}

	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return sql.ErrNoRows
	}

	return nil
}

func (d *SqliteMoviesRepository) UpdateStatus(ctx context.Context, id int, status string) error {
	query := "UPDATE movies SET status = ? WHERE id = ?"

	result, err := d.pool.Write.ExecContext(ctx, query, status, id)
	if err != nil {
		return err
	}

	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return sql.ErrNoRows
	}

	return nil
}

func (d *SqliteMoviesRepository) UpdateStatusIf(ctx context.Context, id int, to, from string) (int64, error) {
	query := "UPDATE movies SET status = ? WHERE id = ? AND status = ?"

	res, err := d.pool.Write.ExecContext(ctx, query, to, id, from)
	if err != nil {
		return 0, err
	}

	return res.RowsAffected()
}

// PromoteToPoolIfRoom flips a stashed movie to "pool" in a single statement,
// gated on the owner's current pool count via a correlated subquery (the owner
// is derived from the movie row itself). Because it is one atomic UPDATE, two
// concurrent promotions cannot both pass a stale count and overshoot maxPool.
func (d *SqliteMoviesRepository) PromoteToPoolIfRoom(ctx context.Context, id, maxPool int) (int64, error) {
	query := `
		UPDATE movies
		SET status = 'pool'
		WHERE id = ?
			AND status = 'stash'
			AND (
				SELECT COUNT(*)
				FROM movies AS p
				WHERE p.added_by_id = movies.added_by_id AND p.status = 'pool'
			) < ?
	`

	res, err := d.pool.Write.ExecContext(ctx, query, id, maxPool)
	if err != nil {
		return 0, err
	}

	return res.RowsAffected()
}

func (d *SqliteMoviesRepository) MarkAsWatched(ctx context.Context, id int, watchedAt time.Time) error {
	query := "UPDATE movies SET status = 'watched', watched_at = ? WHERE id = ?"

	result, err := d.pool.Write.ExecContext(ctx, query, db.ToUnix(watchedAt), id)
	if err != nil {
		return err
	}

	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return sql.ErrNoRows
	}

	return nil
}

func (d *SqliteMoviesRepository) Delete(ctx context.Context, id int) error {
	query := "DELETE FROM movies WHERE id = ?"

	result, err := d.pool.Write.ExecContext(ctx, query, id)
	if err != nil {
		return err
	}

	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return sql.ErrNoRows
	}

	return nil
}

type SqliteNextUpRepository struct {
	pool *db.Pool
}

func NewSqliteNextUpRepository(pool *db.Pool) *SqliteNextUpRepository {
	return &SqliteNextUpRepository{pool: pool}
}

func (d *SqliteNextUpRepository) Get(ctx context.Context) (*domain.User, error) {
	query := `
		SELECT 
		    u.id,
			u.name,
			u.created_at,
			u.updated_at
		FROM next_up n
		JOIN users u ON n.user_id = u.id AND u.archived_at IS NULL
		WHERE n.id = 1
		LIMIT 1
	`

	user, err := scanUser(d.pool.Read.QueryRowContext(ctx, query))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, sql.ErrNoRows
	}

	return user, nil
}

func (d *SqliteNextUpRepository) Set(ctx context.Context, userID int) error {
	query := `
		INSERT INTO next_up (id, user_id)
		VALUES (1, ?)
		ON CONFLICT(id) DO UPDATE SET user_id = excluded.user_id
	`
	_, err := d.pool.Write.ExecContext(ctx, query, userID)
	if err != nil {
		return err
	}

	return nil
}

type SqliteSettingsRepository struct {
	pool *db.Pool
}

func NewSqliteSettingsRepository(pool *db.Pool) *SqliteSettingsRepository {
	return &SqliteSettingsRepository{pool: pool}
}

func (d *SqliteSettingsRepository) List(ctx context.Context) ([]*domain.Settings, error) {
	query := "SELECT key, value FROM settings"

	rows, err := d.pool.Read.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	settings := make([]*domain.Settings, 0)
	for rows.Next() {
		setting := &domain.Settings{}
		err := rows.Scan(&setting.Key, &setting.Value)
		if err != nil {
			return nil, err
		}
		settings = append(settings, setting)
	}

	return settings, nil
}

func (d *SqliteSettingsRepository) FindByKey(ctx context.Context, key string) (string, error) {
	query := "SELECT value FROM settings WHERE key = ?"

	var value string
	err := d.pool.Read.QueryRowContext(ctx, query, key).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("%w: setting %s", domain.ErrNotFound, key)
	}
	if err != nil {
		return "", err
	}

	return value, nil
}

func (d *SqliteSettingsRepository) Set(ctx context.Context, key string, value string) error {
	query := `
		INSERT INTO settings (key, value)
		VALUES (?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value
	`
	_, err := d.pool.Write.ExecContext(ctx, query, key, value)
	if err != nil {
		return err
	}

	return nil
}
