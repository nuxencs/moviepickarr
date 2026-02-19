package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"moviepickarr/internal/domain"
)

func scanLocalAccount(scanner rowScanner) (*domain.LocalAccount, error) {
	account := &domain.LocalAccount{}
	var lockedUntil sql.NullTime
	var lastLoginAt sql.NullTime
	var createdAt sql.NullTime
	var updatedAt sql.NullTime
	var role string

	if err := scanner.Scan(
		&account.UserID,
		&account.Username,
		&account.PasswordHash,
		&role,
		&account.FailedAttempts,
		&lockedUntil,
		&lastLoginAt,
		&createdAt,
		&updatedAt,
	); err != nil {
		return nil, err
	}

	account.Role = domain.UserRole(role)
	account.LockedUntil = nullTimePtr(lockedUntil)
	account.LastLoginAt = nullTimePtr(lastLoginAt)
	account.CreatedAt = nullTimePtr(createdAt)
	account.UpdatedAt = nullTimePtr(updatedAt)

	return account, nil
}

type SqliteAuthRepository struct {
	db *sql.DB
}

func NewSqliteAuthRepository(db *sql.DB) *SqliteAuthRepository {
	return &SqliteAuthRepository{db: db}
}

func (d *SqliteAuthRepository) CountAccounts(ctx context.Context) (int, error) {
	var count int
	if err := d.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM local_accounts").Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}

func (d *SqliteAuthRepository) CountAdmins(ctx context.Context) (int, error) {
	var count int
	if err := d.db.QueryRowContext(
		ctx,
		"SELECT COUNT(*) FROM local_accounts WHERE role = ?",
		string(domain.RoleAdmin),
	).Scan(&count); err != nil {
		return 0, err
	}

	return count, nil
}

func (d *SqliteAuthRepository) FindAccountByUsername(ctx context.Context, username string) (*domain.LocalAccount, *domain.User, error) {
	query := `
		SELECT
			la.user_id,
			la.username,
			la.password_hash,
			la.role,
			la.failed_attempts,
			la.locked_until,
			la.last_login_at,
			la.created_at,
			la.updated_at,
			u.id,
			u.name,
			u.created_at,
			u.updated_at
		FROM local_accounts la
		JOIN users u ON u.id = la.user_id
		WHERE la.username = ? COLLATE NOCASE
	`

	row := d.db.QueryRowContext(ctx, query, username)

	account := &domain.LocalAccount{}
	user := &domain.User{HasAccount: true}
	var lockedUntil sql.NullTime
	var lastLoginAt sql.NullTime
	var accountCreatedAt sql.NullTime
	var accountUpdatedAt sql.NullTime
	var userCreatedAt sql.NullTime
	var userUpdatedAt sql.NullTime
	var role string

	err := row.Scan(
		&account.UserID,
		&account.Username,
		&account.PasswordHash,
		&role,
		&account.FailedAttempts,
		&lockedUntil,
		&lastLoginAt,
		&accountCreatedAt,
		&accountUpdatedAt,
		&user.ID,
		&user.Name,
		&userCreatedAt,
		&userUpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, fmt.Errorf("%w: username %q", domain.ErrNotFound, username)
	}
	if err != nil {
		return nil, nil, err
	}

	account.Role = domain.UserRole(role)
	account.LockedUntil = nullTimePtr(lockedUntil)
	account.LastLoginAt = nullTimePtr(lastLoginAt)
	account.CreatedAt = nullTimePtr(accountCreatedAt)
	account.UpdatedAt = nullTimePtr(accountUpdatedAt)

	user.Username = account.Username
	user.Role = account.Role
	user.CreatedAt = nullTimePtr(userCreatedAt)
	user.UpdatedAt = nullTimePtr(userUpdatedAt)

	return account, user, nil
}

func (d *SqliteAuthRepository) FindAccountByUserID(ctx context.Context, userID int) (*domain.LocalAccount, error) {
	query := `
		SELECT
			user_id,
			username,
			password_hash,
			role,
			failed_attempts,
			locked_until,
			last_login_at,
			created_at,
			updated_at
		FROM local_accounts
		WHERE user_id = ?
	`

	account, err := scanLocalAccount(d.db.QueryRowContext(ctx, query, userID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("%w: account user id %d", domain.ErrNotFound, userID)
	}
	if err != nil {
		return nil, err
	}

	return account, nil
}

func (d *SqliteAuthRepository) UpsertAccount(ctx context.Context, userID int, username, passwordHash string, role domain.UserRole) (*domain.LocalAccount, error) {
	query := `
		INSERT INTO local_accounts (user_id, username, password_hash, role)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(user_id)
		DO UPDATE SET
			username = excluded.username,
			password_hash = excluded.password_hash,
			role = excluded.role,
			failed_attempts = 0,
			locked_until = NULL,
			updated_at = CURRENT_TIMESTAMP
	`

	if _, err := d.db.ExecContext(ctx, query, userID, username, passwordHash, string(role)); err != nil {
		return nil, err
	}

	return d.FindAccountByUserID(ctx, userID)
}

func (d *SqliteAuthRepository) UpdateLoginState(ctx context.Context, userID int, failedAttempts int, lockedUntil *time.Time, lastLoginAt *time.Time) error {
	query := `
		UPDATE local_accounts
		SET
			failed_attempts = ?,
			locked_until = ?,
			last_login_at = ?,
			updated_at = CURRENT_TIMESTAMP
		WHERE user_id = ?
	`

	_, err := d.db.ExecContext(ctx, query, failedAttempts, lockedUntil, lastLoginAt, userID)
	return err
}

func (d *SqliteAuthRepository) CreateSession(ctx context.Context, session *domain.Session) error {
	query := `
		INSERT INTO sessions (id, user_id, token_hash, expires_at, user_agent, ip)
		VALUES (?, ?, ?, ?, ?, ?)
	`

	_, err := d.db.ExecContext(
		ctx,
		query,
		session.ID,
		session.UserID,
		session.TokenHash,
		session.ExpiresAt,
		session.UserAgent,
		session.IP,
	)
	return err
}

func (d *SqliteAuthRepository) FindSessionByTokenHash(ctx context.Context, tokenHash string) (*domain.Session, *domain.LocalAccount, *domain.User, error) {
	query := `
		SELECT
			s.id,
			s.user_id,
			s.token_hash,
			s.expires_at,
			s.last_seen_at,
			s.user_agent,
			s.ip,
			s.created_at,
			la.username,
			la.password_hash,
			la.role,
			la.failed_attempts,
			la.locked_until,
			la.last_login_at,
			la.created_at,
			la.updated_at,
			u.id,
			u.name,
			u.created_at,
			u.updated_at
		FROM sessions s
		JOIN local_accounts la ON la.user_id = s.user_id
		JOIN users u ON u.id = s.user_id
		WHERE s.token_hash = ?
	`

	row := d.db.QueryRowContext(ctx, query, tokenHash)

	session := &domain.Session{}
	account := &domain.LocalAccount{}
	user := &domain.User{HasAccount: true}
	var expiresAt time.Time
	var sessionLastSeenAt sql.NullTime
	var sessionCreatedAt sql.NullTime
	var accountRole string
	var accountLockedUntil sql.NullTime
	var accountLastLoginAt sql.NullTime
	var accountCreatedAt sql.NullTime
	var accountUpdatedAt sql.NullTime
	var userCreatedAt sql.NullTime
	var userUpdatedAt sql.NullTime

	err := row.Scan(
		&session.ID,
		&session.UserID,
		&session.TokenHash,
		&expiresAt,
		&sessionLastSeenAt,
		&session.UserAgent,
		&session.IP,
		&sessionCreatedAt,
		&account.Username,
		&account.PasswordHash,
		&accountRole,
		&account.FailedAttempts,
		&accountLockedUntil,
		&accountLastLoginAt,
		&accountCreatedAt,
		&accountUpdatedAt,
		&user.ID,
		&user.Name,
		&userCreatedAt,
		&userUpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, nil, fmt.Errorf("%w: session", domain.ErrNotFound)
	}
	if err != nil {
		return nil, nil, nil, err
	}

	session.ExpiresAt = expiresAt
	session.LastSeenAt = nullTimePtr(sessionLastSeenAt)
	session.CreatedAt = nullTimePtr(sessionCreatedAt)

	account.UserID = session.UserID
	account.Role = domain.UserRole(accountRole)
	account.LockedUntil = nullTimePtr(accountLockedUntil)
	account.LastLoginAt = nullTimePtr(accountLastLoginAt)
	account.CreatedAt = nullTimePtr(accountCreatedAt)
	account.UpdatedAt = nullTimePtr(accountUpdatedAt)

	user.Username = account.Username
	user.Role = account.Role
	user.CreatedAt = nullTimePtr(userCreatedAt)
	user.UpdatedAt = nullTimePtr(userUpdatedAt)

	return session, account, user, nil
}

func (d *SqliteAuthRepository) DeleteSessionByTokenHash(ctx context.Context, tokenHash string) error {
	_, err := d.db.ExecContext(ctx, "DELETE FROM sessions WHERE token_hash = ?", tokenHash)
	return err
}

func (d *SqliteAuthRepository) DeleteExpiredSessions(ctx context.Context, now time.Time) error {
	_, err := d.db.ExecContext(ctx, "DELETE FROM sessions WHERE expires_at <= ?", now)
	return err
}
