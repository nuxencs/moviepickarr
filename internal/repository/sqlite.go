package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"moviepickarr/internal/domain"
)

type rowScanner interface {
	Scan(dest ...any) error
}

func nullTimePtr(value sql.NullTime) *time.Time {
	if !value.Valid {
		return nil
	}
	return &value.Time
}

func scanUser(scanner rowScanner) (*domain.User, error) {
	user := &domain.User{}
	var createdAt sql.NullTime
	var updatedAt sql.NullTime

	if err := scanner.Scan(&user.ID, &user.Name, &createdAt, &updatedAt); err != nil {
		return nil, err
	}

	user.CreatedAt = nullTimePtr(createdAt)
	user.UpdatedAt = nullTimePtr(updatedAt)

	return user, nil
}

func scanMovie(scanner rowScanner) (*domain.Movie, error) {
	movie := &domain.Movie{}
	var addedAt sql.NullTime
	var watchedAt sql.NullTime
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

	movie.AddedAt = nullTimePtr(addedAt)
	movie.WatchedAt = nullTimePtr(watchedAt)
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
	db *sql.DB
}

func NewSqliteUserRepository(db *sql.DB) *SqliteUserRepository {
	return &SqliteUserRepository{db: db}
}

func (d *SqliteUserRepository) FindByID(ctx context.Context, id int) (*domain.User, error) {
	query := "SELECT id, name, created_at, updated_at FROM users WHERE id = ?"

	user, err := scanUser(d.db.QueryRowContext(ctx, query, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("%w: user id %d", domain.ErrNotFound, id)
	}
	if err != nil {
		return nil, err
	}

	return user, nil
}

func (d *SqliteUserRepository) List(ctx context.Context) ([]*domain.User, error) {
	query := "SELECT id, name, created_at, updated_at FROM users ORDER BY created_at ASC"

	rows, err := d.db.QueryContext(ctx, query)
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

	result, err := d.db.ExecContext(ctx, query, name)
	if err != nil {
		return nil, err
	}

	id, err := result.LastInsertId()
	if err != nil {
		return nil, err
	}

	return d.FindByID(ctx, int(id))
}

func (d *SqliteUserRepository) Delete(ctx context.Context, id int) error {
	query := "DELETE FROM users WHERE id = ?"

	result, err := d.db.ExecContext(ctx, query, id)
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

type SqliteMoviesRepository struct {
	db *sql.DB
}

func NewSqliteMoviesRepository(db *sql.DB) *SqliteMoviesRepository {
	return &SqliteMoviesRepository{db: db}
}

func (d *SqliteMoviesRepository) FindByID(ctx context.Context, id int) (*domain.Movie, error) {
	query := `
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
		JOIN users u ON m.added_by_id = u.id
		WHERE m.id = ?
	`

	movie, err := scanMovie(d.db.QueryRowContext(ctx, query, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("%w: movie id %d", domain.ErrNotFound, id)
	}
	if err != nil {
		return nil, err
	}

	return movie, nil
}

func (d *SqliteMoviesRepository) List(ctx context.Context) ([]*domain.Movie, error) {
	query := `
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
		JOIN users u ON m.added_by_id = u.id
		ORDER BY title DESC
	`

	rows, err := d.db.QueryContext(ctx, query)
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
	return movies, nil
}

func (d *SqliteMoviesRepository) FindByUserID(ctx context.Context, userID int) ([]*domain.Movie, error) {
	query := `
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
		JOIN users u ON m.added_by_id = u.id
		WHERE m.added_by_id = ?
		ORDER BY title DESC
	`

	rows, err := d.db.QueryContext(ctx, query, userID)
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

	return movies, nil
}

func (d *SqliteMoviesRepository) FindByStatus(ctx context.Context, status string) ([]*domain.Movie, error) {
	query := `
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
		JOIN users u ON m.added_by_id = u.id
		WHERE m.status = ?
		ORDER BY title
	`
	if status == "watched" {
		query = `
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
			JOIN users u ON m.added_by_id = u.id
			WHERE m.status = ?
			ORDER BY m.watched_at DESC, m.title
		`
	}

	rows, err := d.db.QueryContext(ctx, query, status)
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

	return movies, nil
}

func (d *SqliteMoviesRepository) FindByUserIDAndStatus(ctx context.Context, userID int, status string) ([]*domain.Movie, error) {
	query := `
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
		JOIN users u ON m.added_by_id = u.id
		WHERE m.added_by_id = ? AND m.status = ?
		ORDER BY title
	`

	rows, err := d.db.QueryContext(ctx, query, userID, status)
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

	return movies, nil
}

func (d *SqliteMoviesRepository) CountByStatus(ctx context.Context, status string) (int, error) {
	query := "SELECT COUNT(*) FROM movies WHERE status = ?"

	var count int
	err := d.db.QueryRowContext(ctx, query, status).Scan(&count)
	if err != nil {
		return 0, err
	}

	return count, nil
}

func (d *SqliteMoviesRepository) CountByUserIDAndStatus(ctx context.Context, userID int, status string) (int, error) {
	query := "SELECT COUNT(*) FROM movies WHERE status = ? AND added_by_id = ?"

	var count int
	err := d.db.QueryRowContext(ctx, query, status, userID).Scan(&count)
	if err != nil {
		return 0, err
	}

	return count, nil
}

func (d *SqliteMoviesRepository) GetRandomPooled(ctx context.Context) (*domain.Movie, error) {
	query := `
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
		JOIN users u ON m.added_by_id = u.id
		WHERE m.status = 'pool'
		ORDER BY RANDOM()
		LIMIT 1
	`

	row := d.db.QueryRowContext(ctx, query)
	movie, err := scanMovie(row)
	if err != nil {
		return nil, err
	}

	return movie, nil
}

func (d *SqliteMoviesRepository) GetCurrent(ctx context.Context) (*domain.Movie, error) {
	query := `
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
		JOIN users u ON m.added_by_id = u.id
		WHERE m.status = 'current'
		LIMIT 1
	`

	row := d.db.QueryRowContext(ctx, query)
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

	result, err := d.db.ExecContext(ctx, query, title, status, userID)
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

	result, err := d.db.ExecContext(ctx, query, tmdbID, imdbID, id)
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

func (d *SqliteMoviesRepository) UpdateTitle(ctx context.Context, id int, title string) error {
	query := "UPDATE movies SET title = ? WHERE id = ?"

	result, err := d.db.ExecContext(ctx, query, title, id)
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

	result, err := d.db.ExecContext(ctx, query, watchedAt, id)
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

	_, err := d.db.ExecContext(ctx, query, status, id)
	if err != nil {
		return err
	}

	return nil
}

func (d *SqliteMoviesRepository) MarkAsWatched(ctx context.Context, id int, time time.Time) error {
	query := "UPDATE movies SET status = 'watched', watched_at = ? WHERE id = ?"

	_, err := d.db.ExecContext(ctx, query, time, id)
	if err != nil {
		return err
	}

	return nil
}

func (d *SqliteMoviesRepository) Delete(ctx context.Context, id int) error {
	query := "DELETE FROM movies WHERE id = ?"

	result, err := d.db.ExecContext(ctx, query, id)
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

type SqliteNextPickerRepository struct {
	db *sql.DB
}

func NewSqliteNextPickerRepository(db *sql.DB) *SqliteNextPickerRepository {
	return &SqliteNextPickerRepository{db: db}
}

func (d *SqliteNextPickerRepository) Get(ctx context.Context) (*domain.User, error) {
	query := `
		SELECT 
		    u.id,
			u.name,
			u.created_at,
			u.updated_at
		FROM next_picker n
		JOIN users u ON n.user_id = u.id
		WHERE n.id = 1
		LIMIT 1
	`

	user, err := scanUser(d.db.QueryRowContext(ctx, query))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, sql.ErrNoRows
	}

	return user, nil
}

func (d *SqliteNextPickerRepository) Set(ctx context.Context, userID int) error {
	query := `
		INSERT INTO next_picker (id, user_id)
		VALUES (1, ?)
		ON CONFLICT(id) DO UPDATE SET user_id = excluded.user_id
	`
	_, err := d.db.ExecContext(ctx, query, userID)
	if err != nil {
		return err
	}

	return nil
}

type SqliteSettingsRepository struct {
	db *sql.DB
}

func NewSqliteSettingsRepository(db *sql.DB) *SqliteSettingsRepository {
	return &SqliteSettingsRepository{db: db}
}

func (d *SqliteSettingsRepository) List(ctx context.Context) ([]*domain.Settings, error) {
	query := "SELECT key, value FROM settings"

	rows, err := d.db.QueryContext(ctx, query)
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
	err := d.db.QueryRowContext(ctx, query, key).Scan(&value)
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
	_, err := d.db.ExecContext(ctx, query, key, value)
	if err != nil {
		return err
	}

	return nil
}
