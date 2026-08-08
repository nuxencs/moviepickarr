package repository

import (
	"database/sql"
	"errors"
	"testing"
	"time"

	"moviepickarr/internal/domain"
	"moviepickarr/internal/integration"
)

type radarrRepositoryTestEnv struct {
	base    *userRemoveEnv
	radarr  *SqliteRadarrRepository
	actorID int
	now     time.Time
}

func setupRadarrRepositoryTest(t *testing.T) *radarrRepositoryTestEnv {
	t.Helper()
	base := setupUserRemoveEnv(t)
	actor, err := base.users.Create(base.ctx, "Admin")
	if err != nil {
		t.Fatalf("create actor: %v", err)
	}
	return &radarrRepositoryTestEnv{
		base:    base,
		radarr:  NewSqliteRadarrRepository(base.pool),
		actorID: actor.ID,
		now:     time.Now().UTC().Truncate(time.Second),
	}
}

func (e *radarrRepositoryTestEnv) createInstance(t *testing.T, name string) RadarrInstance {
	t.Helper()
	instance, err := e.radarr.CreateInstance(e.base.ctx, RadarrInstanceSave{
		Name:            name,
		BaseURL:         "http://radarr.test",
		EncryptedAPIKey: []byte("encrypted-key"),
		State:           "connected",
		CheckedAt:       e.now,
	})
	if err != nil {
		t.Fatalf("create Radarr instance: %v", err)
	}
	return instance
}

func (e *radarrRepositoryTestEnv) createPreset(
	t *testing.T,
	instance RadarrInstance,
	name string,
) RadarrPreset {
	t.Helper()
	preset, err := e.radarr.CreatePreset(e.base.ctx, RadarrPresetSave{
		Name:                name,
		InstanceID:          instance.ID,
		RootFolderID:        10,
		RootFolderPath:      "/media/movies",
		QualityProfileID:    20,
		QualityProfileName:  "HD-1080p",
		Tags:                []RadarrTagSnapshot{{ID: 30, Label: "movies"}},
		MinimumAvailability: "released",
		AcquisitionMode:     "manual",
		Valid:               true,
		ValidatedAt:         e.now,
	})
	if err != nil {
		t.Fatalf("create Radarr preset: %v", err)
	}
	return preset
}

func (e *radarrRepositoryTestEnv) createDestination(
	t *testing.T,
	name string,
	reasons []string,
	enabled bool,
) RadarrWebhookDestination {
	t.Helper()
	var verifiedAt *time.Time
	if enabled {
		at := e.now
		verifiedAt = &at
	}
	destination, err := e.radarr.CreateWebhookDestination(e.base.ctx, RadarrWebhookDestinationSave{
		Name:          name,
		Kind:          "generic",
		EncryptedURL:  []byte("encrypted-url-" + name),
		ReasonFilters: reasons,
		Enabled:       enabled,
		VerifiedAt:    verifiedAt,
	})
	if err != nil {
		t.Fatalf("create webhook destination %q: %v", name, err)
	}
	return destination
}

func (e *radarrRepositoryTestEnv) startDraw(t *testing.T) int64 {
	t.Helper()
	movie, err := e.base.movies.Add(e.base.ctx, "Heat", "pool", e.actorID)
	if err != nil {
		t.Fatalf("add pooled movie: %v", err)
	}
	if err := e.base.movies.StartDraw(
		e.base.ctx,
		movie.ID,
		e.now,
		e.now.Add(16_500*time.Millisecond),
		"drawer",
	); err != nil {
		t.Fatalf("start draw: %v", err)
	}
	var acquisitionID int64
	if err := e.base.pool.Read.QueryRowContext(e.base.ctx, `
		SELECT id FROM radarr_acquisitions WHERE movie_id = ?
	`, movie.ID).Scan(&acquisitionID); err != nil {
		t.Fatalf("read acquisition id: %v", err)
	}
	return acquisitionID
}

func (e *radarrRepositoryTestEnv) reveal(t *testing.T, acquisitionID int64) {
	t.Helper()
	acquisition, err := readRadarrAcquisition(e.base.ctx, e.radarr.pool.Read, acquisitionID, false)
	if err != nil {
		t.Fatalf("get concealed acquisition: %v", err)
	}
	if err := e.base.movies.RevealDraw(
		e.base.ctx,
		acquisition.MovieID,
		e.now.Add(17*time.Second),
	); err != nil {
		t.Fatalf("reveal acquisition: %v", err)
	}
}

func (e *radarrRepositoryTestEnv) deliveryCount(t *testing.T) int {
	t.Helper()
	var count int
	if err := e.base.pool.Read.QueryRowContext(e.base.ctx,
		`SELECT COUNT(*) FROM radarr_webhook_deliveries`).Scan(&count); err != nil {
		t.Fatalf("count webhook deliveries: %v", err)
	}
	return count
}

func TestRadarrUnusedInstanceRemovalHardDeletesItsUnusedPresets(t *testing.T) {
	e := setupRadarrRepositoryTest(t)
	instance := e.createInstance(t, "Movies")
	preset := e.createPreset(t, instance, "1080p")

	outcome, err := e.radarr.RemoveInstance(e.base.ctx, instance.ID, e.now.Add(time.Minute))
	if err != nil {
		t.Fatalf("remove unused instance: %v", err)
	}
	if outcome != RadarrOutcomeDeleted {
		t.Fatalf("unused instance outcome = %q, want %q", outcome, RadarrOutcomeDeleted)
	}
	if _, err := e.radarr.GetInstance(e.base.ctx, instance.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("get deleted instance = %v, want not found", err)
	}
	if _, err := e.radarr.GetPreset(e.base.ctx, preset.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("get deleted child preset = %v, want not found", err)
	}
}

func TestRadarrArchivedUnusedInstanceRemovalHardDeletesLegacySetup(t *testing.T) {
	e := setupRadarrRepositoryTest(t)
	instance := e.createInstance(t, "Legacy unused")
	preset := e.createPreset(t, instance, "Legacy preset")
	if err := e.radarr.ArchiveInstance(e.base.ctx, instance.ID, e.now.Add(time.Minute)); err != nil {
		t.Fatalf("archive unused instance: %v", err)
	}

	outcome, err := e.radarr.RemoveInstance(e.base.ctx, instance.ID, e.now.Add(2*time.Minute))
	if err != nil {
		t.Fatalf("remove archived unused instance: %v", err)
	}
	if outcome != RadarrOutcomeDeleted {
		t.Fatalf("archived unused instance outcome = %q, want %q", outcome, RadarrOutcomeDeleted)
	}
	if _, err := e.radarr.GetInstance(e.base.ctx, instance.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("get deleted archived instance = %v, want not found", err)
	}
	if _, err := e.radarr.GetPreset(e.base.ctx, preset.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("get deleted archived child preset = %v, want not found", err)
	}
}

func TestRadarrUsedInstanceRemovalRefusesUnresolvedThenArchivesHistory(t *testing.T) {
	e := setupRadarrRepositoryTest(t)
	instance := e.createInstance(t, "Movies")
	preset := e.createPreset(t, instance, "1080p")
	unusedPreset := e.createPreset(t, instance, "Unused")
	acquisitionID := e.startDraw(t)
	e.reveal(t, acquisitionID)
	if _, err := e.radarr.SelectAcquisitionPreset(
		e.base.ctx,
		acquisitionID,
		preset.ID,
		e.actorID,
		e.now.Add(time.Minute),
	); err != nil {
		t.Fatalf("select preset: %v", err)
	}

	if _, err := e.radarr.RemoveInstance(
		e.base.ctx,
		instance.ID,
		e.now.Add(2*time.Minute),
	); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("archive instance with unresolved target = %v, want conflict", err)
	}
	stored, err := e.radarr.GetInstance(e.base.ctx, instance.ID)
	if err != nil {
		t.Fatalf("get instance: %v", err)
	}
	if stored.ArchivedAt != nil {
		t.Fatalf("refused archive persisted archived_at = %v", stored.ArchivedAt)
	}

	selected, err := e.radarr.GetVisibleAcquisition(e.base.ctx, acquisitionID)
	if err != nil {
		t.Fatalf("read selected acquisition: %v", err)
	}
	if _, err := e.radarr.AbandonAcquisition(
		e.base.ctx,
		acquisitionID,
		selected.Revision,
		e.actorID,
		"No suitable release",
		e.now.Add(3*time.Minute),
	); err != nil {
		t.Fatalf("abandon acquisition: %v", err)
	}
	outcome, err := e.radarr.RemoveInstance(
		e.base.ctx,
		instance.ID,
		e.now.Add(4*time.Minute),
	)
	if err != nil {
		t.Fatalf("remove instance after terminal acquisition: %v", err)
	}
	if outcome != RadarrOutcomeArchived {
		t.Fatalf("used instance outcome = %q, want %q", outcome, RadarrOutcomeArchived)
	}
	stored, err = e.radarr.GetInstance(e.base.ctx, instance.ID)
	if err != nil {
		t.Fatalf("get archived instance: %v", err)
	}
	if stored.ArchivedAt == nil {
		t.Fatal("expected instance to be archived")
	}
	if !stored.Used {
		t.Fatal("archived instance did not report its Acquisition use")
	}
	storedPreset, err := e.radarr.GetPreset(e.base.ctx, preset.ID)
	if err != nil {
		t.Fatalf("get preset after instance archive: %v", err)
	}
	if storedPreset.ArchivedAt == nil {
		t.Fatal("expected instance archive to archive its presets")
	}
	if _, err := e.radarr.GetPreset(e.base.ctx, unusedPreset.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("get unused child preset = %v, want not found", err)
	}
}

