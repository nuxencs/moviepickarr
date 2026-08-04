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

// SqliteSessionRepository is the session store over the `sessions` table.
// Reads route to the read pool, mutations to the write pool, matching the
// single-writer discipline the rest of the repositories follow.
type SqliteSessionRepository struct {
	pool *db.Pool
}

func NewSqliteSessionRepository(pool *db.Pool) *SqliteSessionRepository {
	return &SqliteSessionRepository{pool: pool}
}

// sessionSelect is THE session-with-role projection: FindByTokenHash joins the
// member's live role so requireSession validates and authorizes off one read.
const sessionSelect = `
	SELECT
		s.id,
		s.public_id,
		s.user_id,
		s.token_hash,
		s.expires_at,
		s.last_seen_at,
		s.user_agent,
		s.created_at,
		u.role
	FROM sessions s
	JOIN users u ON u.id = s.user_id AND u.archived_at IS NULL`

// scanSession reads the bare session row (no role join) the device list needs.
// It duplicates scanAuthSession's column order minus u.role rather than sharing
// it: database/sql wants every destination in one Scan call, so the two shapes
// can't be composed without a slice of anys and a cast per field.
func scanSession(scanner rowScanner) (*domain.Session, error) {
	s := &domain.Session{}
	var expiresAt, lastSeenAt, createdAt int64
	var userAgent sql.NullString

	if err := scanner.Scan(
		&s.ID,
		&s.PublicID,
		&s.UserID,
		&s.TokenHash,
		&expiresAt,
		&lastSeenAt,
		&userAgent,
		&createdAt,
	); err != nil {
		return nil, err
	}

	s.ExpiresAt = db.FromUnix(expiresAt)
	s.LastSeenAt = db.FromUnix(lastSeenAt)
	s.CreatedAt = db.FromUnix(createdAt)
	if userAgent.Valid {
		s.UserAgent = &userAgent.String
	}
	return s, nil
}

func scanAuthSession(scanner rowScanner) (*domain.AuthSession, error) {
	as := &domain.AuthSession{}
	var expiresAt int64
	var lastSeenAt int64
	var createdAt int64
	var userAgent sql.NullString

	if err := scanner.Scan(
		&as.ID,
		&as.PublicID,
		&as.UserID,
		&as.TokenHash,
		&expiresAt,
		&lastSeenAt,
		&userAgent,
		&createdAt,
		&as.Role,
	); err != nil {
		return nil, err
	}

	as.ExpiresAt = db.FromUnix(expiresAt)
	as.LastSeenAt = db.FromUnix(lastSeenAt)
	as.CreatedAt = db.FromUnix(createdAt)
	if userAgent.Valid {
		as.UserAgent = &userAgent.String
	}
	return as, nil
}

func (d *SqliteSessionRepository) Create(ctx context.Context, s domain.Session) error {
	query := `
		INSERT INTO sessions (
			public_id, token_hash, user_id, expires_at, last_seen_at, user_agent, created_at
		)
		SELECT ?, ?, ?, ?, ?, ?, ?
		FROM users
		WHERE id = ? AND archived_at IS NULL
	`
	res, err := d.pool.Write.ExecContext(ctx, query,
		s.PublicID,
		s.TokenHash,
		s.UserID,
		db.ToUnix(s.ExpiresAt),
		db.ToUnix(s.LastSeenAt),
		s.UserAgent,
		db.ToUnix(s.CreatedAt),
		s.UserID,
	)
	if err != nil {
		return err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return fmt.Errorf("%w: active member %d", domain.ErrNotFound, s.UserID)
	}
	return nil
}

func (d *SqliteSessionRepository) FindByTokenHash(ctx context.Context, tokenHash string) (*domain.AuthSession, error) {
	row := d.pool.Read.QueryRowContext(ctx, sessionSelect+" WHERE s.token_hash = ?", tokenHash)
	return scanAuthSession(row)
}

func (d *SqliteSessionRepository) TouchLastSeen(ctx context.Context, id int64, lastSeen time.Time) error {
	query := "UPDATE sessions SET last_seen_at = ? WHERE id = ?"
	_, err := d.pool.Write.ExecContext(ctx, query, db.ToUnix(lastSeen), id)
	return err
}

func (d *SqliteSessionRepository) DeleteByTokenHash(ctx context.Context, tokenHash string) error {
	query := "DELETE FROM sessions WHERE token_hash = ?"
	_, err := d.pool.Write.ExecContext(ctx, query, tokenHash)
	return err
}

func (d *SqliteSessionRepository) DeleteByUserID(ctx context.Context, userID int) (int64, error) {
	query := "DELETE FROM sessions WHERE user_id = ?"
	res, err := d.pool.Write.ExecContext(ctx, query, userID)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func (d *SqliteSessionRepository) DeleteOthersByUserID(ctx context.Context, userID int, keepTokenHash string) (int64, error) {
	query := "DELETE FROM sessions WHERE user_id = ? AND token_hash <> ?"
	res, err := d.pool.Write.ExecContext(ctx, query, userID, keepTokenHash)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func (d *SqliteSessionRepository) DeleteByPublicIDForUser(ctx context.Context, publicID string, userID int) (string, error) {
	// user_id is the authorization, not a filter: without it any member could
	// revoke any session by guessing a public handle. RETURNING hands back what was
	// actually removed, so the caller learns whether it just ended its own
	// device in the same statement.
	query := "DELETE FROM sessions WHERE public_id = ? AND user_id = ? RETURNING token_hash"
	var tokenHash string
	err := d.pool.Write.QueryRowContext(ctx, query, publicID, userID).Scan(&tokenHash)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return tokenHash, nil
}

func (d *SqliteSessionRepository) ListLiveByUserID(ctx context.Context, userID int, now, idleCutoff time.Time) ([]domain.Session, error) {
	// Live mirrors Authenticate's two windows (strict >), so the list holds
	// exactly the sessions that would still authenticate. Ordered by most recent
	// activity, id breaking ties, so the device list is stable across reads.
	query := `
		SELECT id, public_id, user_id, token_hash, expires_at, last_seen_at, user_agent, created_at
		FROM sessions
		WHERE user_id = ? AND expires_at > ? AND last_seen_at > ?
		ORDER BY last_seen_at DESC, id DESC`

	rows, err := d.pool.Read.QueryContext(ctx, query, userID, db.ToUnix(now), db.ToUnix(idleCutoff))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	sessions := []domain.Session{}
	for rows.Next() {
		s, err := scanSession(rows)
		if err != nil {
			return nil, err
		}
		sessions = append(sessions, *s)
	}
	return sessions, rows.Err()
}

func (d *SqliteSessionRepository) DeleteExpired(ctx context.Context, now, idleCutoff time.Time) (int64, error) {
	query := "DELETE FROM sessions WHERE expires_at <= ? OR last_seen_at <= ?"
	res, err := d.pool.Write.ExecContext(ctx, query, db.ToUnix(now), db.ToUnix(idleCutoff))
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
