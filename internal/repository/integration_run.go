package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"moviepickarr/internal/db"
	"moviepickarr/internal/integration"
)

type SqliteIntegrationRunRepository struct {
	pool *db.Pool
}

var _ integration.RunLedger = (*SqliteIntegrationRunRepository)(nil)

func NewSqliteIntegrationRunRepository(pool *db.Pool) *SqliteIntegrationRunRepository {
	return &SqliteIntegrationRunRepository{pool: pool}
}

func (r *SqliteIntegrationRunRepository) Start(ctx context.Context, start integration.RunStart) (*integration.Run, error) {
	result, err := r.pool.Write.ExecContext(ctx, `
		INSERT INTO integration_runs (
			integration, operation, trigger, initiated_by, config_revision,
			status, started_at, total, remaining
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		start.Integration,
		start.Operation,
		start.Trigger,
		start.InitiatedBy,
		start.ConfigRevision,
		integration.RunStatusRunning,
		db.ToUnix(start.StartedAt),
		start.Total,
		start.Total,
	)
	if err != nil {
		return nil, err
	}

	id, err := result.LastInsertId()
	if err != nil {
		return nil, err
	}
	return r.findByID(ctx, id)
}

// Update stores absolute counts. A caller can drop intermediate snapshots
// without losing increments from earlier work.
func (r *SqliteIntegrationRunRepository) Update(ctx context.Context, id int64, progress integration.RunProgress) error {
	result, err := r.pool.Write.ExecContext(ctx, `
		UPDATE integration_runs
		SET total = ?, processed = ?, succeeded = ?, failed = ?, skipped = ?, remaining = ?
		WHERE id = ? AND status = ?
	`,
		progress.Total,
		progress.Processed,
		progress.Succeeded,
		progress.Failed,
		progress.Skipped,
		progress.Remaining,
		id,
		integration.RunStatusRunning,
	)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected != 1 {
		return fmt.Errorf("%w: run %d", integration.ErrRunNotRunning, id)
	}
	return nil
}

func (r *SqliteIntegrationRunRepository) Finish(ctx context.Context, id int64, finish integration.RunFinish) (*integration.Run, error) {
	failedSubjects, err := marshalFailedSubjects(finish.FailedSubjects)
	if err != nil {
		return nil, err
	}
	result, err := r.pool.Write.ExecContext(ctx, `
		UPDATE integration_runs
		SET status = ?, finished_at = ?, total = ?, processed = ?, succeeded = ?,
			failed = ?, skipped = ?, remaining = ?, error_summary = ?, failed_subjects = ?
		WHERE id = ? AND status = ?
	`,
		finish.Status,
		db.ToUnix(finish.FinishedAt),
		finish.Progress.Total,
		finish.Progress.Processed,
		finish.Progress.Succeeded,
		finish.Progress.Failed,
		finish.Progress.Skipped,
		finish.Progress.Remaining,
		finish.ErrorSummary,
		failedSubjects,
		id,
		integration.RunStatusRunning,
	)
	if err != nil {
		return nil, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return nil, err
	}
	if affected != 1 {
		return nil, fmt.Errorf("%w: run %d", integration.ErrRunNotRunning, id)
	}
	return r.findByID(ctx, id)
}

func (r *SqliteIntegrationRunRepository) InterruptRunning(ctx context.Context, interruptedAt time.Time) (int64, error) {
	result, err := r.pool.Write.ExecContext(ctx, `
		UPDATE integration_runs
		SET status = ?, finished_at = ?
		WHERE status = ?
	`, integration.RunStatusInterrupted, db.ToUnix(interruptedAt), integration.RunStatusRunning)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func (r *SqliteIntegrationRunRepository) Prune(ctx context.Context, now time.Time) (int64, error) {
	cutoff := now.AddDate(0, -integration.RunRetentionMonths, 0)
	tx, err := r.pool.Write.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()

	ageResult, err := tx.ExecContext(ctx, `
		DELETE FROM integration_runs
		WHERE status <> ? AND started_at < ?
	`, integration.RunStatusRunning, db.ToUnix(cutoff))
	if err != nil {
		return 0, err
	}
	ageRemoved, err := ageResult.RowsAffected()
	if err != nil {
		return 0, err
	}

	capResult, err := tx.ExecContext(ctx, `
		DELETE FROM integration_runs
		WHERE id IN (
			SELECT id
			FROM (
				SELECT
					id,
					row_number() OVER (
						PARTITION BY integration
						ORDER BY started_at DESC, id DESC
					) AS position
				FROM integration_runs
				WHERE status <> ?
			)
			WHERE position > ?
		)
	`, integration.RunStatusRunning, integration.MaxRunsPerIntegration)
	if err != nil {
		return 0, err
	}
	capRemoved, err := capResult.RowsAffected()
	if err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return ageRemoved + capRemoved, nil
}

func (r *SqliteIntegrationRunRepository) Current(ctx context.Context, integrationID string) (*integration.Run, error) {
	run, err := scanIntegrationRun(r.pool.Read.QueryRowContext(ctx, `
		SELECT
			id, integration, operation, trigger, initiated_by, config_revision,
			status, started_at, finished_at, total, processed, succeeded,
			failed, skipped, remaining, error_summary, failed_subjects
		FROM integration_runs
		WHERE integration = ? AND status = ?
		ORDER BY started_at DESC, id DESC
		LIMIT 1
	`, integrationID, integration.RunStatusRunning))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return run, err
}

func (r *SqliteIntegrationRunRepository) CurrentLibrary(ctx context.Context, integrationID string) (*integration.Run, error) {
	run, err := scanIntegrationRun(r.pool.Read.QueryRowContext(ctx, `
		SELECT
			id, integration, operation, trigger, initiated_by, config_revision,
			status, started_at, finished_at, total, processed, succeeded,
			failed, skipped, remaining, error_summary, failed_subjects
		FROM integration_runs
		WHERE integration = ?
			AND status = ?
			AND operation IN (?, ?)
		ORDER BY started_at DESC, id DESC
		LIMIT 1
	`,
		integrationID,
		integration.RunStatusRunning,
		integration.RunOperationRefreshStale,
		integration.RunOperationReEnrichAll,
	))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return run, err
}

func (r *SqliteIntegrationRunRepository) Latest(ctx context.Context, integrationID string) (*integration.Run, error) {
	run, err := scanIntegrationRun(r.pool.Read.QueryRowContext(ctx, `
		SELECT
			id, integration, operation, trigger, initiated_by, config_revision,
			status, started_at, finished_at, total, processed, succeeded,
			failed, skipped, remaining, error_summary, failed_subjects
		FROM integration_runs
		WHERE integration = ?
		ORDER BY started_at DESC, id DESC
		LIMIT 1
	`, integrationID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return run, err
}

func (r *SqliteIntegrationRunRepository) List(ctx context.Context, filter integration.RunListFilter) (integration.RunPage, error) {
	query, args, limit := integrationRunListQuery(filter)
	rows, err := r.pool.Read.QueryContext(ctx, query, args...)
	if err != nil {
		return integration.RunPage{}, err
	}
	defer rows.Close()

	runs := make([]integration.Run, 0, limit+1)
	for rows.Next() {
		run, err := scanIntegrationRun(rows)
		if err != nil {
			return integration.RunPage{}, err
		}
		runs = append(runs, *run)
	}
	if err := rows.Err(); err != nil {
		return integration.RunPage{}, err
	}

	page := integration.RunPage{Runs: runs}
	if len(runs) > limit {
		page.Runs = runs[:limit]
		last := page.Runs[len(page.Runs)-1]
		page.Next = &integration.RunCursor{StartedAt: last.StartedAt, ID: last.ID}
	}
	return page, nil
}

func integrationRunListQuery(filter integration.RunListFilter) (string, []any, int) {
	conditions := []string{"1 = 1"}
	args := make([]any, 0, 11)
	if filter.FinishedOnly {
		conditions = append(conditions, "finished_at IS NOT NULL")
	}
	if filter.Integration != "" {
		conditions = append(conditions, "integration = ?")
		args = append(args, filter.Integration)
	}
	if filter.Operation != "" {
		conditions = append(conditions, "operation = ?")
		args = append(args, filter.Operation)
	}
	if filter.Status != "" {
		conditions = append(conditions, "status = ?")
		args = append(args, filter.Status)
	}
	if filter.Trigger != "" {
		conditions = append(conditions, "trigger = ?")
		args = append(args, filter.Trigger)
	}
	if filter.Before != nil {
		conditions = append(conditions, "(started_at, id) < (?, ?)")
		startedAt := db.ToUnix(filter.Before.StartedAt)
		args = append(args, startedAt, filter.Before.ID)
	}

	limit := filter.Limit
	if limit <= 0 {
		limit = integration.DefaultRunListLimit
	}
	if limit > integration.MaxRunListLimit {
		limit = integration.MaxRunListLimit
	}
	args = append(args, limit+1)
	query := `
		SELECT
			id, integration, operation, trigger, initiated_by, config_revision,
			status, started_at, finished_at, total, processed, succeeded,
			failed, skipped, remaining, error_summary, failed_subjects
		FROM integration_runs
		WHERE ` + strings.Join(conditions, " AND ") + `
		ORDER BY started_at DESC, id DESC
		LIMIT ?
	`
	return query, args, limit
}

func (r *SqliteIntegrationRunRepository) findByID(ctx context.Context, id int64) (*integration.Run, error) {
	return scanIntegrationRun(r.pool.Read.QueryRowContext(ctx, `
		SELECT
			id, integration, operation, trigger, initiated_by, config_revision,
			status, started_at, finished_at, total, processed, succeeded,
			failed, skipped, remaining, error_summary, failed_subjects
		FROM integration_runs
		WHERE id = ?
	`, id))
}

func scanIntegrationRun(scanner rowScanner) (*integration.Run, error) {
	run := &integration.Run{}
	var initiatedBy sql.NullInt64
	var startedAt int64
	var finishedAt sql.NullInt64
	var failedSubjects string
	if err := scanner.Scan(
		&run.ID,
		&run.Integration,
		&run.Operation,
		&run.Trigger,
		&initiatedBy,
		&run.ConfigRevision,
		&run.Status,
		&startedAt,
		&finishedAt,
		&run.Progress.Total,
		&run.Progress.Processed,
		&run.Progress.Succeeded,
		&run.Progress.Failed,
		&run.Progress.Skipped,
		&run.Progress.Remaining,
		&run.ErrorSummary,
		&failedSubjects,
	); err != nil {
		return nil, err
	}
	if initiatedBy.Valid {
		value := int(initiatedBy.Int64)
		run.InitiatedBy = &value
	}
	run.StartedAt = db.FromUnix(startedAt)
	run.FinishedAt = unixTimePtr(finishedAt)
	if err := json.Unmarshal([]byte(failedSubjects), &run.FailedSubjects); err != nil {
		return nil, err
	}
	return run, nil
}

func marshalFailedSubjects(subjects []integration.FailedSubject) (string, error) {
	if len(subjects) > integration.FailedSubjectSampleLimit {
		subjects = subjects[:integration.FailedSubjectSampleLimit]
	}
	if subjects == nil {
		subjects = []integration.FailedSubject{}
	}
	encoded, err := json.Marshal(subjects)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}