func TestRadarrInstanceRemovalDoesNotFollowPresetMovedAfterTargetSelection(t *testing.T) {
	e := setupRadarrRepositoryTest(t)
	targetInstance := e.createInstance(t, "Original target")
	otherInstance := e.createInstance(t, "Later preset host")
	preset := e.createPreset(t, targetInstance, "1080p")
	acquisitionID := e.startDraw(t)
	e.reveal(t, acquisitionID)
	if _, err := e.radarr.SelectAcquisitionPreset(
		e.base.ctx,
		acquisitionID,
		preset.ID,
		e.actorID,
		e.now.Add(time.Minute),
	); err != nil {
		t.Fatalf("select target preset: %v", err)
	}
	if _, err := e.radarr.UpdatePreset(e.base.ctx, preset.ID, preset.Revision, RadarrPresetSave{
		Name:                preset.Name,
		InstanceID:          otherInstance.ID,
		RootFolderID:        preset.RootFolderID,
		RootFolderPath:      preset.RootFolderPath,
		QualityProfileID:    preset.QualityProfileID,
		QualityProfileName:  preset.QualityProfileName,
		Tags:                preset.Tags,
		MinimumAvailability: preset.MinimumAvailability,
		AcquisitionMode:     preset.AcquisitionMode,
		Valid:               true,
		ValidatedAt:         e.now.Add(2 * time.Minute),
	}); err != nil {
		t.Fatalf("move selected preset: %v", err)
	}

	outcome, err := e.radarr.RemoveInstance(
		e.base.ctx,
		otherInstance.ID,
		e.now.Add(3*time.Minute),
	)
	if err != nil {
		t.Fatalf("remove non-target instance: %v", err)
	}
	if outcome != RadarrOutcomeArchived {
		t.Fatalf("non-target instance outcome = %q, want %q", outcome, RadarrOutcomeArchived)
	}
	storedTarget, err := e.radarr.GetInstance(e.base.ctx, targetInstance.ID)
	if err != nil {
		t.Fatalf("get original target instance: %v", err)
	}
	if storedTarget.ArchivedAt != nil {
		t.Fatalf("original target archived_at = %v, want active", storedTarget.ArchivedAt)
	}
}

func TestRadarrPresetRemovalDeletesUnusedAndArchivesUsed(t *testing.T) {
	e := setupRadarrRepositoryTest(t)
	instance := e.createInstance(t, "Movies")
	unused := e.createPreset(t, instance, "Unused")

	outcome, err := e.radarr.RemovePreset(e.base.ctx, unused.ID, e.now.Add(time.Minute))
	if err != nil {
		t.Fatalf("remove unused preset: %v", err)
	}
	if outcome != RadarrOutcomeDeleted {
		t.Fatalf("unused preset outcome = %q, want %q", outcome, RadarrOutcomeDeleted)
	}
	if _, err := e.radarr.GetPreset(e.base.ctx, unused.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("get deleted preset = %v, want not found", err)
	}

	used := e.createPreset(t, instance, "Used")
	acquisitionID := e.startDraw(t)
	e.reveal(t, acquisitionID)
	if _, err := e.radarr.SelectAcquisitionPreset(
		e.base.ctx,
		acquisitionID,
		used.ID,
		e.actorID,
		e.now.Add(2*time.Minute),
	); err != nil {
		t.Fatalf("select used preset: %v", err)
	}

	outcome, err = e.radarr.RemovePreset(e.base.ctx, used.ID, e.now.Add(3*time.Minute))
	if err != nil {
		t.Fatalf("remove used preset: %v", err)
	}
	if outcome != RadarrOutcomeArchived {
		t.Fatalf("used preset outcome = %q, want %q", outcome, RadarrOutcomeArchived)
	}
	stored, err := e.radarr.GetPreset(e.base.ctx, used.ID)
	if err != nil {
		t.Fatalf("get archived preset: %v", err)
	}
	if stored.ArchivedAt == nil || !stored.Used {
		t.Fatalf("used preset after removal = %+v, want archived and used", stored)
	}
}

func TestRadarrArchivedUnusedPresetRemovalHardDeletesLegacySetup(t *testing.T) {
	e := setupRadarrRepositoryTest(t)
	instance := e.createInstance(t, "Movies")
	preset := e.createPreset(t, instance, "Legacy unused")
	if err := e.radarr.ArchivePreset(e.base.ctx, preset.ID, e.now.Add(time.Minute)); err != nil {
		t.Fatalf("archive unused preset: %v", err)
	}

	outcome, err := e.radarr.RemovePreset(e.base.ctx, preset.ID, e.now.Add(2*time.Minute))
	if err != nil {
		t.Fatalf("remove archived unused preset: %v", err)
	}
	if outcome != RadarrOutcomeDeleted {
		t.Fatalf("archived unused preset outcome = %q, want %q", outcome, RadarrOutcomeDeleted)
	}
	if _, err := e.radarr.GetPreset(e.base.ctx, preset.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("get deleted archived preset = %v, want not found", err)
	}
}

func TestRadarrAbandonmentClearsEveryUnresolvedMutationState(t *testing.T) {
	for _, state := range []string{
		"idle", "adding", "checking_replacement", "recreating", "searching", "grabbing",
	} {
		t.Run(state, func(t *testing.T) {
			e := setupRadarrRepositoryTest(t)
			acquisitionID := e.startDraw(t)
			e.reveal(t, acquisitionID)
			if _, err := e.base.pool.Write.ExecContext(e.base.ctx, `
				UPDATE radarr_acquisitions
				SET mutation_state = ?, revision = revision + 1
				WHERE id = ?
			`, state, acquisitionID); err != nil {
				t.Fatalf("set mutation state %q: %v", state, err)
			}
			current, err := e.radarr.GetVisibleAcquisition(e.base.ctx, acquisitionID)
			if err != nil {
				t.Fatalf("read %q acquisition: %v", state, err)
			}
			abandoned, err := e.radarr.AbandonAcquisition(
				e.base.ctx,
				acquisitionID,
				current.Revision,
				e.actorID,
				"No longer required",
				e.now.Add(time.Minute),
			)
			if err != nil {
				t.Fatalf("abandon %q acquisition: %v", state, err)
			}
			if abandoned.Status != "abandoned" || abandoned.MutationState != "idle" {
				t.Fatalf("abandoned %q acquisition = %+v", state, abandoned)
			}
		})
	}
}

