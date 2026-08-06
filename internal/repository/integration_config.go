package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"moviepickarr/internal/db"
	"moviepickarr/internal/domain"
	"moviepickarr/internal/integration"
)

type SqliteIntegrationConfigRepository struct {
	pool *db.Pool
}

func NewSqliteIntegrationConfigRepository(pool *db.Pool) *SqliteIntegrationConfigRepository {
	return &SqliteIntegrationConfigRepository{pool: pool}
}

func (r *SqliteIntegrationConfigRepository) Get(ctx context.Context, name string) (integration.ConfigRecord, error) {
	return readIntegrationConfig(ctx, r.pool.Read, name)
}

type queryRower interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func readIntegrationConfig(ctx context.Context, q queryRower, name string) (integration.ConfigRecord, error) {
	var record integration.ConfigRecord
	var encryptedSecret []byte
	var lastCheckedAt, lastConnectionTestedAt, nextCheckAt, lastSuccessfulRunAt sql.NullInt64
	var updatedAt int64
	err := q.QueryRowContext(ctx, `
		SELECT integration, revision, admin_config, encrypted_secret, state, state_reason,
		       last_checked_at, last_connection_tested_at, next_check_at,
		       last_successful_refresh_at, updated_at
		FROM integration_configs
		WHERE integration = ?
	`, name).Scan(
		&record.Integration,
		&record.Revision,
		&record.AdminConfig,
		&encryptedSecret,
		&record.State,
		&record.StateReason,
		&lastCheckedAt,
		&lastConnectionTestedAt,
		&nextCheckAt,
		&lastSuccessfulRunAt,
		&updatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return integration.ConfigRecord{}, domain.ErrNotFound
	}
	if err != nil {
		return integration.ConfigRecord{}, fmt.Errorf("read integration config: %w", err)
	}
	record.EncryptedSecret = encryptedSecret
	record.LastCheckedAt = unixTimePtr(lastCheckedAt)
	record.LastConnectionTestedAt = unixTimePtr(lastConnectionTestedAt)
	record.NextCheckAt = unixTimePtr(nextCheckAt)
	record.LastSuccessfulRunAt = unixTimePtr(lastSuccessfulRunAt)
	record.UpdatedAt = db.FromUnix(updatedAt)
	return record, nil
}

