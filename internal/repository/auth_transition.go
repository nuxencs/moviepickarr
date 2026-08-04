package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"moviepickarr/internal/db"
	"moviepickarr/internal/domain"
)

// SqliteAuthTransitionStore owns the credential-plus-invite transitions whose
// partial states would leave a reusable claim link after a credential changed.
// Password hashing and OIDC exchange happen before these short writer txs.
type SqliteAuthTransitionStore struct {
	pool *db.Pool
}

func NewSqliteAuthTransitionStore(pool *db.Pool) *SqliteAuthTransitionStore {
	return &SqliteAuthTransitionStore{pool: pool}
}

type transitionInvite struct {
	id          int64
	userID      int
	expiresAt   time.Time
	usedAt      *time.Time
	revokedAt   *time.Time
	username    sql.NullString
	hasIdentity bool
}

func loadClaimInvite(ctx context.Context, tx *sql.Tx, tokenHash string, now time.Time) (transitionInvite, error) {
	var invite transitionInvite
	var expiresAt int64
	var usedAt, revokedAt sql.NullInt64
	var hasIdentity int
	err := tx.QueryRowContext(ctx, `
		SELECT
			i.id,
			i.user_id,
			i.expires_at,
			i.used_at,
			i.revoked_at,
			la.username,
			EXISTS (SELECT 1 FROM oidc_identities oi WHERE oi.user_id = i.user_id)
		FROM invites i
		JOIN users u ON u.id = i.user_id AND u.archived_at IS NULL
		LEFT JOIN local_accounts la ON la.user_id = i.user_id
		WHERE i.token_hash = ?
	`, tokenHash).Scan(
		&invite.id,
		&invite.userID,
		&expiresAt,
		&usedAt,
		&revokedAt,
		&invite.username,
		&hasIdentity,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return transitionInvite{}, domain.ErrInviteInvalid
	}
	if err != nil {
		return transitionInvite{}, err
	}
	invite.expiresAt = db.FromUnix(expiresAt)
	invite.usedAt = unixTimePtr(usedAt)
	invite.revokedAt = unixTimePtr(revokedAt)
	invite.hasIdentity = hasIdentity == 1
	if invite.usedAt != nil {
		return transitionInvite{}, domain.ErrInviteUsed
	}
	if invite.revokedAt != nil || !now.Before(invite.expiresAt) {
		return transitionInvite{}, domain.ErrInviteInvalid
	}
	return invite, nil
}

