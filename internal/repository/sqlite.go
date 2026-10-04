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
	// Archived members are off the roster; only Restore reads them.
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
	// Archived members keep their row for attribution but never show in a live read.
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

// Remove deletes or archives a member, by whether they authored movies. One tx
// keeps the movie and admin counts from racing the write they drive.
func (d *SqliteUserRepository) Remove(ctx context.Context, id int) (domain.RemoveOutcome, error) {
	tx, err := d.pool.Write.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer func() { _ = tx.Rollback() }()

	// Check existence up front: a missing member is a 404, not a silent no-op.
	var role domain.Role
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

// removeMember hard-deletes a member with no authored movies (FK cascade clears
// logins) and archives one with movies, so attribution survives.
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
	// Nothing cascades on archive, so strip the login rows by hand.
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

// Restore reactivates an archived member. Residual login rows are stripped in
// the same tx, so a pre-upgrade credential or session cannot become live.
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

// rosterSelect is the admin roster read, active and archived, with login state
// derived in-query. The invite EXISTS mirrors the app's invite validity rule.
// Active-before-archived order lets the handler split sections without re-sorting.
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

// SetRole changes an active member's role. Guards, handoff confirmation, and
// turn move share one tx, so a stale client cannot bypass the confirmation.
func (d *SqliteUserRepository) SetRole(ctx context.Context, change domain.RoleChange) (domain.RoleChangeResult, error) {
	tx, err := d.pool.Write.BeginTx(ctx, nil)
	if err != nil {
		return domain.RoleChangeResult{}, err
	}
	defer func() { _ = tx.Rollback() }()

	var current domain.Role
	var createdAt int64
	err = tx.QueryRowContext(ctx,
		"SELECT role, created_at FROM users WHERE id = ? AND archived_at IS NULL", change.MemberID,
	).Scan(&current, &createdAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.RoleChangeResult{}, fmt.Errorf("%w: user id %d", domain.ErrNotFound, change.MemberID)
		}
		return domain.RoleChangeResult{}, err
	}
	if current == change.Role {
		return domain.RoleChangeResult{}, nil
	}

	if current == domain.RoleAdmin && change.Role != domain.RoleAdmin {
		var admins int
		if err := tx.QueryRowContext(ctx,
			"SELECT COUNT(*) FROM users WHERE role = ? AND archived_at IS NULL",
			domain.RoleAdmin,
		).Scan(&admins); err != nil {
			return domain.RoleChangeResult{}, err
		}
		if admins <= 1 {
			return domain.RoleChangeResult{}, fmt.Errorf("%w: cannot demote the last admin", domain.ErrConflict)
		}
	}

	if change.Role == domain.RoleGuest && !change.ConfirmTurnHandoff {
		var holdsTurn int
		if err := tx.QueryRowContext(ctx,
			"SELECT EXISTS(SELECT 1 FROM next_up WHERE id = 1 AND user_id = ?)",
			change.MemberID,
		).Scan(&holdsTurn); err != nil {
			return domain.RoleChangeResult{}, err
		}
		if holdsTurn == 1 {
			return domain.RoleChangeResult{}, domain.ErrTurnHandoffConfirmationRequired
		}
	}

	if _, err := tx.ExecContext(ctx,
		"UPDATE users SET role = ?, updated_at = unixepoch() WHERE id = ?", change.Role, change.MemberID,
	); err != nil {
		return domain.RoleChangeResult{}, err
	}

	result := domain.RoleChangeResult{Changed: true}
	if change.Role == domain.RoleGuest {
		// If the demoted member holds Next up, pass it on in roster order, wrapping
		// once; a guest-only roster leaves it empty.
		handoff, err := tx.ExecContext(ctx, `
			UPDATE next_up
			SET user_id = (
				SELECT candidate.id
				FROM turn_participants candidate
				ORDER BY
				  CASE WHEN candidate.created_at > ?
				         OR (candidate.created_at = ? AND candidate.id > ?)
				       THEN 0 ELSE 1 END,
				  candidate.created_at,
				  candidate.id
				LIMIT 1
			)
			WHERE id = 1 AND user_id = ?
		`, createdAt, createdAt, change.MemberID, change.MemberID)
		if err != nil {
			return domain.RoleChangeResult{}, err
		}
		rows, err := handoff.RowsAffected()
		if err != nil {
			return domain.RoleChangeResult{}, err
		}
		if rows > 0 {
			result.TurnChanged = true
			nextUp, err := scanUser(tx.QueryRowContext(ctx, `
				SELECT u.id, u.name, u.created_at, u.updated_at
				FROM next_up n
				JOIN users u ON u.id = n.user_id
				WHERE n.id = 1
			`))
			switch {
			case err == nil:
				result.NextUp = nextUp
			case errors.Is(err, sql.ErrNoRows):
			default:
				return domain.RoleChangeResult{}, err
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return domain.RoleChangeResult{}, err
	}
	return result, nil
}

type SqliteMoviesRepository struct {
	pool *db.Pool
}

func NewSqliteMoviesRepository(pool *db.Pool) *SqliteMoviesRepository {
	return &SqliteMoviesRepository{pool: pool}
}

// movieSelect is the one movies projection; keep it in step with scanMovie.
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

	// Read inside the tx: the handler broadcasts after return, so a failed read must undo the add.
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
		// Neutral message: either the TMDB or the IMDb index can reject the write.
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

// EditMovie commits one authored edit, including authorization and identity
// cleanup, in one tx so no reader sees a partial edit.
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
		// Credits and metadata describe the prior movie. Delete only the joins:
		// people are shared across movies.
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

// StartDraw moves the movie from pool to current and snapshots its concealed
// Acquisition in one tx, so a later edit cannot retarget Radarr work.
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

// RevealDrawAndAdvanceNextUp persists the Reveal and its turn rotation; next is
// nil when the turn stays put. Call it once per draw: every call rotates.
func (d *SqliteMoviesRepository) RevealDrawAndAdvanceNextUp(
	ctx context.Context,
	movieID int,
	revealedAt time.Time,
) (next *domain.User, err error) {
	tx, err := d.pool.Write.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	found, err := revealAcquisitionTx(ctx, tx, movieID, revealedAt, true)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, fmt.Errorf("%w: acquisition for current movie %d", domain.ErrNotFound, movieID)
	}
	if next, err = advanceNextUpTx(ctx, tx); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return next, nil
}

// revealAcquisitionTx reveals the Acquisition and queues its first webhook
// outbox rows; a concealed Acquisition never reaches the outbox. Idempotent.
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

// ConcealedCurrentDraw loads the unrevealed draw to rebuild the Held draw after
// a restart. A revealed draw returns found=false.
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

// PromoteToPoolIfRoom moves a stashed movie to the pool when the owner has room.
// One atomic UPDATE, so two concurrent promotions cannot overshoot maxPool.
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

// WatchCurrentDraw marks the current draw watched. With revealsDraw it is also
// the Reveal and rotates the turn; next is nil when the turn stays put. Reads
// stay on tx so the handoff cannot come from a different snapshot.
func (d *SqliteMoviesRepository) WatchCurrentDraw(
	ctx context.Context,
	watchedAt time.Time,
	revealsDraw bool,
) (watched *domain.Movie, next *domain.User, err error) {
	tx, err := d.pool.Write.BeginTx(ctx, nil)
	if err != nil {
		return nil, nil, err
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
			return nil, nil, checkErr
		}
		if activeWildcard {
			return nil, nil, domain.ErrActiveWildcard
		}
		return nil, nil, domain.ErrNoCurrentDraw
	}
	if err != nil {
		return nil, nil, err
	}

	// Watching is a Reveal. Legacy current rows can lack an Acquisition, so
	// found=false is valid here.
	if _, err = revealAcquisitionTx(ctx, tx, movieID, watchedAt, false); err != nil {
		return nil, nil, err
	}

	watched, err = scanMovie(tx.QueryRowContext(ctx, movieSelect+" WHERE m.id = ?", movieID))
	if err != nil {
		return nil, nil, err
	}

	if revealsDraw {
		if next, err = advanceNextUpTx(ctx, tx); err != nil {
			return nil, nil, err
		}
	}

	if err = tx.Commit(); err != nil {
		return nil, nil, err
	}

	// SQLite stores whole seconds; the response keeps the original instant.
	watchedAt = watchedAt.UTC()
	watched.WatchedAt = &watchedAt
	return watched, next, nil
}