func TestRadarrPresetSelectionSnapshotsAndLocksTarget(t *testing.T) {
	e := setupRadarrRepositoryTest(t)
	instance := e.createInstance(t, "Movies")
	preset := e.createPreset(t, instance, "1080p")
	acquisitionID := e.startDraw(t)
	e.reveal(t, acquisitionID)

	selected, err := e.radarr.SelectAcquisitionPreset(
		e.base.ctx,
		acquisitionID,
		preset.ID,
		e.actorID,
		e.now.Add(time.Minute),
	)
	if err != nil {
		t.Fatalf("select preset: %v", err)
	}
	if selected.PresetID == nil || *selected.PresetID != preset.ID ||
		selected.PresetName != "1080p" ||
		selected.TargetInstanceID == nil || *selected.TargetInstanceID != instance.ID ||
		selected.TargetInstanceName != "Movies" ||
		selected.TargetRootFolderID == nil || *selected.TargetRootFolderID != 10 ||
		selected.TargetRootFolderPath != "/media/movies" ||
		selected.TargetQualityProfileID == nil || *selected.TargetQualityProfileID != 20 ||
		selected.TargetQualityProfileName != "HD-1080p" ||
		selected.TargetMinimumAvailability != "released" ||
		selected.TargetAcquisitionMode != "manual" ||
		selected.TargetSelectedBy == nil || *selected.TargetSelectedBy != e.actorID ||
		len(selected.TargetTags) != 1 || selected.TargetTags[0].ID != 30 {
		t.Fatalf("selected target snapshot = %+v", selected)
	}

	if _, err := e.radarr.UpdatePreset(e.base.ctx, preset.ID, preset.Revision, RadarrPresetSave{
		Name:                "Changed preset",
		InstanceID:          instance.ID,
		RootFolderID:        11,
		RootFolderPath:      "/media/changed",
		QualityProfileID:    21,
		QualityProfileName:  "Changed quality",
		Tags:                []RadarrTagSnapshot{{ID: 31, Label: "changed"}},
		MinimumAvailability: "announced",
		AcquisitionMode:     "automatic",
		Valid:               true,
		ValidatedAt:         e.now.Add(2 * time.Minute),
	}); err != nil {
		t.Fatalf("update selected preset: %v", err)
	}
	if _, err := e.radarr.UpdateInstance(e.base.ctx, instance.ID, instance.Revision, RadarrInstanceSave{
		Name:            "Changed instance",
		BaseURL:         "http://changed.test",
		EncryptedAPIKey: []byte("changed-key"),
		State:           "connected",
		CheckedAt:       e.now.Add(2 * time.Minute),
	}); err != nil {
		t.Fatalf("update selected instance: %v", err)
	}

	if _, err := e.radarr.BeginAcquisitionMutation(
		e.base.ctx,
		acquisitionID,
		selected.Revision,
		"adding",
		e.now.Add(3*time.Minute),
	); err != nil {
		t.Fatalf("begin target mutation: %v", err)
	}
	locked, err := e.radarr.LockAcquisitionTarget(e.base.ctx, acquisitionID, RadarrTargetLock{
		RadarrMovieID: 42,
		Existing:      false,
		EffectiveConfiguration: RadarrEffectiveConfiguration{
			RootFolderPath:      "/media/movies",
			QualityProfileID:    20,
			QualityProfileName:  "HD-1080p",
			Tags:                []RadarrTagSnapshot{{ID: 30, Label: "movies"}},
			MinimumAvailability: "released",
			Monitored:           false,
		},
		LockedBy:     e.actorID,
		Status:       "needs_release",
		ActionReason: "release_required",
		At:           e.now.Add(4 * time.Minute),
	})
	if err != nil {
		t.Fatalf("lock acquisition target: %v", err)
	}
	if !locked.TargetLocked() ||
		locked.RadarrMovieID == nil || *locked.RadarrMovieID != 42 ||
		locked.TargetLockedBy == nil || *locked.TargetLockedBy != e.actorID ||
		locked.PresetName != "1080p" ||
		locked.TargetInstanceName != "Movies" ||
		locked.TargetRootFolderPath != "/media/movies" ||
		locked.TargetQualityProfileName != "HD-1080p" ||
		locked.EffectiveConfiguration.RootFolderPath != "/media/movies" ||
		locked.EffectiveConfiguration.QualityProfileID != 20 {
		t.Fatalf("locked target = %+v", locked)
	}
	if _, err := e.radarr.SelectAcquisitionPreset(
		e.base.ctx,
		acquisitionID,
		preset.ID,
		e.actorID,
		e.now.Add(5*time.Minute),
	); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("replace locked target = %v, want conflict", err)
	}
}

func TestRadarrBeginMutationRejectsStaleTargetSnapshot(t *testing.T) {
	e := setupRadarrRepositoryTest(t)
	instance := e.createInstance(t, "Movies")
	preset := e.createPreset(t, instance, "1080p")
	acquisitionID := e.startDraw(t)
	e.reveal(t, acquisitionID)
	stale, err := e.radarr.SelectAcquisitionPreset(
		e.base.ctx, acquisitionID, preset.ID, e.actorID, e.now.Add(time.Minute),
	)
	if err != nil {
		t.Fatalf("select initial preset: %v", err)
	}
	if _, err := e.radarr.SelectAcquisitionPreset(
		e.base.ctx, acquisitionID, preset.ID, e.actorID, e.now.Add(2*time.Minute),
	); err != nil {
		t.Fatalf("replace target review: %v", err)
	}
	if _, err := e.radarr.BeginAcquisitionMutation(
		e.base.ctx, acquisitionID, stale.Revision, "adding", e.now.Add(3*time.Minute),
	); !errors.Is(err, integration.ErrStaleRevision) {
		t.Fatalf("begin stale target mutation = %v, want stale revision", err)
	}
	current, err := e.radarr.GetVisibleAcquisition(e.base.ctx, acquisitionID)
	if err != nil {
		t.Fatalf("read acquisition after stale mutation: %v", err)
	}
	if current.MutationState != "idle" || current.TargetLocked() {
		t.Fatalf("stale mutation changed acquisition = %+v", current)
	}
}

func TestRadarrReconciliationTransitionRejectsStaleSnapshot(t *testing.T) {
	e := setupRadarrRepositoryTest(t)
	instance := e.createInstance(t, "Movies")
	preset := e.createPreset(t, instance, "1080p")
	acquisitionID := e.startDraw(t)
	e.reveal(t, acquisitionID)
	selected, err := e.radarr.SelectAcquisitionPreset(
		e.base.ctx, acquisitionID, preset.ID, e.actorID, e.now.Add(time.Minute),
	)
	if err != nil {
		t.Fatalf("select preset: %v", err)
	}
	if _, err := e.radarr.BeginAcquisitionMutation(
		e.base.ctx, acquisitionID, selected.Revision, "adding", e.now.Add(2*time.Minute),
	); err != nil {
		t.Fatalf("begin add: %v", err)
	}
	stale, err := e.radarr.LockAcquisitionTarget(e.base.ctx, acquisitionID, RadarrTargetLock{
		RadarrMovieID: 81,
		EffectiveConfiguration: RadarrEffectiveConfiguration{
			RootFolderPath: "/media/movies", QualityProfileID: 20,
			QualityProfileName: "HD-1080p", MinimumAvailability: "released",
		},
		LockedBy: e.actorID,
		Status:   "needs_release", ActionReason: "release_required",
		At: e.now.Add(3 * time.Minute),
	})
	if err != nil {
		t.Fatalf("lock target: %v", err)
	}
	newer, err := e.radarr.TransitionAcquisition(e.base.ctx, acquisitionID, RadarrAcquisitionTransition{
		Status: "queued", QueueStatus: "queued", At: e.now.Add(4 * time.Minute),
	})
	if err != nil {
		t.Fatalf("record newer state: %v", err)
	}
	if _, err := e.radarr.TransitionAcquisitionAtRevision(
		e.base.ctx, acquisitionID, stale.Revision, RadarrAcquisitionTransition{
			Status: "needs_release", ActionReason: "release_required", At: e.now.Add(5 * time.Minute),
		},
	); !errors.Is(err, integration.ErrStaleRevision) {
		t.Fatalf("stale reconciliation transition = %v, want stale revision", err)
	}
	current, err := e.radarr.GetVisibleAcquisition(e.base.ctx, acquisitionID)
	if err != nil {
		t.Fatalf("read acquisition: %v", err)
	}
	if current.Revision != newer.Revision || current.Status != "queued" || current.ActionReason != "" {
		t.Fatalf("stale reconciliation overwrote current state: %+v", current)
	}
}

func TestRadarrConcealedAcquisitionIsExcludedFromAdminSurfaces(t *testing.T) {
	e := setupRadarrRepositoryTest(t)
	acquisitionID := e.startDraw(t)

	if _, err := e.radarr.GetVisibleAcquisition(e.base.ctx, acquisitionID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("get concealed acquisition = %v, want not found", err)
	}
	listed, err := e.radarr.ListAcquisitions(e.base.ctx, "")
	if err != nil {
		t.Fatalf("list acquisitions: %v", err)
	}
	if len(listed) != 0 {
		t.Fatalf("admin list contains %d concealed acquisitions", len(listed))
	}
	attention, err := e.radarr.AttentionCount(e.base.ctx)
	if err != nil {
		t.Fatalf("attention count: %v", err)
	}
	if attention != 0 {
		t.Fatalf("concealed attention count = %d, want 0", attention)
	}

	e.reveal(t, acquisitionID)
	if _, err := e.radarr.GetVisibleAcquisition(e.base.ctx, acquisitionID); err != nil {
		t.Fatalf("get revealed acquisition: %v", err)
	}
	listed, err = e.radarr.ListAcquisitions(e.base.ctx, "")
	if err != nil {
		t.Fatalf("list revealed acquisitions: %v", err)
	}
	if len(listed) != 1 || listed[0].ID != acquisitionID {
		t.Fatalf("revealed admin list = %+v", listed)
	}
	attention, err = e.radarr.AttentionCount(e.base.ctx)
	if err != nil {
		t.Fatalf("revealed attention count: %v", err)
	}
	if attention != 1 {
		t.Fatalf("revealed attention count = %d, want 1", attention)
	}

	visible, err := e.radarr.GetVisibleAcquisition(e.base.ctx, acquisitionID)
	if err != nil {
		t.Fatalf("read revealed acquisition before abandon: %v", err)
	}
	if _, err := e.radarr.AbandonAcquisition(
		e.base.ctx,
		acquisitionID,
		visible.Revision,
		e.actorID,
		"Not obtainable",
		e.now.Add(time.Minute),
	); err != nil {
		t.Fatalf("abandon acquisition: %v", err)
	}
	attention, err = e.radarr.AttentionCount(e.base.ctx)
	if err != nil {
		t.Fatalf("terminal attention count: %v", err)
	}
	if attention != 0 {
		t.Fatalf("terminal attention count = %d, want 0", attention)
	}
}