func (d *SqliteAuthTransitionStore) RedeemPasswordInvite(
	ctx context.Context,
	claim domain.PasswordInviteClaim,
	now time.Time,
) (domain.InviteClaimResult, error) {
	tx, err := d.pool.Write.BeginTx(ctx, nil)
	if err != nil {
		return domain.InviteClaimResult{}, err
	}
	defer func() { _ = tx.Rollback() }()

	invite, err := loadClaimInvite(ctx, tx, claim.TokenHash, now)
	if err != nil {
		return domain.InviteClaimResult{}, err
	}

	wasRecovery := invite.username.Valid || invite.hasIdentity
	if invite.username.Valid {
		if claim.Username != "" && !strings.EqualFold(claim.Username, invite.username.String) {
			return domain.InviteClaimResult{}, fmt.Errorf(
				"%w: username is immutable through this flow",
				domain.ErrInvalidInput,
			)
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE local_accounts
			SET password_hash = ?, failed_attempts = 0, locked_until = NULL, updated_at = ?
			WHERE user_id = ?
		`, claim.PasswordHash, db.ToUnix(now), invite.userID); err != nil {
			return domain.InviteClaimResult{}, err
		}
	} else {
		if claim.Username == "" {
			return domain.InviteClaimResult{}, fmt.Errorf("%w: username is required", domain.ErrInvalidInput)
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO local_accounts (user_id, username, password_hash)
			VALUES (?, ?, ?)
		`, invite.userID, claim.Username, claim.PasswordHash); err != nil {
			return domain.InviteClaimResult{}, mapLocalCredentialConstraint(err, invite.userID)
		}
	}

	// A credential-less member can still have sessions left from a prior
	// credential removal or older application version. Treat every claim as a
	// session rotation so none of those bearer tokens survives the new login.
	if _, err := tx.ExecContext(ctx, "DELETE FROM sessions WHERE user_id = ?", invite.userID); err != nil {
		return domain.InviteClaimResult{}, err
	}
	if err := consumeClaimInvite(ctx, tx, invite.id, claim.TokenHash, now); err != nil {
		return domain.InviteClaimResult{}, err
	}
	if claim.Session != nil {
		session := *claim.Session
		session.UserID = invite.userID
		if err := insertTransitionSession(ctx, tx, session); err != nil {
			return domain.InviteClaimResult{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return domain.InviteClaimResult{}, err
	}
	return domain.InviteClaimResult{
		MemberID:    invite.userID,
		WasReset:    invite.username.Valid,
		WasRecovery: wasRecovery,
	}, nil
}

func (d *SqliteAuthTransitionStore) SetLocalCredential(
	ctx context.Context,
	change domain.LocalCredentialChange,
	now time.Time,
) (domain.LocalCredentialResult, error) {
	tx, err := d.pool.Write.BeginTx(ctx, nil)
	if err != nil {
		return domain.LocalCredentialResult{}, err
	}
	defer func() { _ = tx.Rollback() }()

	var username sql.NullString
	err = tx.QueryRowContext(ctx, `
		SELECT la.username
		FROM users u
		LEFT JOIN local_accounts la ON la.user_id = u.id
		WHERE u.id = ? AND u.archived_at IS NULL
	`, change.UserID).Scan(&username)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.LocalCredentialResult{}, fmt.Errorf("%w: member %d", domain.ErrNotFound, change.UserID)
	}
	if err != nil {
		return domain.LocalCredentialResult{}, err
	}

	result := domain.LocalCredentialResult{WasReset: username.Valid}
	if username.Valid {
		if change.Mode == domain.LocalCredentialFirst {
			return domain.LocalCredentialResult{}, fmt.Errorf("%w: member already has a local login", domain.ErrConflict)
		}
		if change.Username != "" && !strings.EqualFold(change.Username, username.String) {
			return domain.LocalCredentialResult{}, fmt.Errorf(
				"%w: username is immutable through this flow",
				domain.ErrInvalidInput,
			)
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE local_accounts
			SET password_hash = ?, failed_attempts = 0, locked_until = NULL, updated_at = ?
			WHERE user_id = ?
		`, change.PasswordHash, db.ToUnix(now), change.UserID); err != nil {
			return domain.LocalCredentialResult{}, err
		}
	} else {
		if change.Username == "" {
			return domain.LocalCredentialResult{}, fmt.Errorf("%w: username is required", domain.ErrInvalidInput)
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO local_accounts (user_id, username, password_hash)
			VALUES (?, ?, ?)
		`, change.UserID, change.Username, change.PasswordHash); err != nil {
			return domain.LocalCredentialResult{}, mapLocalCredentialConstraint(err, change.UserID)
		}
	}

	if err := retireCurrentInvite(ctx, tx, change.UserID, now); err != nil {
		return domain.LocalCredentialResult{}, err
	}
	if result.WasReset && change.RevokeSessionsOnReset {
		if _, err := tx.ExecContext(ctx, "DELETE FROM sessions WHERE user_id = ?", change.UserID); err != nil {
			return domain.LocalCredentialResult{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return domain.LocalCredentialResult{}, err
	}
	return result, nil
}

func (d *SqliteAuthTransitionStore) ChangeVerifiedPassword(
	ctx context.Context,
	change domain.VerifiedPasswordChange,
	now time.Time,
) error {
	tx, err := d.pool.Write.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	res, err := tx.ExecContext(ctx, `
		UPDATE local_accounts
		SET password_hash = ?, failed_attempts = 0, locked_until = NULL, updated_at = ?
		WHERE user_id = ?
			AND password_hash = ?
			AND EXISTS (
				SELECT 1 FROM users
				WHERE id = local_accounts.user_id AND archived_at IS NULL
			)
	`, change.PasswordHash, db.ToUnix(now), change.UserID, change.ExpectedPasswordHash)
	if err != nil {
		return err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected != 1 {
		var hasLocal int
		err := tx.QueryRowContext(ctx, `
			SELECT EXISTS (SELECT 1 FROM local_accounts WHERE user_id = u.id)
			FROM users u
			WHERE u.id = ? AND u.archived_at IS NULL
		`, change.UserID).Scan(&hasLocal)
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("%w: active member %d", domain.ErrNotFound, change.UserID)
		}
		if err != nil {
			return err
		}
		return fmt.Errorf("%w: local credential changed during password verification", domain.ErrConflict)
	}

	if _, err := tx.ExecContext(ctx, "DELETE FROM sessions WHERE user_id = ?", change.UserID); err != nil {
		return err
	}
	if err := retireCurrentInvite(ctx, tx, change.UserID, now); err != nil {
		return err
	}
	if change.Session != nil {
		session := *change.Session
		session.UserID = change.UserID
		if err := insertTransitionSession(ctx, tx, session); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (d *SqliteAuthTransitionStore) CompleteLocalLogin(
	ctx context.Context,
	login domain.VerifiedLocalLogin,
	session domain.Session,
	now time.Time,
) error {
	if session.UserID != login.UserID {
		return fmt.Errorf("%w: login and session member differ", domain.ErrInvalidInput)
	}
	tx, err := d.pool.Write.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	res, err := tx.ExecContext(ctx, `
		UPDATE local_accounts
		SET password_hash = COALESCE(?, password_hash),
			failed_attempts = 0,
			locked_until = NULL,
			last_login_at = ?,
			updated_at = ?
		WHERE user_id = ?
			AND password_hash = ?
			AND EXISTS (
				SELECT 1 FROM users
				WHERE id = local_accounts.user_id AND archived_at IS NULL
			)
	`,
		login.NewPasswordHash,
		db.ToUnix(now),
		db.ToUnix(now),
		login.UserID,
		login.ExpectedPasswordHash,
	)
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

	if err := insertTransitionSession(ctx, tx, session); err != nil {
		return err
	}
	return tx.Commit()
}

func (d *SqliteAuthTransitionStore) DeleteLocalCredential(
	ctx context.Context,
	userID, actorID int,
	now time.Time,
) error {
	tx, err := d.pool.Write.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	var hasLocal, hasIdentity int
	err = tx.QueryRowContext(ctx, `
		SELECT
			EXISTS (SELECT 1 FROM local_accounts WHERE user_id = u.id),
			EXISTS (SELECT 1 FROM oidc_identities WHERE user_id = u.id)
		FROM users u
		WHERE u.id = ? AND u.archived_at IS NULL
	`, userID).Scan(&hasLocal, &hasIdentity)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("%w: active member %d", domain.ErrNotFound, userID)
	}
	if err != nil {
		return err
	}
	if hasLocal == 0 {
		return fmt.Errorf("%w: member %d has no local login", domain.ErrNotFound, userID)
	}
	if userID == actorID && hasIdentity == 0 {
		return fmt.Errorf("%w: cannot remove your own last credential", domain.ErrConflict)
	}

	if _, err := tx.ExecContext(ctx, "DELETE FROM local_accounts WHERE user_id = ?", userID); err != nil {
		return err
	}
	if hasIdentity == 0 {
		if _, err := tx.ExecContext(ctx, "DELETE FROM sessions WHERE user_id = ?", userID); err != nil {
			return err
		}
	}
	if err := retireCurrentInvite(ctx, tx, userID, now); err != nil {
		return err
	}
	return tx.Commit()
}

func (d *SqliteAuthTransitionStore) RedeemOIDCInvite(
	ctx context.Context,
	tokenHash string,
	identity domain.OIDCIdentity,
	session *domain.Session,
	now time.Time,
) (domain.InviteClaimResult, error) {
	tx, err := d.pool.Write.BeginTx(ctx, nil)
	if err != nil {
		return domain.InviteClaimResult{}, err
	}
	defer func() { _ = tx.Rollback() }()

	invite, err := loadClaimInvite(ctx, tx, tokenHash, now)
	if err != nil {
		return domain.InviteClaimResult{}, err
	}
	// A reset generation authorizes one operation: replace the existing local
	// password. Do not turn that bearer link into a second credential path if a
	// stale or hand-written client reaches the OIDC callback directly.
	if invite.username.Valid {
		return domain.InviteClaimResult{}, domain.ErrInviteInvalid
	}
	identity.UserID = invite.userID
	identity.LastLoginAt = &now
	if err := linkOIDCIdentity(ctx, tx, identity, now); err != nil {
		return domain.InviteClaimResult{}, err
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM sessions WHERE user_id = ?", invite.userID); err != nil {
		return domain.InviteClaimResult{}, err
	}
	if err := consumeClaimInvite(ctx, tx, invite.id, tokenHash, now); err != nil {
		return domain.InviteClaimResult{}, err
	}
	if session != nil {
		owned := *session
		owned.UserID = invite.userID
		if err := insertTransitionSession(ctx, tx, owned); err != nil {
			return domain.InviteClaimResult{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return domain.InviteClaimResult{}, err
	}
	return domain.InviteClaimResult{
		MemberID:    invite.userID,
		WasRecovery: invite.username.Valid || invite.hasIdentity,
	}, nil
}

func (d *SqliteAuthTransitionStore) CompleteOIDCLogin(
	ctx context.Context,
	identity domain.OIDCIdentity,
	session domain.Session,
	now time.Time,
) (int, error) {
	tx, err := d.pool.Write.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()

	var identityID int64
	var userID int
	err = tx.QueryRowContext(ctx, `
		SELECT oi.id, oi.user_id
		FROM oidc_identities oi
		JOIN users u ON u.id = oi.user_id AND u.archived_at IS NULL
		WHERE oi.issuer = ? AND oi.subject = ?
	`, identity.Issuer, identity.Subject).Scan(&identityID, &userID)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, domain.ErrNotFound
	}
	if err != nil {
		return 0, err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE oidc_identities
		SET email = ?, preferred_username = ?, last_login_at = ?, updated_at = ?
		WHERE id = ?
	`, identity.Email, identity.PreferredUsername, db.ToUnix(now), db.ToUnix(now), identityID); err != nil {
		return 0, err
	}
	session.UserID = userID
	if err := insertTransitionSession(ctx, tx, session); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return userID, nil
}

func (d *SqliteAuthTransitionStore) LinkOIDCAndRetireInvite(
	ctx context.Context,
	identity domain.OIDCIdentity,
	sessionTokenHash string,
	now, idleCutoff time.Time,
) error {
	tx, err := d.pool.Write.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	var authorized int
	err = tx.QueryRowContext(ctx, `
		SELECT 1
		FROM sessions s
		JOIN users u ON u.id = s.user_id AND u.archived_at IS NULL
		WHERE s.token_hash = ?
			AND s.user_id = ?
			AND s.expires_at > ?
			AND s.last_seen_at > ?
	`, sessionTokenHash, identity.UserID, db.ToUnix(now), db.ToUnix(idleCutoff)).Scan(&authorized)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.ErrSessionInvalid
	}
	if err != nil {
		return err
	}

	identity.LastLoginAt = &now
	if err := linkOIDCIdentity(ctx, tx, identity, now); err != nil {
		return err
	}
	if err := retireCurrentInvite(ctx, tx, identity.UserID, now); err != nil {
		return err
	}
	return tx.Commit()
}

func (d *SqliteAuthTransitionStore) DeleteOIDCIdentity(
	ctx context.Context,
	userID, actorID int,
	now time.Time,
) error {
	tx, err := d.pool.Write.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	var hasIdentity, hasLocal int
	err = tx.QueryRowContext(ctx, `
		SELECT
			EXISTS (SELECT 1 FROM oidc_identities WHERE user_id = u.id),
			EXISTS (SELECT 1 FROM local_accounts WHERE user_id = u.id)
		FROM users u
		WHERE u.id = ? AND u.archived_at IS NULL
	`, userID).Scan(&hasIdentity, &hasLocal)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("%w: active member %d", domain.ErrNotFound, userID)
	}
	if err != nil {
		return err
	}
	if hasIdentity == 0 {
		return fmt.Errorf("%w: member %d has no linked identity", domain.ErrNotFound, userID)
	}
	if userID == actorID && hasLocal == 0 {
		return fmt.Errorf("%w: cannot remove your own last credential", domain.ErrConflict)
	}

	if _, err := tx.ExecContext(ctx, "DELETE FROM oidc_identities WHERE user_id = ?", userID); err != nil {
		return err
	}
	if hasLocal == 0 {
		if _, err := tx.ExecContext(ctx, "DELETE FROM sessions WHERE user_id = ?", userID); err != nil {
			return err
		}
	}
	if err := retireCurrentInvite(ctx, tx, userID, now); err != nil {
		return err
	}
	return tx.Commit()
}

func consumeClaimInvite(ctx context.Context, tx *sql.Tx, id int64, tokenHash string, now time.Time) error {
	res, err := tx.ExecContext(ctx, `
		UPDATE invites
		SET used_at = ?
		WHERE id = ?
			AND token_hash = ?
			AND used_at IS NULL
			AND revoked_at IS NULL
			AND expires_at > ?
	`, db.ToUnix(now), id, tokenHash, db.ToUnix(now))
	if err != nil {
		return err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected != 1 {
		return domain.ErrInviteInvalid
	}
	return nil
}

func retireCurrentInvite(ctx context.Context, tx *sql.Tx, userID int, now time.Time) error {
	_, err := tx.ExecContext(ctx, `
		UPDATE invites
		SET revoked_at = ?
		WHERE user_id = ? AND used_at IS NULL AND revoked_at IS NULL
	`, db.ToUnix(now), userID)
	return err
}

func linkOIDCIdentity(ctx context.Context, tx *sql.Tx, identity domain.OIDCIdentity, now time.Time) error {
	var active int
	if err := tx.QueryRowContext(ctx,
		"SELECT 1 FROM users WHERE id = ? AND archived_at IS NULL",
		identity.UserID,
	).Scan(&active); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("%w: member %d", domain.ErrNotFound, identity.UserID)
		}
		return err
	}

	var existingID int64
	var ownerID int
	err := tx.QueryRowContext(ctx, `
		SELECT id, user_id
		FROM oidc_identities
		WHERE issuer = ? AND subject = ?
	`, identity.Issuer, identity.Subject).Scan(&existingID, &ownerID)
	switch {
	case err == nil:
		if ownerID != identity.UserID {
			return fmt.Errorf("%w: identity already linked to another member", domain.ErrConflict)
		}
		_, err = tx.ExecContext(ctx, `
			UPDATE oidc_identities
			SET email = ?, preferred_username = ?, last_login_at = ?, updated_at = ?
			WHERE id = ?
		`, identity.Email, identity.PreferredUsername, db.ToUnix(now), db.ToUnix(now), existingID)
		return err
	case !errors.Is(err, sql.ErrNoRows):
		return err
	}

	var otherID int64
	err = tx.QueryRowContext(ctx,
		"SELECT id FROM oidc_identities WHERE user_id = ?",
		identity.UserID,
	).Scan(&otherID)
	if err == nil {
		return fmt.Errorf("%w: member already has a linked identity", domain.ErrConflict)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}

	_, err = tx.ExecContext(ctx, `
		INSERT INTO oidc_identities (
			user_id, issuer, subject, email, preferred_username,
			last_login_at, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`,
		identity.UserID,
		identity.Issuer,
		identity.Subject,
		identity.Email,
		identity.PreferredUsername,
		db.ToUnix(now),
		db.ToUnix(now),
		db.ToUnix(now),
	)
	if err != nil {
		if db.IsUniqueViolation(err) {
			return fmt.Errorf("%w: identity already linked", domain.ErrConflict)
		}
		if db.IsForeignKeyViolation(err) {
			return fmt.Errorf("%w: member %d", domain.ErrNotFound, identity.UserID)
		}
	}
	return err
}

func mapLocalCredentialConstraint(err error, userID int) error {
	if db.IsUniqueViolation(err) {
		return fmt.Errorf("%w: username already taken", domain.ErrConflict)
	}
	if db.IsForeignKeyViolation(err) {
		return fmt.Errorf("%w: member %d", domain.ErrNotFound, userID)
	}
	return err
}

func insertTransitionSession(ctx context.Context, tx *sql.Tx, session domain.Session) error {
	_, err := tx.ExecContext(ctx, `
		INSERT INTO sessions (
			public_id, token_hash, user_id, expires_at, last_seen_at, user_agent, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?)
	`,
		session.PublicID,
		session.TokenHash,
		session.UserID,
		db.ToUnix(session.ExpiresAt),
		db.ToUnix(session.LastSeenAt),
		session.UserAgent,
		db.ToUnix(session.CreatedAt),
	)
	return err
}