// advanceNextUpTx passes the turn to the next participant in roster order, or
// to the first when the holder left the rotation. Nil with fewer than two.
func advanceNextUpTx(ctx context.Context, tx *sql.Tx) (*domain.User, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT id, name, created_at, updated_at
		FROM turn_participants
		ORDER BY created_at ASC, id ASC
	`)
	if err != nil {
		return nil, err
	}

	users := make([]*domain.User, 0)
	for rows.Next() {
		user, scanErr := scanUser(rows)
		if scanErr != nil {
			_ = rows.Close()
			return nil, scanErr
		}
		users = append(users, user)
	}
	if err = rows.Err(); err != nil {
		_ = rows.Close()
		return nil, err
	}
	if err = rows.Close(); err != nil {
		return nil, err
	}
	if len(users) < 2 {
		return nil, nil
	}

	var storedNextUp sql.NullInt64
	err = tx.QueryRowContext(ctx, "SELECT user_id FROM next_up WHERE id = 1").Scan(&storedNextUp)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}

	currentIndex := -1
	if !storedNextUp.Valid {
		// Fresh install: behave as if the first member held the turn.
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
	next := users[nextIndex]

	if _, err = tx.ExecContext(ctx, `
		INSERT INTO next_up (id, user_id)
		VALUES (1, ?)
		ON CONFLICT(id) DO UPDATE SET user_id = excluded.user_id
	`, next.ID); err != nil {
		return nil, err
	}
	return next, nil
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
		JOIN turn_participants u ON n.user_id = u.id
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

// SetFirstEligible stores the oldest Turn participant: the self-heal for an
// empty, archived, or Guest pointer.
func (d *SqliteNextUpRepository) SetFirstEligible(ctx context.Context) (*domain.User, error) {
	tx, err := d.pool.Write.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	user, err := scanUser(tx.QueryRowContext(ctx, `
		SELECT id, name, created_at, updated_at
		FROM turn_participants
		ORDER BY created_at ASC, id ASC
		LIMIT 1
	`))
	if err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO next_up (id, user_id)
		VALUES (1, ?)
		ON CONFLICT(id) DO UPDATE SET user_id = excluded.user_id
	`, user.ID); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return user, nil
}

