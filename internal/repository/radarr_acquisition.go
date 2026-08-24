package repository

import (
	"context"
	"database/sql"
	"encoding/json/v2"
	"errors"
	"fmt"
	"strings"
	"time"

	"moviepickarr/internal/domain"
)

const radarrAcquisitionHandoffLease = 30 * time.Second

type RadarrEffectiveConfiguration struct {
	RootFolderPath      string              `json:"rootFolderPath,omitempty"`
	QualityProfileID    int                 `json:"qualityProfileId,omitzero"`
	QualityProfileName  string              `json:"qualityProfileName,omitempty"`
	Tags                []RadarrTagSnapshot `json:"tags,omitempty"`
	MinimumAvailability string              `json:"minimumAvailability,omitempty"`
	Monitored           bool                `json:"monitored"`
}

type RadarrAcquisition struct {
	ID                         int64
	MovieID                    int
	Revision                   int64
	Status                     string
	ActionReason               string
	ActionVersion              int64
	ActionStartedAt            *time.Time
	MovieTitle                 string
	MovieYear                  int
	TMDBID                     *int
	IMDbID                     string
	IdentitySource             string
	IdentityOverrideTMDBID     *int
	DrawnAt                    time.Time
	RevealAt                   time.Time
	DrawClientID               string
	RevealedAt                 *time.Time
	PresetID                   *int64
	PresetName                 string
	TargetInstanceID           *int64
	TargetInstanceName         string
	TargetRootFolderID         *int
	TargetRootFolderPath       string
	TargetQualityProfileID     *int
	TargetQualityProfileName   string
	TargetTags                 []RadarrTagSnapshot
	TargetMinimumAvailability  string
	TargetAcquisitionMode      string
	TargetSelectedAt           *time.Time
	TargetSelectedBy           *int
	TargetLockedAt             *time.Time
	TargetLockedBy             *int
	RadarrMovieID              *int
	AdoptedExisting            bool
	EffectiveConfiguration     RadarrEffectiveConfiguration
	TargetPreviewExisting      bool
	TargetPreviewedAt          *time.Time
	MutationState              string
	AutomaticSearchClaimedAt   *time.Time
	AutomaticSearchCommandID   *int
	AutomaticSearchCompletedAt *time.Time
	LatestReleaseTitle         string
	LatestReleaseQuality       string
	LatestReleaseSelectedAt    *time.Time
	LatestReleaseSelectedBy    *int
	ManualAttemptCount         int
	LatestFailureSummary       string
	LatestFailureAt            *time.Time
	QueueStatus                string
	QueueSummary               string
	LastCheckedAt              *time.Time
	NextCheckAt                *time.Time
	QueuedAt                   *time.Time
	DownloadingAt              *time.Time
	ImportingAt                *time.Time
	DownloadedAt               *time.Time
	AbandonedAt                *time.Time
	AbandonedBy                *int
	AbandonmentReason          string
	CreatedAt                  time.Time
	UpdatedAt                  time.Time
}

func (a RadarrAcquisition) ResolvedTMDBID() (int, bool) {
	if a.IdentityOverrideTMDBID != nil {
		return *a.IdentityOverrideTMDBID, true
	}
	if a.TMDBID != nil {
		return *a.TMDBID, true
	}
	return 0, false
}

func (a RadarrAcquisition) Terminal() bool {
	return a.Status == "downloaded" || a.Status == "abandoned"
}

func (a RadarrAcquisition) TargetLocked() bool { return a.TargetLockedAt != nil }

type RadarrTargetLock struct {
	RadarrMovieID          int
	Existing               bool
	EffectiveConfiguration RadarrEffectiveConfiguration
	LockedBy               int
	Status                 string
	ActionReason           string
	AutomaticCommandID     *int
	At                     time.Time
}

type RadarrAcquisitionTransition struct {
	Status         string
	ActionReason   string
	FailureSummary string
	QueueStatus    string
	QueueSummary   string
	NextCheckAt    *time.Time
	At             time.Time
}

const radarrAcquisitionSelect = `
	SELECT id, movie_id, revision, status, action_reason, action_version,
	       action_started_at, movie_title, movie_year, tmdb_id, imdb_id, identity_source,
	       identity_override_tmdb_id, drawn_at, reveal_at, draw_client_id,
	       revealed_at, preset_id, preset_name, target_instance_id,
	       target_instance_name, target_root_folder_id, target_root_folder_path,
	       target_quality_profile_id, target_quality_profile_name, target_tags,
	       target_minimum_availability, target_acquisition_mode,
	       target_selected_at, target_selected_by, target_locked_at,
	       target_locked_by, radarr_movie_id, adopted_existing,
	       effective_configuration, target_preview_existing, target_previewed_at,
	       mutation_state, automatic_search_claimed_at, automatic_search_command_id,
	       automatic_search_completed_at, latest_release_title,
	       latest_release_quality, latest_release_selected_at,
	       latest_release_selected_by, manual_attempt_count,
	       latest_failure_summary, latest_failure_at, queue_status, queue_summary,
	       last_checked_at, next_check_at, queued_at, downloading_at, importing_at,
	       downloaded_at, abandoned_at, abandoned_by, abandonment_reason,
	       created_at, updated_at
	FROM radarr_acquisitions
`