func TestRadarrActionOutboxFiltersAndDeduplicatesConditions(t *testing.T) {
	e := setupRadarrRepositoryTest(t)
	e.createDestination(t, "all relevant", []string{"preset_required", "identity_required"}, true)
	e.createDestination(t, "identity only", []string{"identity_required"}, true)
	e.createDestination(t, "other reason", []string{"release_required"}, true)
	e.createDestination(t, "disabled", []string{"preset_required", "identity_required"}, false)
	acquisitionID := e.startDraw(t)
	if got := e.deliveryCount(t); got != 0 {
		t.Fatalf("concealed draw queued %d deliveries, want 0", got)
	}

	e.reveal(t, acquisitionID)
	if got := e.deliveryCount(t); got != 1 {
		t.Fatalf("preset-required Reveal queued %d deliveries, want 1", got)
	}
	if _, err := e.radarr.TransitionAcquisition(e.base.ctx, acquisitionID, RadarrAcquisitionTransition{
		Status:       "needs_preset",
		ActionReason: "preset_required",
		At:           e.now.Add(time.Minute),
	}); err != nil {
		t.Fatalf("repeat preset-required condition: %v", err)
	}
	if got := e.deliveryCount(t); got != 1 {
		t.Fatalf("repeated condition queued %d deliveries, want 1", got)
	}

	transitioned, err := e.radarr.TransitionAcquisition(e.base.ctx, acquisitionID, RadarrAcquisitionTransition{
		Status:       "action_needed",
		ActionReason: "identity_required",
		At:           e.now.Add(2 * time.Minute),
	})
	if err != nil {
		t.Fatalf("start identity-required condition: %v", err)
	}
	if transitioned.ActionVersion != 2 {
		t.Fatalf("identity condition action version = %d, want 2", transitioned.ActionVersion)
	}
	if got := e.deliveryCount(t); got != 3 {
		t.Fatalf("filtered identity condition total deliveries = %d, want 3", got)
	}
	if _, err := e.radarr.TransitionAcquisition(e.base.ctx, acquisitionID, RadarrAcquisitionTransition{
		Status:       "action_needed",
		ActionReason: "identity_required",
		At:           e.now.Add(3 * time.Minute),
	}); err != nil {
		t.Fatalf("repeat identity-required condition: %v", err)
	}
	if got := e.deliveryCount(t); got != 3 {
		t.Fatalf("repeated identity condition total deliveries = %d, want 3", got)
	}

	rows, err := e.base.pool.Read.QueryContext(e.base.ctx, `
		SELECT q.action_version, q.reason, d.name
		FROM radarr_webhook_deliveries q
		JOIN radarr_webhook_destinations d ON d.id = q.destination_id
		ORDER BY q.action_version, d.name
	`)
	if err != nil {
		t.Fatalf("list action deliveries: %v", err)
	}
	defer rows.Close()
	type deliveredCondition struct {
		version int
		reason  string
		name    string
	}
	var got []deliveredCondition
	for rows.Next() {
		var condition deliveredCondition
		if err := rows.Scan(&condition.version, &condition.reason, &condition.name); err != nil {
			t.Fatalf("scan action delivery: %v", err)
		}
		got = append(got, condition)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate action deliveries: %v", err)
	}
	want := []deliveredCondition{
		{version: 1, reason: "preset_required", name: "all relevant"},
		{version: 2, reason: "identity_required", name: "all relevant"},
		{version: 2, reason: "identity_required", name: "identity only"},
	}
	if len(got) != len(want) {
		t.Fatalf("action deliveries = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("action delivery %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestRadarrDownloadedSelectedTargetIsTerminal(t *testing.T) {
	e := setupRadarrRepositoryTest(t)
	instance := e.createInstance(t, "Movies")
	preset := e.createPreset(t, instance, "1080p")
	acquisitionID := e.startDraw(t)
	e.reveal(t, acquisitionID)
	selected, err := e.radarr.SelectAcquisitionPreset(
		e.base.ctx,
		acquisitionID,
		preset.ID,
		e.actorID,
		e.now.Add(time.Minute),
	)
	if err != nil {
		t.Fatalf("select preset: %v", err)
	}
	if _, err := e.radarr.BeginAcquisitionMutation(
		e.base.ctx,
		acquisitionID,
		selected.Revision,
		"adding",
		e.now.Add(2*time.Minute),
	); err != nil {
		t.Fatalf("begin target mutation: %v", err)
	}
	downloadedAt := e.now.Add(3 * time.Minute)
	downloaded, err := e.radarr.LockAcquisitionTarget(e.base.ctx, acquisitionID, RadarrTargetLock{
		RadarrMovieID: 77,
		Existing:      true,
		EffectiveConfiguration: RadarrEffectiveConfiguration{
			RootFolderPath:      "/existing",
			QualityProfileID:    99,
			QualityProfileName:  "Existing profile",
			MinimumAvailability: "announced",
			Monitored:           true,
		},
		LockedBy: e.actorID,
		Status:   "downloaded",
		At:       downloadedAt,
	})
	if err != nil {
		t.Fatalf("lock downloaded target: %v", err)
	}
	if !downloaded.Terminal() ||
		downloaded.Status != "downloaded" ||
		downloaded.DownloadedAt == nil || !downloaded.DownloadedAt.Equal(downloadedAt) ||
		!downloaded.AdoptedExisting ||
		downloaded.RadarrMovieID == nil || *downloaded.RadarrMovieID != 77 ||
		downloaded.ActionReason != "" {
		t.Fatalf("downloaded acquisition = %+v", downloaded)
	}

	if _, err := e.radarr.TransitionAcquisition(e.base.ctx, acquisitionID, RadarrAcquisitionTransition{
		Status:       "action_needed",
		ActionReason: "release_failed",
		At:           e.now.Add(4 * time.Minute),
	}); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("transition downloaded acquisition = %v, want conflict", err)
	}
	if _, err := e.radarr.SelectAcquisitionPreset(
		e.base.ctx,
		acquisitionID,
		preset.ID,
		e.actorID,
		e.now.Add(4*time.Minute),
	); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("retarget downloaded acquisition = %v, want conflict", err)
	}
	due, err := e.radarr.DueAcquisitions(e.base.ctx, e.now.Add(24*time.Hour), 10)
	if err != nil {
		t.Fatalf("list due acquisitions: %v", err)
	}
	if len(due) != 0 {
		t.Fatalf("downloaded acquisition remains due: %+v", due)
	}
	attention, err := e.radarr.AttentionCount(e.base.ctx)
	if err != nil {
		t.Fatalf("attention count: %v", err)
	}
	if attention != 0 {
		t.Fatalf("downloaded acquisition attention count = %d, want 0", attention)
	}
}

func TestRadarrDueAcquisitionsRotatesRowsWithoutNextCheckTime(t *testing.T) {
	e := setupRadarrRepositoryTest(t)
	instance := e.createInstance(t, "Movies")
	preset := e.createPreset(t, instance, "1080p")

	createDueAcquisition := func(title string, offset time.Duration, radarrMovieID int) int64 {
		t.Helper()
		drawnAt := e.now.Add(offset)
		movie, err := e.base.movies.Add(e.base.ctx, title, "pool", e.actorID)
		if err != nil {
			t.Fatalf("add %s: %v", title, err)
		}
		if err := e.base.movies.StartDraw(
			e.base.ctx,
			movie.ID,
			drawnAt,
			drawnAt.Add(16_500*time.Millisecond),
			"drawer",
		); err != nil {
			t.Fatalf("start %s draw: %v", title, err)
		}
		var acquisitionID int64
		if err := e.base.pool.Read.QueryRowContext(e.base.ctx,
			`SELECT id FROM radarr_acquisitions WHERE movie_id = ?`, movie.ID,
		).Scan(&acquisitionID); err != nil {
			t.Fatalf("read %s acquisition: %v", title, err)
		}
		if err := e.base.movies.RevealDraw(e.base.ctx, movie.ID, drawnAt.Add(17*time.Second)); err != nil {
			t.Fatalf("reveal %s: %v", title, err)
		}
		selected, err := e.radarr.SelectAcquisitionPreset(
			e.base.ctx, acquisitionID, preset.ID, e.actorID, drawnAt.Add(18*time.Second),
		)
		if err != nil {
			t.Fatalf("select %s preset: %v", title, err)
		}
		if _, err := e.radarr.BeginAcquisitionMutation(
			e.base.ctx, acquisitionID, selected.Revision, "adding", drawnAt.Add(19*time.Second),
		); err != nil {
			t.Fatalf("begin %s mutation: %v", title, err)
		}
		if _, err := e.radarr.LockAcquisitionTarget(e.base.ctx, acquisitionID, RadarrTargetLock{
			RadarrMovieID: radarrMovieID,
			EffectiveConfiguration: RadarrEffectiveConfiguration{
				RootFolderPath:      "/media/movies",
				QualityProfileID:    20,
				QualityProfileName:  "HD-1080p",
				MinimumAvailability: "released",
				Monitored:           true,
			},
			LockedBy: e.actorID,
			Status:   "waiting_for_radarr",
			At:       drawnAt.Add(20 * time.Second),
		}); err != nil {
			t.Fatalf("lock %s target: %v", title, err)
		}
		if _, err := e.radarr.TransitionAcquisition(e.base.ctx, acquisitionID, RadarrAcquisitionTransition{
			Status: "waiting_for_radarr",
			At:     drawnAt.Add(20*time.Second + 500*time.Millisecond),
		}); err != nil {
			t.Fatalf("clear %s handoff lease: %v", title, err)
		}
		if err := e.base.movies.MarkAsWatched(e.base.ctx, movie.ID, drawnAt.Add(21*time.Second)); err != nil {
			t.Fatalf("mark %s watched: %v", title, err)
		}
		return acquisitionID
	}

	firstID := createDueAcquisition("Heat", 0, 71)
	secondID := createDueAcquisition("Arrival", time.Hour, 72)
	if _, err := e.radarr.TransitionAcquisition(e.base.ctx, firstID, RadarrAcquisitionTransition{
		Status: "waiting_for_radarr",
		At:     e.now.Add(2 * time.Hour),
	}); err != nil {
		t.Fatalf("refresh first acquisition: %v", err)
	}

	due, err := e.radarr.DueAcquisitions(e.base.ctx, e.now.Add(24*time.Hour), 1)
	if err != nil {
		t.Fatalf("list due acquisitions: %v", err)
	}
	if len(due) != 1 || due[0].ID != secondID {
		t.Fatalf("first due acquisition = %+v, want id %d", due, secondID)
	}
	if due[0].NextCheckAt != nil {
		t.Fatalf("selected due acquisition has next check time %v, want nil", due[0].NextCheckAt)
	}
}

func TestRadarrMutationHandoffsLeaseAcquisitionFromReconciler(t *testing.T) {
	e := setupRadarrRepositoryTest(t)
	instance := e.createInstance(t, "Movies")
	preset := e.createPreset(t, instance, "1080p")
	acquisitionID := e.startDraw(t)
	e.reveal(t, acquisitionID)
	selected, err := e.radarr.SelectAcquisitionPreset(
		e.base.ctx, acquisitionID, preset.ID, e.actorID, e.now.Add(time.Minute),
	)
	if err != nil {
		t.Fatalf("select preset: %v", err)
	}
	adding, err := e.radarr.BeginAcquisitionMutation(
		e.base.ctx, acquisitionID, selected.Revision, "adding", e.now.Add(2*time.Minute),
	)
	if err != nil {
		t.Fatalf("begin add mutation: %v", err)
	}

	assertLease := func(name string, acquisition RadarrAcquisition, at time.Time) {
		t.Helper()
		want := at.Add(30 * time.Second)
		if acquisition.NextCheckAt == nil || !acquisition.NextCheckAt.Equal(want) {
			t.Fatalf("%s next check = %v, want %v", name, acquisition.NextCheckAt, want)
		}
		due, dueErr := e.radarr.DueAcquisitions(e.base.ctx, want.Add(-time.Millisecond), 10)
		if dueErr != nil {
			t.Fatalf("list %s lease acquisitions: %v", name, dueErr)
		}
		if len(due) != 0 {
			t.Fatalf("%s acquisition became due during handoff: %+v", name, due)
		}
	}
	assertLease("add claim", adding, e.now.Add(2*time.Minute))

	lockAt := e.now.Add(3 * time.Minute)
	locked, err := e.radarr.LockAcquisitionTarget(e.base.ctx, acquisitionID, RadarrTargetLock{
		RadarrMovieID: 171,
		EffectiveConfiguration: RadarrEffectiveConfiguration{
			RootFolderPath:      "/media/movies",
			QualityProfileID:    20,
			QualityProfileName:  "HD-1080p",
			MinimumAvailability: "released",
			Monitored:           true,
		},
		LockedBy: e.actorID,
		Status:   "waiting_for_radarr",
		At:       lockAt,
	})
	if err != nil {
		t.Fatalf("lock target after revision %d: %v", adding.Revision, err)
	}
	assertLease("lock", locked, lockAt)

	checkAt := lockAt.Add(10 * time.Second)
	checking, err := e.radarr.BeginLockedReplacementCheck(
		e.base.ctx, acquisitionID, locked.Revision, checkAt,
	)
	if err != nil {
		t.Fatalf("begin replacement check: %v", err)
	}
	assertLease("replacement check", checking, checkAt)

	recreateAt := checkAt.Add(10 * time.Second)
	recreating, err := e.radarr.BeginLockedRecreation(
		e.base.ctx, acquisitionID, checking.Revision, recreateAt,
	)
	if err != nil {
		t.Fatalf("begin recreation: %v", err)
	}
	assertLease("recreation", recreating, recreateAt)

	if _, err := e.radarr.ReclaimLockedReplacement(
		e.base.ctx, acquisitionID, recreating.Revision, "recreating", recreateAt.Add(10*time.Second),
	); !errors.Is(err, integration.ErrStaleRevision) {
		t.Fatalf("reclaim active recreation = %v, want stale revision", err)
	}
	unchanged, err := e.radarr.GetVisibleAcquisition(e.base.ctx, acquisitionID)
	if err != nil {
		t.Fatalf("read active recreation: %v", err)
	}
	if unchanged.Revision != recreating.Revision || unchanged.NextCheckAt == nil ||
		!unchanged.NextCheckAt.Equal(*recreating.NextCheckAt) {
		t.Fatalf("active recreation changed during refused reclaim: before=%+v after=%+v", recreating, unchanged)
	}

	reclaimAt := recreating.NextCheckAt.Add(time.Millisecond)
	recreating, err = e.radarr.ReclaimLockedReplacement(
		e.base.ctx, acquisitionID, recreating.Revision, "recreating", reclaimAt,
	)
	if err != nil {
		t.Fatalf("reclaim expired recreation: %v", err)
	}
	assertLease("reclaimed recreation", recreating, reclaimAt)

	renewAt := reclaimAt.Add(10 * time.Second)
	renewed, err := e.radarr.RenewLockedRecreationLease(
		e.base.ctx, acquisitionID, recreating.Revision, renewAt,
	)
	if err != nil {
		t.Fatalf("renew recreation lease: %v", err)
	}
	assertLease("renewed recreation", renewed, renewAt)
	if _, err := e.radarr.RenewLockedRecreationLease(
		e.base.ctx, acquisitionID, recreating.Revision, renewAt.Add(time.Second),
	); !errors.Is(err, integration.ErrStaleRevision) {
		t.Fatalf("renew stale recreation = %v, want stale revision", err)
	}
	recreating = renewed

	replaceAt := renewAt.Add(10 * time.Second)
	if err := e.radarr.ReplaceLockedRadarrMovie(
		e.base.ctx, acquisitionID, recreating.Revision, 172, false,
		recreating.EffectiveConfiguration, e.actorID, replaceAt,
	); err != nil {
		t.Fatalf("replace locked movie: %v", err)
	}
	replaced, err := e.radarr.GetVisibleAcquisition(e.base.ctx, acquisitionID)
	if err != nil {
		t.Fatalf("read replaced acquisition: %v", err)
	}
	assertLease("replacement", replaced, replaceAt)
}

func TestRadarrDestinationRevisionRetiresPendingDelivery(t *testing.T) {
	e := setupRadarrRepositoryTest(t)
	destination := e.createDestination(t, "Alerts", []string{"preset_required", "identity_required"}, true)
	acquisitionID := e.startDraw(t)
	e.reveal(t, acquisitionID)
	due, err := e.radarr.DueWebhookDeliveries(e.base.ctx, e.now.Add(time.Hour), 10)
	if err != nil {
		t.Fatalf("list initial delivery: %v", err)
	}
	if len(due) != 1 || due[0].DestinationRevision != 1 {
		t.Fatalf("initial deliveries = %+v", due)
	}
	oldDeliveryID := due[0].ID
	verifiedAt := e.now.Add(time.Minute)
	updated, err := e.radarr.UpdateWebhookDestination(
		e.base.ctx,
		destination.ID,
		destination.Revision,
		RadarrWebhookDestinationSave{
			Name:          "Alerts",
			Kind:          "generic",
			EncryptedURL:  []byte("new-encrypted-url"),
			ReasonFilters: []string{"preset_required", "identity_required"},
			Enabled:       true,
			VerifiedAt:    &verifiedAt,
		},
	)
	if err != nil {
		t.Fatalf("update webhook destination: %v", err)
	}
	if updated.Revision != 2 {
		t.Fatalf("updated destination revision = %d, want 2", updated.Revision)
	}
	var oldStatus, oldSummary string
	var oldResolvedAt sql.NullInt64
	if err := e.base.pool.Read.QueryRowContext(e.base.ctx, `
		SELECT status, resolved_at, error_summary
		FROM radarr_webhook_deliveries
		WHERE id = ?
	`, oldDeliveryID).Scan(&oldStatus, &oldResolvedAt, &oldSummary); err != nil {
		t.Fatalf("read retired delivery: %v", err)
	}
	if oldStatus != "terminal_failed" || !oldResolvedAt.Valid || oldSummary != "destination configuration changed" {
		t.Fatalf("retired delivery = status %q resolved %v summary %q", oldStatus, oldResolvedAt, oldSummary)
	}

	if _, err := e.radarr.TransitionAcquisition(e.base.ctx, acquisitionID, RadarrAcquisitionTransition{
		Status:       "action_needed",
		ActionReason: "identity_required",
		At:           e.now.Add(2 * time.Minute),
	}); err != nil {
		t.Fatalf("start new actionable condition: %v", err)
	}
	due, err = e.radarr.DueWebhookDeliveries(e.base.ctx, e.now.Add(time.Hour), 10)
	if err != nil {
		t.Fatalf("list revised delivery: %v", err)
	}
	if len(due) != 1 ||
		due[0].ID == oldDeliveryID ||
		due[0].DestinationRevision != 2 ||
		due[0].Reason != "identity_required" {
		t.Fatalf("revised deliveries = %+v", due)
	}
}

func TestRadarrActionChangeSupersedesPendingWebhook(t *testing.T) {
	e := setupRadarrRepositoryTest(t)
	e.createDestination(t, "Alerts", []string{"preset_required", "identity_required"}, true)
	acquisitionID := e.startDraw(t)
	e.reveal(t, acquisitionID)
	due, err := e.radarr.DueWebhookDeliveries(e.base.ctx, e.now.Add(time.Hour), 10)
	if err != nil || len(due) != 1 {
		t.Fatalf("initial due delivery = %+v, err=%v", due, err)
	}
	oldID := due[0].ID

	if _, err := e.radarr.TransitionAcquisition(e.base.ctx, acquisitionID, RadarrAcquisitionTransition{
		Status: "action_needed", ActionReason: "identity_required",
		At: e.now.Add(2 * time.Minute),
	}); err != nil {
		t.Fatalf("change actionable condition: %v", err)
	}

	var status string
	var resolvedAt sql.NullInt64
	if err := e.base.pool.Read.QueryRowContext(e.base.ctx, `
		SELECT status, resolved_at FROM radarr_webhook_deliveries WHERE id = ?
	`, oldID).Scan(&status, &resolvedAt); err != nil {
		t.Fatalf("read superseded delivery: %v", err)
	}
	if status != "superseded" || !resolvedAt.Valid {
		t.Fatalf("superseded delivery = status %q resolved %v", status, resolvedAt)
	}
	due, err = e.radarr.DueWebhookDeliveries(e.base.ctx, e.now.Add(time.Hour), 10)
	if err != nil || len(due) != 1 || due[0].ID == oldID || due[0].Reason != "identity_required" {
		t.Fatalf("current due delivery = %+v, err=%v", due, err)
	}
}

func TestRadarrWebhookRetryAndTerminalWarningLifecycle(t *testing.T) {
	e := setupRadarrRepositoryTest(t)
	destination := e.createDestination(t, "Alerts", []string{"preset_required", "identity_required"}, true)
	acquisitionID := e.startDraw(t)
	e.reveal(t, acquisitionID)
	due, err := e.radarr.DueWebhookDeliveries(e.base.ctx, e.now.Add(time.Hour), 10)
	if err != nil || len(due) != 1 {
		t.Fatalf("initial due delivery = %+v, err=%v", due, err)
	}
	deliveryID := due[0].ID
	failedAt := e.now.Add(time.Minute)
	nextAttemptAt := e.now.Add(10 * time.Minute)
	claimed, claimedOK, err := e.radarr.ClaimWebhookDeliveryForSend(
		e.base.ctx, deliveryID, failedAt, failedAt.Add(time.Minute),
	)
	if err != nil || !claimedOK {
		t.Fatalf("claim initial delivery = %+v, claimed=%v, err=%v", claimed, claimedOK, err)
	}
	if err := e.radarr.MarkWebhookDeliveryFailed(
		e.base.ctx,
		deliveryID,
		claimed.ClaimVersion,
		"temporary timeout",
		nextAttemptAt,
		false,
		failedAt,
	); err != nil {
		t.Fatalf("record retryable delivery failure: %v", err)
	}
	due, err = e.radarr.DueWebhookDeliveries(e.base.ctx, nextAttemptAt.Add(-time.Second), 10)
	if err != nil {
		t.Fatalf("list delivery before retry: %v", err)
	}
	if len(due) != 0 {
		t.Fatalf("delivery became due before retry time: %+v", due)
	}
	due, err = e.radarr.DueWebhookDeliveries(e.base.ctx, nextAttemptAt, 10)
	if err != nil {
		t.Fatalf("list retry delivery: %v", err)
	}
	if len(due) != 1 ||
		due[0].AttemptCount != 1 ||
		due[0].ErrorSummary != "temporary timeout" ||
		due[0].LastAttemptAt == nil || !due[0].LastAttemptAt.Equal(failedAt) {
		t.Fatalf("retry delivery = %+v", due)
	}
	storedDestination, err := e.radarr.GetWebhookDestination(e.base.ctx, destination.ID)
	if err != nil {
		t.Fatalf("get destination after retryable failure: %v", err)
	}
	if storedDestination.HealthWarningAt != nil || storedDestination.HealthWarningReason != "" {
		t.Fatalf("retryable failure set warning: %+v", storedDestination)
	}

	terminalAt := nextAttemptAt.Add(time.Minute)
	claimed, claimedOK, err = e.radarr.ClaimWebhookDeliveryForSend(
		e.base.ctx, deliveryID, terminalAt, terminalAt.Add(time.Minute),
	)
	if err != nil || !claimedOK {
		t.Fatalf("claim retry delivery = %+v, claimed=%v, err=%v", claimed, claimedOK, err)
	}
	if err := e.radarr.MarkWebhookDeliveryFailed(
		e.base.ctx,
		deliveryID,
		claimed.ClaimVersion,
		"webhook rejected",
		terminalAt,
		true,
		terminalAt,
	); err != nil {
		t.Fatalf("record terminal delivery failure: %v", err)
	}
	storedDestination, err = e.radarr.GetWebhookDestination(e.base.ctx, destination.ID)
	if err != nil {
		t.Fatalf("get destination warning: %v", err)
	}
	if storedDestination.HealthWarningAt == nil ||
		!storedDestination.HealthWarningAt.Equal(terminalAt) ||
		storedDestination.HealthWarningReason != "webhook rejected" {
		t.Fatalf("terminal warning = %+v", storedDestination)
	}
	var terminalStatus string
	var terminalAttempts int
	var terminalResolvedAt sql.NullInt64
	if err := e.base.pool.Read.QueryRowContext(e.base.ctx, `
		SELECT status, attempt_count, resolved_at
		FROM radarr_webhook_deliveries
		WHERE id = ?
	`, deliveryID).Scan(&terminalStatus, &terminalAttempts, &terminalResolvedAt); err != nil {
		t.Fatalf("read terminal delivery: %v", err)
	}
	if terminalStatus != "terminal_failed" || terminalAttempts != 2 || terminalResolvedAt.Valid {
		t.Fatalf(
			"terminal delivery = status %q attempts %d resolved %v",
			terminalStatus,
			terminalAttempts,
			terminalResolvedAt,
		)
	}

	if _, err := e.radarr.TransitionAcquisition(e.base.ctx, acquisitionID, RadarrAcquisitionTransition{
		Status:       "action_needed",
		ActionReason: "identity_required",
		At:           terminalAt.Add(time.Minute),
	}); err != nil {
		t.Fatalf("start recovery condition: %v", err)
	}
	due, err = e.radarr.DueWebhookDeliveries(e.base.ctx, terminalAt.Add(time.Hour), 10)
	if err != nil || len(due) != 1 {
		t.Fatalf("recovery delivery = %+v, err=%v", due, err)
	}
	deliveredAt := terminalAt.Add(2 * time.Minute)
	claimed, claimedOK, err = e.radarr.ClaimWebhookDeliveryForSend(
		e.base.ctx, due[0].ID, deliveredAt, deliveredAt.Add(time.Minute),
	)
	if err != nil || !claimedOK {
		t.Fatalf("claim recovery delivery = %+v, claimed=%v, err=%v", claimed, claimedOK, err)
	}
	if err := e.radarr.MarkWebhookDelivered(
		e.base.ctx, claimed.ID, claimed.ClaimVersion, deliveredAt,
	); err != nil {
		t.Fatalf("mark recovery delivery delivered: %v", err)
	}
	storedDestination, err = e.radarr.GetWebhookDestination(e.base.ctx, destination.ID)
	if err != nil {
		t.Fatalf("get recovered destination: %v", err)
	}
	if storedDestination.HealthWarningAt != nil || storedDestination.HealthWarningReason != "" {
		t.Fatalf("successful recovery left warning: %+v", storedDestination)
	}
	if err := e.base.pool.Read.QueryRowContext(e.base.ctx, `
		SELECT resolved_at FROM radarr_webhook_deliveries WHERE id = ?
	`, deliveryID).Scan(&terminalResolvedAt); err != nil {
		t.Fatalf("read resolved terminal delivery: %v", err)
	}
	if !terminalResolvedAt.Valid || terminalResolvedAt.Int64 != deliveredAt.Unix() {
		t.Fatalf("terminal failure resolved_at = %v, want %d", terminalResolvedAt, deliveredAt.Unix())
	}
}

func TestRadarrWebhookDeliveryRetention(t *testing.T) {
	e := setupRadarrRepositoryTest(t)
	destination := e.createDestination(t, "Archive", []string{"identity_required"}, false)
	acquisitionID := e.startDraw(t)
	e.reveal(t, acquisitionID)
	now := e.now.Add(24 * time.Hour)

	type deliverySeed struct {
		name        string
		status      string
		deliveredAt *time.Time
		resolvedAt  *time.Time
	}
	at := func(value time.Time) *time.Time { return &value }
	seeds := []deliverySeed{
		{name: "old delivered", status: "delivered", deliveredAt: at(now.Add(-31 * 24 * time.Hour))},
		{name: "boundary delivered", status: "delivered", deliveredAt: at(now.Add(-30 * 24 * time.Hour))},
		{name: "old resolved failure", status: "terminal_failed", resolvedAt: at(now.Add(-91 * 24 * time.Hour))},
		{name: "boundary resolved failure", status: "terminal_failed", resolvedAt: at(now.Add(-90 * 24 * time.Hour))},
		{name: "unresolved failure", status: "terminal_failed"},
		{name: "pending", status: "pending"},
	}
	ids := make(map[string]int64, len(seeds))
	for i, seed := range seeds {
		var deliveredAt any
		if seed.deliveredAt != nil {
			deliveredAt = seed.deliveredAt.Unix()
		}
		var resolvedAt any
		if seed.resolvedAt != nil {
			resolvedAt = seed.resolvedAt.Unix()
		}
		result, err := e.base.pool.Write.ExecContext(e.base.ctx, `
			INSERT INTO radarr_webhook_deliveries (
			    destination_id, destination_revision, acquisition_id,
			    reason, action_version, status, next_attempt_at,
			    delivered_at, resolved_at, error_summary
			) VALUES (?, ?, ?, 'identity_required', ?, ?, ?, ?, ?, ?)
		`, destination.ID, destination.Revision, acquisitionID, 100+i, seed.status,
			now.Add(-120*24*time.Hour).Unix(), deliveredAt, resolvedAt, seed.name)
		if err != nil {
			t.Fatalf("insert %s delivery: %v", seed.name, err)
		}
		id, err := result.LastInsertId()
		if err != nil {
			t.Fatalf("read %s delivery id: %v", seed.name, err)
		}
		ids[seed.name] = id
	}

	removed, err := e.radarr.PruneWebhookDeliveries(e.base.ctx, now)
	if err != nil {
		t.Fatalf("prune webhook deliveries: %v", err)
	}
	if removed != 2 {
		t.Fatalf("pruned deliveries = %d, want 2", removed)
	}
	for _, name := range []string{"old delivered", "old resolved failure"} {
		var count int
		if err := e.base.pool.Read.QueryRowContext(e.base.ctx,
			`SELECT COUNT(*) FROM radarr_webhook_deliveries WHERE id = ?`, ids[name]).Scan(&count); err != nil {
			t.Fatalf("count pruned %s delivery: %v", name, err)
		}
		if count != 0 {
			t.Errorf("%s delivery survived retention", name)
		}
	}
	for _, name := range []string{
		"boundary delivered",
		"boundary resolved failure",
		"unresolved failure",
		"pending",
	} {
		var count int
		if err := e.base.pool.Read.QueryRowContext(e.base.ctx,
			`SELECT COUNT(*) FROM radarr_webhook_deliveries WHERE id = ?`, ids[name]).Scan(&count); err != nil {
			t.Fatalf("count retained %s delivery: %v", name, err)
		}
		if count != 1 {
			t.Errorf("%s delivery was pruned", name)
		}
	}
}

func TestRadarrWebhookDeliveryClaimRequiresCurrentDestinationAndAction(t *testing.T) {
	e := setupRadarrRepositoryTest(t)
	destination := e.createDestination(
		t, "Alerts", []string{"preset_required", "identity_required"}, true,
	)
	acquisitionID := e.startDraw(t)
	e.reveal(t, acquisitionID)
	due, err := e.radarr.DueWebhookDeliveries(e.base.ctx, e.now.Add(time.Hour), 10)
	if err != nil || len(due) != 1 {
		t.Fatalf("initial due delivery = %+v, err=%v", due, err)
	}
	current, claimed, err := e.radarr.ClaimWebhookDeliveryForSend(
		e.base.ctx, due[0].ID, e.now.Add(time.Hour), e.now.Add(2*time.Hour),
	)
	if err != nil || !claimed || current.ID != due[0].ID {
		t.Fatalf("initial delivery claim = %+v, claimed=%v, err=%v", current, claimed, err)
	}

	if _, err := e.radarr.TransitionAcquisition(e.base.ctx, acquisitionID, RadarrAcquisitionTransition{
		Status: "action_needed", ActionReason: "identity_required", At: e.now.Add(2 * time.Minute),
	}); err != nil {
		t.Fatalf("change Acquisition action: %v", err)
	}
	if err := e.radarr.MarkWebhookDelivered(
		e.base.ctx, current.ID, current.ClaimVersion, e.now.Add(2*time.Minute),
	); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("complete superseded action delivery = %v, want conflict", err)
	}
	due, err = e.radarr.DueWebhookDeliveries(e.base.ctx, e.now.Add(time.Hour), 10)
	if err != nil || len(due) != 1 || due[0].Reason != "identity_required" {
		t.Fatalf("new action delivery = %+v, err=%v", due, err)
	}
	current, claimed, err = e.radarr.ClaimWebhookDeliveryForSend(
		e.base.ctx, due[0].ID, e.now.Add(time.Hour), e.now.Add(2*time.Hour),
	)
	if err != nil || !claimed {
		t.Fatalf("claim new action delivery = %+v, claimed=%v, err=%v", current, claimed, err)
	}

	if err := e.radarr.ArchiveWebhookDestination(
		e.base.ctx, destination.ID, e.now.Add(3*time.Minute),
	); err != nil {
		t.Fatalf("archive destination: %v", err)
	}
	if err := e.radarr.MarkWebhookDelivered(
		e.base.ctx, current.ID, current.ClaimVersion, e.now.Add(4*time.Minute),
	); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("complete archived destination delivery = %v, want conflict", err)
	}
}