// Skip is the admin's explicit turn handoff. One tx, so a skip cannot race a
// Reveal into a double rotation or skip a member the admin never saw.
func (d *SqliteNextUpRepository) Skip(ctx context.Context, holderID int) (*domain.User, error) {
	tx, err := d.pool.Write.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	var stored sql.NullInt64
	err = tx.QueryRowContext(ctx, `
		SELECT n.user_id
		FROM next_up n
		JOIN turn_participants u ON u.id = n.user_id
		WHERE n.id = 1
	`).Scan(&stored)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	if !stored.Valid || stored.Int64 != int64(holderID) {
		return nil, domain.ErrNextUpChanged
	}

	// The drawer keeps the turn until Reveal, and Reveal rotates on its own.
	var unrevealed int
	if err := tx.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM radarr_acquisitions AS a
			JOIN movies AS m ON m.id = a.movie_id
			WHERE m.status = 'current' AND a.revealed_at IS NULL
		)
	`).Scan(&unrevealed); err != nil {
		return nil, err
	}
	if unrevealed == 1 {
		return nil, domain.ErrDrawNotRevealed
	}

	next, err := advanceNextUpTx(ctx, tx)
	if err != nil {
		return nil, err
	}
	if next == nil {
		return nil, fmt.Errorf("%w: only one turn participant", domain.ErrConflict)
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return next, nil
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