func (r *SqliteIntegrationConfigRepository) Save(ctx context.Context, save integration.ConfigSave) (integration.ConfigRecord, error) {
	secretMode := "preserve"
	var encryptedSecret any
	switch save.SecretAction {
	case integration.SecretReplace:
		secretMode = "replace"
		encryptedSecret = save.EncryptedSecret
	case integration.SecretClear:
		secretMode = "clear"
	case integration.SecretPreserve:
	default:
		return integration.ConfigRecord{}, fmt.Errorf("invalid secret action %d", save.SecretAction)
	}
	var connectionTestedAt any
	if save.ConnectionTestedAt != nil {
		connectionTestedAt = db.ToUnix(*save.ConnectionTestedAt)
	}

	result, err := r.pool.Write.ExecContext(ctx, `
		UPDATE integration_configs
		SET admin_config = ?,
		    encrypted_secret = CASE ?
		        WHEN 'replace' THEN ?
		        WHEN 'clear' THEN NULL
		        ELSE encrypted_secret
		    END,
		    state = ?,
		    state_reason = ?,
		    last_connection_tested_at = COALESCE(?, last_connection_tested_at),
		    revision = revision + 1,
		    updated_at = unixepoch()
		WHERE integration = ? AND revision = ?
	`, string(save.AdminConfig), secretMode, encryptedSecret, save.State, save.StateReason, connectionTestedAt, save.Integration, save.ExpectedRevision)
	if err != nil {
		return integration.ConfigRecord{}, fmt.Errorf("save integration config: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return integration.ConfigRecord{}, fmt.Errorf("read integration config save result: %w", err)
	}
	if rows == 0 {
		if _, err := r.Get(ctx, save.Integration); errors.Is(err, domain.ErrNotFound) {
			return integration.ConfigRecord{}, domain.ErrNotFound
		}
		return integration.ConfigRecord{}, integration.ErrStaleRevision
	}
	return readIntegrationConfig(ctx, r.pool.Write, save.Integration)
}

func (r *SqliteIntegrationConfigRepository) UpdateState(
	ctx context.Context,
	name string,
	state integration.State,
	reason string,
) error {
	result, err := r.pool.Write.ExecContext(ctx, `
		UPDATE integration_configs
		SET state = ?, state_reason = ?, updated_at = unixepoch()
		WHERE integration = ?
	`, state, reason, name)
	if err != nil {
		return fmt.Errorf("update integration state: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read integration state update result: %w", err)
	}
	if rows == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (r *SqliteIntegrationConfigRepository) UpdateConnectionTest(
	ctx context.Context,
	name string,
	state integration.State,
	reason string,
	testedAt time.Time,
) error {
	result, err := r.pool.Write.ExecContext(ctx, `
		UPDATE integration_configs
		SET state = ?,
		    state_reason = ?,
		    last_connection_tested_at = ?,
		    updated_at = unixepoch()
		WHERE integration = ?
	`, state, reason, db.ToUnix(testedAt), name)
	if err != nil {
		return fmt.Errorf("update integration connection test: %w", err)
	}
	return integrationConfigUpdateResult(result, "update integration connection test")
}

func (r *SqliteIntegrationConfigRepository) UpdateSchedule(
	ctx context.Context,
	name string,
	checkedAt time.Time,
	nextCheckAt *time.Time,
) error {
	var next any
	if nextCheckAt != nil {
		next = db.ToUnix(*nextCheckAt)
	}
	result, err := r.pool.Write.ExecContext(ctx, `
		UPDATE integration_configs
		SET last_checked_at = ?, next_check_at = ?, updated_at = unixepoch()
		WHERE integration = ?
	`, db.ToUnix(checkedAt), next, name)
	if err != nil {
		return fmt.Errorf("update integration schedule: %w", err)
	}
	return integrationConfigUpdateResult(result, "update integration schedule")
}

func (r *SqliteIntegrationConfigRepository) UpdateLastChecked(
	ctx context.Context,
	name string,
	checkedAt time.Time,
) error {
	result, err := r.pool.Write.ExecContext(ctx, `
		UPDATE integration_configs
		SET last_checked_at = ?, updated_at = unixepoch()
		WHERE integration = ?
	`, db.ToUnix(checkedAt), name)
	if err != nil {
		return fmt.Errorf("update integration last checked time: %w", err)
	}
	return integrationConfigUpdateResult(result, "update integration last checked time")
}

func (r *SqliteIntegrationConfigRepository) UpdateNextCheck(
	ctx context.Context,
	name string,
	nextCheckAt *time.Time,
) error {
	var next any
	if nextCheckAt != nil {
		next = db.ToUnix(*nextCheckAt)
	}
	result, err := r.pool.Write.ExecContext(ctx, `
		UPDATE integration_configs
		SET next_check_at = ?, updated_at = unixepoch()
		WHERE integration = ?
	`, next, name)
	if err != nil {
		return fmt.Errorf("update integration next check time: %w", err)
	}
	return integrationConfigUpdateResult(result, "update integration next check time")
}

func (r *SqliteIntegrationConfigRepository) UpdateSuccessfulRun(
	ctx context.Context,
	name string,
	succeededAt time.Time,
) error {
	result, err := r.pool.Write.ExecContext(ctx, `
		UPDATE integration_configs
		SET last_successful_refresh_at = ?, updated_at = unixepoch()
		WHERE integration = ?
	`, db.ToUnix(succeededAt), name)
	if err != nil {
		return fmt.Errorf("update integration successful run: %w", err)
	}
	return integrationConfigUpdateResult(result, "update integration successful run")
}

func integrationConfigUpdateResult(result sql.Result, operation string) error {
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("%s result: %w", operation, err)
	}
	if rows == 0 {
		return domain.ErrNotFound
	}
	return nil
}