func TestRadarrWebhookDeliveryClaimIsExclusiveAndExpiresSafely(t *testing.T) {
	e := setupRadarrRepositoryTest(t)
	e.createDestination(t, "Alerts", []string{"preset_required"}, true)
	acquisitionID := e.startDraw(t)
	e.reveal(t, acquisitionID)
	now := e.now.Add(time.Hour)
	due, err := e.radarr.DueWebhookDeliveries(e.base.ctx, now, 10)
	if err != nil || len(due) != 1 {
		t.Fatalf("initial due delivery = %+v, err=%v", due, err)
	}

	type claimResult struct {
		delivery RadarrWebhookDelivery
		claimed  bool
		err      error
	}
	const claimers = 8
	start := make(chan struct{})
	results := make(chan claimResult, claimers)
	for range claimers {
		go func() {
			<-start
			delivery, claimed, claimErr := e.radarr.ClaimWebhookDeliveryForSend(
				e.base.ctx, due[0].ID, now, now.Add(time.Minute),
			)
			results <- claimResult{delivery: delivery, claimed: claimed, err: claimErr}
		}()
	}
	close(start)
	var firstClaim RadarrWebhookDelivery
	claimedCount := 0
	for range claimers {
		result := <-results
		if result.err != nil {
			t.Fatalf("concurrent claim failed: %v", result.err)
		}
		if result.claimed {
			claimedCount++
			firstClaim = result.delivery
		}
	}
	if claimedCount != 1 || firstClaim.ClaimVersion != 1 || firstClaim.ClaimExpiresAt == nil {
		t.Fatalf("concurrent claims = %d, winner = %+v", claimedCount, firstClaim)
	}
	if recovered, err := e.radarr.RecoverExpiredWebhookDeliveryClaims(
		e.base.ctx, now.Add(time.Minute-time.Second),
	); err != nil || recovered != 0 {
		t.Fatalf("early claim recovery = %d, err=%v", recovered, err)
	}
	if err := e.radarr.MarkWebhookDelivered(
		e.base.ctx, firstClaim.ID, firstClaim.ClaimVersion, now.Add(time.Minute),
	); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("expired claim completion = %v, want conflict", err)
	}
	if recovered, err := e.radarr.RecoverExpiredWebhookDeliveryClaims(
		e.base.ctx, now.Add(time.Minute),
	); err != nil || recovered != 1 {
		t.Fatalf("expired claim recovery = %d, err=%v", recovered, err)
	}

	secondClaim, claimed, err := e.radarr.ClaimWebhookDeliveryForSend(
		e.base.ctx, due[0].ID, now.Add(time.Minute), now.Add(2*time.Minute),
	)
	if err != nil || !claimed || secondClaim.ClaimVersion != firstClaim.ClaimVersion+1 {
		t.Fatalf("replacement claim = %+v, claimed=%v, err=%v", secondClaim, claimed, err)
	}
	if err := e.radarr.MarkWebhookDeliveryFailed(
		e.base.ctx,
		firstClaim.ID,
		firstClaim.ClaimVersion,
		"stale worker",
		now.Add(3*time.Minute),
		false,
		now.Add(time.Minute),
	); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("stale claim completion = %v, want conflict", err)
	}
	if err := e.radarr.MarkWebhookDelivered(
		e.base.ctx, secondClaim.ID, secondClaim.ClaimVersion, now.Add(time.Minute),
	); err != nil {
		t.Fatalf("complete current claim: %v", err)
	}
	var status string
	var attempts int
	if err := e.base.pool.Read.QueryRowContext(e.base.ctx, `
		SELECT status, attempt_count
		FROM radarr_webhook_deliveries
		WHERE id = ?
	`, secondClaim.ID).Scan(&status, &attempts); err != nil {
		t.Fatalf("read completed delivery: %v", err)
	}
	if status != "delivered" || attempts != 2 {
		t.Fatalf("completed delivery = status %q attempts %d", status, attempts)
	}
}

