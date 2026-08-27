package repository

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"moviepickarr/internal/db"
	"moviepickarr/internal/domain"
)

type pendingAcquisition struct {
	MovieID      int
	Source       string
	WildcardID   *int64
	DrawnAt      time.Time
	RevealAt     time.Time
	DrawClientID string
	Visible      bool
}

func insertPendingAcquisitionTx(
	ctx context.Context,
	tx *sql.Tx,
	spec pendingAcquisition,
) (int64, error) {
	drawnEpoch := spec.DrawnAt.UTC().UnixMilli()
	revealEpoch := spec.RevealAt.UTC().UnixMilli()
	var revealedAt any
	var actionStartedAt any
	actionVersion := 0
	if spec.Visible {
		revealedAt = revealEpoch
		actionStartedAt = revealEpoch
		actionVersion = 1
	}

	result, err := tx.ExecContext(ctx, `
		INSERT INTO radarr_acquisitions (
			movie_id, status, action_reason, action_version, action_started_at,
			movie_title, movie_year, tmdb_id, imdb_id, identity_source,
			drawn_at, reveal_at, draw_client_id, revealed_at,
			created_at, updated_at, source, wildcard_id
		)
		SELECT
			m.id, 'needs_preset', 'preset_required', ?, ?, m.title,
			CASE
				WHEN substr(mm.release_date, 1, 4) GLOB '[0-9][0-9][0-9][0-9]'
				 AND CAST(substr(mm.release_date, 1, 4) AS INTEGER) BETWEEN 1870 AND 2100
				THEN CAST(substr(mm.release_date, 1, 4) AS INTEGER)
				ELSE NULL
			END,
			m.tmdb_id, m.imdb_id,
			CASE
				WHEN m.tmdb_id IS NOT NULL THEN 'tmdb'
				WHEN m.imdb_id IS NOT NULL THEN 'imdb'
				ELSE NULL
			END,
			?, ?, ?, ?, ?, ?, ?, ?
		FROM movies AS m
		LEFT JOIN movie_metadata AS mm ON mm.movie_id = m.id
		WHERE m.id = ?
	`, actionVersion, actionStartedAt, drawnEpoch, revealEpoch, spec.DrawClientID,
		revealedAt, drawnEpoch, drawnEpoch, spec.Source, spec.WildcardID, spec.MovieID)
	if err != nil {
		return 0, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return 0, err
	}
	if affected != 1 {
		return 0, domain.ErrNotFound
	}
	return result.LastInsertId()
}

const wildcardSelect = `
	SELECT
		w.id, w.host_movie_id, w.selected_by_id, w.canceled_by_id,
		w.source_status, w.created_for_wildcard, w.status,
		w.selected_at, w.watched_at, w.canceled_at,
		m.id, m.title, m.status, m.added_at, m.added_by_id, u.name,
		u.archived_at IS NOT NULL, m.watched_at, m.tmdb_id, m.imdb_id,
		watched_wildcard.host_movie_id
	FROM wildcards AS w
	JOIN movies AS m ON m.id = w.movie_id
	JOIN users AS u ON u.id = m.added_by_id
	LEFT JOIN wildcards AS watched_wildcard
	  ON watched_wildcard.movie_id = m.id AND watched_wildcard.status = 'watched'`

