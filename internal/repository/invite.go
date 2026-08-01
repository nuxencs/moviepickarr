package repository

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"moviepickarr/internal/db"
	"moviepickarr/internal/domain"
)

// SqliteInviteRepository is the invite/claim store over the 009 invites table,
// joined to users + local_accounts for the claim-page context. Reads route to
// the read pool and mutations to the write pool, matching the single-writer
// discipline the other repositories follow.
type SqliteInviteRepository struct {
	pool *db.Pool
}

func NewSqliteInviteRepository(pool *db.Pool) *SqliteInviteRepository {
	return &SqliteInviteRepository{pool: pool}
}

func (d *SqliteInviteRepository) Create(ctx context.Context, userID int, tokenHash string, expiresAt time.Time, createdBy *int) error {
	// created_at defaults to unixepoch() in the schema; expires_at carries the
	// injected clock so expiry is testable. created_by is nullable so the invite
	// outlives the issuing admin (the column is ON DELETE SET NULL).
	query := `
		INSERT INTO invites (user_id, token_hash, expires_at, created_by)
		SELECT ?, ?, ?, ?
		FROM users
		WHERE id = ? AND archived_at IS NULL
	`
	res, err := d.pool.Write.ExecContext(ctx, query, userID, tokenHash, db.ToUnix(expiresAt), createdBy, userID)
	if err != nil {
		// A created_by FK miss still comes through the driver. A missing or
		// archived target selects no row and is mapped below.
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

func (d *SqliteInviteRepository) RevokeValidByUserID(ctx context.Context, userID int, now, revokedAt time.Time) (int64, error) {
	// The WHERE mirrors the validity predicate exactly (unused, unrevoked, not
	// expired), so this revokes precisely the one invite the app treats as live.
	query := `
		UPDATE invites
		SET revoked_at = ?
		WHERE user_id = ?
			AND used_at IS NULL
			AND revoked_at IS NULL
			AND expires_at > ?
	`
	res, err := d.pool.Write.ExecContext(ctx, query, db.ToUnix(revokedAt), userID, db.ToUnix(now))
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
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

func (d *SqliteInviteRepository) MarkUsed(ctx context.Context, id int64, usedAt time.Time) error {
	res, err := d.pool.Write.ExecContext(ctx,
		"UPDATE invites SET used_at = ? WHERE id = ?", db.ToUnix(usedAt), id)
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
