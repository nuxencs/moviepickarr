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

// SqliteSessionRepository is the session store.
type SqliteSessionRepository struct {
	pool *db.Pool
}

func NewSqliteSessionRepository(pool *db.Pool) *SqliteSessionRepository {
	return &SqliteSessionRepository{pool: pool}
}

// sessionSelect joins the member's live role, so one read validates and authorizes.
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

// scanSession reads a session row without the role join. Keep its column order
// in step with scanAuthSession.
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
	// user_id is the authorization: without it a guessed handle revokes anyone's session.
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
	// Keep in step with Authenticate's two windows (strict >).
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
