package repository

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"moviepickarr/internal/domain"
)

type SqliteMovieCreditsRepository struct {
	db *sql.DB
}

func NewSqliteMovieCreditsRepository(db *sql.DB) *SqliteMovieCreditsRepository {
	return &SqliteMovieCreditsRepository{db: db}
}

func (d *SqliteMovieCreditsRepository) ReplaceCredits(ctx context.Context, movieID int, credits []domain.MovieCredit) error {
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	// Upsert people first so the credit FKs resolve. A person's name/photo can
	// change on TMDB, so re-enrichment refreshes the shared row. Both statements
	// run ~15-40 times per movie, so prepare each once and reuse it — modernc's
	// sqlite recompiles the SQL text on every bare ExecContext, and reusing the
	// compiled statement also shortens the single shared connection's write hold.
	upsertPerson, err := tx.PrepareContext(ctx, `
		INSERT INTO people (id, name, profile_path) VALUES (?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			name         = excluded.name,
			profile_path = excluded.profile_path
	`)
	if err != nil {
		return err
	}
	defer func() { _ = upsertPerson.Close() }()
	for i := range credits {
		p := credits[i].Person
		if _, err := upsertPerson.ExecContext(ctx, p.ID, p.Name, p.ProfilePath); err != nil {
			return err
		}
	}

	if _, err := tx.ExecContext(ctx, `DELETE FROM movie_credits WHERE movie_id = ?`, movieID); err != nil {
		return err
	}

	insertCredit, err := tx.PrepareContext(ctx, `
		INSERT INTO movie_credits (movie_id, person_id, kind, "character", job, department, cast_order)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return err
	}
	defer func() { _ = insertCredit.Close() }()
	for i := range credits {
		c := credits[i]
		if _, err := insertCredit.ExecContext(ctx,
			movieID, c.Person.ID, c.Kind, c.Character, c.Job, c.Department, c.CastOrder,
		); err != nil {
			return err
		}
	}

	// Stamp the marker even when credits are empty, so genuinely credit-less
	// titles stop being backfill candidates.
	stamp := `UPDATE movie_metadata SET credits_refreshed_at = CURRENT_TIMESTAMP WHERE movie_id = ?`
	if _, err := tx.ExecContext(ctx, stamp, movieID); err != nil {
		return err
	}

	return tx.Commit()
}

func (d *SqliteMovieCreditsRepository) GetCreditsByMovieIDs(ctx context.Context, ids []int) (map[int][]domain.MovieCredit, error) {
	result := make(map[int][]domain.MovieCredit, len(ids))
	if len(ids) == 0 {
		return result, nil
	}

	placeholders := make([]string, len(ids))
	args := make([]any, len(ids))
	for i, id := range ids {
		placeholders[i] = "?"
		args[i] = id
	}

	// 'cast' sorts before 'crew', so each movie lists its cast (in billing
	// order) first, then crew alphabetically.
	query := fmt.Sprintf(`
		SELECT
			mc.movie_id,
			p.id,
			p.name,
			p.profile_path,
			mc.kind,
			mc."character",
			mc.job,
			mc.department,
			mc.cast_order
		FROM movie_credits mc
		JOIN people p ON p.id = mc.person_id
		WHERE mc.movie_id IN (%s)
		ORDER BY mc.movie_id, mc.kind, mc.cast_order, p.name
	`, strings.Join(placeholders, ", "))

	rows, err := d.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var c domain.MovieCredit
		var profile sql.NullString
		if err := rows.Scan(
			&c.MovieID,
			&c.Person.ID,
			&c.Person.Name,
			&profile,
			&c.Kind,
			&c.Character,
			&c.Job,
			&c.Department,
			&c.CastOrder,
		); err != nil {
			return nil, err
		}
		if profile.Valid {
			c.Person.ProfilePath = &profile.String
		}
		result[c.MovieID] = append(result[c.MovieID], c)
	}

	return result, rows.Err()
}
