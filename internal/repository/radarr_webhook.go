package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"moviepickarr/internal/db"
	"moviepickarr/internal/domain"
)

const RadarrWebhookMaxAttempts = 5

const radarrWebhookInterruptedFinalAttempt = "Webhook delivery was interrupted after its final attempt."

type RadarrWebhookDestination struct {
	ID                  int64
	Name                string
	Kind                string
	EncryptedURL        []byte
	ReasonFilters       []string
	DiscordRoleMention  string
	Enabled             bool
	VerifiedAt          *time.Time
	Revision            int64
	HealthWarningAt     *time.Time
	HealthWarningReason string
	CreatedAt           time.Time
	UpdatedAt           time.Time
	ArchivedAt          *time.Time
}

type RadarrWebhookDestinationSave struct {
	Name               string
	Kind               string
	EncryptedURL       []byte
	ReasonFilters      []string
	DiscordRoleMention string
	Enabled            bool
	VerifiedAt         *time.Time
}

type RadarrWebhookDelivery struct {
	ID                  int64
	DestinationID       int64
	DestinationRevision int64
	DestinationName     string
	Kind                string
	EncryptedURL        []byte
	DiscordRoleMention  string
	AcquisitionID       int64
	MovieTitle          string
	Event               string
	Reason              string
	ActionVersion       int64
	TargetLabel         string
	Status              string
	ClaimVersion        int64
	ClaimExpiresAt      *time.Time
	AttemptCount        int
	NextAttemptAt       time.Time
	LastAttemptAt       *time.Time
	DeliveredAt         *time.Time
	ResolvedAt          *time.Time
	ErrorSummary        string
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

func (r *SqliteRadarrRepository) ListWebhookDestinations(
	ctx context.Context,
	includeArchived bool,
) ([]RadarrWebhookDestination, error) {
	where := "WHERE archived_at IS NULL"
	if includeArchived {
		where = ""
	}
	rows, err := r.pool.Read.QueryContext(ctx, radarrWebhookDestinationSelect+" "+where+`
		ORDER BY archived_at IS NOT NULL, name COLLATE NOCASE, id
	`)
	if err != nil {
		return nil, fmt.Errorf("list Radarr webhook destinations: %w", err)
	}
	defer rows.Close()

	destinations := make([]RadarrWebhookDestination, 0)
	for rows.Next() {
		destination, scanErr := scanRadarrWebhookDestination(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		destinations = append(destinations, destination)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list Radarr webhook destinations: %w", err)
	}
	return destinations, nil
}

func (r *SqliteRadarrRepository) GetWebhookDestination(
	ctx context.Context,
	id int64,
) (RadarrWebhookDestination, error) {
	return readRadarrWebhookDestination(ctx, r.pool.Read, id)
}

func (r *SqliteRadarrRepository) CreateWebhookDestination(
	ctx context.Context,
	save RadarrWebhookDestinationSave,
) (RadarrWebhookDestination, error) {
	filters, err := json.Marshal(save.ReasonFilters)
	if err != nil {
		return RadarrWebhookDestination{}, fmt.Errorf("encode Radarr webhook reason filters: %w", err)
	}
	var verifiedAt any
	if save.VerifiedAt != nil {
		verifiedAt = db.ToUnix(*save.VerifiedAt)
	}
	result, err := r.pool.Write.ExecContext(ctx, `
		INSERT INTO radarr_webhook_destinations (
			name, kind, encrypted_url, reason_filters, discord_role_mention,
			enabled, verified_at
		) VALUES (?, ?, ?, ?, ?, ?, ?)
	`, save.Name, save.Kind, save.EncryptedURL, string(filters),
		save.DiscordRoleMention, boolInt(save.Enabled), verifiedAt)
	if err != nil {
		return RadarrWebhookDestination{}, fmt.Errorf("create Radarr webhook destination: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return RadarrWebhookDestination{}, fmt.Errorf("read Radarr webhook destination id: %w", err)
	}
	return readRadarrWebhookDestination(ctx, r.pool.Write, id)
}

func (r *SqliteRadarrRepository) UpdateWebhookDestination(
	ctx context.Context,
	id, expectedRevision int64,
	save RadarrWebhookDestinationSave,
) (RadarrWebhookDestination, error) {
	filters, err := json.Marshal(save.ReasonFilters)
	if err != nil {
		return RadarrWebhookDestination{}, fmt.Errorf("encode Radarr webhook reason filters: %w", err)
	}
	var verifiedAt any
	if save.VerifiedAt != nil {
		verifiedAt = db.ToUnix(*save.VerifiedAt)
	}
	tx, err := r.pool.Write.BeginTx(ctx, nil)
	if err != nil {
		return RadarrWebhookDestination{}, fmt.Errorf("update Radarr webhook destination: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	result, err := tx.ExecContext(ctx, `
		UPDATE radarr_webhook_destinations
		SET name = ?, kind = ?, encrypted_url = ?, reason_filters = ?,
		    discord_role_mention = ?, enabled = ?, verified_at = ?,
		    revision = revision + 1, updated_at = unixepoch()
		WHERE id = ? AND revision = ? AND archived_at IS NULL
	`, save.Name, save.Kind, save.EncryptedURL, string(filters),
		save.DiscordRoleMention, boolInt(save.Enabled), verifiedAt, id, expectedRevision)
	if err != nil {
		return RadarrWebhookDestination{}, fmt.Errorf("update Radarr webhook destination: %w", err)
	}
	if err := requireRevisionUpdate(result); err != nil {
		return RadarrWebhookDestination{}, err
	}
	// A delivery belongs to the destination revision that produced it. Never
	// send an old condition to a newly edited URL or payload format.
	if _, err := tx.ExecContext(ctx, `
		UPDATE radarr_webhook_deliveries
		SET status = 'terminal_failed', claim_expires_at = NULL,
		    resolved_at = unixepoch(),
		    error_summary = 'destination configuration changed', updated_at = unixepoch()
		WHERE destination_id = ? AND status IN ('pending', 'sending')
	`, id); err != nil {
		return RadarrWebhookDestination{}, fmt.Errorf("retire stale Radarr webhook deliveries: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return RadarrWebhookDestination{}, fmt.Errorf("update Radarr webhook destination: %w", err)
	}
	return readRadarrWebhookDestination(ctx, r.pool.Write, id)
}

func (r *SqliteRadarrRepository) VerifyWebhookDestination(
	ctx context.Context,
	id, expectedRevision int64,
	at time.Time,
) (RadarrWebhookDestination, error) {
	tx, err := r.pool.Write.BeginTx(ctx, nil)
	if err != nil {
		return RadarrWebhookDestination{}, fmt.Errorf("verify Radarr webhook destination: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	result, err := tx.ExecContext(ctx, `
		UPDATE radarr_webhook_destinations
		SET verified_at = ?, health_warning_at = NULL, health_warning_reason = '',
		    updated_at = unixepoch()
		WHERE id = ? AND revision = ? AND archived_at IS NULL
	`, db.ToUnix(at), id, expectedRevision)
	if err != nil {
		return RadarrWebhookDestination{}, fmt.Errorf("verify Radarr webhook destination: %w", err)
	}
	if err := requireRevisionUpdate(result); err != nil {
		return RadarrWebhookDestination{}, err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE radarr_webhook_deliveries
		SET resolved_at = COALESCE(resolved_at, ?), updated_at = unixepoch()
		WHERE destination_id = ? AND status = 'terminal_failed'
	`, db.ToUnix(at), id); err != nil {
		return RadarrWebhookDestination{}, fmt.Errorf("resolve Radarr webhook failures: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return RadarrWebhookDestination{}, fmt.Errorf("verify Radarr webhook destination: %w", err)
	}
	return readRadarrWebhookDestination(ctx, r.pool.Write, id)
}

func (r *SqliteRadarrRepository) ArchiveWebhookDestination(
	ctx context.Context,
	id int64,
	at time.Time,
) error {
	tx, err := r.pool.Write.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("archive Radarr webhook destination: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	result, err := tx.ExecContext(ctx, `
		UPDATE radarr_webhook_destinations
		SET enabled = 0, health_warning_at = NULL, health_warning_reason = '',
		    archived_at = ?, updated_at = unixepoch()
		WHERE id = ? AND archived_at IS NULL
	`, db.ToUnix(at), id)
	if err != nil {
		return fmt.Errorf("archive Radarr webhook destination: %w", err)
	}
	if err := requireFoundUpdate(result); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE radarr_webhook_deliveries
		SET status = 'terminal_failed', claim_expires_at = NULL, resolved_at = ?,
		    error_summary = 'destination archived', updated_at = unixepoch()
		WHERE destination_id = ? AND status IN ('pending', 'sending')
	`, db.ToUnix(at), id); err != nil {
		return fmt.Errorf("resolve archived Radarr webhook deliveries: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE radarr_webhook_deliveries
		SET resolved_at = COALESCE(resolved_at, ?), updated_at = unixepoch()
		WHERE destination_id = ? AND status = 'terminal_failed'
	`, db.ToUnix(at), id); err != nil {
		return fmt.Errorf("resolve Radarr webhook warning: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("archive Radarr webhook destination: %w", err)
	}
	return nil
}

func (r *SqliteRadarrRepository) DueWebhookDeliveries(
	ctx context.Context,
	now time.Time,
	limit int,
) ([]RadarrWebhookDelivery, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := r.pool.Read.QueryContext(ctx, radarrWebhookDeliverySelect+`
		WHERE q.status = 'pending' AND q.next_attempt_at <= ?
		  AND d.enabled = 1 AND d.verified_at IS NOT NULL AND d.archived_at IS NULL
		  AND d.revision = q.destination_revision
		  AND a.revealed_at IS NOT NULL
		  AND a.status NOT IN ('downloaded', 'abandoned')
		  AND a.action_reason = q.reason
		  AND a.action_version = q.action_version
		ORDER BY q.next_attempt_at, q.id
		LIMIT ?
	`, db.ToUnix(now), limit)
	if err != nil {
		return nil, fmt.Errorf("list due Radarr webhook deliveries: %w", err)
	}
	defer rows.Close()

	deliveries := make([]RadarrWebhookDelivery, 0)
	for rows.Next() {
		delivery, scanErr := scanRadarrWebhookDelivery(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		deliveries = append(deliveries, delivery)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list due Radarr webhook deliveries: %w", err)
	}
	return deliveries, nil
}

// ClaimWebhookDeliveryForSend atomically reserves one due delivery. The claim
// version prevents a worker with an expired lease from completing a newer claim.
func (r *SqliteRadarrRepository) ClaimWebhookDeliveryForSend(
	ctx context.Context,
	id int64,
	now time.Time,
	claimExpiresAt time.Time,
) (RadarrWebhookDelivery, bool, error) {
	tx, err := r.pool.Write.BeginTx(ctx, nil)
	if err != nil {
		return RadarrWebhookDelivery{}, false, fmt.Errorf("claim Radarr webhook delivery: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	var claimVersion int64
	err = tx.QueryRowContext(ctx, `
		UPDATE radarr_webhook_deliveries
		SET status = 'sending', claim_version = claim_version + 1,
		    claim_expires_at = ?, attempt_count = attempt_count + 1,
		    last_attempt_at = ?, updated_at = unixepoch()
		WHERE id = ? AND status = 'pending' AND next_attempt_at <= ?
		  AND attempt_count < ?
		  AND EXISTS (
		      SELECT 1
		      FROM radarr_webhook_destinations d
		      WHERE d.id = radarr_webhook_deliveries.destination_id
		        AND d.enabled = 1 AND d.verified_at IS NOT NULL AND d.archived_at IS NULL
		        AND d.revision = radarr_webhook_deliveries.destination_revision
		  )
		  AND EXISTS (
		      SELECT 1
		      FROM radarr_acquisitions a
		      WHERE a.id = radarr_webhook_deliveries.acquisition_id
		        AND a.revealed_at IS NOT NULL
		        AND a.status NOT IN ('downloaded', 'abandoned')
		        AND a.action_reason = radarr_webhook_deliveries.reason
		        AND a.action_version = radarr_webhook_deliveries.action_version
		  )
		RETURNING claim_version
	`, db.ToUnix(claimExpiresAt), db.ToUnix(now), id, db.ToUnix(now),
		RadarrWebhookMaxAttempts).Scan(&claimVersion)
	if errors.Is(err, sql.ErrNoRows) {
		return RadarrWebhookDelivery{}, false, nil
	}
	if err != nil {
		return RadarrWebhookDelivery{}, false, fmt.Errorf("claim Radarr webhook delivery: %w", err)
	}
	delivery, err := scanRadarrWebhookDelivery(tx.QueryRowContext(ctx, radarrWebhookDeliverySelect+`
		WHERE q.id = ? AND q.status = 'sending' AND q.claim_version = ?
	`, id, claimVersion))
	if err != nil {
		return RadarrWebhookDelivery{}, false, fmt.Errorf("read claimed Radarr webhook delivery: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return RadarrWebhookDelivery{}, false, fmt.Errorf("claim Radarr webhook delivery: %w", err)
	}
	return delivery, true, nil
}

func (r *SqliteRadarrRepository) RecoverExpiredWebhookDeliveryClaims(
	ctx context.Context,
	now time.Time,
) (int64, error) {
	tx, err := r.pool.Write.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("recover expired Radarr webhook delivery claims: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `
		UPDATE radarr_webhook_destinations AS d
		SET health_warning_at = ?, health_warning_reason = ?, updated_at = unixepoch()
		WHERE d.archived_at IS NULL AND EXISTS (
			SELECT 1
			FROM radarr_webhook_deliveries q
			WHERE q.destination_id = d.id AND q.status = 'sending'
			  AND q.claim_expires_at <= ? AND q.attempt_count >= ?
		)
	`, db.ToUnix(now), radarrWebhookInterruptedFinalAttempt,
		db.ToUnix(now), RadarrWebhookMaxAttempts); err != nil {
		return 0, fmt.Errorf("warn about expired final Radarr webhook delivery claims: %w", err)
	}
	terminalResult, err := tx.ExecContext(ctx, `
		UPDATE radarr_webhook_deliveries
		SET status = 'terminal_failed', claim_expires_at = NULL,
		    error_summary = ?, updated_at = unixepoch()
		WHERE status = 'sending' AND claim_expires_at <= ? AND attempt_count >= ?
	`, radarrWebhookInterruptedFinalAttempt, db.ToUnix(now), RadarrWebhookMaxAttempts)
	if err != nil {
		return 0, fmt.Errorf("retire expired final Radarr webhook delivery claims: %w", err)
	}
	pendingResult, err := tx.ExecContext(ctx, `
		UPDATE radarr_webhook_deliveries
		SET status = 'pending', claim_expires_at = NULL, updated_at = unixepoch()
		WHERE status = 'sending' AND claim_expires_at <= ? AND attempt_count < ?
	`, db.ToUnix(now), RadarrWebhookMaxAttempts)
	if err != nil {
		return 0, fmt.Errorf("recover expired Radarr webhook delivery claims: %w", err)
	}
	terminalCount, err := terminalResult.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("read expired final Radarr webhook delivery claim count: %w", err)
	}
	pendingCount, err := pendingResult.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("read recovered Radarr webhook delivery claim count: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("recover expired Radarr webhook delivery claims: %w", err)
	}
	return terminalCount + pendingCount, nil
}

func (r *SqliteRadarrRepository) MarkWebhookDelivered(
	ctx context.Context,
	id int64,
	claimVersion int64,
	at time.Time,
) error {
	tx, err := r.pool.Write.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("complete Radarr webhook delivery: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	var destinationID int64
	if err := tx.QueryRowContext(ctx, `
		UPDATE radarr_webhook_deliveries
		SET status = 'delivered', claim_expires_at = NULL,
		    delivered_at = ?, error_summary = '',
		    updated_at = unixepoch()
		WHERE id = ? AND status = 'sending' AND claim_version = ?
		  AND claim_expires_at > ?
		RETURNING destination_id
	`, db.ToUnix(at), id, claimVersion, db.ToUnix(at)).Scan(&destinationID); err != nil {
		return mapWebhookClaimNoRows(err)
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE radarr_webhook_destinations
		SET health_warning_at = NULL, health_warning_reason = '', updated_at = unixepoch()
		WHERE id = ?
	`, destinationID); err != nil {
		return fmt.Errorf("clear Radarr webhook warning: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE radarr_webhook_deliveries
		SET resolved_at = COALESCE(resolved_at, ?), updated_at = unixepoch()
		WHERE destination_id = ? AND status = 'terminal_failed'
	`, db.ToUnix(at), destinationID); err != nil {
		return fmt.Errorf("resolve Radarr webhook failures: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("complete Radarr webhook delivery: %w", err)
	}
	return nil
}

func (r *SqliteRadarrRepository) MarkWebhookDeliveryFailed(
	ctx context.Context,
	id int64,
	claimVersion int64,
	summary string,
	nextAttemptAt time.Time,
	terminal bool,
	at time.Time,
) error {
	tx, err := r.pool.Write.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("fail Radarr webhook delivery: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	status := "pending"
	if terminal {
		status = "terminal_failed"
	}
	var destinationID int64
	if err := tx.QueryRowContext(ctx, `
		UPDATE radarr_webhook_deliveries
		SET status = ?, claim_expires_at = NULL,
		    next_attempt_at = ?, error_summary = ?, updated_at = unixepoch()
		WHERE id = ? AND status = 'sending' AND claim_version = ?
		  AND claim_expires_at > ?
		RETURNING destination_id
	`, status, db.ToUnix(nextAttemptAt), summary,
		id, claimVersion, db.ToUnix(at)).Scan(&destinationID); err != nil {
		return mapWebhookClaimNoRows(err)
	}
	if terminal {
		if _, err := tx.ExecContext(ctx, `
			UPDATE radarr_webhook_destinations
			SET health_warning_at = ?, health_warning_reason = ?, updated_at = unixepoch()
			WHERE id = ? AND archived_at IS NULL
		`, db.ToUnix(at), summary, destinationID); err != nil {
			return fmt.Errorf("set Radarr webhook warning: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("fail Radarr webhook delivery: %w", err)
	}
	return nil
}

func (r *SqliteRadarrRepository) PruneWebhookDeliveries(
	ctx context.Context,
	now time.Time,
) (int64, error) {
	successBefore := db.ToUnix(now.Add(-30 * 24 * time.Hour))
	failureBefore := db.ToUnix(now.Add(-90 * 24 * time.Hour))
	result, err := r.pool.Write.ExecContext(ctx, `
		DELETE FROM radarr_webhook_deliveries
		WHERE (status = 'delivered' AND delivered_at < ?)
		   OR (status IN ('terminal_failed', 'superseded') AND resolved_at IS NOT NULL AND resolved_at < ?)
	`, successBefore, failureBefore)
	if err != nil {
		return 0, fmt.Errorf("prune Radarr webhook deliveries: %w", err)
	}
	removed, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("read pruned Radarr webhook delivery count: %w", err)
	}
	return removed, nil
}

const radarrWebhookDestinationSelect = `
	SELECT id, name, kind, encrypted_url, reason_filters, discord_role_mention,
	       enabled, verified_at, revision, health_warning_at,
	       health_warning_reason, created_at, updated_at, archived_at
	FROM radarr_webhook_destinations
`

const radarrWebhookDeliverySelect = `
	SELECT q.id, q.destination_id, q.destination_revision, d.name, d.kind,
	       d.encrypted_url, d.discord_role_mention, q.acquisition_id,
	       a.movie_title, q.event, q.reason, q.action_version, q.target_label,
	       q.status, q.claim_version, q.claim_expires_at, q.attempt_count,
	       q.next_attempt_at, q.last_attempt_at, q.delivered_at, q.resolved_at,
	       q.error_summary, q.created_at, q.updated_at
	FROM radarr_webhook_deliveries q
	JOIN radarr_webhook_destinations d ON d.id = q.destination_id
	JOIN radarr_acquisitions a ON a.id = q.acquisition_id
`

func readRadarrWebhookDestination(
	ctx context.Context,
	q queryRower,
	id int64,
) (RadarrWebhookDestination, error) {
	destination, err := scanRadarrWebhookDestination(q.QueryRowContext(
		ctx, radarrWebhookDestinationSelect+" WHERE id = ?", id,
	))
	if errors.Is(err, sql.ErrNoRows) {
		return RadarrWebhookDestination{}, domain.ErrNotFound
	}
	return destination, err
}

func scanRadarrWebhookDestination(row rowScanner) (RadarrWebhookDestination, error) {
	var destination RadarrWebhookDestination
	var filters string
	var enabled int
	var verifiedAt, warningAt, archivedAt sql.NullInt64
	var createdAt, updatedAt int64
	if err := row.Scan(
		&destination.ID, &destination.Name, &destination.Kind,
		&destination.EncryptedURL, &filters, &destination.DiscordRoleMention,
		&enabled, &verifiedAt, &destination.Revision, &warningAt,
		&destination.HealthWarningReason, &createdAt, &updatedAt, &archivedAt,
	); err != nil {
		return RadarrWebhookDestination{}, fmt.Errorf("scan Radarr webhook destination: %w", err)
	}
	if err := json.Unmarshal([]byte(filters), &destination.ReasonFilters); err != nil {
		return RadarrWebhookDestination{}, fmt.Errorf("decode Radarr webhook reason filters: %w", err)
	}
	destination.Enabled = enabled == 1
	destination.VerifiedAt = unixTimePtr(verifiedAt)
	destination.HealthWarningAt = unixTimePtr(warningAt)
	destination.CreatedAt = db.FromUnix(createdAt)
	destination.UpdatedAt = db.FromUnix(updatedAt)
	destination.ArchivedAt = unixTimePtr(archivedAt)
	return destination, nil
}

func scanRadarrWebhookDelivery(row rowScanner) (RadarrWebhookDelivery, error) {
	var delivery RadarrWebhookDelivery
	var nextAttemptAt, createdAt, updatedAt int64
	var claimExpiresAt, lastAttemptAt, deliveredAt, resolvedAt sql.NullInt64
	if err := row.Scan(
		&delivery.ID, &delivery.DestinationID, &delivery.DestinationRevision,
		&delivery.DestinationName, &delivery.Kind, &delivery.EncryptedURL,
		&delivery.DiscordRoleMention, &delivery.AcquisitionID, &delivery.MovieTitle,
		&delivery.Event, &delivery.Reason, &delivery.ActionVersion,
		&delivery.TargetLabel, &delivery.Status, &delivery.ClaimVersion,
		&claimExpiresAt, &delivery.AttemptCount, &nextAttemptAt,
		&lastAttemptAt, &deliveredAt, &resolvedAt,
		&delivery.ErrorSummary, &createdAt, &updatedAt,
	); err != nil {
		return RadarrWebhookDelivery{}, fmt.Errorf("scan Radarr webhook delivery: %w", err)
	}
	delivery.ClaimExpiresAt = unixTimePtr(claimExpiresAt)
	delivery.NextAttemptAt = db.FromUnix(nextAttemptAt)
	delivery.LastAttemptAt = unixTimePtr(lastAttemptAt)
	delivery.DeliveredAt = unixTimePtr(deliveredAt)
	delivery.ResolvedAt = unixTimePtr(resolvedAt)
	delivery.CreatedAt = db.FromUnix(createdAt)
	delivery.UpdatedAt = db.FromUnix(updatedAt)
	return delivery, nil
}

func mapWebhookClaimNoRows(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return domain.ErrConflict
	}
	return err
}
