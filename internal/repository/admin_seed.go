package repository

import (
	"context"
	"database/sql"
	"fmt"

	"moviepickarr/internal/db"
	"moviepickarr/internal/domain"
)

// SqliteAdminSeedRepository backs the break-glass admin seed.
type SqliteAdminSeedRepository struct {
	pool *db.Pool
}

func NewSqliteAdminSeedRepository(pool *db.Pool) *SqliteAdminSeedRepository {
	return &SqliteAdminSeedRepository{pool: pool}
}

type adminSeedMatch struct {
	id       int
	name     string
	role     domain.Role
	archived bool
	hasLogin bool
}

// SeedAdmin resolves the seed in one tx. A nil passwordHash is a probe, so the
// caller runs Argon2 without holding the single writer, then retries.
func (d *SqliteAdminSeedRepository) SeedAdmin(
	ctx context.Context,
	name string,
	username string,
	passwordHash *string,
) (domain.AdminSeedResult, error) {
	tx, err := d.pool.Write.BeginTx(ctx, nil)
	if err != nil {
		return domain.AdminSeedResult{}, fmt.Errorf("begin admin seed transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	matches, err := findAdminSeedMatches(ctx, tx, name)
	if err != nil {
		return domain.AdminSeedResult{}, fmt.Errorf("read admin seed target %q: %w", name, err)
	}

	switch len(matches) {
	case 0:
		if passwordHash == nil {
			return domain.AdminSeedResult{NeedsPasswordHash: true}, nil
		}
		return createSeedAdmin(ctx, tx, name, username, *passwordHash)
	case 1:
		return adoptSeedAdmin(ctx, tx, matches[0], username, passwordHash)
	default:
		names := make([]string, 0, len(matches))
		for _, match := range matches {
			names = append(names, match.name)
		}
		return domain.AdminSeedResult{AmbiguousNames: names}, nil
	}
}

// findAdminSeedMatches matches names case-insensitively. users.name is
// case-sensitive UNIQUE, so the match can be ambiguous ("Bob" and "bob").
func findAdminSeedMatches(ctx context.Context, tx *sql.Tx, name string) ([]adminSeedMatch, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT
			u.id,
			u.name,
			u.role,
			u.archived_at IS NOT NULL,
			EXISTS(SELECT 1 FROM local_accounts la WHERE la.user_id = u.id)
		FROM users u
		WHERE u.name = ? COLLATE NOCASE
		ORDER BY u.id
	`, name)
	if err != nil {
		return nil, err
	}

	matches := make([]adminSeedMatch, 0)
	for rows.Next() {
		var match adminSeedMatch
		if err := rows.Scan(
			&match.id,
			&match.name,
			&match.role,
			&match.archived,
			&match.hasLogin,
		); err != nil {
			_ = rows.Close()
			return nil, err
		}
		matches = append(matches, match)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	return matches, nil
}

func createSeedAdmin(
	ctx context.Context,
	tx *sql.Tx,
	name string,
	username string,
	passwordHash string,
) (domain.AdminSeedResult, error) {
	result, err := tx.ExecContext(ctx, "INSERT INTO users (name, role) VALUES (?, 'admin')", name)
	if err != nil {
		return domain.AdminSeedResult{}, fmt.Errorf(
			"create admin member %q: %w",
			name,
			mapAdminSeedConstraint(err, "member name"),
		)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return domain.AdminSeedResult{}, fmt.Errorf("read created admin member id: %w", err)
	}
	if err := insertSeedLogin(ctx, tx, int(id), username, passwordHash); err != nil {
		return domain.AdminSeedResult{}, err
	}

	seeded := domain.AdminSeedResult{
		UserID:       int(id),
		Name:         name,
		Created:      true,
		LoginCreated: true,
	}
	return commitAdminSeed(tx, seeded)
}

func adoptSeedAdmin(
	ctx context.Context,
	tx *sql.Tx,
	match adminSeedMatch,
	username string,
	passwordHash *string,
) (domain.AdminSeedResult, error) {
	if match.archived {
		return domain.AdminSeedResult{}, fmt.Errorf(
			"%w: member %q is archived; choose unused MPA_ADMIN_NAME and MPA_ADMIN_USERNAME values, then restore this member explicitly",
			domain.ErrInvalidState,
			match.name,
		)
	}

	if !match.hasLogin && passwordHash == nil {
		return domain.AdminSeedResult{
			UserID:            match.id,
			Name:              match.name,
			NeedsPasswordHash: true,
		}, nil
	}

	seeded := domain.AdminSeedResult{
		UserID:         match.id,
		Name:           match.name,
		LoginPreserved: match.hasLogin,
	}
	if match.role != domain.RoleAdmin {
		result, err := tx.ExecContext(ctx,
			"UPDATE users SET role = 'admin' WHERE id = ? AND archived_at IS NULL",
			match.id,
		)
		if err != nil {
			return domain.AdminSeedResult{}, fmt.Errorf(
				"promote member %d to admin: %w",
				match.id,
				err,
			)
		}
		affected, err := result.RowsAffected()
		if err != nil {
			return domain.AdminSeedResult{}, fmt.Errorf(
				"read promotion result for member %d: %w",
				match.id,
				err,
			)
		}
		if affected == 0 {
			return domain.AdminSeedResult{}, fmt.Errorf(
				"%w: active member %d",
				domain.ErrNotFound,
				match.id,
			)
		}
		seeded.Promoted = true
	}

	if !match.hasLogin {
		if err := insertSeedLogin(ctx, tx, match.id, username, *passwordHash); err != nil {
			return domain.AdminSeedResult{}, err
		}
		if err := retireSeedInvite(ctx, tx, match.id); err != nil {
			return domain.AdminSeedResult{}, err
		}
		seeded.LoginCreated = true
	}

	return commitAdminSeed(tx, seeded)
}

func insertSeedLogin(
	ctx context.Context,
	tx *sql.Tx,
	userID int,
	username string,
	passwordHash string,
) error {
	result, err := tx.ExecContext(ctx, `
		INSERT INTO local_accounts (user_id, username, password_hash)
		SELECT ?, ?, ?
		FROM users
		WHERE id = ? AND archived_at IS NULL`,
		userID, username, passwordHash, userID)
	if err != nil {
		return fmt.Errorf(
			"create local login for member %d: %w",
			userID,
			mapAdminSeedConstraint(err, "username"),
		)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read local login result for member %d: %w", userID, err)
	}
	if affected == 0 {
		return fmt.Errorf("%w: active member %d", domain.ErrNotFound, userID)
	}
	return nil
}

// retireSeedInvite retires the member's invite, so an old claim link cannot
// reset the seeded admin's password.
func retireSeedInvite(ctx context.Context, tx *sql.Tx, userID int) error {
	if _, err := tx.ExecContext(ctx, `
		UPDATE invites
		SET revoked_at = unixepoch()
		WHERE user_id = ? AND used_at IS NULL AND revoked_at IS NULL
	`, userID); err != nil {
		return fmt.Errorf("retire current invite for seeded admin %d: %w", userID, err)
	}
	return nil
}

func commitAdminSeed(tx *sql.Tx, result domain.AdminSeedResult) (domain.AdminSeedResult, error) {
	if err := tx.Commit(); err != nil {
		return domain.AdminSeedResult{}, fmt.Errorf("commit admin seed transaction: %w", err)
	}
	return result, nil
}

func mapAdminSeedConstraint(err error, identity string) error {
	if db.IsUniqueViolation(err) {
		return fmt.Errorf("%w: %s already exists", domain.ErrConflict, identity)
	}
	if db.IsForeignKeyViolation(err) {
		return fmt.Errorf("%w: active seed member", domain.ErrNotFound)
	}
	return err
}

func (d *SqliteAdminSeedRepository) CountAdmins(ctx context.Context) (int, error) {
	var count int
	err := d.pool.Read.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM users WHERE role = 'admin' AND archived_at IS NULL",
	).Scan(&count)
	if err != nil {
		return 0, err
	}
	return count, nil
}
