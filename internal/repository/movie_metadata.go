package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"moviepickarr/internal/db"
	"moviepickarr/internal/domain"
)

type SqliteMovieMetadataRepository struct {
	pool *db.Pool
}

type contextExecer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

func NewSqliteMovieMetadataRepository(pool *db.Pool) *SqliteMovieMetadataRepository {
	return &SqliteMovieMetadataRepository{pool: pool}
}

func scanMetadata(scanner rowScanner) (*domain.MovieMetadata, error) {
	md := &domain.MovieMetadata{}
	var poster sql.NullString
	var backdrop sql.NullString
	var genresJSON string
	var enrichedAt sql.NullInt64

	if err := scanner.Scan(
		&md.MovieID,
		&md.Overview,
		&poster,
		&backdrop,
		&md.ReleaseDate,
		&md.Runtime,
		&genresJSON,
		&md.VoteAverage,
		&md.VoteCount,
		&md.Tagline,
		&enrichedAt,
	); err != nil {
		return nil, err
	}

	if poster.Valid {
		md.PosterPath = &poster.String
	}
	if backdrop.Valid {
		md.BackdropPath = &backdrop.String
	}
	if genresJSON != "" {
		if err := json.Unmarshal([]byte(genresJSON), &md.Genres); err != nil {
			return nil, err
		}
	}
	md.EnrichedAt = unixTimePtr(enrichedAt)

	return md, nil
}

func marshalMetadataGenres(md domain.MovieMetadata) (string, error) {
	genres := md.Genres
	if genres == nil {
		genres = []string{} // marshal to "[]", never "null"
	}
	genresJSON, err := json.Marshal(genres)
	if err != nil {
		return "", err
	}
	return string(genresJSON), nil
}

func upsertMovieMetadata(
	ctx context.Context,
	exec contextExecer,
	md domain.MovieMetadata,
	genresJSON string,
	refreshCredits bool,
) error {
	creditsColumn := ""
	creditsValue := ""
	creditsUpdate := ""
	if refreshCredits {
		creditsColumn = ", credits_refreshed_at"
		creditsValue = ", unixepoch()"
		creditsUpdate = ", credits_refreshed_at = unixepoch()"
	}
	query := `
			INSERT INTO movie_metadata (
				movie_id, overview, poster_path, backdrop_path,
				release_date, runtime, genres, vote_average, vote_count, tagline, enriched_at` + creditsColumn + `
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, unixepoch()` + creditsValue + `)
			ON CONFLICT(movie_id) DO UPDATE SET
				overview      = excluded.overview,
				poster_path   = excluded.poster_path,
				backdrop_path = excluded.backdrop_path,
				release_date  = excluded.release_date,
				runtime       = excluded.runtime,
				genres        = excluded.genres,
				vote_average  = excluded.vote_average,
				vote_count    = excluded.vote_count,
				tagline       = excluded.tagline,
				enriched_at   = unixepoch()` + creditsUpdate + `
		`

	_, err := exec.ExecContext(ctx, query,
		md.MovieID, md.Overview, md.PosterPath, md.BackdropPath,
		md.ReleaseDate, md.Runtime, genresJSON, md.VoteAverage, md.VoteCount, md.Tagline,
	)
	return err
}

func (d *SqliteMovieMetadataRepository) UpsertMetadata(ctx context.Context, md domain.MovieMetadata) error {
	genresJSON, err := marshalMetadataGenres(md)
	if err != nil {
		return err
	}
	return upsertMovieMetadata(ctx, d.pool.Write, md, genresJSON, false)
}

func (d *SqliteMovieMetadataRepository) GetMetadata(ctx context.Context, movieID int) (*domain.MovieMetadata, error) {
	query := `
		SELECT
			movie_id,
			overview,
			poster_path,
			backdrop_path,
			release_date,
			runtime,
			genres,
			vote_average,
			vote_count,
			tagline,
			enriched_at
		FROM movie_metadata
		WHERE movie_id = ?
	`

	md, err := scanMetadata(d.pool.Read.QueryRowContext(ctx, query, movieID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("%w: movie metadata for movie id %d", domain.ErrNotFound, movieID)
	}
	if err != nil {
		return nil, err
	}

	return md, nil
}

func (d *SqliteMovieMetadataRepository) GetMetadataByMovieIDs(ctx context.Context, ids []int) (map[int]*domain.MovieMetadata, error) {
	result := make(map[int]*domain.MovieMetadata, len(ids))
	if len(ids) == 0 {
		return result, nil
	}

	placeholders := make([]string, len(ids))
	args := make([]any, len(ids))
	for i, id := range ids {
		placeholders[i] = "?"
		args[i] = id
	}

	query := fmt.Sprintf(`
		SELECT
			movie_id,
			overview,
			poster_path,
			backdrop_path,
			release_date,
			runtime,
			genres,
			vote_average,
			vote_count,
			tagline,
			enriched_at
		FROM movie_metadata
		WHERE movie_id IN (%s)
	`, strings.Join(placeholders, ", "))

	rows, err := d.pool.Read.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		md, err := scanMetadata(rows)
		if err != nil {
			return nil, err
		}
		result[md.MovieID] = md
	}

	return result, rows.Err()
}

func (d *SqliteMovieMetadataRepository) MarkEnrichmentStale(ctx context.Context, movieID int) error {
	query := `UPDATE movie_metadata SET credits_refreshed_at = NULL WHERE movie_id = ?`
	_, err := d.pool.Write.ExecContext(ctx, query, movieID)
	return err
}

func (d *SqliteMovieMetadataRepository) NeedsEnrichment(ctx context.Context, staleBefore time.Time, limit int) ([]domain.EnrichmentCandidate, error) {
	// A NULL credits_refreshed_at means credits were never ingested for an
	// otherwise-enriched row, so existing libraries backfill credits on the
	// first drain after the credits migration.
	query := `
		SELECT m.id
		FROM movies m
		LEFT JOIN movie_metadata md ON md.movie_id = m.id
		WHERE (m.tmdb_id IS NOT NULL OR m.imdb_id IS NOT NULL)
		  AND (md.movie_id IS NULL OR md.enriched_at < ? OR md.credits_refreshed_at IS NULL)
		ORDER BY m.id
		LIMIT ?
	`

	staleArg := db.ToUnix(staleBefore)
	rows, err := d.pool.Read.QueryContext(ctx, query, staleArg, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	candidates := make([]domain.EnrichmentCandidate, 0)
	for rows.Next() {
		var c domain.EnrichmentCandidate
		if err := rows.Scan(&c.MovieID); err != nil {
			return nil, err
		}
		candidates = append(candidates, c)
	}

	return candidates, rows.Err()
}
