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

func (d *SqliteInviteRepository) ListOutstanding(ctx context.Context) ([]domain.InviteOverview, error) {
	// "Outstanding" is unused and unrevoked, for an active member who still has
	// no way in. Expiry is deliberately absent from the predicate: a lapsed
	// invite is the Expired group, and dropping it here would leave the surface
	// with nothing to dismiss.
	//
	// The two NOT EXISTS are the self-clearing rule. Issue never revokes an
	// already-expired invite, so a member who set a password or signed in with
	// SSO in the meantime would otherwise keep a dead row on the admin's screen
	// forever.
	//
	// The id subquery is the one-row-per-member rule. Because Issue only revokes
	// *valid* invites, a member re-invited after each lapse leaves an unrevoked
	// row behind each time; without this they'd appear once per attempt. Newest
	// is by created_at with the row id breaking ties, since created_at is only
	// second-precise and two invites can share a second.
	//
	// It deliberately ranks over ALL of the member's invites, spent ones
	// included, and lets the outer unused/unrevoked filter reject the winner.
	// Ranking over only the outstanding rows would make an older lapsed invite
	// resurface the moment the newest was revoked: an admin revoking Ben's open
	// invite would watch Ben reappear under Expired, from a link that has been
	// dead for weeks. The member's latest invite is the only one that describes
	// where they stand.
	query := `
		SELECT
			i.id,
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
			AND NOT EXISTS (SELECT 1 FROM local_accounts la WHERE la.user_id = i.user_id)
			AND NOT EXISTS (SELECT 1 FROM oidc_identities oi WHERE oi.user_id = i.user_id)
			AND i.id = (
				SELECT i2.id
				FROM invites i2
				WHERE i2.user_id = i.user_id
				ORDER BY i2.created_at DESC, i2.id DESC
				LIMIT 1
			)
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
		if err := rows.Scan(&o.ID, &o.UserID, &o.MemberName, &expiresAt, &createdAt, &issuedBy); err != nil {
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

func (d *SqliteInviteRepository) RevokeByID(ctx context.Context, id int64, revokedAt time.Time) (int64, error) {
	// No expiry predicate: this is precisely the revoke that reaches a lapsed
	// invite, which RevokeValidByUserID cannot. The used_at/revoked_at guards
	// keep it to rows that are still outstanding, so dismissing twice affects
	// nothing and a claimed invite keeps its history.
	query := `
		UPDATE invites
		SET revoked_at = ?
		WHERE id = ?
			AND used_at IS NULL
			AND revoked_at IS NULL
	`
	res, err := d.pool.Write.ExecContext(ctx, query, db.ToUnix(revokedAt), id)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
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
