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

// SqliteInviteRepository is the invite/claim store over the current invites
// schema, joined to users + local_accounts for claim context. Reads route to
// the read pool and mutations to the write pool, matching the single-writer
// discipline the other repositories follow.
type SqliteInviteRepository struct {
	pool *db.Pool
}

func NewSqliteInviteRepository(pool *db.Pool) *SqliteInviteRepository {
	return &SqliteInviteRepository{pool: pool}
}

func (d *SqliteInviteRepository) Create(
	ctx context.Context,
	userID int,
	publicID, tokenHash string,
	expiresAt, createdAt time.Time,
	createdBy *int,
	passwordReset ...bool,
) error {
	tx, err := d.pool.Write.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	var hasLocal, hasIdentity int
	err = tx.QueryRowContext(ctx, `
		SELECT
			EXISTS (SELECT 1 FROM local_accounts la WHERE la.user_id = u.id),
			EXISTS (SELECT 1 FROM oidc_identities oi WHERE oi.user_id = u.id)
		FROM users u
		WHERE u.id = ? AND u.archived_at IS NULL
	`, userID).Scan(&hasLocal, &hasIdentity)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("%w: active member %d", domain.ErrNotFound, userID)
	}
	if err != nil {
		return err
	}
	isPasswordReset := len(passwordReset) > 0 && passwordReset[0]
	if isPasswordReset && hasLocal == 0 {
		return fmt.Errorf("%w: member %d has no local login to reset", domain.ErrConflict, userID)
	}
	if !isPasswordReset && (hasLocal == 1 || hasIdentity == 1) {
		return fmt.Errorf("%w: member %d already has a login credential", domain.ErrConflict, userID)
	}

	_, err = tx.ExecContext(ctx, `
		INSERT INTO invites (public_id, user_id, token_hash, expires_at, created_by, created_at)
		VALUES (?, ?, ?, ?, ?, ?)
	`, publicID, userID, tokenHash, db.ToUnix(expiresAt), createdBy, db.ToUnix(createdAt))
	if err != nil {
		if db.IsUniqueViolation(err) {
			return fmt.Errorf("%w: member %d already has a current invite", domain.ErrConflict, userID)
		}
		if db.IsForeignKeyViolation(err) {
			return fmt.Errorf("%w: member %d", domain.ErrNotFound, userID)
		}
		return err
	}
	return tx.Commit()
}