func TestRadarrWebhookExpiredFinalClaimBecomesTerminal(t *testing.T) {
	e := setupRadarrRepositoryTest(t)
	destination := e.createDestination(t, "Alerts", []string{"preset_required"}, true)
	acquisitionID := e.startDraw(t)
	e.reveal(t, acquisitionID)
	now := e.now.Add(time.Hour)
	due, err := e.radarr.DueWebhookDeliveries(e.base.ctx, now, 10)
	if err != nil || len(due) != 1 {
		t.Fatalf("initial due delivery = %+v, err=%v", due, err)
	}
	if _, err := e.base.pool.Write.ExecContext(e.base.ctx, `
		UPDATE radarr_webhook_deliveries
		SET attempt_count = ?
		WHERE id = ?
	`, RadarrWebhookMaxAttempts-1, due[0].ID); err != nil {
		t.Fatalf("seed prior delivery attempts: %v", err)
	}
	claimed, claimedOK, err := e.radarr.ClaimWebhookDeliveryForSend(
		e.base.ctx, due[0].ID, now, now.Add(time.Minute),
	)
	if err != nil || !claimedOK || claimed.AttemptCount != RadarrWebhookMaxAttempts {
		t.Fatalf("claim final delivery = %+v, claimed=%v, err=%v", claimed, claimedOK, err)
	}
	if recovered, err := e.radarr.RecoverExpiredWebhookDeliveryClaims(
		e.base.ctx, now.Add(time.Minute),
	); err != nil || recovered != 1 {
		t.Fatalf("recover expired final delivery = %d, err=%v", recovered, err)
	}
	var status, summary string
	var attempts int
	var claimExpiresAt sql.NullInt64
	if err := e.base.pool.Read.QueryRowContext(e.base.ctx, `
		SELECT status, attempt_count, claim_expires_at, error_summary
		FROM radarr_webhook_deliveries
		WHERE id = ?
	`, due[0].ID).Scan(&status, &attempts, &claimExpiresAt, &summary); err != nil {
		t.Fatalf("read expired final delivery: %v", err)
	}
	if status != "terminal_failed" || attempts != RadarrWebhookMaxAttempts ||
		claimExpiresAt.Valid || summary != radarrWebhookInterruptedFinalAttempt {
		t.Fatalf("expired final delivery = status %q attempts %d claim %v summary %q",
			status, attempts, claimExpiresAt, summary)
	}
	storedDestination, err := e.radarr.GetWebhookDestination(e.base.ctx, destination.ID)
	if err != nil {
		t.Fatalf("read destination warning: %v", err)
	}
	if storedDestination.HealthWarningAt == nil ||
		storedDestination.HealthWarningReason != radarrWebhookInterruptedFinalAttempt {
		t.Fatalf("expired final delivery warning = %+v", storedDestination)
	}
}

