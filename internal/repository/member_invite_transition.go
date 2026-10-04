package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"moviepickarr/internal/db"
	"moviepickarr/internal/domain"
)

// CreateMemberWithInvite inserts the member, claims an unresolved Next up when
// eligible, and stores the first invite, in one tx.
func (d *SqliteAuthTransitionStore) CreateMemberWithInvite(
	ctx context.Context,
	name string,
	role domain.Role,
	invite domain.MemberInviteGeneration,
) (*domain.User, error) {
	tx, err := d.pool.Write.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	result, err := tx.ExecContext(ctx, `
		INSERT INTO users (name, role, created_at, updated_at)
		VALUES (?, ?, ?, ?)
	`, name, role, db.ToUnix(invite.CreatedAt), db.ToUnix(invite.CreatedAt))
	if err != nil {
		if db.IsUniqueViolation(err) {
			return nil, fmt.Errorf("%w: member name %q already exists", domain.ErrConflict, name)
		}
		return nil, err
	}
	userID, err := result.LastInsertId()
	if err != nil {
		return nil, err
	}

	if role.IsTurnParticipant() {
		// The upsert also repairs a missing singleton; its WHERE keeps a valid holder.
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO next_up (id, user_id)
			VALUES (1, ?)
			ON CONFLICT(id) DO UPDATE SET user_id = excluded.user_id
			WHERE NOT EXISTS (
				SELECT 1
				FROM turn_participants u
				WHERE u.id = next_up.user_id
			)
		`, userID); err != nil {
			return nil, err
		}
	}

	if err := insertMemberInvite(ctx, tx, int(userID), invite); err != nil {
		return nil, err
	}

	// Read before commit, so a failed read rolls back the writes.
	member, err := scanUser(tx.QueryRowContext(ctx,
		"SELECT id, name, created_at, updated_at FROM users WHERE id = ?", userID,
	))
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return member, nil
}

// RestoreMemberWithInvite strips residual logins, unarchives the member, and
// stores a fresh invite, in one tx.
func (d *SqliteAuthTransitionStore) RestoreMemberWithInvite(
	ctx context.Context,
	userID int,
	invite domain.MemberInviteGeneration,
) (*domain.User, error) {
	tx, err := d.pool.Write.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	var exists int
	err = tx.QueryRowContext(ctx,
		"SELECT 1 FROM users WHERE id = ? AND archived_at IS NOT NULL", userID,
	).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("%w: no archived member %d", domain.ErrNotFound, userID)
	}
	if err != nil {
		return nil, err
	}

	if err := deleteUserAuthRows(ctx, tx, userID); err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE users
		SET archived_at = NULL, updated_at = ?
		WHERE id = ?
	`, db.ToUnix(invite.CreatedAt), userID); err != nil {
		return nil, err
	}
	if err := insertMemberInvite(ctx, tx, userID, invite); err != nil {
		return nil, err
	}
	member, err := scanUser(tx.QueryRowContext(ctx,
		"SELECT id, name, created_at, updated_at FROM users WHERE id = ?", userID,
	))
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return member, nil
}

func insertMemberInvite(
	ctx context.Context,
	tx *sql.Tx,
	userID int,
	invite domain.MemberInviteGeneration,
) error {
	_, err := tx.ExecContext(ctx, `
		INSERT INTO invites (
			public_id, user_id, token_hash, expires_at, created_by, created_at
		) VALUES (?, ?, ?, ?, ?, ?)
	`,
		invite.PublicID,
		userID,
		invite.TokenHash,
		db.ToUnix(invite.ExpiresAt),
		invite.CreatedBy,
		db.ToUnix(invite.CreatedAt),
	)
	if err != nil {
		if db.IsUniqueViolation(err) {
			return fmt.Errorf("%w: invite generation already exists", domain.ErrConflict)
		}
		if db.IsForeignKeyViolation(err) {
			return fmt.Errorf("%w: member or issuer no longer exists", domain.ErrNotFound)
		}
	}
	return err
}
