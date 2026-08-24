package repository

import (
	"context"
	"database/sql"
	"encoding/json/v2"
	"errors"
	"fmt"
	"time"

	"moviepickarr/internal/db"
	"moviepickarr/internal/domain"
	"moviepickarr/internal/integration"
)

// SqliteRadarrRepository is the single persistence boundary for Radarr setup,
// Acquisition state, and the webhook outbox. Configuration and lifecycle
// methods are split across radarr_*.go files, but share this writer.
type SqliteRadarrRepository struct {
	pool *db.Pool
}

func NewSqliteRadarrRepository(pool *db.Pool) *SqliteRadarrRepository {
	return &SqliteRadarrRepository{pool: pool}
}

// RadarrRemoveOutcome reports whether an unused setup row was removed or a
// used row was archived to preserve Acquisition history.
type RadarrRemoveOutcome string

const (
	RadarrOutcomeDeleted  RadarrRemoveOutcome = "deleted"
	RadarrOutcomeArchived RadarrRemoveOutcome = "archived"
)

type RadarrInstance struct {
	ID              int64
	Name            string
	BaseURL         string
	EncryptedAPIKey []byte
	Revision        int64
	State           string
	StateReason     string
	LastCheckedAt   time.Time
	CreatedAt       time.Time
	UpdatedAt       time.Time
	ArchivedAt      *time.Time
	Used            bool
}

type RadarrInstanceSave struct {
	Name            string
	BaseURL         string
	EncryptedAPIKey []byte
	State           string
	StateReason     string
	CheckedAt       time.Time
}

type RadarrTagSnapshot struct {
	ID    int    `json:"id"`
	Label string `json:"label"`
}

type RadarrPreset struct {
	ID                  int64
	Name                string
	InstanceID          int64
	InstanceName        string
	RootFolderID        int
	RootFolderPath      string
	QualityProfileID    int
	QualityProfileName  string
	Tags                []RadarrTagSnapshot
	MinimumAvailability string
	AcquisitionMode     string
	Revision            int64
	Valid               bool
	ValidationReason    string
	ValidatedAt         time.Time
	CreatedAt           time.Time
	UpdatedAt           time.Time
	ArchivedAt          *time.Time
	Used                bool
}

type RadarrPresetSave struct {
	Name                string
	InstanceID          int64
	RootFolderID        int
	RootFolderPath      string
	QualityProfileID    int
	QualityProfileName  string
	Tags                []RadarrTagSnapshot
	MinimumAvailability string
	AcquisitionMode     string
	Valid               bool
	ValidationReason    string
	ValidatedAt         time.Time
}

