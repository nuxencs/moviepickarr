package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"moviepickarr/internal/db"
	"moviepickarr/internal/domain"
)

// CreateMemberWithInvite inserts the member, claims an unresolved next-up slot
// when the role is eligible, and stores the first invite in one writer
// transaction. SQLite orders concurrent creates on the single writer, so only
// the first eligible committed member can claim an unresolved pointer.
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
		// The singleton normally exists from migration 001, but the upsert also
		// repairs a missing row. Its WHERE keeps a resolvable active participant
		// intact. A Guest never claims the turn.
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

	// Build the response before commit. A projection failure must roll back the
	// member, next-up assignment, and invite instead of returning a failed request
	// after durable lifecycle writes.
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

// RestoreMemberWithInvite strips any residual authentication state, reopens an
// archived member, and stores a fresh invite in one writer transaction.
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
