package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strings"
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

func canonicalIMDbID(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func canonicalIMDbIDPtr(value *string) *string {
	if value == nil {
		return nil
	}
	normalized := canonicalIMDbID(*value)
	if normalized == "" {
		return nil
	}
	return &normalized
}

var canonicalIMDbIDRegex = regexp.MustCompile(`^tt\d{7,8}$`)

func canonicalMovieIdentityTarget(target domain.MovieIdentityTarget) (domain.MovieIdentityTarget, error) {
	if (target.TMDBID == nil) == (target.IMDbID == nil) {
		return domain.MovieIdentityTarget{}, domain.ErrInvalidInput
	}

	imdbID := canonicalIMDbIDPtr(target.IMDbID)
	if target.TMDBID != nil {
		if *target.TMDBID <= 0 {
			return domain.MovieIdentityTarget{}, domain.ErrInvalidInput
		}
		tmdbID := *target.TMDBID
		return domain.MovieIdentityTarget{TMDBID: &tmdbID}, nil
	}
	if imdbID == nil || !canonicalIMDbIDRegex.MatchString(*imdbID) {
		return domain.MovieIdentityTarget{}, domain.ErrInvalidInput
	}
	return domain.MovieIdentityTarget{IMDbID: imdbID}, nil
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
	var wildcardHostMovieID sql.NullInt64

	if err := scanner.Scan(
		&movie.ID,
		&movie.Title,
		&movie.Status,
		&addedAt,
		&movie.AddedByID,
		&movie.AddedByName,
		&movie.AddedByArchived,
		&watchedAt,
		&tmdbID,
		&imdbID,
		&wildcardHostMovieID,
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
	if wildcardHostMovieID.Valid {
		v := int(wildcardHostMovieID.Int64)
		movie.WildcardOfMovieID = &v
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
	query := "SELECT id, name, created_at, updated_at FROM users WHERE archived_at IS NULL ORDER BY created_at ASC, id ASC"

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
// write transaction by whether they authored any movies. The whole decision,
// including the last-admin guard, runs under one tx so neither the movie count
// nor the active-admin count can race the delete/archive they drive.
func (d *SqliteUserRepository) Remove(ctx context.Context, id int) (domain.RemoveOutcome, error) {
	tx, err := d.pool.Write.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer func() { _ = tx.Rollback() }()

	// Existence is the movies-join's problem otherwise, so check it up front: a
	// missing member is a 404, not a silent no-op.
	var role string
	var archivedAt sql.NullInt64
	if err := tx.QueryRowContext(ctx,
		"SELECT role, archived_at FROM users WHERE id = ?", id,
	).Scan(&role, &archivedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", fmt.Errorf("%w: user id %d", domain.ErrNotFound, id)
		}
		return "", err
	}
	if role == domain.RoleAdmin && !archivedAt.Valid {
		var admins int
		if err := tx.QueryRowContext(ctx,
			"SELECT COUNT(*) FROM users WHERE role = ? AND archived_at IS NULL",
			domain.RoleAdmin,
		).Scan(&admins); err != nil {
			return "", err
		}
		if admins <= 1 {
			return "", fmt.Errorf("%w: cannot remove the last admin", domain.ErrConflict)
		}
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
	if err := deleteUserAuthRows(ctx, tx, id); err != nil {
		return "", err
	}
	return domain.OutcomeArchived, nil
}

func deleteUserAuthRows(ctx context.Context, tx *sql.Tx, id int) error {
	for _, stmt := range []string{
		"DELETE FROM local_accounts WHERE user_id = ?",
		"DELETE FROM oidc_identities WHERE user_id = ?",
		"DELETE FROM sessions WHERE user_id = ?",
		"DELETE FROM invites WHERE user_id = ?",
	} {
		if _, err := tx.ExecContext(ctx, stmt, id); err != nil {
			return err
		}
	}
	return nil
}

// Restore reactivates an archived member only after stripping any residual
// authentication rows. The cleanup and archived_at change share one
// transaction, so a pre-upgrade credential or session can never become live
// during restore.
func (d *SqliteUserRepository) Restore(ctx context.Context, id int) error {
	tx, err := d.pool.Write.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	var exists int
	if err := tx.QueryRowContext(ctx,
		"SELECT 1 FROM users WHERE id = ? AND archived_at IS NOT NULL", id,
	).Scan(&exists); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("%w: no archived member %d", domain.ErrNotFound, id)
		}
		return err
	}

	if err := deleteUserAuthRows(ctx, tx, id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx,
		"UPDATE users SET archived_at = NULL, updated_at = unixepoch() WHERE id = ?", id,
	); err != nil {
		return err
	}
	return tx.Commit()
}

// rosterSelect is the admin roster read: one row per member, active and archived,
// with login state derived in-query. The three EXISTS subqueries are the
// link-state axes (no stored flag), the invite one mirrors the app's validity
// rule (unredeemed, unrevoked, unexpired), and the movie count decides
// delete-vs-archive on removal. last_seen_at is the newest session touch (nullable
// for members with no session, e.g. placeholders and archived rows). Ordering is
// active-before-archived, then oldest-first, so the handler splits the sections
// without re-sorting.
const rosterSelect = `
SELECT
    u.id,
    u.name,
    (SELECT la.username FROM local_accounts la WHERE la.user_id = u.id) AS username,
    u.role,
    u.archived_at,
    EXISTS (SELECT 1 FROM local_accounts la WHERE la.user_id = u.id)   AS has_local,
    EXISTS (SELECT 1 FROM oidc_identities oi WHERE oi.user_id = u.id)  AS has_oidc,
    EXISTS (
        SELECT 1 FROM invites iv
        WHERE iv.user_id = u.id
          AND iv.used_at IS NULL
          AND iv.revoked_at IS NULL
          AND iv.expires_at > unixepoch()
    ) AS invite_pending,
    (SELECT COUNT(*) FROM movies m WHERE m.added_by_id = u.id)         AS movies_authored,
    (SELECT MAX(s.last_seen_at) FROM sessions s WHERE s.user_id = u.id) AS last_seen_at
FROM users u
ORDER BY (u.archived_at IS NOT NULL), u.created_at ASC`

func scanRosterMember(scanner rowScanner) (*domain.RosterMember, error) {
	m := &domain.RosterMember{}
	var archivedAt, lastSeenAt sql.NullInt64
	var username sql.NullString
	if err := scanner.Scan(
		&m.ID,
		&m.Name,
		&username,
		&m.Role,
		&archivedAt,
		&m.HasLocalLogin,
		&m.HasLinkedIdentity,
		&m.InvitePending,
		&m.MoviesAuthored,
		&lastSeenAt,
	); err != nil {
		return nil, err
	}
	m.Username = username.String
	m.Archived = archivedAt.Valid
	m.LastSeenAt = unixTimePtr(lastSeenAt)
	return m, nil
}

func (d *SqliteUserRepository) Roster(ctx context.Context) ([]*domain.RosterMember, error) {
	rows, err := d.pool.Read.QueryContext(ctx, rosterSelect)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	members := make([]*domain.RosterMember, 0)
	for rows.Next() {
		m, err := scanRosterMember(rows)
		if err != nil {
			return nil, err
		}
		members = append(members, m)
	}
	return members, rows.Err()
}

// SetRole changes an active member's role under one write transaction so the
// last-admin guard can't race a concurrent demotion. It reads the current role
// (skipping archived members, whose role is frozen), short-circuits a no-op when
// the role already matches, and refuses a demotion that would empty the admin set
// so the roster is never left unmanageable. role is validated by the caller.
func (d *SqliteUserRepository) SetRole(ctx context.Context, id int, role string) error {
	tx, err := d.pool.Write.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	var current string
	err = tx.QueryRowContext(ctx,
		"SELECT role FROM users WHERE id = ? AND archived_at IS NULL", id,
	).Scan(&current)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("%w: user id %d", domain.ErrNotFound, id)
		}
		return err
	}
	if current == role {
		return nil
	}

	if role == domain.RoleMember {
		var admins int
		if err := tx.QueryRowContext(ctx,
			"SELECT COUNT(*) FROM users WHERE role = ? AND archived_at IS NULL",
			domain.RoleAdmin,
		).Scan(&admins); err != nil {
			return err
		}
		if admins <= 1 {
			return fmt.Errorf("%w: cannot demote the last admin", domain.ErrConflict)
		}
	}

	if _, err := tx.ExecContext(ctx,
		"UPDATE users SET role = ?, updated_at = unixepoch() WHERE id = ?", role, id,
	); err != nil {
		return err
	}
	return tx.Commit()
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
		u.archived_at IS NOT NULL,
		m.watched_at,
		m.tmdb_id,
		m.imdb_id,
		w.host_movie_id
	FROM movies m
	JOIN users u ON m.added_by_id = u.id
	LEFT JOIN wildcards w ON w.movie_id = m.id AND w.status = 'watched'`

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

func (d *SqliteMoviesRepository) AddToStash(
	ctx context.Context,
	title string,
	userID int,
	tmdbID *int,
	imdbID *string,
) (*domain.Movie, error) {
	imdbID = canonicalIMDbIDPtr(imdbID)

	tx, err := d.pool.Write.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	query := `
		INSERT INTO movies (title, status, added_by_id, tmdb_id, imdb_id)
		VALUES (?, 'stash', ?, ?, ?)
	`

	result, err := tx.ExecContext(ctx, query, title, userID, tmdbID, imdbID)
	if err != nil {
		if db.IsUniqueViolation(err) {
			return nil, fmt.Errorf("%w: another movie already has this identity", domain.ErrConflict)
		}
		return nil, err
	}

	id, err := result.LastInsertId()
	if err != nil {
		return nil, err
	}

	// Keep the response projection inside the transaction. The handler only
	// broadcasts after this record returns, so a failed read must undo the add.
	movie, err := scanMovie(tx.QueryRowContext(ctx, movieSelect+" WHERE m.id = ?", int(id)))
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}

	return movie, nil
}

func (d *SqliteMoviesRepository) SetExternalIDs(ctx context.Context, id int, tmdbID *int, imdbID *string) error {
	imdbID = canonicalIMDbIDPtr(imdbID)
	query := "UPDATE movies SET tmdb_id = ?, imdb_id = ? WHERE id = ?"

	result, err := d.pool.Write.ExecContext(ctx, query, tmdbID, imdbID, id)
	if err != nil {
		// Stable identities are unique whenever present. Keep this message
		// neutral because either the TMDB or IMDb index can reject the write.
		if db.IsUniqueViolation(err) {
			return fmt.Errorf("%w: another movie already has this identity", domain.ErrConflict)
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

// EditMovie commits one authored edit as a unit. The initial movie read owns
// authorization, status validation, and identity comparison; changed identity
// cleanup removes derived rows; the final read owns the response. All run on tx
// so neither callers nor other writers can observe a partial command.
func (d *SqliteMoviesRepository) EditMovie(
	ctx context.Context,
	movieID, actorID int,
	title string,
	target domain.MovieIdentityTarget,
	watchedAt *time.Time,
) (*domain.Movie, bool, error) {
	target, err := canonicalMovieIdentityTarget(target)
	if err != nil {
		return nil, false, err
	}

	tx, err := d.pool.Write.BeginTx(ctx, nil)
	if err != nil {
		return nil, false, err
	}
	defer func() { _ = tx.Rollback() }()

	current, err := scanMovie(tx.QueryRowContext(ctx, movieSelect+" WHERE m.id = ?", movieID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, fmt.Errorf("%w: movie id %d", domain.ErrNotFound, movieID)
	}
	if err != nil {
		return nil, false, err
	}
	if current.AddedByID != actorID {
		return nil, false, domain.ErrForbidden
	}
	if watchedAt != nil && current.Status != string(domain.MovieStatusWatched) {
		return nil, false, domain.ErrInvalidInput
	}

	storedIMDb := ""
	if current.IMDbID != nil {
		storedIMDb = *current.IMDbID
	}
	currentIMDb := canonicalIMDbID(storedIMDb)
	matchingTMDB := target.TMDBID != nil && current.TMDBID != nil &&
		*target.TMDBID == *current.TMDBID
	matchingIMDb := target.IMDbID != nil && currentIMDb != "" &&
		*target.IMDbID == currentIMDb
	identityChanged := !matchingTMDB && !matchingIMDb
	identityNeedsNormalization := current.IMDbID != nil &&
		(storedIMDb != currentIMDb || currentIMDb == "")

	query := "UPDATE movies SET title = ?"
	args := []any{title}
	if watchedAt != nil {
		query += ", watched_at = ?"
		args = append(args, db.ToUnix(watchedAt.UTC()))
	}
	if identityChanged {
		query += ", tmdb_id = ?, imdb_id = ?"
		args = append(args, target.TMDBID, target.IMDbID)
	} else if identityNeedsNormalization {
		query += ", imdb_id = ?"
		if currentIMDb == "" {
			args = append(args, nil)
		} else {
			args = append(args, currentIMDb)
		}
	}
	query += " WHERE id = ?"
	args = append(args, movieID)

	result, err := tx.ExecContext(ctx, query, args...)
	if err != nil {
		if db.IsUniqueViolation(err) {
			return nil, false, fmt.Errorf("%w: another movie already has this identity", domain.ErrConflict)
		}
		return nil, false, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return nil, false, err
	}
	if affected == 0 {
		return nil, false, fmt.Errorf("%w: movie id %d", domain.ErrNotFound, movieID)
	}

	if identityChanged {
		// Credits and metadata describe the prior movie, so do not serve them while
		// the replacement identity awaits enrichment. Delete only the movie joins;
		// people are shared across movies and stay in place.
		if _, err := tx.ExecContext(ctx,
			"DELETE FROM movie_credits WHERE movie_id = ?",
			movieID,
		); err != nil {
			return nil, false, err
		}
		if _, err := tx.ExecContext(ctx,
			"DELETE FROM movie_metadata WHERE movie_id = ?",
			movieID,
		); err != nil {
			return nil, false, err
		}
	}

	updated, err := scanMovie(tx.QueryRowContext(ctx, movieSelect+" WHERE m.id = ?", movieID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, fmt.Errorf("%w: movie id %d", domain.ErrNotFound, movieID)
	}
	if err != nil {
		return nil, false, err
	}
	if err := tx.Commit(); err != nil {
		return nil, false, err
	}

	return updated, identityChanged, nil
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

// StartDraw commits the movie's pool -> current transition and its concealed
// Pending acquisition in one writer transaction. The Acquisition snapshots the
// authored movie identity at the same boundary as the status change, so a later
// edit cannot retarget Radarr work and a failed insert cannot strand a Current
// draw without its Acquisition.
func (d *SqliteMoviesRepository) StartDraw(
	ctx context.Context,
	movieID int,
	drawnAt, revealAt time.Time,
	drawClientID string,
) error {
	tx, err := d.pool.Write.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	result, err := tx.ExecContext(ctx, `
		UPDATE movies
		SET status = 'current'
		WHERE id = ? AND status = 'pool'
	`, movieID)
	if err != nil {
		if db.IsUniqueViolation(err) {
			return domain.ErrCurrentDrawExists
		}
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected != 1 {
		var status string
		if err := tx.QueryRowContext(ctx, "SELECT status FROM movies WHERE id = ?", movieID).Scan(&status); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return fmt.Errorf("%w: movie id %d", domain.ErrNotFound, movieID)
			}
			return err
		}
		return domain.ErrInvalidState
	}

	if _, err = insertPendingAcquisitionTx(ctx, tx, pendingAcquisition{
		MovieID:      movieID,
		Source:       "draw",
		DrawnAt:      drawnAt,
		RevealAt:     revealAt,
		DrawClientID: drawClientID,
	}); err != nil {
		return fmt.Errorf("%w: acquisition snapshot for current movie %d", domain.ErrNotFound, movieID)
	}

	return tx.Commit()
}

// RevealDraw persists the Acquisition visibility boundary before the movie
// service flips its process-local draw or publishes movie:revealed. Repeating it
// is safe: the first call starts the actionable preset-required condition; later
// calls leave its version and timestamp unchanged.
func (d *SqliteMoviesRepository) RevealDraw(ctx context.Context, movieID int, revealedAt time.Time) error {
	tx, err := d.pool.Write.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	found, err := revealAcquisitionTx(ctx, tx, movieID, revealedAt, true)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("%w: acquisition for current movie %d", domain.ErrNotFound, movieID)
	}
	return tx.Commit()
}

// revealAcquisitionTx crosses the admin visibility boundary and creates the
// first actionable webhook outbox rows in the same transaction. A concealed
// Acquisition never reaches the outbox. Existing delivery rows make repeated
// Reveal calls idempotent through the condition uniqueness constraint.
func revealAcquisitionTx(
	ctx context.Context,
	tx *sql.Tx,
	movieID int,
	revealedAt time.Time,
	requireCurrent bool,
) (bool, error) {
	revealedEpoch := revealedAt.UTC().UnixMilli()
	requireCurrentValue := 0
	if requireCurrent {
		requireCurrentValue = 1
	}

	var acquisitionID int
	err := tx.QueryRowContext(ctx, `
		UPDATE radarr_acquisitions
		SET revealed_at = COALESCE(revealed_at, ?),
			action_reason = COALESCE(action_reason, 'preset_required'),
			action_version = action_version + CASE WHEN revealed_at IS NULL THEN 1 ELSE 0 END,
			action_started_at = COALESCE(action_started_at, ?),
			revision = revision + CASE WHEN revealed_at IS NULL THEN 1 ELSE 0 END,
			updated_at = CASE WHEN revealed_at IS NULL THEN ? ELSE updated_at END
		WHERE id = (
			SELECT a.id
			FROM radarr_acquisitions AS a
			JOIN movies AS m ON m.id = a.movie_id
			WHERE a.movie_id = ?
			  AND (? = 0 OR m.status = 'current')
			  AND a.status NOT IN ('downloaded', 'abandoned')
			ORDER BY a.id DESC
			LIMIT 1
		)
		RETURNING id
	`, revealedEpoch, revealedEpoch, revealedEpoch, movieID, requireCurrentValue).Scan(&acquisitionID)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}

	deliveryEpoch := revealedAt.UTC().Unix()
	_, err = tx.ExecContext(ctx, `
		INSERT INTO radarr_webhook_deliveries (
			destination_id,
			acquisition_id,
			event,
			reason,
			destination_revision,
			action_version,
			target_label,
			status,
			attempt_count,
			next_attempt_at,
			created_at,
			updated_at
		)
		SELECT
			d.id,
			a.id,
			'acquisition.action_required',
			a.action_reason,
			d.revision,
			a.action_version,
			COALESCE(a.target_instance_name, ''),
			'pending',
			0,
			?,
			?,
			?
		FROM radarr_acquisitions AS a
		JOIN radarr_webhook_destinations AS d
		  ON d.enabled = 1
		 AND d.verified_at IS NOT NULL
		 AND d.archived_at IS NULL
		WHERE a.id = ?
		  AND a.revealed_at IS NOT NULL
		  AND a.action_reason IS NOT NULL
		  AND a.action_version > 0
		  AND EXISTS (
			SELECT 1
			FROM json_each(d.reason_filters) AS reason_filter
			WHERE reason_filter.value = a.action_reason
		  )
		ON CONFLICT(destination_id, acquisition_id, action_version) DO NOTHING
	`, deliveryEpoch, deliveryEpoch, deliveryEpoch, acquisitionID)
	if err != nil {
		return false, err
	}
	return true, nil
}

// ConcealedCurrentDraw loads the durable draw needed to rebuild the Held draw
// after a restart. A revealed Acquisition deliberately returns found=false: the
// ceremony is complete and the Current draw should render directly.
func (d *SqliteMoviesRepository) ConcealedCurrentDraw(
	ctx context.Context,
) (movieID int, drawnAt, revealAt time.Time, drawClientID string, found bool, err error) {
	var drawnEpoch, revealEpoch int64
	err = d.pool.Read.QueryRowContext(ctx, `
		SELECT a.movie_id, a.drawn_at, a.reveal_at, a.draw_client_id
		FROM radarr_acquisitions AS a
		JOIN movies AS m ON m.id = a.movie_id
		WHERE m.status = 'current' AND a.revealed_at IS NULL
		ORDER BY a.id DESC
		LIMIT 1
	`).Scan(&movieID, &drawnEpoch, &revealEpoch, &drawClientID)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, time.Time{}, time.Time{}, "", false, nil
	}
	if err != nil {
		return 0, time.Time{}, time.Time{}, "", false, err
	}
	return movieID,
		time.UnixMilli(drawnEpoch).UTC(),
		time.UnixMilli(revealEpoch).UTC(),
		drawClientID,
		true,
		nil
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
				SELECT COUNT(*) + EXISTS (
					SELECT 1
					FROM wildcards AS w
					JOIN movies AS wm ON wm.id = w.movie_id
					WHERE w.status = 'active'
					  AND w.source_status = 'pool'
					  AND wm.added_by_id = movies.added_by_id
				)
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

// WatchCurrentAndAdvanceNextUp commits the watched lifecycle change and its
// rotation-on-watch handoff as one writer transaction. Every dependent read
// stays on tx: using the read pool here could derive the handoff from a
// different snapshot than the watched update.
func (d *SqliteMoviesRepository) WatchCurrentAndAdvanceNextUp(
	ctx context.Context,
	watchedAt time.Time,
) (watched *domain.Movie, next *domain.User, changed bool, err error) {
	tx, err := d.pool.Write.BeginTx(ctx, nil)
	if err != nil {
		return nil, nil, false, err
	}
	defer func() { _ = tx.Rollback() }()

	var movieID int
	err = tx.QueryRowContext(ctx, `
		UPDATE movies
		SET status = 'watched', watched_at = ?
		WHERE status = 'current'
		  AND NOT EXISTS (SELECT 1 FROM wildcards WHERE status = 'active')
		RETURNING id
	`, db.ToUnix(watchedAt)).Scan(&movieID)
	if errors.Is(err, sql.ErrNoRows) {
		var activeWildcard bool
		if checkErr := tx.QueryRowContext(ctx,
			"SELECT EXISTS(SELECT 1 FROM wildcards WHERE status = 'active')",
		).Scan(&activeWildcard); checkErr != nil {
			return nil, nil, false, checkErr
		}
		if activeWildcard {
			return nil, nil, false, domain.ErrActiveWildcard
		}
		return nil, nil, false, domain.ErrNoCurrentDraw
	}
	if err != nil {
		return nil, nil, false, err
	}

	// Watching an unrevealed draw is itself a Reveal. Keep that visibility and
	// its actionable webhook outbox rows inside the same transaction as the
	// watched movie and next-up handoff. Legacy current rows can have no
	// Acquisition, so found=false remains valid on this compatibility path.
	if _, err = revealAcquisitionTx(ctx, tx, movieID, watchedAt, false); err != nil {
		return nil, nil, false, err
	}

	watched, err = scanMovie(tx.QueryRowContext(ctx, movieSelect+" WHERE m.id = ?", movieID))
	if err != nil {
		return nil, nil, false, err
	}

	var poolRemains bool
	if err = tx.QueryRowContext(ctx,
		"SELECT EXISTS(SELECT 1 FROM movies WHERE status = 'pool')",
	).Scan(&poolRemains); err != nil {
		return nil, nil, false, err
	}

	if poolRemains {
		rows, queryErr := tx.QueryContext(ctx, `
			SELECT id, name, created_at, updated_at
			FROM users
			WHERE archived_at IS NULL
			ORDER BY created_at ASC, id ASC
		`)
		if queryErr != nil {
			return nil, nil, false, queryErr
		}

		users := make([]*domain.User, 0)
		for rows.Next() {
			user, scanErr := scanUser(rows)
			if scanErr != nil {
				_ = rows.Close()
				return nil, nil, false, scanErr
			}
			users = append(users, user)
		}
		if err = rows.Err(); err != nil {
			_ = rows.Close()
			return nil, nil, false, err
		}
		if err = rows.Close(); err != nil {
			return nil, nil, false, err
		}

		if len(users) > 1 {
			var storedNextUp sql.NullInt64
			err = tx.QueryRowContext(ctx,
				"SELECT user_id FROM next_up WHERE id = 1",
			).Scan(&storedNextUp)
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				return nil, nil, false, err
			}

			currentIndex := -1
			if !storedNextUp.Valid {
				// Advance historically self-seeds the first member, then rotates to
				// the second. Preserve that fresh-install behavior in one write.
				currentIndex = 0
			} else {
				for i := range users {
					if int64(users[i].ID) == storedNextUp.Int64 {
						currentIndex = i
						break
					}
				}
			}

			nextIndex := 0
			if currentIndex >= 0 {
				nextIndex = (currentIndex + 1) % len(users)
			}
			next = users[nextIndex]

			if _, err = tx.ExecContext(ctx, `
				INSERT INTO next_up (id, user_id)
				VALUES (1, ?)
				ON CONFLICT(id) DO UPDATE SET user_id = excluded.user_id
			`, next.ID); err != nil {
				return nil, nil, false, err
			}
			changed = true
		}
	}

	if err = tx.Commit(); err != nil {
		return nil, nil, false, err
	}

	// SQLite persists epoch seconds, but the successful request keeps the
	// original UTC instant in its response.
	watchedAt = watchedAt.UTC()
	watched.WatchedAt = &watchedAt
	return watched, next, changed, nil
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
