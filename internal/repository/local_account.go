package repository

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"moviepickarr/internal/db"
	"moviepickarr/internal/domain"
)

// SqliteLocalAccountRepository is the local-login store over the 009
// local_accounts table, with one read of oidc_identities for derived
// link-state. Reads route to the read pool and mutations to the write pool,
// matching the single-writer discipline the other repositories follow.
type SqliteLocalAccountRepository struct {
	pool *db.Pool
}

func NewSqliteLocalAccountRepository(pool *db.Pool) *SqliteLocalAccountRepository {
	return &SqliteLocalAccountRepository{pool: pool}
}

// localAccountSelect is THE local_accounts projection: every credential read
// starts from this exact column list and scans via scanLocalAccount.
const localAccountSelect = `
	SELECT
		user_id,
		username,
		password_hash,
		failed_attempts,
		locked_until,
		last_login_at
	FROM local_accounts`

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
	// The NOCASE collation on the username column folds case, so a plain equality
	// match is the trimmed/case-insensitive lookup login needs.
	return scanLocalAccount(d.pool.Read.QueryRowContext(ctx, localAccountSelect+" WHERE username = ?", username))
}

func (d *SqliteLocalAccountRepository) FindByUserID(ctx context.Context, userID int) (*domain.LocalAccount, error) {
	return scanLocalAccount(d.pool.Read.QueryRowContext(ctx, localAccountSelect+" WHERE user_id = ?", userID))
}

func (d *SqliteLocalAccountRepository) Create(ctx context.Context, userID int, username, passwordHash string) error {
	query := `
		INSERT INTO local_accounts (user_id, username, password_hash)
		VALUES (?, ?, ?)
	`
	_, err := d.pool.Write.ExecContext(ctx, query, userID, username, passwordHash)
	if err != nil {
		// A NOCASE username collision is a client conflict, not a 500; an insert
		// against a missing member trips the user_id FK, meaning the member does
		// not exist.
		if db.IsUniqueViolation(err) {
			return fmt.Errorf("%w: username already taken", domain.ErrConflict)
		}
		if db.IsForeignKeyViolation(err) {
			return fmt.Errorf("%w: member %d", domain.ErrNotFound, userID)
		}
		return err
	}
	return nil
}

func (d *SqliteLocalAccountRepository) UpdatePasswordHash(ctx context.Context, userID int, passwordHash string, updatedAt time.Time) error {
	query := "UPDATE local_accounts SET password_hash = ?, updated_at = ? WHERE user_id = ?"
	return d.execExpectingRow(ctx, query, passwordHash, db.ToUnix(updatedAt), userID)
}

func (d *SqliteLocalAccountRepository) UpdatePasswordAndClearLockout(ctx context.Context, userID int, passwordHash string, updatedAt time.Time) error {
	query := `
		UPDATE local_accounts
		SET password_hash = ?, failed_attempts = 0, locked_until = NULL, updated_at = ?
		WHERE user_id = ?
	`
	return d.execExpectingRow(ctx, query, passwordHash, db.ToUnix(updatedAt), userID)
}

func (d *SqliteLocalAccountRepository) RecordFailedAttempt(ctx context.Context, userID, failedAttempts int, lockedUntil *time.Time, updatedAt time.Time) error {
	query := "UPDATE local_accounts SET failed_attempts = ?, locked_until = ?, updated_at = ? WHERE user_id = ?"
	return d.execExpectingRow(ctx, query, failedAttempts, db.ToUnixPtr(lockedUntil), db.ToUnix(updatedAt), userID)
}

func (d *SqliteLocalAccountRepository) RecordSuccessfulLogin(ctx context.Context, userID int, newPasswordHash *string, lastLoginAt, updatedAt time.Time) error {
	// A nil newPasswordHash means "keep the stored hash": COALESCE leaves it
	// untouched, so the common no-rehash login is one statement, same as a
	// rehash-on-login.
	query := `
		UPDATE local_accounts
		SET password_hash = COALESCE(?, password_hash),
			failed_attempts = 0,
			locked_until = NULL,
			last_login_at = ?,
			updated_at = ?
		WHERE user_id = ?
	`
	return d.execExpectingRow(ctx, query, newPasswordHash, db.ToUnix(lastLoginAt), db.ToUnix(updatedAt), userID)
}

func (d *SqliteLocalAccountRepository) Delete(ctx context.Context, userID int) error {
	return d.execExpectingRow(ctx, "DELETE FROM local_accounts WHERE user_id = ?", userID)
}

func (d *SqliteLocalAccountRepository) HasLinkedIdentity(ctx context.Context, userID int) (bool, error) {
	var exists int
	err := d.pool.Read.QueryRowContext(ctx,
		"SELECT EXISTS (SELECT 1 FROM oidc_identities WHERE user_id = ?)", userID).Scan(&exists)
	if err != nil {
		return false, err
	}
	return exists == 1, nil
}

func (d *SqliteLocalAccountRepository) GetMemberIdentity(ctx context.Context, userID int) (*domain.MemberIdentity, error) {
	// One read joins the member to its derived link-state: username (and thus
	// hasLocalLogin) from the LEFT JOIN, hasLinkedIdentity from an EXISTS. Both
	// flags are presence-derived, never stored.
	query := `
		SELECT
			u.id,
			u.name,
			la.username,
			u.role,
			EXISTS (SELECT 1 FROM oidc_identities oi WHERE oi.user_id = u.id)
		FROM users u
		LEFT JOIN local_accounts la ON la.user_id = u.id
		WHERE u.id = ?
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

// execExpectingRow runs a write that must hit exactly one row and maps a
// zero-row result to sql.ErrNoRows, so callers can tell "no such local login"
// from a real failure.
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