func scanWildcard(scanner rowScanner) (*domain.Wildcard, error) {
	wildcard := &domain.Wildcard{Movie: &domain.Movie{}}
	var selectedByID, canceledByID sql.NullInt64
	var selectedAt int64
	var watchedAt, canceledAt sql.NullInt64
	var createdForWildcard int
	var movieAddedAt, movieWatchedAt sql.NullInt64
	var tmdbID sql.NullInt64
	var imdbID sql.NullString
	var wildcardHostMovieID sql.NullInt64
	if err := scanner.Scan(
		&wildcard.ID, &wildcard.HostMovieID, &selectedByID, &canceledByID,
		&wildcard.SourceStatus, &createdForWildcard, &wildcard.Status,
		&selectedAt, &watchedAt, &canceledAt,
		&wildcard.Movie.ID, &wildcard.Movie.Title, &wildcard.Movie.Status,
		&movieAddedAt, &wildcard.Movie.AddedByID, &wildcard.Movie.AddedByName,
		&wildcard.Movie.AddedByArchived, &movieWatchedAt, &tmdbID, &imdbID,
		&wildcardHostMovieID,
	); err != nil {
		return nil, err
	}
	wildcard.SelectedByID = nullIntPtr(selectedByID)
	wildcard.CanceledByID = nullIntPtr(canceledByID)
	wildcard.CreatedForWildcard = createdForWildcard == 1
	wildcard.SelectedAt = db.FromUnix(selectedAt)
	wildcard.WatchedAt = unixTimePtr(watchedAt)
	wildcard.CanceledAt = unixTimePtr(canceledAt)
	wildcard.Movie.AddedAt = unixTimePtr(movieAddedAt)
	wildcard.Movie.WatchedAt = unixTimePtr(movieWatchedAt)
	wildcard.Movie.TMDBID = nullIntPtr(tmdbID)
	if imdbID.Valid {
		wildcard.Movie.IMDbID = &imdbID.String
	}
	if wildcardHostMovieID.Valid {
		hostMovieID := int(wildcardHostMovieID.Int64)
		wildcard.Movie.WildcardOfMovieID = &hostMovieID
	}
	return wildcard, nil
}

func activeWildcardTx(ctx context.Context, tx *sql.Tx) (*domain.Wildcard, error) {
	wildcard, err := scanWildcard(tx.QueryRowContext(ctx, wildcardSelect+" WHERE w.status = 'active'"))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.ErrNoActiveWildcard
	}
	return wildcard, err
}

func (d *SqliteMoviesRepository) ActiveWildcard(ctx context.Context) (*domain.Wildcard, error) {
	wildcard, err := scanWildcard(d.pool.Read.QueryRowContext(ctx, wildcardSelect+" WHERE w.status = 'active'"))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.ErrNoActiveWildcard
	}
	return wildcard, err
}

func currentRevealedMovieID(ctx context.Context, tx *sql.Tx) (int, error) {
	var movieID int
	if err := tx.QueryRowContext(ctx,
		"SELECT id FROM movies WHERE status = 'current'",
	).Scan(&movieID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, domain.ErrNoCurrentDraw
		}
		return 0, err
	}

	var revealedAt sql.NullInt64
	err := tx.QueryRowContext(ctx, `
		SELECT revealed_at
		FROM radarr_acquisitions
		WHERE movie_id = ? AND source = 'draw'
		ORDER BY id DESC
		LIMIT 1
	`, movieID).Scan(&revealedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return movieID, nil
	}
	if err != nil {
		return 0, err
	}
	if !revealedAt.Valid {
		return 0, domain.ErrDrawNotRevealed
	}
	return movieID, nil
}

