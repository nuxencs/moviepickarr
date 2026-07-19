package repository

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"moviepickarr/internal/db"
	"moviepickarr/internal/domain"
)

// SqliteOIDCIdentityRepository is the linked-identity store over the 009
// oidc_identities table. Reads route to the read pool and mutations to the write
// pool, matching the single-writer discipline the other repositories follow.
type SqliteOIDCIdentityRepository struct {
	pool *db.Pool
}

func NewSqliteOIDCIdentityRepository(pool *db.Pool) *SqliteOIDCIdentityRepository {
	return &SqliteOIDCIdentityRepository{pool: pool}
}

// oidcIdentitySelect is THE oidc_identities projection: every identity read
// starts from this exact column list and scans via scanOIDCIdentity.
const oidcIdentitySelect = `
	SELECT
		id,
		user_id,
		issuer,
		subject,
		email,
		preferred_username,
		last_login_at
	FROM oidc_identities`

func scanOIDCIdentity(scanner rowScanner) (*domain.OIDCIdentity, error) {
	oi := &domain.OIDCIdentity{}
	var email sql.NullString
	var preferredUsername sql.NullString
	var lastLoginAt sql.NullInt64

	if err := scanner.Scan(
		&oi.ID,
		&oi.UserID,
		&oi.Issuer,
		&oi.Subject,
		&email,
		&preferredUsername,
		&lastLoginAt,
	); err != nil {
		return nil, err
	}

	if email.Valid {
		oi.Email = &email.String
	}
	if preferredUsername.Valid {
		oi.PreferredUsername = &preferredUsername.String
	}
	oi.LastLoginAt = unixTimePtr(lastLoginAt)
	return oi, nil
}

func (d *SqliteOIDCIdentityRepository) FindByIssuerSubject(ctx context.Context, issuer, subject string) (*domain.OIDCIdentity, error) {
	return scanOIDCIdentity(d.pool.Read.QueryRowContext(ctx,
		oidcIdentitySelect+" WHERE issuer = ? AND subject = ?", issuer, subject))
}

func (d *SqliteOIDCIdentityRepository) FindByUserID(ctx context.Context, userID int) (*domain.OIDCIdentity, error) {
	return scanOIDCIdentity(d.pool.Read.QueryRowContext(ctx,
		oidcIdentitySelect+" WHERE user_id = ?", userID))
}

func (d *SqliteOIDCIdentityRepository) Insert(ctx context.Context, id domain.OIDCIdentity, createdAt time.Time) error {
	query := `
		INSERT INTO oidc_identities (
			user_id, issuer, subject, email, preferred_username, last_login_at, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`
	_, err := d.pool.Write.ExecContext(ctx, query,
		id.UserID,
		id.Issuer,
		id.Subject,
		id.Email,
		id.PreferredUsername,
		db.ToUnixPtr(id.LastLoginAt),
		db.ToUnix(createdAt),
		db.ToUnix(createdAt),
	)
	if err != nil {
		// Either UNIQUE (user_id already linked, or this issuer+subject linked to
		// another member) is a client conflict, not a 500; an insert against a
		// missing member trips the user_id FK, meaning the member does not exist.
		if db.IsUniqueViolation(err) {
			return fmt.Errorf("%w: identity already linked", domain.ErrConflict)
		}
		if db.IsForeignKeyViolation(err) {
			return fmt.Errorf("%w: member %d", domain.ErrNotFound, id.UserID)
		}
		return err
	}
	return nil
}

func (d *SqliteOIDCIdentityRepository) TouchLogin(ctx context.Context, id int64, email, preferredUsername *string, lastLoginAt, updatedAt time.Time) error {
	query := `
		UPDATE oidc_identities
		SET email = ?, preferred_username = ?, last_login_at = ?, updated_at = ?
		WHERE id = ?
	`
	res, err := d.pool.Write.ExecContext(ctx, query,
		email, preferredUsername, db.ToUnix(lastLoginAt), db.ToUnix(updatedAt), id)
	if err != nil {
		return err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (d *SqliteOIDCIdentityRepository) DeleteByUserID(ctx context.Context, userID int) (int64, error) {
	res, err := d.pool.Write.ExecContext(ctx,
		"DELETE FROM oidc_identities WHERE user_id = ?", userID)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