func (r *SqliteRadarrRepository) ListAcquisitions(
	ctx context.Context,
	query string,
) ([]RadarrAcquisition, error) {
	query = strings.TrimSpace(query)
	args := make([]any, 0, 1)
	where := "WHERE revealed_at IS NOT NULL"
	if query != "" {
		where += ` AND (
			movie_title LIKE ? ESCAPE '\' OR
			COALESCE(preset_name, '') LIKE ? ESCAPE '\' OR
			COALESCE(target_instance_name, '') LIKE ? ESCAPE '\'
		)`
		like := "%" + escapeLike(query) + "%"
		args = append(args, like, like, like)
	}
	rows, err := r.pool.Read.QueryContext(ctx, radarrAcquisitionSelect+" "+where+`
		ORDER BY status IN ('downloaded', 'abandoned'), updated_at DESC, id DESC
	`, args...)
	if err != nil {
		return nil, fmt.Errorf("list Radarr acquisitions: %w", err)
	}
	defer rows.Close()

	acquisitions := make([]RadarrAcquisition, 0)
	for rows.Next() {
		acquisition, scanErr := scanRadarrAcquisition(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		acquisitions = append(acquisitions, acquisition)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list Radarr acquisitions: %w", err)
	}
	return acquisitions, nil
}

func (r *SqliteRadarrRepository) AttentionCount(ctx context.Context) (int, error) {
	var count int
	if err := r.pool.Read.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM radarr_acquisitions
		WHERE revealed_at IS NOT NULL AND status NOT IN ('downloaded', 'abandoned')
	`).Scan(&count); err != nil {
		return 0, fmt.Errorf("count Radarr acquisitions needing attention: %w", err)
	}
	return count, nil
}

func (r *SqliteRadarrRepository) GetVisibleAcquisition(
	ctx context.Context,
	id int64,
) (RadarrAcquisition, error) {
	return readRadarrAcquisition(ctx, r.pool.Read, id, true)
}

func (r *SqliteRadarrRepository) DueAcquisitions(
	ctx context.Context,
	now time.Time,
	limit int,
) ([]RadarrAcquisition, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := r.pool.Read.QueryContext(ctx, radarrAcquisitionSelect+`
		WHERE revealed_at IS NOT NULL
		  AND target_locked_at IS NOT NULL
		  AND status NOT IN ('downloaded', 'abandoned')
		  AND (next_check_at IS NULL OR next_check_at <= ?)
		ORDER BY COALESCE(next_check_at, updated_at), id
		LIMIT ?
	`, now.UnixMilli(), limit)
	if err != nil {
		return nil, fmt.Errorf("list due Radarr acquisitions: %w", err)
	}
	defer rows.Close()

	acquisitions := make([]RadarrAcquisition, 0)
	for rows.Next() {
		acquisition, scanErr := scanRadarrAcquisition(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		acquisitions = append(acquisitions, acquisition)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list due Radarr acquisitions: %w", err)
	}
	return acquisitions, nil
}

func (r *SqliteRadarrRepository) SelectAcquisitionPreset(
	ctx context.Context,
	acquisitionID, presetID int64,
	actorID int,
	at time.Time,
) (RadarrAcquisition, error) {
	tx, err := r.pool.Write.BeginTx(ctx, nil)
	if err != nil {
		return RadarrAcquisition{}, fmt.Errorf("select Radarr acquisition preset: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var lockedAt sql.NullInt64
	var mutationState, status string
	var revealedAt sql.NullInt64
	if err := tx.QueryRowContext(ctx, `
		SELECT target_locked_at, mutation_state, status, revealed_at
		FROM radarr_acquisitions WHERE id = ?
	`, acquisitionID).Scan(&lockedAt, &mutationState, &status, &revealedAt); err != nil {
		return RadarrAcquisition{}, mapNoRows(err)
	}
	if !revealedAt.Valid {
		return RadarrAcquisition{}, domain.ErrNotFound
	}
	if lockedAt.Valid || mutationState != "idle" || status == "downloaded" || status == "abandoned" {
		return RadarrAcquisition{}, domain.ErrConflict
	}

	preset, err := readRadarrPreset(ctx, tx, presetID)
	if err != nil {
		return RadarrAcquisition{}, err
	}
	if preset.ArchivedAt != nil || !preset.Valid {
		return RadarrAcquisition{}, domain.ErrConflict
	}
	instance, err := readRadarrInstance(ctx, tx, preset.InstanceID)
	if err != nil {
		return RadarrAcquisition{}, err
	}
	if instance.ArchivedAt != nil {
		return RadarrAcquisition{}, domain.ErrConflict
	}
	tags, err := json.Marshal(preset.Tags)
	if err != nil {
		return RadarrAcquisition{}, fmt.Errorf("encode Radarr target tags: %w", err)
	}
	result, err := tx.ExecContext(ctx, `
		UPDATE radarr_acquisitions
		SET preset_id = ?, preset_name = ?, target_instance_id = ?,
		    target_instance_name = ?, target_root_folder_id = ?,
		    target_root_folder_path = ?, target_quality_profile_id = ?,
		    target_quality_profile_name = ?, target_tags = ?,
		    target_minimum_availability = ?, target_acquisition_mode = ?,
		    target_selected_at = ?, target_selected_by = ?, status = 'waiting_for_radarr',
		    action_reason = NULL, action_started_at = NULL,
		    target_preview_existing = 0, target_previewed_at = NULL,
		    effective_configuration = '{}',
		    revision = revision + 1, updated_at = ?
		WHERE id = ? AND target_locked_at IS NULL AND mutation_state = 'idle'
	`, preset.ID, preset.Name, instance.ID, instance.Name, preset.RootFolderID,
		preset.RootFolderPath, preset.QualityProfileID, preset.QualityProfileName,
		string(tags), preset.MinimumAvailability, preset.AcquisitionMode,
		at.UnixMilli(), actorID, at.UnixMilli(), acquisitionID)
	if err != nil {
		return RadarrAcquisition{}, fmt.Errorf("select Radarr acquisition preset: %w", err)
	}
	if err := requireFoundUpdate(result); err != nil {
		return RadarrAcquisition{}, err
	}
	if err := tx.Commit(); err != nil {
		return RadarrAcquisition{}, fmt.Errorf("select Radarr acquisition preset: %w", err)
	}
	return readRadarrAcquisition(ctx, r.pool.Write, acquisitionID, true)
}

func (r *SqliteRadarrRepository) SetAcquisitionIdentityOverride(
	ctx context.Context,
	id, expectedRevision int64,
	tmdbID int,
	at time.Time,
) (RadarrAcquisition, error) {
	result, err := r.pool.Write.ExecContext(ctx, `
		UPDATE radarr_acquisitions
		SET identity_override_tmdb_id = ?, identity_source = 'override',
		    status = 'waiting_for_radarr', action_reason = NULL,
		    action_started_at = NULL, target_preview_existing = 0,
		    target_previewed_at = NULL, effective_configuration = '{}',
		    revision = revision + 1, updated_at = ?
		WHERE id = ? AND revealed_at IS NOT NULL AND target_locked_at IS NULL
		  AND mutation_state = 'idle' AND status NOT IN ('downloaded', 'abandoned')
		  AND tmdb_id IS NULL AND revision = ?
	`, tmdbID, at.UnixMilli(), id, expectedRevision)
	if err != nil {
		return RadarrAcquisition{}, fmt.Errorf("set Radarr acquisition identity: %w", err)
	}
	if err := requireRevisionUpdate(result); err != nil {
		return RadarrAcquisition{}, err
	}
	return readRadarrAcquisition(ctx, r.pool.Write, id, true)
}

func (r *SqliteRadarrRepository) RecordAcquisitionTargetPreview(
	ctx context.Context,
	id, expectedRevision int64,
	existing bool,
	effective RadarrEffectiveConfiguration,
	at time.Time,
) (RadarrAcquisition, error) {
	encoded, err := json.Marshal(effective)
	if err != nil {
		return RadarrAcquisition{}, fmt.Errorf("encode Radarr target preview: %w", err)
	}
	result, err := r.pool.Write.ExecContext(ctx, `
		UPDATE radarr_acquisitions
		SET target_preview_existing = ?, target_previewed_at = ?,
		    effective_configuration = ?, status = 'waiting_for_radarr',
		    action_reason = NULL, action_started_at = NULL,
		    revision = revision + 1, updated_at = ?
		WHERE id = ? AND revealed_at IS NOT NULL AND target_locked_at IS NULL
		  AND mutation_state = 'idle' AND status NOT IN ('downloaded', 'abandoned')
		  AND revision = ?
	`, boolInt(existing), at.UnixMilli(), string(encoded), at.UnixMilli(), id, expectedRevision)
	if err != nil {
		return RadarrAcquisition{}, fmt.Errorf("record Radarr target preview: %w", err)
	}
	if err := requireRevisionUpdate(result); err != nil {
		return RadarrAcquisition{}, err
	}
	return readRadarrAcquisition(ctx, r.pool.Write, id, true)
}

func (r *SqliteRadarrRepository) ResolveIMDbIdentity(
	ctx context.Context,
	id, expectedRevision int64,
	tmdbID int,
	at time.Time,
) (RadarrAcquisition, error) {
	result, err := r.pool.Write.ExecContext(ctx, `
		UPDATE radarr_acquisitions
		SET identity_override_tmdb_id = ?, identity_source = 'imdb',
		    revision = revision + 1, updated_at = ?
		WHERE id = ? AND tmdb_id IS NULL AND imdb_id IS NOT NULL
		  AND target_locked_at IS NULL AND status NOT IN ('downloaded', 'abandoned')
		  AND revision = ?
	`, tmdbID, at.UnixMilli(), id, expectedRevision)
	if err != nil {
		return RadarrAcquisition{}, fmt.Errorf("resolve Radarr acquisition IMDb identity: %w", err)
	}
	if err := requireRevisionUpdate(result); err != nil {
		return RadarrAcquisition{}, err
	}
	return readRadarrAcquisition(ctx, r.pool.Write, id, true)
}

func (r *SqliteRadarrRepository) BeginAcquisitionMutation(
	ctx context.Context,
	id, expectedRevision int64,
	state string,
	at time.Time,
) (RadarrAcquisition, error) {
	result, err := r.pool.Write.ExecContext(ctx, `
		UPDATE radarr_acquisitions
		SET mutation_state = ?, status = 'waiting_for_radarr', action_reason = NULL,
		    action_started_at = NULL, next_check_at = ?,
		    revision = revision + 1, updated_at = ?
		WHERE id = ? AND revealed_at IS NOT NULL AND target_instance_id IS NOT NULL
		  AND target_locked_at IS NULL AND mutation_state = 'idle'
		  AND status NOT IN ('downloaded', 'abandoned') AND revision = ?
	`, state, at.Add(radarrAcquisitionHandoffLease).UnixMilli(), at.UnixMilli(), id, expectedRevision)
	if err != nil {
		return RadarrAcquisition{}, fmt.Errorf("begin Radarr acquisition mutation: %w", err)
	}
	if err := requireRevisionUpdate(result); err != nil {
		return RadarrAcquisition{}, err
	}
	return readRadarrAcquisition(ctx, r.pool.Write, id, true)
}

func (r *SqliteRadarrRepository) BeginLockedReplacementCheck(
	ctx context.Context,
	id, expectedRevision int64,
	at time.Time,
) (RadarrAcquisition, error) {
	result, err := r.pool.Write.ExecContext(ctx, `
		UPDATE radarr_acquisitions
		SET mutation_state = 'checking_replacement', status = 'waiting_for_radarr',
		    action_reason = NULL, action_started_at = NULL,
		    next_check_at = ?, revision = revision + 1, updated_at = ?
		WHERE id = ? AND revealed_at IS NOT NULL AND target_locked_at IS NOT NULL
		  AND mutation_state = 'idle' AND status NOT IN ('downloaded', 'abandoned')
		  AND revision = ?
	`, at.Add(radarrAcquisitionHandoffLease).UnixMilli(), at.UnixMilli(), id, expectedRevision)
	if err != nil {
		return RadarrAcquisition{}, fmt.Errorf("begin locked Radarr replacement check: %w", err)
	}
	if err := requireRevisionUpdate(result); err != nil {
		return RadarrAcquisition{}, err
	}
	return readRadarrAcquisition(ctx, r.pool.Write, id, true)
}

func (r *SqliteRadarrRepository) BeginLockedRecreation(
	ctx context.Context,
	id, expectedRevision int64,
	at time.Time,
) (RadarrAcquisition, error) {
	result, err := r.pool.Write.ExecContext(ctx, `
		UPDATE radarr_acquisitions
		SET mutation_state = 'recreating', status = 'waiting_for_radarr',
		    action_reason = NULL, action_started_at = NULL,
		    next_check_at = ?, revision = revision + 1, updated_at = ?
		WHERE id = ? AND revealed_at IS NOT NULL AND target_locked_at IS NOT NULL
		  AND mutation_state = 'checking_replacement'
		  AND status NOT IN ('downloaded', 'abandoned') AND revision = ?
	`, at.Add(radarrAcquisitionHandoffLease).UnixMilli(), at.UnixMilli(), id, expectedRevision)
	if err != nil {
		return RadarrAcquisition{}, fmt.Errorf("begin locked Radarr recreation: %w", err)
	}
	if err := requireRevisionUpdate(result); err != nil {
		return RadarrAcquisition{}, err
	}
	return readRadarrAcquisition(ctx, r.pool.Write, id, true)
}

func (r *SqliteRadarrRepository) ReclaimLockedReplacement(
	ctx context.Context,
	id, expectedRevision int64,
	state string,
	at time.Time,
) (RadarrAcquisition, error) {
	if state != "checking_replacement" && state != "recreating" {
		return RadarrAcquisition{}, domain.ErrConflict
	}
	result, err := r.pool.Write.ExecContext(ctx, `
		UPDATE radarr_acquisitions
		SET status = 'waiting_for_radarr', action_reason = NULL,
		    action_started_at = NULL, next_check_at = ?,
		    revision = revision + 1, updated_at = ?
		WHERE id = ? AND revealed_at IS NOT NULL AND target_locked_at IS NOT NULL
		  AND mutation_state = ? AND status NOT IN ('downloaded', 'abandoned')
		  AND (next_check_at IS NULL OR next_check_at <= ?) AND revision = ?
	`, at.Add(radarrAcquisitionHandoffLease).UnixMilli(), at.UnixMilli(), id,
		state, at.UnixMilli(), expectedRevision)
	if err != nil {
		return RadarrAcquisition{}, fmt.Errorf("reclaim locked Radarr replacement: %w", err)
	}
	if err := requireRevisionUpdate(result); err != nil {
		return RadarrAcquisition{}, err
	}
	return readRadarrAcquisition(ctx, r.pool.Write, id, true)
}

// RenewLockedRecreationLease is the final local fence before an AddMovie call.
// Catalog and lookup requests can outlive an earlier claim. The revision check
// prevents that stale worker from sending after another worker reclaimed it.
func (r *SqliteRadarrRepository) RenewLockedRecreationLease(
	ctx context.Context,
	id, expectedRevision int64,
	at time.Time,
) (RadarrAcquisition, error) {
	result, err := r.pool.Write.ExecContext(ctx, `
		UPDATE radarr_acquisitions
		SET status = 'waiting_for_radarr', action_reason = NULL,
		    action_started_at = NULL, next_check_at = ?,
		    revision = revision + 1, updated_at = ?
		WHERE id = ? AND revealed_at IS NOT NULL AND target_locked_at IS NOT NULL
		  AND mutation_state = 'recreating'
		  AND status NOT IN ('downloaded', 'abandoned') AND revision = ?
	`, at.Add(radarrAcquisitionHandoffLease).UnixMilli(), at.UnixMilli(), id, expectedRevision)
	if err != nil {
		return RadarrAcquisition{}, fmt.Errorf("renew locked Radarr recreation lease: %w", err)
	}
	if err := requireRevisionUpdate(result); err != nil {
		return RadarrAcquisition{}, err
	}
	return readRadarrAcquisition(ctx, r.pool.Write, id, true)
}

func (r *SqliteRadarrRepository) ReplaceLockedRadarrMovie(
	ctx context.Context,
	id, expectedRevision int64,
	radarrMovieID int,
	existing bool,
	effective RadarrEffectiveConfiguration,
	actorID int,
	at time.Time,
) error {
	encodedEffective, err := json.Marshal(effective)
	if err != nil {
		return fmt.Errorf("encode replacement Radarr configuration: %w", err)
	}
	expectedState := "recreating"
	if existing {
		expectedState = "checking_replacement"
	}
	result, err := r.pool.Write.ExecContext(ctx, `
		UPDATE radarr_acquisitions
		SET radarr_movie_id = ?, adopted_existing = ?, effective_configuration = ?,
		    mutation_state = 'idle', automatic_search_claimed_at = NULL,
		    automatic_search_command_id = NULL, automatic_search_completed_at = NULL,
		    target_locked_by = ?, status = 'waiting_for_radarr',
		    action_reason = NULL, action_started_at = NULL, next_check_at = ?,
		    revision = revision + 1, updated_at = ?
		WHERE id = ? AND target_locked_at IS NOT NULL
		  AND mutation_state = ? AND status NOT IN ('downloaded', 'abandoned')
		  AND revision = ?
	`, radarrMovieID, boolInt(existing), string(encodedEffective), actorID,
		at.Add(radarrAcquisitionHandoffLease).UnixMilli(), at.UnixMilli(),
		id, expectedState, expectedRevision)
	if err != nil {
		return fmt.Errorf("replace locked Radarr movie: %w", err)
	}
	return requireRevisionUpdate(result)
}

func (r *SqliteRadarrRepository) LockAcquisitionTarget(
	ctx context.Context,
	id int64,
	lock RadarrTargetLock,
) (RadarrAcquisition, error) {
	effective, err := json.Marshal(lock.EffectiveConfiguration)
	if err != nil {
		return RadarrAcquisition{}, fmt.Errorf("encode effective Radarr configuration: %w", err)
	}
	tx, err := r.pool.Write.BeginTx(ctx, nil)
	if err != nil {
		return RadarrAcquisition{}, fmt.Errorf("lock Radarr acquisition target: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	oldReason, oldStatus, oldActionStartedAt, actionVersion, revealed, err := acquisitionActionState(ctx, tx, id)
	if err != nil {
		return RadarrAcquisition{}, err
	}
	actionVersion, actionStartedAt, enqueue := nextActionVersion(
		oldStatus, oldReason, oldActionStartedAt, lock.Status, lock.ActionReason, actionVersion, lock.At,
	)
	var downloadedAt any
	if lock.Status == "downloaded" {
		downloadedAt = lock.At.UnixMilli()
	}
	result, err := tx.ExecContext(ctx, `
		UPDATE radarr_acquisitions
		SET target_locked_at = ?, target_locked_by = ?, radarr_movie_id = ?,
		    adopted_existing = ?, effective_configuration = ?, mutation_state = 'idle',
		    automatic_search_command_id = ?, status = ?, action_reason = NULLIF(?, ''),
		    action_version = ?, action_started_at = ?, downloaded_at = ?, next_check_at = ?,
		    revision = revision + 1, updated_at = ?
		WHERE id = ? AND revealed_at IS NOT NULL AND target_instance_id IS NOT NULL
		  AND target_locked_at IS NULL AND mutation_state IN ('adding', 'recreating')
		  AND status NOT IN ('downloaded', 'abandoned')
	`, lock.At.UnixMilli(), lock.LockedBy, lock.RadarrMovieID, boolInt(lock.Existing),
		string(effective), lock.AutomaticCommandID, lock.Status, lock.ActionReason,
		actionVersion, actionStartedAt, downloadedAt,
		lock.At.Add(radarrAcquisitionHandoffLease).UnixMilli(), lock.At.UnixMilli(), id)
	if err != nil {
		return RadarrAcquisition{}, fmt.Errorf("lock Radarr acquisition target: %w", err)
	}
	if err := requireFoundUpdate(result); err != nil {
		return RadarrAcquisition{}, err
	}
	if enqueue && revealed {
		if err := enqueueAcquisitionAction(ctx, tx, id, actionVersion, lock.ActionReason, lock.At); err != nil {
			return RadarrAcquisition{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return RadarrAcquisition{}, fmt.Errorf("lock Radarr acquisition target: %w", err)
	}
	return readRadarrAcquisition(ctx, r.pool.Write, id, true)
}

func (r *SqliteRadarrRepository) ResetAcquisitionMutation(
	ctx context.Context,
	id int64,
	transition RadarrAcquisitionTransition,
) (RadarrAcquisition, error) {
	return r.transitionAcquisition(ctx, id, nil, transition, true)
}

func (r *SqliteRadarrRepository) ResetAcquisitionMutationAtRevision(
	ctx context.Context,
	id, expectedRevision int64,
	transition RadarrAcquisitionTransition,
) (RadarrAcquisition, error) {
	return r.transitionAcquisition(ctx, id, &expectedRevision, transition, true)
}

func (r *SqliteRadarrRepository) TransitionAcquisition(
	ctx context.Context,
	id int64,
	transition RadarrAcquisitionTransition,
) (RadarrAcquisition, error) {
	return r.transitionAcquisition(ctx, id, nil, transition, false)
}

func (r *SqliteRadarrRepository) TransitionAcquisitionAtRevision(
	ctx context.Context,
	id, expectedRevision int64,
	transition RadarrAcquisitionTransition,
) (RadarrAcquisition, error) {
	return r.transitionAcquisition(ctx, id, &expectedRevision, transition, false)
}

func (r *SqliteRadarrRepository) transitionAcquisition(
	ctx context.Context,
	id int64,
	expectedRevision *int64,
	transition RadarrAcquisitionTransition,
	resetMutation bool,
) (RadarrAcquisition, error) {
	if transition.At.IsZero() {
		transition.At = time.Now().UTC()
	}
	if transition.QueueStatus == "" {
		transition.QueueStatus = "none"
	}
	tx, err := r.pool.Write.BeginTx(ctx, nil)
	if err != nil {
		return RadarrAcquisition{}, fmt.Errorf("transition Radarr acquisition: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	oldReason, oldStatus, oldActionStartedAt, actionVersion, revealed, err := acquisitionActionState(ctx, tx, id)
	if err != nil {
		return RadarrAcquisition{}, err
	}
	if oldStatus == "downloaded" || oldStatus == "abandoned" {
		return RadarrAcquisition{}, domain.ErrConflict
	}
	actionVersion, actionStartedAt, enqueue := nextActionVersion(
		oldStatus, oldReason, oldActionStartedAt, transition.Status, transition.ActionReason,
		actionVersion, transition.At,
	)
	var latestFailureAt any
	if transition.FailureSummary != "" {
		latestFailureAt = transition.At.UnixMilli()
	}
	var nextCheckAt any
	if transition.NextCheckAt != nil {
		nextCheckAt = transition.NextCheckAt.UnixMilli()
	}
	mutationState := "mutation_state"
	automaticSearchClaimedAt := "automatic_search_claimed_at"
	if resetMutation {
		mutationState = "'idle'"
		automaticSearchClaimedAt = "NULL"
	}
	query := `
		UPDATE radarr_acquisitions
		SET status = ?, action_reason = NULLIF(?, ''), action_version = ?,
		    action_started_at = ?, latest_failure_summary = CASE
		        WHEN ? = '' THEN latest_failure_summary ELSE ? END,
		    latest_failure_at = COALESCE(?, latest_failure_at), queue_status = ?,
		    queue_summary = ?, last_checked_at = ?, next_check_at = ?,
		    queued_at = CASE WHEN ? = 'queued' THEN COALESCE(queued_at, ?) ELSE queued_at END,
		    downloading_at = CASE WHEN ? = 'downloading' THEN COALESCE(downloading_at, ?) ELSE downloading_at END,
		    importing_at = CASE WHEN ? = 'importing' THEN COALESCE(importing_at, ?) ELSE importing_at END,
		    downloaded_at = CASE WHEN ? = 'downloaded' THEN COALESCE(downloaded_at, ?) ELSE downloaded_at END,
		    mutation_state = ` + mutationState + `,
		    automatic_search_claimed_at = ` + automaticSearchClaimedAt + `,
		    revision = revision + 1, updated_at = ?
		WHERE id = ? AND status NOT IN ('downloaded', 'abandoned')
	`
	args := []any{
		transition.Status, transition.ActionReason, actionVersion, actionStartedAt,
		transition.FailureSummary, transition.FailureSummary, latestFailureAt,
		transition.QueueStatus, transition.QueueSummary, transition.At.UnixMilli(), nextCheckAt,
		transition.Status, transition.At.UnixMilli(), transition.Status, transition.At.UnixMilli(),
		transition.Status, transition.At.UnixMilli(), transition.Status, transition.At.UnixMilli(),
		transition.At.UnixMilli(), id,
	}
	if expectedRevision != nil {
		query += " AND revision = ?"
		args = append(args, *expectedRevision)
	}
	result, err := tx.ExecContext(ctx, query, args...)
	if err != nil {
		return RadarrAcquisition{}, fmt.Errorf("transition Radarr acquisition: %w", err)
	}
	if expectedRevision != nil {
		if err := requireRevisionUpdate(result); err != nil {
			return RadarrAcquisition{}, err
		}
	} else if err := requireFoundUpdate(result); err != nil {
		return RadarrAcquisition{}, err
	}
	if enqueue && revealed {
		if err := enqueueAcquisitionAction(ctx, tx, id, actionVersion, transition.ActionReason, transition.At); err != nil {
			return RadarrAcquisition{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return RadarrAcquisition{}, fmt.Errorf("transition Radarr acquisition: %w", err)
	}
	return readRadarrAcquisition(ctx, r.pool.Write, id, true)
}

func (r *SqliteRadarrRepository) BeginAutomaticSearchAttempt(
	ctx context.Context,
	id, expectedRevision int64,
	at time.Time,
) (RadarrAcquisition, error) {
	result, err := r.pool.Write.ExecContext(ctx, `
		UPDATE radarr_acquisitions
		SET mutation_state = 'searching', automatic_search_claimed_at = ?,
		    automatic_search_command_id = NULL,
		    automatic_search_completed_at = NULL, status = 'waiting_for_radarr',
		    action_reason = NULL, action_started_at = NULL, queue_status = 'none',
		    queue_summary = '', next_check_at = ?, revision = revision + 1, updated_at = ?
		WHERE id = ? AND target_locked_at IS NOT NULL AND radarr_movie_id IS NOT NULL
		  AND target_acquisition_mode = 'automatic' AND mutation_state = 'idle'
		  AND status NOT IN ('downloaded', 'abandoned') AND revision = ?
	`, at.UnixMilli(), at.Add(radarrAcquisitionHandoffLease).UnixMilli(), at.UnixMilli(), id, expectedRevision)
	if err != nil {
		return RadarrAcquisition{}, fmt.Errorf("begin Radarr automatic search: %w", err)
	}
	if err := requireRevisionUpdate(result); err != nil {
		return RadarrAcquisition{}, err
	}
	return readRadarrAcquisition(ctx, r.pool.Write, id, true)
}

func (r *SqliteRadarrRepository) RecordAutomaticSearchCommand(
	ctx context.Context,
	id, expectedRevision int64,
	commandID int,
	at time.Time,
) (RadarrAcquisition, error) {
	result, err := r.pool.Write.ExecContext(ctx, `
		UPDATE radarr_acquisitions
		SET automatic_search_command_id = ?, automatic_search_completed_at = NULL,
		    mutation_state = 'idle', status = 'waiting_for_radarr', action_reason = NULL,
		    action_started_at = NULL, queue_status = 'none', queue_summary = '',
		    next_check_at = ?, revision = revision + 1, updated_at = ?
		WHERE id = ? AND target_locked_at IS NOT NULL AND mutation_state = 'searching'
		  AND status NOT IN ('downloaded', 'abandoned') AND revision = ?
	`, commandID, at.Add(radarrAcquisitionHandoffLease).UnixMilli(), at.UnixMilli(), id, expectedRevision)
	if err != nil {
		return RadarrAcquisition{}, fmt.Errorf("record Radarr automatic search command: %w", err)
	}
	if err := requireRevisionUpdate(result); err != nil {
		return RadarrAcquisition{}, err
	}
	return readRadarrAcquisition(ctx, r.pool.Write, id, true)
}

func (r *SqliteRadarrRepository) CompleteAutomaticSearch(
	ctx context.Context,
	id, expectedRevision int64,
	expectedCommandID int,
	at time.Time,
) error {
	result, err := r.pool.Write.ExecContext(ctx, `
		UPDATE radarr_acquisitions
		SET automatic_search_completed_at = ?, revision = revision + 1, updated_at = ?
		WHERE id = ? AND automatic_search_command_id IS NOT NULL
		  AND automatic_search_command_id = ? AND revision = ?
		  AND status NOT IN ('downloaded', 'abandoned')
	`, at.UnixMilli(), at.UnixMilli(), id, expectedCommandID, expectedRevision)
	if err != nil {
		return fmt.Errorf("complete Radarr automatic search: %w", err)
	}
	return requireRevisionUpdate(result)
}

func (r *SqliteRadarrRepository) BeginReleaseAttempt(
	ctx context.Context,
	id, expectedRevision int64,
	title, quality string,
	actorID int,
	at time.Time,
) (RadarrAcquisition, error) {
	result, err := r.pool.Write.ExecContext(ctx, `
		UPDATE radarr_acquisitions
		SET latest_release_title = ?, latest_release_quality = ?,
		    latest_release_selected_at = ?, latest_release_selected_by = ?,
		    manual_attempt_count = manual_attempt_count + 1,
		    status = 'waiting_for_radarr', action_reason = NULL,
		    action_started_at = NULL, queue_status = 'none', queue_summary = '',
		    mutation_state = 'grabbing', next_check_at = ?,
		    revision = revision + 1, updated_at = ?
		WHERE id = ? AND revealed_at IS NOT NULL AND target_locked_at IS NOT NULL
		  AND mutation_state = 'idle' AND status NOT IN ('downloaded', 'abandoned')
		  AND revision = ?
	`, title, quality, at.UnixMilli(), actorID,
		at.Add(radarrAcquisitionHandoffLease).UnixMilli(), at.UnixMilli(), id, expectedRevision)
	if err != nil {
		return RadarrAcquisition{}, fmt.Errorf("begin Radarr release attempt: %w", err)
	}
	if err := requireRevisionUpdate(result); err != nil {
		return RadarrAcquisition{}, err
	}
	return readRadarrAcquisition(ctx, r.pool.Write, id, true)
}

func (r *SqliteRadarrRepository) AbandonAcquisition(
	ctx context.Context,
	id, expectedRevision int64,
	actorID int,
	reason string,
	at time.Time,
) (RadarrAcquisition, error) {
	result, err := r.pool.Write.ExecContext(ctx, `
		UPDATE radarr_acquisitions
		SET status = 'abandoned', action_reason = NULL, action_started_at = NULL,
		    abandoned_at = ?, abandoned_by = ?, abandonment_reason = ?,
		    mutation_state = 'idle', automatic_search_claimed_at = NULL,
		    next_check_at = NULL, revision = revision + 1, updated_at = ?
		WHERE id = ? AND revealed_at IS NOT NULL
		  AND status NOT IN ('downloaded', 'abandoned') AND revision = ?
	`, at.UnixMilli(), actorID, reason, at.UnixMilli(), id, expectedRevision)
	if err != nil {
		return RadarrAcquisition{}, fmt.Errorf("abandon Radarr acquisition: %w", err)
	}
	if err := requireRevisionUpdate(result); err != nil {
		return RadarrAcquisition{}, err
	}
	return readRadarrAcquisition(ctx, r.pool.Write, id, true)
}

type sqlQueryRower interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func readRadarrAcquisition(
	ctx context.Context,
	q sqlQueryRower,
	id int64,
	visibleOnly bool,
) (RadarrAcquisition, error) {
	where := " WHERE id = ?"
	if visibleOnly {
		where += " AND revealed_at IS NOT NULL"
	}
	acquisition, err := scanRadarrAcquisition(q.QueryRowContext(ctx, radarrAcquisitionSelect+where, id))
	if errors.Is(err, sql.ErrNoRows) {
		return RadarrAcquisition{}, domain.ErrNotFound
	}
	return acquisition, err
}

func scanRadarrAcquisition(row rowScanner) (RadarrAcquisition, error) {
	var acquisition RadarrAcquisition
	var actionReason, imdbID, identitySource sql.NullString
	var movieYear, tmdbID, overrideTMDBID, presetID, targetInstanceID sql.NullInt64
	var targetRootID, targetQualityID, selectedBy, lockedBy, radarrMovieID sql.NullInt64
	var automaticCommandID, releaseSelectedBy, abandonedBy sql.NullInt64
	var actionStartedAt, revealedAt, targetSelectedAt, targetLockedAt sql.NullInt64
	var previewedAt, automaticClaimedAt, automaticCompletedAt, releaseSelectedAt, failureAt sql.NullInt64
	var lastCheckedAt, nextCheckAt, queuedAt, downloadingAt, importingAt sql.NullInt64
	var downloadedAt, abandonedAt sql.NullInt64
	var presetName, targetInstanceName, targetRootPath, targetQualityName sql.NullString
	var targetAvailability, targetMode, releaseTitle, releaseQuality sql.NullString
	var failureSummary, abandonmentReason sql.NullString
	var targetTags, effectiveConfiguration string
	var adoptedExisting, previewExisting int
	var drawnAt, revealAt, createdAt, updatedAt int64
	if err := row.Scan(
		&acquisition.ID, &acquisition.MovieID, &acquisition.Revision,
		&acquisition.Status, &actionReason, &acquisition.ActionVersion,
		&actionStartedAt, &acquisition.MovieTitle, &movieYear, &tmdbID, &imdbID,
		&identitySource, &overrideTMDBID, &drawnAt, &revealAt,
		&acquisition.DrawClientID, &revealedAt, &presetID, &presetName,
		&targetInstanceID, &targetInstanceName, &targetRootID, &targetRootPath,
		&targetQualityID, &targetQualityName, &targetTags, &targetAvailability,
		&targetMode, &targetSelectedAt, &selectedBy, &targetLockedAt, &lockedBy,
		&radarrMovieID, &adoptedExisting, &effectiveConfiguration, &previewExisting,
		&previewedAt, &acquisition.MutationState, &automaticClaimedAt,
		&automaticCommandID, &automaticCompletedAt,
		&releaseTitle, &releaseQuality, &releaseSelectedAt, &releaseSelectedBy,
		&acquisition.ManualAttemptCount, &failureSummary, &failureAt,
		&acquisition.QueueStatus, &acquisition.QueueSummary, &lastCheckedAt,
		&nextCheckAt, &queuedAt, &downloadingAt, &importingAt, &downloadedAt,
		&abandonedAt, &abandonedBy, &abandonmentReason, &createdAt, &updatedAt,
	); err != nil {
		return RadarrAcquisition{}, fmt.Errorf("scan Radarr acquisition: %w", err)
	}
	acquisition.ActionReason = actionReason.String
	if movieYear.Valid {
		acquisition.MovieYear = int(movieYear.Int64)
	}
	acquisition.IMDbID = imdbID.String
	acquisition.IdentitySource = identitySource.String
	acquisition.TMDBID = nullIntPtr(tmdbID)
	acquisition.IdentityOverrideTMDBID = nullIntPtr(overrideTMDBID)
	acquisition.PresetID = nullInt64Ptr(presetID)
	acquisition.PresetName = presetName.String
	acquisition.TargetInstanceID = nullInt64Ptr(targetInstanceID)
	acquisition.TargetInstanceName = targetInstanceName.String
	acquisition.TargetRootFolderID = nullIntPtr(targetRootID)
	acquisition.TargetRootFolderPath = targetRootPath.String
	acquisition.TargetQualityProfileID = nullIntPtr(targetQualityID)
	acquisition.TargetQualityProfileName = targetQualityName.String
	acquisition.TargetMinimumAvailability = targetAvailability.String
	acquisition.TargetAcquisitionMode = targetMode.String
	acquisition.TargetSelectedBy = nullIntPtr(selectedBy)
	acquisition.TargetLockedBy = nullIntPtr(lockedBy)
	acquisition.RadarrMovieID = nullIntPtr(radarrMovieID)
	acquisition.AutomaticSearchCommandID = nullIntPtr(automaticCommandID)
	acquisition.LatestReleaseSelectedBy = nullIntPtr(releaseSelectedBy)
	acquisition.AbandonedBy = nullIntPtr(abandonedBy)
	acquisition.AdoptedExisting = adoptedExisting == 1
	acquisition.TargetPreviewExisting = previewExisting == 1
	acquisition.LatestReleaseTitle = releaseTitle.String
	acquisition.LatestReleaseQuality = releaseQuality.String
	acquisition.LatestFailureSummary = failureSummary.String
	acquisition.AbandonmentReason = abandonmentReason.String
	acquisition.DrawnAt = time.UnixMilli(drawnAt).UTC()
	acquisition.RevealAt = time.UnixMilli(revealAt).UTC()
	acquisition.CreatedAt = time.UnixMilli(createdAt).UTC()
	acquisition.UpdatedAt = time.UnixMilli(updatedAt).UTC()
	acquisition.ActionStartedAt = milliTimePtr(actionStartedAt)
	acquisition.RevealedAt = milliTimePtr(revealedAt)
	acquisition.TargetSelectedAt = milliTimePtr(targetSelectedAt)
	acquisition.TargetLockedAt = milliTimePtr(targetLockedAt)
	acquisition.TargetPreviewedAt = milliTimePtr(previewedAt)
	acquisition.AutomaticSearchClaimedAt = milliTimePtr(automaticClaimedAt)
	acquisition.AutomaticSearchCompletedAt = milliTimePtr(automaticCompletedAt)
	acquisition.LatestReleaseSelectedAt = milliTimePtr(releaseSelectedAt)
	acquisition.LatestFailureAt = milliTimePtr(failureAt)
	acquisition.LastCheckedAt = milliTimePtr(lastCheckedAt)
	acquisition.NextCheckAt = milliTimePtr(nextCheckAt)
	acquisition.QueuedAt = milliTimePtr(queuedAt)
	acquisition.DownloadingAt = milliTimePtr(downloadingAt)
	acquisition.ImportingAt = milliTimePtr(importingAt)
	acquisition.DownloadedAt = milliTimePtr(downloadedAt)
	acquisition.AbandonedAt = milliTimePtr(abandonedAt)
	if err := json.Unmarshal([]byte(targetTags), &acquisition.TargetTags); err != nil {
		return RadarrAcquisition{}, fmt.Errorf("decode Radarr target tags: %w", err)
	}
	if err := json.Unmarshal([]byte(effectiveConfiguration), &acquisition.EffectiveConfiguration); err != nil {
		return RadarrAcquisition{}, fmt.Errorf("decode effective Radarr configuration: %w", err)
	}
	return acquisition, nil
}

type acquisitionTx interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

func acquisitionActionState(
	ctx context.Context,
	tx acquisitionTx,
	id int64,
) (reason, status string, actionStartedAt sql.NullInt64, version int64, revealed bool, err error) {
	var nullableReason sql.NullString
	var revealedAt sql.NullInt64
	err = tx.QueryRowContext(ctx, `
		SELECT action_reason, status, action_started_at, action_version, revealed_at
		FROM radarr_acquisitions WHERE id = ?
	`, id).Scan(&nullableReason, &status, &actionStartedAt, &version, &revealedAt)
	if errors.Is(err, sql.ErrNoRows) {
		err = domain.ErrNotFound
	}
	return nullableReason.String, status, actionStartedAt, version, revealedAt.Valid, err
}

func nextActionVersion(
	oldStatus, oldReason string,
	oldActionStartedAt sql.NullInt64,
	newStatus, newReason string,
	version int64,
	at time.Time,
) (int64, any, bool) {
	if newReason == "" {
		return version, nil, false
	}
	if oldStatus == newStatus && oldReason == newReason {
		if oldActionStartedAt.Valid {
			return version, oldActionStartedAt.Int64, false
		}
		return version, at.UnixMilli(), false
	}
	return version + 1, at.UnixMilli(), true
}

func enqueueAcquisitionAction(
	ctx context.Context,
	tx acquisitionTx,
	acquisitionID, actionVersion int64,
	reason string,
	at time.Time,
) error {
	_, err := tx.ExecContext(ctx, `
		INSERT INTO radarr_webhook_deliveries (
			destination_id, destination_revision, acquisition_id, reason,
			action_version, target_label, next_attempt_at
		)
		SELECT d.id, d.revision, a.id, ?, ?,
		       COALESCE(NULLIF(a.preset_name, ''), NULLIF(a.target_instance_name, ''), ''), ?
		FROM radarr_webhook_destinations d
		JOIN radarr_acquisitions a ON a.id = ?
		WHERE d.enabled = 1 AND d.verified_at IS NOT NULL AND d.archived_at IS NULL
		  AND a.revealed_at IS NOT NULL
		  AND EXISTS (
		      SELECT 1 FROM json_each(d.reason_filters) WHERE value = ?
		  )
		ON CONFLICT(destination_id, acquisition_id, action_version) DO NOTHING
	`, reason, actionVersion, at.Unix(), acquisitionID, reason)
	if err != nil {
		return fmt.Errorf("enqueue Radarr acquisition webhook: %w", err)
	}
	return nil
}

func mapNoRows(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return domain.ErrNotFound
	}
	return err
}

func nullIntPtr(value sql.NullInt64) *int {
	if !value.Valid {
		return nil
	}
	n := int(value.Int64)
	return &n
}

func nullInt64Ptr(value sql.NullInt64) *int64 {
	if !value.Valid {
		return nil
	}
	n := value.Int64
	return &n
}

func milliTimePtr(value sql.NullInt64) *time.Time {
	if !value.Valid {
		return nil
	}
	t := time.UnixMilli(value.Int64).UTC()
	return &t
}

func escapeLike(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, `%`, `\%`)
	return strings.ReplaceAll(value, `_`, `\_`)
}
