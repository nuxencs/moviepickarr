package repository

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"moviepickarr/internal/db"
	"moviepickarr/internal/domain"
)

// SqliteLocalAccountRepository is the local-login store over local_accounts.
type SqliteLocalAccountRepository struct {
	pool *db.Pool
}

func NewSqliteLocalAccountRepository(pool *db.Pool) *SqliteLocalAccountRepository {
	return &SqliteLocalAccountRepository{pool: pool}
}

// localAccountSelect is the one local_accounts projection; keep it in step with scanLocalAccount.
const localAccountSelect = `
	SELECT
		la.user_id,
		la.username,
		la.password_hash,
		la.failed_attempts,
		la.locked_until,
		la.last_login_at
	FROM local_accounts la
	JOIN users u ON u.id = la.user_id AND u.archived_at IS NULL`

func scanLocalAccount(scanner rowScanner) (*domain.LocalAccount, error) {
	acct := &domain.LocalAccount{}
	var lockedUntil sql.NullInt64
	var lastLoginAt sql.NullInt64

	if err := scanner.Scan(
		&acct.UserID,
		&acct.Username,
		&acct.PasswordHash,
		&acct.FailedAttempts,
		&lockedUntil,
		&lastLoginAt,
	); err != nil {
		return nil, err
	}

	acct.LockedUntil = unixTimePtr(lockedUntil)
	acct.LastLoginAt = unixTimePtr(lastLoginAt)
	return acct, nil
}

func (d *SqliteLocalAccountRepository) FindByUsername(ctx context.Context, username string) (*domain.LocalAccount, error) {
	// The column's NOCASE collation makes this match case-insensitive.
	return scanLocalAccount(d.pool.Read.QueryRowContext(ctx, localAccountSelect+" WHERE la.username = ?", username))
}

func (d *SqliteLocalAccountRepository) FindByUserID(ctx context.Context, userID int) (*domain.LocalAccount, error) {
	return scanLocalAccount(d.pool.Read.QueryRowContext(ctx, localAccountSelect+" WHERE la.user_id = ?", userID))
}

func (d *SqliteLocalAccountRepository) Create(ctx context.Context, userID int, username, passwordHash string) error {
	query := `
		INSERT INTO local_accounts (user_id, username, password_hash)
		SELECT ?, ?, ?
		FROM users
		WHERE id = ? AND archived_at IS NULL
	`
	res, err := d.pool.Write.ExecContext(ctx, query, userID, username, passwordHash, userID)
	if err != nil {
		// A NOCASE collision is a 409, not a 500; an FK failure means no such member.
		if db.IsUniqueViolation(err) {
			return fmt.Errorf("%w: username already taken", domain.ErrConflict)
		}
		if db.IsForeignKeyViolation(err) {
			return fmt.Errorf("%w: member %d", domain.ErrNotFound, userID)
		}
		return err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return fmt.Errorf("%w: active member %d", domain.ErrNotFound, userID)
	}
	return nil
}

func (d *SqliteLocalAccountRepository) UpdatePasswordHash(ctx context.Context, userID int, passwordHash string, updatedAt time.Time) error {
	query := `
		UPDATE local_accounts
		SET password_hash = ?, updated_at = ?
		WHERE user_id = ?
			AND EXISTS (
				SELECT 1 FROM users u
				WHERE u.id = local_accounts.user_id AND u.archived_at IS NULL
			)`
	return d.execExpectingRow(ctx, query, passwordHash, db.ToUnix(updatedAt), userID)
}

func (d *SqliteLocalAccountRepository) UpdatePasswordAndClearLockout(ctx context.Context, userID int, passwordHash string, updatedAt time.Time) error {
	query := `
		UPDATE local_accounts
		SET password_hash = ?, failed_attempts = 0, locked_until = NULL, updated_at = ?
		WHERE user_id = ?
			AND EXISTS (
				SELECT 1 FROM users u
				WHERE u.id = local_accounts.user_id AND u.archived_at IS NULL
			)
	`
	return d.execExpectingRow(ctx, query, passwordHash, db.ToUnix(updatedAt), userID)
}