// ReplaceCurrent changes generations in one writer transaction. The update is
// a compare-and-swap on the immutable public handle; a second click or another
// tab replacing first matches nothing. The insert happens before commit, so any
// constraint/store failure rolls the retirement back.
func (d *SqliteInviteRepository) ReplaceCurrent(
	ctx context.Context,
	currentPublicID, replacementPublicID, tokenHash string,
	expiresAt, createdAt time.Time,
	createdBy *int,
) error {
	tx, err := d.pool.Write.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	var userID int
	err = tx.QueryRowContext(ctx, `
		UPDATE invites
		SET revoked_at = ?
		WHERE public_id = ?
			AND used_at IS NULL
			AND revoked_at IS NULL
		RETURNING user_id
	`, db.ToUnix(createdAt), currentPublicID).Scan(&userID)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("%w: invite generation is stale", domain.ErrConflict)
	}
	if err != nil {
		return err
	}

	res, err := tx.ExecContext(ctx, `
		INSERT INTO invites (public_id, user_id, token_hash, expires_at, created_by, created_at)
		SELECT ?, ?, ?, ?, ?, ?
		FROM users
		WHERE id = ? AND archived_at IS NULL
	`, replacementPublicID, userID, tokenHash, db.ToUnix(expiresAt), createdBy, db.ToUnix(createdAt), userID)
	if err != nil {
		if db.IsUniqueViolation(err) {
			return fmt.Errorf("%w: replacement invite collides with current state", domain.ErrConflict)
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
	if affected != 1 {
		return fmt.Errorf("%w: active member %d", domain.ErrNotFound, userID)
	}
	return tx.Commit()
}

func (d *SqliteInviteRepository) RevokeOpen(ctx context.Context, publicID string, now, revokedAt time.Time) error {
	return d.retireExact(ctx, publicID, now, revokedAt, "expires_at > ?", "open")
}

func (d *SqliteInviteRepository) DismissExpired(ctx context.Context, publicID string, now, revokedAt time.Time) error {
	return d.retireExact(ctx, publicID, now, revokedAt, "expires_at <= ?", "expired")
}

func (d *SqliteInviteRepository) retireExact(
	ctx context.Context,
	publicID string,
	now, retiredAt time.Time,
	statePredicate, stateName string,
) error {
	query := `
		UPDATE invites
		SET revoked_at = ?
		WHERE public_id = ?
			AND used_at IS NULL
			AND revoked_at IS NULL
			AND ` + statePredicate
	res, err := d.pool.Write.ExecContext(
		ctx,
		query,
		db.ToUnix(retiredAt),
		publicID,
		db.ToUnix(now),
	)
	if err != nil {
		return err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return fmt.Errorf("%w: invite is stale or not %s", domain.ErrConflict, stateName)
	}
	return nil
}

func (d *SqliteInviteRepository) FindContextByTokenHash(ctx context.Context, tokenHash string) (*domain.InviteContext, error) {
	// One read joins the invite to its member's display name and derived
	// local-login presence: hasLocalLogin decides placeholder-vs-reset, and the
	// raw state columns (used_at, revoked_at, expires_at) drive the caller's
	// state machine.
	query := `
		SELECT
			i.id,
			i.user_id,
			i.expires_at,
			i.used_at,
			i.revoked_at,
			u.name,
			EXISTS (SELECT 1 FROM local_accounts la WHERE la.user_id = i.user_id)
		FROM invites i
		JOIN users u ON u.id = i.user_id AND u.archived_at IS NULL
		WHERE i.token_hash = ?
	`
	ic := &domain.InviteContext{}
	var expiresAt int64
	var usedAt sql.NullInt64
	var revokedAt sql.NullInt64
	var hasLocal int
	if err := d.pool.Read.QueryRowContext(ctx, query, tokenHash).Scan(
		&ic.ID,
		&ic.UserID,
		&expiresAt,
		&usedAt,
		&revokedAt,
		&ic.DisplayName,
		&hasLocal,
	); err != nil {
		return nil, err
	}
	ic.ExpiresAt = db.FromUnix(expiresAt)
	ic.UsedAt = unixTimePtr(usedAt)
	ic.RevokedAt = unixTimePtr(revokedAt)
	ic.HasLocalLogin = hasLocal == 1
	return ic, nil
}

func (d *SqliteInviteRepository) ListCurrent(ctx context.Context) ([]domain.InviteOverview, error) {
	// The partial unique index guarantees one unused, unrevoked generation per
	// member. Expiry is deliberately absent: an expired generation still needs a
	// roster action. Credentialed members stay present when an explicit password
	// reset invite exists, so its public handle remains manageable.
	query := `
		SELECT
			i.public_id,
			i.user_id,
			u.name,
			i.expires_at,
			i.created_at,
			issuer.name
		FROM invites i
		JOIN users u ON u.id = i.user_id AND u.archived_at IS NULL
		LEFT JOIN users issuer ON issuer.id = i.created_by
		WHERE i.used_at IS NULL
			AND i.revoked_at IS NULL
	`

	rows, err := d.pool.Read.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	overviews := []domain.InviteOverview{}
	for rows.Next() {
		var o domain.InviteOverview
		var expiresAt, createdAt int64
		var issuedBy sql.NullString
		if err := rows.Scan(&o.PublicID, &o.UserID, &o.MemberName, &expiresAt, &createdAt, &issuedBy); err != nil {
			return nil, err
		}
		o.ExpiresAt = db.FromUnix(expiresAt)
		o.CreatedAt = db.FromUnix(createdAt)
		if issuedBy.Valid {
			name := issuedBy.String
			o.IssuedBy = &name
		}
		overviews = append(overviews, o)
	}
	return overviews, rows.Err()
}