func (d *SqliteMoviesRepository) StartWildcard(
	ctx context.Context,
	actorID int,
	selection domain.WildcardSelection,
	selectedAt time.Time,
	poolLocked bool,
) (*domain.Wildcard, error) {
	tx, err := d.pool.Write.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	hostMovieID, err := currentRevealedMovieID(ctx, tx)
	if err != nil {
		return nil, err
	}
	if selection.ExpectedHostMovieID <= 0 || hostMovieID != selection.ExpectedHostMovieID {
		return nil, domain.ErrCurrentDrawChanged
	}
	var active bool
	if err := tx.QueryRowContext(ctx,
		"SELECT EXISTS(SELECT 1 FROM wildcards WHERE status = 'active')",
	).Scan(&active); err != nil {
		return nil, err
	}
	if active {
		return nil, domain.ErrActiveWildcard
	}

	// Resolve an existing identity before inserting. A new TMDB result is born
	// directly in Wildcard state and returns to the recording member's Stash if
	// canceled.
	var movie *domain.Movie
	created := false
	if selection.ExistingMovieID != nil {
		movie, err = scanMovie(tx.QueryRowContext(ctx, movieSelect+" WHERE m.id = ?", *selection.ExistingMovieID))
		if errors.Is(err, sql.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
	} else {
		title := strings.TrimSpace(selection.Title)
		selection.IMDbID = canonicalIMDbIDPtr(selection.IMDbID)
		if title == "" || (selection.TMDBID == nil && selection.IMDbID == nil) {
			return nil, domain.ErrInvalidInput
		}
		if selection.TMDBID != nil && *selection.TMDBID <= 0 {
			return nil, domain.ErrInvalidInput
		}
		var existingRow *sql.Row
		if selection.TMDBID != nil {
			existingRow = tx.QueryRowContext(ctx, movieSelect+" WHERE m.tmdb_id = ?", *selection.TMDBID)
		} else {
			existingRow = tx.QueryRowContext(ctx, movieSelect+" WHERE m.imdb_id = ? COLLATE NOCASE", *selection.IMDbID)
		}
		movie, err = scanMovie(existingRow)
		if errors.Is(err, sql.ErrNoRows) {
			result, insertErr := tx.ExecContext(ctx, `
				INSERT INTO movies (title, status, added_by_id, tmdb_id, imdb_id)
				VALUES (?, 'wildcard', ?, ?, ?)
			`, title, actorID, selection.TMDBID, selection.IMDbID)
			if insertErr != nil {
				if db.IsUniqueViolation(insertErr) {
					return nil, domain.ErrConflict
				}
				return nil, insertErr
			}
			movieID, insertErr := result.LastInsertId()
			if insertErr != nil {
				return nil, insertErr
			}
			movie, err = scanMovie(tx.QueryRowContext(ctx, movieSelect+" WHERE m.id = ?", movieID))
			created = true
		}
	}
	if err != nil {
		return nil, err
	}
	if movie.ID == hostMovieID || (movie.Status != "pool" && movie.Status != "stash" && movie.Status != "wildcard") {
		return nil, domain.ErrInvalidState
	}
	sourceStatus := domain.MovieStatusStash
	if !created {
		sourceStatus = domain.MovieStatus(movie.Status)
		if sourceStatus == domain.MovieStatusPool && poolLocked {
			return nil, domain.ErrPoolLocked
		}
		result, updateErr := tx.ExecContext(ctx, `
			UPDATE movies SET status = 'wildcard'
			WHERE id = ? AND status IN ('pool', 'stash')
		`, movie.ID)
		if updateErr != nil {
			return nil, updateErr
		}
		if affected, updateErr := result.RowsAffected(); updateErr != nil || affected != 1 {
			if updateErr != nil {
				return nil, updateErr
			}
			return nil, domain.ErrInvalidState
		}
	}

	result, err := tx.ExecContext(ctx, `
		INSERT INTO wildcards (
			host_movie_id, movie_id, selected_by_id, source_status,
			created_for_wildcard, selected_at
		) VALUES (?, ?, ?, ?, ?, ?)
	`, hostMovieID, movie.ID, actorID, sourceStatus, created, db.ToUnix(selectedAt))
	if err != nil {
		if db.IsUniqueViolation(err) {
			return nil, domain.ErrActiveWildcard
		}
		return nil, err
	}
	wildcardID, err := result.LastInsertId()
	if err != nil {
		return nil, err
	}
	acquisitionID, err := insertPendingAcquisitionTx(ctx, tx, pendingAcquisition{
		MovieID:    movie.ID,
		Source:     "wildcard",
		WildcardID: &wildcardID,
		DrawnAt:    selectedAt,
		RevealAt:   selectedAt,
		Visible:    true,
	})
	if err != nil {
		return nil, err
	}
	if err := enqueueAcquisitionAction(ctx, tx, acquisitionID, 1, "preset_required", selectedAt); err != nil {
		return nil, err
	}

	wildcard, err := scanWildcard(tx.QueryRowContext(ctx, wildcardSelect+" WHERE w.id = ?", wildcardID))
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return wildcard, nil
}

func (d *SqliteMoviesRepository) CancelWildcard(
	ctx context.Context,
	actorID int,
	expectedWildcardID int64,
	canceledAt time.Time,
) (*domain.Wildcard, error) {
	tx, err := d.pool.Write.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	wildcard, err := activeWildcardTx(ctx, tx)
	if err != nil {
		return nil, err
	}
	if expectedWildcardID <= 0 || wildcard.ID != expectedWildcardID {
		return nil, domain.ErrWildcardChanged
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE movies SET status = ? WHERE id = ? AND status = 'wildcard'
	`, wildcard.SourceStatus, wildcard.Movie.ID); err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE wildcards
		SET status = 'canceled', canceled_at = ?, canceled_by_id = ?
		WHERE id = ? AND status = 'active'
	`, db.ToUnix(canceledAt), actorID, wildcard.ID); err != nil {
		return nil, err
	}
	canceledEpoch := canceledAt.UTC().UnixMilli()
	result, err := tx.ExecContext(ctx, `
		UPDATE radarr_acquisitions
		SET status = CASE WHEN status = 'downloaded' THEN status ELSE 'abandoned' END,
			action_reason = NULL,
			action_started_at = NULL,
			abandoned_at = CASE WHEN status = 'downloaded' THEN abandoned_at ELSE COALESCE(abandoned_at, ?) END,
			abandoned_by = CASE WHEN status = 'downloaded' THEN abandoned_by ELSE COALESCE(abandoned_by, ?) END,
			abandonment_reason = CASE
				WHEN status = 'downloaded' THEN abandonment_reason
				ELSE COALESCE(abandonment_reason, 'Wildcard canceled')
			END,
			mutation_state = 'idle', automatic_search_claimed_at = NULL,
			next_check_at = NULL, canceled_at = ?, canceled_by = ?,
			revision = revision + 1, updated_at = ?
		WHERE wildcard_id = ?
	`, canceledEpoch, actorID, canceledEpoch, actorID, canceledEpoch, wildcard.ID)
	if err != nil {
		return nil, err
	}
	if affected, err := result.RowsAffected(); err != nil || affected != 1 {
		if err != nil {
			return nil, err
		}
		return nil, domain.ErrNotFound
	}
	wildcard, err = scanWildcard(tx.QueryRowContext(ctx, wildcardSelect+" WHERE w.id = ?", wildcard.ID))
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return wildcard, nil
}

func (d *SqliteMoviesRepository) WatchWildcard(
	ctx context.Context,
	expectedWildcardID int64,
	watchedAt time.Time,
) (*domain.Wildcard, error) {
	tx, err := d.pool.Write.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	wildcard, err := activeWildcardTx(ctx, tx)
	if err != nil {
		return nil, err
	}
	if expectedWildcardID <= 0 || wildcard.ID != expectedWildcardID {
		return nil, domain.ErrWildcardChanged
	}
	result, err := tx.ExecContext(ctx, `
		UPDATE movies
		SET status = 'watched', watched_at = ?
		WHERE id = ? AND status = 'wildcard'
	`, db.ToUnix(watchedAt), wildcard.Movie.ID)
	if err != nil {
		return nil, err
	}
	if affected, err := result.RowsAffected(); err != nil || affected != 1 {
		if err != nil {
			return nil, err
		}
		return nil, domain.ErrInvalidState
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE wildcards
		SET status = 'watched', watched_at = ?
		WHERE id = ? AND status = 'active'
	`, db.ToUnix(watchedAt), wildcard.ID); err != nil {
		return nil, err
	}
	wildcard, err = scanWildcard(tx.QueryRowContext(ctx, wildcardSelect+" WHERE w.id = ?", wildcard.ID))
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	watchedAt = watchedAt.UTC()
	wildcard.WatchedAt = &watchedAt
	wildcard.Movie.WatchedAt = &watchedAt
	return wildcard, nil
}