func (d *SqliteLocalAccountRepository) RecordFailedAttempt(
	ctx context.Context,
	userID int,
	expectedPasswordHash string,
	lockThreshold int,
	lockUntil time.Time,
	updatedAt time.Time,
) error {
	query := `
		UPDATE local_accounts
		SET failed_attempts = failed_attempts + 1,
			locked_until = CASE
				WHEN failed_attempts + 1 >= ? THEN ?
				ELSE NULL
			END,
			updated_at = ?
		WHERE user_id = ?
			AND password_hash = ?
			AND EXISTS (
				SELECT 1 FROM users u
				WHERE u.id = local_accounts.user_id AND u.archived_at IS NULL
			)`
	return d.execCredentialCAS(
		ctx,
		query,
		lockThreshold,
		db.ToUnix(lockUntil),
		db.ToUnix(updatedAt),
		userID,
		expectedPasswordHash,
	)
}

func (d *SqliteLocalAccountRepository) RecordSuccessfulLogin(
	ctx context.Context,
	userID int,
	expectedPasswordHash string,
	newPasswordHash *string,
	lastLoginAt, updatedAt time.Time,
) error {
	// A nil newPasswordHash keeps the stored hash (COALESCE).
	query := `
		UPDATE local_accounts
		SET password_hash = COALESCE(?, password_hash),
			failed_attempts = 0,
			locked_until = NULL,
			last_login_at = ?,
			updated_at = ?
		WHERE user_id = ?
			AND password_hash = ?
			AND EXISTS (
				SELECT 1 FROM users u
				WHERE u.id = local_accounts.user_id AND u.archived_at IS NULL
			)
	`
	return d.execCredentialCAS(
		ctx,
		query,
		newPasswordHash,
		db.ToUnix(lastLoginAt),
		db.ToUnix(updatedAt),
		userID,
		expectedPasswordHash,
	)
}

func (d *SqliteLocalAccountRepository) execCredentialCAS(
	ctx context.Context,
	query string,
	args ...any,
) error {
	res, err := d.pool.Write.ExecContext(ctx, query, args...)
	if err != nil {
		return err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected != 1 {
		return domain.ErrInvalidCredentials
	}
	return nil
}

func (d *SqliteLocalAccountRepository) Delete(ctx context.Context, userID int) error {
	return d.execExpectingRow(ctx, "DELETE FROM local_accounts WHERE user_id = ?", userID)
}

func (d *SqliteLocalAccountRepository) HasLinkedIdentity(ctx context.Context, userID int) (bool, error) {
	var exists int
	err := d.pool.Read.QueryRowContext(ctx,
		`SELECT EXISTS (SELECT 1 FROM oidc_identities WHERE user_id = ?)
		 FROM users WHERE id = ? AND archived_at IS NULL`,
		userID, userID,
	).Scan(&exists)
	if err != nil {
		return false, err
	}
	return exists == 1, nil
}

func (d *SqliteLocalAccountRepository) GetMemberIdentity(ctx context.Context, userID int) (*domain.MemberIdentity, error) {
	// Link-state flags derive from row presence; none is stored.
	query := `
		SELECT
			u.id,
			u.name,
			la.username,
			u.role,
			EXISTS (SELECT 1 FROM oidc_identities oi WHERE oi.user_id = u.id)
		FROM users u
		LEFT JOIN local_accounts la ON la.user_id = u.id
		WHERE u.id = ? AND u.archived_at IS NULL
	`
	id := &domain.MemberIdentity{}
	var username sql.NullString
	var linked int
	if err := d.pool.Read.QueryRowContext(ctx, query, userID).Scan(
		&id.ID, &id.DisplayName, &username, &id.Role, &linked,
	); err != nil {
		return nil, err
	}
	if username.Valid {
		id.Username = &username.String
		id.HasLocalLogin = true
	}
	id.HasLinkedIdentity = linked == 1
	return id, nil
}

// execExpectingRow runs a one-row write and maps zero rows to sql.ErrNoRows.
func (d *SqliteLocalAccountRepository) execExpectingRow(ctx context.Context, query string, args ...any) error {
	res, err := d.pool.Write.ExecContext(ctx, query, args...)
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
