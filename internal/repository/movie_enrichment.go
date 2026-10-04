package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"moviepickarr/internal/db"
	"moviepickarr/internal/domain"
)

// ApplyEnrichment commits one TMDB result, all or nothing, only while the movie
// keeps the identity seen before the remote calls.
func (d *SqliteMoviesRepository) ApplyEnrichment(
	ctx context.Context,
	write domain.MovieEnrichmentWrite,
) (bool, error) {
	write.Resolved.IMDbID = canonicalIMDbIDPtr(write.Resolved.IMDbID)
	write.Metadata.MovieID = write.MovieID
	genresJSON, err := marshalMetadataGenres(write.Metadata)
	if err != nil {
		return false, err
	}

	tx, err := d.pool.Write.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()

	var matchedID int
	err = tx.QueryRowContext(ctx, `
		UPDATE movies
		SET tmdb_id = ?, imdb_id = ?
		WHERE id = ? AND tmdb_id IS ? AND imdb_id IS ?
		RETURNING id
	`,
		write.Resolved.TMDBID,
		write.Resolved.IMDbID,
		write.MovieID,
		write.Expected.TMDBID,
		write.Expected.IMDbID,
	).Scan(&matchedID)
	switch {
	case err == nil:
	case db.IsUniqueViolation(err):
		// Another movie owns the resolved identity: enrich without changing ids,
		// so this row does not loop in the backlog.
	case errors.Is(err, sql.ErrNoRows):
		var exists bool
		if err := tx.QueryRowContext(ctx,
			"SELECT EXISTS(SELECT 1 FROM movies WHERE id = ?)",
			write.MovieID,
		).Scan(&exists); err != nil {
			return false, err
		}
		if !exists {
			return false, fmt.Errorf("%w: movie id %d", domain.ErrNotFound, write.MovieID)
		}
		return false, nil
	default:
		return false, err
	}

	if err := upsertMovieMetadata(ctx, tx, write.Metadata, genresJSON, true); err != nil {
		return false, err
	}
	if err := replaceMovieCredits(ctx, tx, write.MovieID, write.Credits); err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
}