func TestRadarrDrawSnapshotsMetadataYear(t *testing.T) {
	e := setupRadarrRepositoryTest(t)
	movie, err := e.base.movies.Add(e.base.ctx, "Heat", "pool", e.actorID)
	if err != nil {
		t.Fatalf("add pooled movie: %v", err)
	}
	metadata := NewSqliteMovieMetadataRepository(e.base.pool)
	if err := metadata.UpsertMetadata(e.base.ctx, domain.MovieMetadata{
		MovieID: movie.ID, ReleaseDate: "1995-12-15",
	}); err != nil {
		t.Fatalf("store movie metadata: %v", err)
	}
	if err := e.base.movies.StartDraw(
		e.base.ctx, movie.ID, e.now, e.now.Add(16_500*time.Millisecond), "drawer",
	); err != nil {
		t.Fatalf("start draw: %v", err)
	}
	var acquisitionID int64
	if err := e.base.pool.Read.QueryRowContext(e.base.ctx,
		`SELECT id FROM radarr_acquisitions WHERE movie_id = ?`, movie.ID,
	).Scan(&acquisitionID); err != nil {
		t.Fatalf("read acquisition id: %v", err)
	}
	acquisition, err := readRadarrAcquisition(e.base.ctx, e.radarr.pool.Read, acquisitionID, false)
	if err != nil {
		t.Fatalf("read acquisition: %v", err)
	}
	if acquisition.MovieYear != 1995 {
		t.Fatalf("movie year = %d, want 1995", acquisition.MovieYear)
	}
}

