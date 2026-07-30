package repository

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"moviepickarr/internal/db"
	"moviepickarr/internal/domain"
)

// SqliteSessionRepository is the session store over the 009 `sessions` table.
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
		s.user_id,
		s.token_hash,
		s.expires_at,
		s.last_seen_at,
		s.user_agent,
		s.ip,
		s.created_at,
		u.role
	FROM sessions s
	JOIN users u ON u.id = s.user_id AND u.archived_at IS NULL`

func scanAuthSession(scanner rowScanner) (*domain.AuthSession, error) {
	as := &domain.AuthSession{}
	var expiresAt int64
	var lastSeenAt int64
	var createdAt int64
	var userAgent sql.NullString
	var ip sql.NullString

	if err := scanner.Scan(
		&as.ID,
		&as.UserID,
		&as.TokenHash,
		&expiresAt,
		&lastSeenAt,
		&userAgent,
		&ip,
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
	if ip.Valid {
		as.IP = &ip.String
	}

	return as, nil
}

func (d *SqliteSessionRepository) Create(ctx context.Context, s domain.Session) error {
	query := `
		INSERT INTO sessions (
			token_hash, user_id, expires_at, last_seen_at, user_agent, ip, created_at
		)
		SELECT ?, ?, ?, ?, ?, ?, ?
		FROM users
		WHERE id = ? AND archived_at IS NULL
	`
	res, err := d.pool.Write.ExecContext(ctx, query,
		s.TokenHash,
		s.UserID,
		db.ToUnix(s.ExpiresAt),
		db.ToUnix(s.LastSeenAt),
		s.UserAgent,
		s.IP,
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

func (d *SqliteSessionRepository) CountOthersByUserID(ctx context.Context, userID int, keepTokenHash string, now, idleCutoff time.Time) (int, error) {
	// Live mirrors Authenticate's two windows (strict >), so the count matches
	// exactly the sessions that would still authenticate.
	query := `
		SELECT COUNT(*) FROM sessions
		WHERE user_id = ? AND token_hash <> ? AND expires_at > ? AND last_seen_at > ?`
	var n int
	err := d.pool.Read.QueryRowContext(ctx, query, userID, keepTokenHash, db.ToUnix(now), db.ToUnix(idleCutoff)).Scan(&n)
	return n, err
}

func (d *SqliteSessionRepository) DeleteExpired(ctx context.Context, now, idleCutoff time.Time) (int64, error) {
	query := "DELETE FROM sessions WHERE expires_at <= ? OR last_seen_at <= ?"
	res, err := d.pool.Write.ExecContext(ctx, query, db.ToUnix(now), db.ToUnix(idleCutoff))
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