func (r *SqliteRadarrRepository) ListInstances(ctx context.Context, includeArchived bool) ([]RadarrInstance, error) {
	where := "WHERE i.archived_at IS NULL"
	if includeArchived {
		where = ""
	}
	rows, err := r.pool.Read.QueryContext(ctx, radarrInstanceSelect+` `+where+`
		ORDER BY i.archived_at IS NOT NULL, i.name COLLATE NOCASE, i.id
	`)
	if err != nil {
		return nil, fmt.Errorf("list Radarr instances: %w", err)
	}
	defer rows.Close()

	instances := make([]RadarrInstance, 0)
	for rows.Next() {
		instance, scanErr := scanRadarrInstance(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		instances = append(instances, instance)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list Radarr instances: %w", err)
	}
	return instances, nil
}

func (r *SqliteRadarrRepository) GetInstance(ctx context.Context, id int64) (RadarrInstance, error) {
	return readRadarrInstance(ctx, r.pool.Read, id)
}

func (r *SqliteRadarrRepository) CreateInstance(
	ctx context.Context,
	save RadarrInstanceSave,
) (RadarrInstance, error) {
	result, err := r.pool.Write.ExecContext(ctx, `
		INSERT INTO radarr_instances (
			name, base_url, encrypted_api_key, state, state_reason, last_checked_at
		) VALUES (?, ?, ?, ?, ?, ?)
	`, save.Name, save.BaseURL, save.EncryptedAPIKey, save.State, save.StateReason, db.ToUnix(save.CheckedAt))
	if err != nil {
		return RadarrInstance{}, fmt.Errorf("create Radarr instance: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return RadarrInstance{}, fmt.Errorf("read Radarr instance id: %w", err)
	}
	return readRadarrInstance(ctx, r.pool.Write, id)
}

func (r *SqliteRadarrRepository) UpdateInstance(
	ctx context.Context,
	id, expectedRevision int64,
	save RadarrInstanceSave,
) (RadarrInstance, error) {
	result, err := r.pool.Write.ExecContext(ctx, `
		UPDATE radarr_instances
		SET name = ?, base_url = ?, encrypted_api_key = ?, state = ?, state_reason = ?,
		    last_checked_at = ?, revision = revision + 1, updated_at = unixepoch()
		WHERE id = ? AND revision = ? AND archived_at IS NULL
	`, save.Name, save.BaseURL, save.EncryptedAPIKey, save.State, save.StateReason,
		db.ToUnix(save.CheckedAt), id, expectedRevision)
	if err != nil {
		return RadarrInstance{}, fmt.Errorf("update Radarr instance: %w", err)
	}
	if err := requireRevisionUpdate(result); err != nil {
		return RadarrInstance{}, err
	}
	return readRadarrInstance(ctx, r.pool.Write, id)
}

func (r *SqliteRadarrRepository) UpdateInstanceState(
	ctx context.Context,
	id int64,
	state, reason string,
	checkedAt time.Time,
) error {
	result, err := r.pool.Write.ExecContext(ctx, `
		UPDATE radarr_instances
		SET state = ?, state_reason = ?, last_checked_at = ?, updated_at = unixepoch()
		WHERE id = ? AND archived_at IS NULL
	`, state, reason, db.ToUnix(checkedAt), id)
	if err != nil {
		return fmt.Errorf("update Radarr instance state: %w", err)
	}
	return requireFoundUpdate(result)
}

// RemoveInstance hard-deletes an instance and its presets when no Acquisition
// has used that setup. A used instance and its referenced presets stay archived
// for history, while unreferenced child presets are deleted. The active-target
// guard applies to both paths.
func (r *SqliteRadarrRepository) RemoveInstance(
	ctx context.Context,
	id int64,
	at time.Time,
) (RadarrRemoveOutcome, error) {
	tx, err := r.pool.Write.BeginTx(ctx, nil)
	if err != nil {
		return "", fmt.Errorf("remove Radarr instance: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var existingID int64
	if err := tx.QueryRowContext(ctx,
		"SELECT id FROM radarr_instances WHERE id = ?", id,
	).Scan(&existingID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", domain.ErrNotFound
		}
		return "", fmt.Errorf("find Radarr instance: %w", err)
	}
	var unresolved int
	if err := tx.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM radarr_acquisitions a
			WHERE a.target_instance_id = ?
			  AND a.status NOT IN ('downloaded', 'abandoned')
		)
	`, id).Scan(&unresolved); err != nil {
		return "", fmt.Errorf("check Radarr instance acquisitions: %w", err)
	}
	if unresolved != 0 {
		return "", domain.ErrConflict
	}

	var used int
	if err := tx.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM radarr_acquisitions a
			LEFT JOIN radarr_presets p ON p.id = a.preset_id
			WHERE a.target_instance_id = ? OR p.instance_id = ?
		)
	`, id, id).Scan(&used); err != nil {
		return "", fmt.Errorf("check Radarr instance history: %w", err)
	}

	if used == 0 {
		if _, err := tx.ExecContext(ctx, "DELETE FROM radarr_presets WHERE instance_id = ?", id); err != nil {
			return "", fmt.Errorf("delete Radarr instance presets: %w", err)
		}
		result, err := tx.ExecContext(ctx,
			"DELETE FROM radarr_instances WHERE id = ?", id,
		)
		if err != nil {
			return "", fmt.Errorf("delete Radarr instance: %w", err)
		}
		if err := requireFoundUpdate(result); err != nil {
			return "", err
		}
		if err := tx.Commit(); err != nil {
			return "", fmt.Errorf("delete Radarr instance: %w", err)
		}
		return RadarrOutcomeDeleted, nil
	}

	if _, err := tx.ExecContext(ctx, `
		DELETE FROM radarr_presets
		WHERE instance_id = ?
		  AND NOT EXISTS (
		      SELECT 1
		      FROM radarr_acquisitions a
		      WHERE a.preset_id = radarr_presets.id
		  )
	`, id); err != nil {
		return "", fmt.Errorf("delete unused Radarr instance presets: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE radarr_presets
		SET archived_at = ?, updated_at = unixepoch()
		WHERE instance_id = ? AND archived_at IS NULL
	`, db.ToUnix(at), id); err != nil {
		return "", fmt.Errorf("archive Radarr instance presets: %w", err)
	}
	result, err := tx.ExecContext(ctx, `
		UPDATE radarr_instances
		SET archived_at = ?, updated_at = unixepoch()
		WHERE id = ?
	`, db.ToUnix(at), id)
	if err != nil {
		return "", fmt.Errorf("archive Radarr instance: %w", err)
	}
	if err := requireFoundUpdate(result); err != nil {
		return "", err
	}
	if err := tx.Commit(); err != nil {
		return "", fmt.Errorf("archive Radarr instance: %w", err)
	}
	return RadarrOutcomeArchived, nil
}

func (r *SqliteRadarrRepository) ArchiveInstance(ctx context.Context, id int64, at time.Time) error {
	tx, err := r.pool.Write.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("archive Radarr instance: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var unresolved int
	if err := tx.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM radarr_acquisitions
		WHERE target_instance_id = ? AND status NOT IN ('downloaded', 'abandoned')
	`, id).Scan(&unresolved); err != nil {
		return fmt.Errorf("check Radarr instance acquisitions: %w", err)
	}
	if unresolved > 0 {
		return domain.ErrConflict
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE radarr_presets
		SET archived_at = ?, updated_at = unixepoch()
		WHERE instance_id = ? AND archived_at IS NULL
	`, db.ToUnix(at), id); err != nil {
		return fmt.Errorf("archive Radarr instance presets: %w", err)
	}
	result, err := tx.ExecContext(ctx, `
		UPDATE radarr_instances
		SET archived_at = ?, updated_at = unixepoch()
		WHERE id = ? AND archived_at IS NULL
	`, db.ToUnix(at), id)
	if err != nil {
		return fmt.Errorf("archive Radarr instance: %w", err)
	}
	if err := requireFoundUpdate(result); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("archive Radarr instance: %w", err)
	}
	return nil
}

func (r *SqliteRadarrRepository) ListPresets(ctx context.Context, includeArchived bool) ([]RadarrPreset, error) {
	where := "WHERE p.archived_at IS NULL"
	if includeArchived {
		where = ""
	}
	rows, err := r.pool.Read.QueryContext(ctx, radarrPresetSelect+" "+where+`
		ORDER BY p.archived_at IS NOT NULL, p.name COLLATE NOCASE, p.id
	`)
	if err != nil {
		return nil, fmt.Errorf("list Radarr presets: %w", err)
	}
	defer rows.Close()

	presets := make([]RadarrPreset, 0)
	for rows.Next() {
		preset, scanErr := scanRadarrPreset(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		presets = append(presets, preset)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list Radarr presets: %w", err)
	}
	return presets, nil
}

func (r *SqliteRadarrRepository) GetPreset(ctx context.Context, id int64) (RadarrPreset, error) {
	return readRadarrPreset(ctx, r.pool.Read, id)
}

func (r *SqliteRadarrRepository) CreatePreset(
	ctx context.Context,
	save RadarrPresetSave,
) (RadarrPreset, error) {
	tags, err := json.Marshal(save.Tags)
	if err != nil {
		return RadarrPreset{}, fmt.Errorf("encode Radarr preset tags: %w", err)
	}
	result, err := r.pool.Write.ExecContext(ctx, `
		INSERT INTO radarr_presets (
			name, instance_id, root_folder_id, root_folder_path,
			quality_profile_id, quality_profile_name, tags, minimum_availability,
			acquisition_mode, valid, validation_reason, validated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, save.Name, save.InstanceID, save.RootFolderID, save.RootFolderPath,
		save.QualityProfileID, save.QualityProfileName, string(tags), save.MinimumAvailability,
		save.AcquisitionMode, boolInt(save.Valid), save.ValidationReason, db.ToUnix(save.ValidatedAt))
	if err != nil {
		return RadarrPreset{}, fmt.Errorf("create Radarr preset: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return RadarrPreset{}, fmt.Errorf("read Radarr preset id: %w", err)
	}
	return readRadarrPreset(ctx, r.pool.Write, id)
}

func (r *SqliteRadarrRepository) UpdatePreset(
	ctx context.Context,
	id, expectedRevision int64,
	save RadarrPresetSave,
) (RadarrPreset, error) {
	tags, err := json.Marshal(save.Tags)
	if err != nil {
		return RadarrPreset{}, fmt.Errorf("encode Radarr preset tags: %w", err)
	}
	result, err := r.pool.Write.ExecContext(ctx, `
		UPDATE radarr_presets
		SET name = ?, instance_id = ?, root_folder_id = ?, root_folder_path = ?,
		    quality_profile_id = ?, quality_profile_name = ?, tags = ?,
		    minimum_availability = ?, acquisition_mode = ?, valid = ?,
		    validation_reason = ?, validated_at = ?, revision = revision + 1,
		    updated_at = unixepoch()
		WHERE id = ? AND revision = ? AND archived_at IS NULL
	`, save.Name, save.InstanceID, save.RootFolderID, save.RootFolderPath,
		save.QualityProfileID, save.QualityProfileName, string(tags), save.MinimumAvailability,
		save.AcquisitionMode, boolInt(save.Valid), save.ValidationReason,
		db.ToUnix(save.ValidatedAt), id, expectedRevision)
	if err != nil {
		return RadarrPreset{}, fmt.Errorf("update Radarr preset: %w", err)
	}
	if err := requireRevisionUpdate(result); err != nil {
		return RadarrPreset{}, err
	}
	return readRadarrPreset(ctx, r.pool.Write, id)
}

func (r *SqliteRadarrRepository) SetPresetValidity(
	ctx context.Context,
	id int64,
	valid bool,
	reason string,
	validatedAt time.Time,
) error {
	result, err := r.pool.Write.ExecContext(ctx, `
		UPDATE radarr_presets
		SET valid = ?, validation_reason = ?, validated_at = ?, updated_at = unixepoch()
		WHERE id = ? AND archived_at IS NULL
	`, boolInt(valid), reason, db.ToUnix(validatedAt), id)
	if err != nil {
		return fmt.Errorf("update Radarr preset validity: %w", err)
	}
	return requireFoundUpdate(result)
}

// RemovePreset hard-deletes a preset that no Acquisition has selected,
// including unused setup archived by the former archive-only behavior. Once a
// preset appears in history, removal archives it and preserves the snapshot's
// foreign-key attribution.
func (r *SqliteRadarrRepository) RemovePreset(
	ctx context.Context,
	id int64,
	at time.Time,
) (RadarrRemoveOutcome, error) {
	tx, err := r.pool.Write.BeginTx(ctx, nil)
	if err != nil {
		return "", fmt.Errorf("remove Radarr preset: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var existingID int64
	if err := tx.QueryRowContext(ctx,
		"SELECT id FROM radarr_presets WHERE id = ?", id,
	).Scan(&existingID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", domain.ErrNotFound
		}
		return "", fmt.Errorf("find Radarr preset: %w", err)
	}
	var used int
	if err := tx.QueryRowContext(ctx, `
		SELECT EXISTS (SELECT 1 FROM radarr_acquisitions WHERE preset_id = ?)
	`, id).Scan(&used); err != nil {
		return "", fmt.Errorf("check Radarr preset history: %w", err)
	}

	var result sql.Result
	if used == 0 {
		result, err = tx.ExecContext(ctx,
			"DELETE FROM radarr_presets WHERE id = ?", id,
		)
	} else {
		result, err = tx.ExecContext(ctx, `
			UPDATE radarr_presets
			SET archived_at = ?, updated_at = unixepoch()
			WHERE id = ?
		`, db.ToUnix(at), id)
	}
	if err != nil {
		return "", fmt.Errorf("remove Radarr preset: %w", err)
	}
	if err := requireFoundUpdate(result); err != nil {
		return "", err
	}
	if err := tx.Commit(); err != nil {
		return "", fmt.Errorf("remove Radarr preset: %w", err)
	}
	if used == 0 {
		return RadarrOutcomeDeleted, nil
	}
	return RadarrOutcomeArchived, nil
}

func (r *SqliteRadarrRepository) ArchivePreset(ctx context.Context, id int64, at time.Time) error {
	result, err := r.pool.Write.ExecContext(ctx, `
		UPDATE radarr_presets
		SET archived_at = ?, updated_at = unixepoch()
		WHERE id = ? AND archived_at IS NULL
	`, db.ToUnix(at), id)
	if err != nil {
		return fmt.Errorf("archive Radarr preset: %w", err)
	}
	return requireFoundUpdate(result)
}

const radarrPresetSelect = `
	SELECT p.id, p.name, p.instance_id, i.name, p.root_folder_id,
	       p.root_folder_path, p.quality_profile_id, p.quality_profile_name,
	       p.tags, p.minimum_availability, p.acquisition_mode, p.revision,
	       p.valid, p.validation_reason, p.validated_at, p.created_at,
	       p.updated_at, p.archived_at,
	       EXISTS (SELECT 1 FROM radarr_acquisitions a WHERE a.preset_id = p.id)
	FROM radarr_presets p
	JOIN radarr_instances i ON i.id = p.instance_id
`

const radarrInstanceSelect = `
	SELECT i.id, i.name, i.base_url, i.encrypted_api_key, i.revision, i.state,
	       i.state_reason, i.last_checked_at, i.created_at, i.updated_at,
	       i.archived_at,
	       EXISTS (
	           SELECT 1
	           FROM radarr_acquisitions a
	           LEFT JOIN radarr_presets p ON p.id = a.preset_id
	           WHERE a.target_instance_id = i.id OR p.instance_id = i.id
	       )
	FROM radarr_instances i
`

func scanRadarrInstance(row rowScanner) (RadarrInstance, error) {
	var instance RadarrInstance
	var checkedAt, createdAt, updatedAt int64
	var archivedAt sql.NullInt64
	var used int
	if err := row.Scan(
		&instance.ID, &instance.Name, &instance.BaseURL, &instance.EncryptedAPIKey,
		&instance.Revision, &instance.State, &instance.StateReason, &checkedAt,
		&createdAt, &updatedAt, &archivedAt, &used,
	); err != nil {
		return RadarrInstance{}, fmt.Errorf("scan Radarr instance: %w", err)
	}
	instance.LastCheckedAt = db.FromUnix(checkedAt)
	instance.CreatedAt = db.FromUnix(createdAt)
	instance.UpdatedAt = db.FromUnix(updatedAt)
	instance.ArchivedAt = unixTimePtr(archivedAt)
	instance.Used = used != 0
	return instance, nil
}

func readRadarrInstance(ctx context.Context, q queryRower, id int64) (RadarrInstance, error) {
	instance, err := scanRadarrInstance(q.QueryRowContext(ctx, radarrInstanceSelect+" WHERE i.id = ?", id))
	if errors.Is(err, sql.ErrNoRows) {
		return RadarrInstance{}, domain.ErrNotFound
	}
	return instance, err
}

func scanRadarrPreset(row rowScanner) (RadarrPreset, error) {
	var preset RadarrPreset
	var tags string
	var valid int
	var validatedAt, createdAt, updatedAt int64
	var archivedAt sql.NullInt64
	var used int
	if err := row.Scan(
		&preset.ID, &preset.Name, &preset.InstanceID, &preset.InstanceName,
		&preset.RootFolderID, &preset.RootFolderPath, &preset.QualityProfileID,
		&preset.QualityProfileName, &tags, &preset.MinimumAvailability,
		&preset.AcquisitionMode, &preset.Revision, &valid, &preset.ValidationReason,
		&validatedAt, &createdAt, &updatedAt, &archivedAt, &used,
	); err != nil {
		return RadarrPreset{}, fmt.Errorf("scan Radarr preset: %w", err)
	}
	if err := json.Unmarshal([]byte(tags), &preset.Tags); err != nil {
		return RadarrPreset{}, fmt.Errorf("decode Radarr preset tags: %w", err)
	}
	preset.Valid = valid == 1
	preset.ValidatedAt = db.FromUnix(validatedAt)
	preset.CreatedAt = db.FromUnix(createdAt)
	preset.UpdatedAt = db.FromUnix(updatedAt)
	preset.ArchivedAt = unixTimePtr(archivedAt)
	preset.Used = used != 0
	return preset, nil
}

func readRadarrPreset(ctx context.Context, q queryRower, id int64) (RadarrPreset, error) {
	preset, err := scanRadarrPreset(q.QueryRowContext(ctx, radarrPresetSelect+" WHERE p.id = ?", id))
	if errors.Is(err, sql.ErrNoRows) {
		return RadarrPreset{}, domain.ErrNotFound
	}
	return preset, err
}

func requireFoundUpdate(result sql.Result) error {
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read update result: %w", err)
	}
	if rows == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func requireRevisionUpdate(result sql.Result) error {
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read revision update result: %w", err)
	}
	if rows == 0 {
		return integration.ErrStaleRevision
	}
	return nil
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}