func TestRadarrPreviewClearsActionAndIdentityChangeInvalidatesIt(t *testing.T) {
	e := setupRadarrRepositoryTest(t)
	instance := e.createInstance(t, "Movies")
	preset := e.createPreset(t, instance, "1080p")
	acquisitionID := e.startDraw(t)
	e.reveal(t, acquisitionID)
	if _, err := e.radarr.SelectAcquisitionPreset(
		e.base.ctx, acquisitionID, preset.ID, e.actorID, e.now.Add(time.Minute),
	); err != nil {
		t.Fatalf("select preset: %v", err)
	}
	actioned, err := e.radarr.TransitionAcquisition(e.base.ctx, acquisitionID, RadarrAcquisitionTransition{
		Status: "action_needed", ActionReason: "identity_required", At: e.now.Add(2 * time.Minute),
	})
	if err != nil {
		t.Fatalf("set identity action: %v", err)
	}
	previewed, err := e.radarr.RecordAcquisitionTargetPreview(
		e.base.ctx, acquisitionID, actioned.Revision, true,
		RadarrEffectiveConfiguration{RootFolderPath: "/existing", Monitored: true},
		e.now.Add(3*time.Minute),
	)
	if err != nil {
		t.Fatalf("record target preview: %v", err)
	}
	if previewed.Status != "waiting_for_radarr" || previewed.ActionReason != "" ||
		previewed.TargetPreviewedAt == nil || !previewed.TargetPreviewExisting {
		t.Fatalf("previewed acquisition = %+v", previewed)
	}

	changed, err := e.radarr.SetAcquisitionIdentityOverride(
		e.base.ctx, acquisitionID, previewed.Revision, 949, e.now.Add(4*time.Minute),
	)
	if err != nil {
		t.Fatalf("change acquisition identity: %v", err)
	}
	if changed.TargetPreviewedAt != nil || changed.TargetPreviewExisting ||
		changed.EffectiveConfiguration.RootFolderPath != "" || changed.Status != "waiting_for_radarr" {
		t.Fatalf("identity change retained stale preview: %+v", changed)
	}
}
