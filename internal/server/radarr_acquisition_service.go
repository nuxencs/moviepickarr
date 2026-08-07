package server

import (
	"context"
	"errors"
	"slices"
	"strings"
	"time"

	"moviepickarr/internal/domain"
	"moviepickarr/internal/integration"
	integrationradarr "moviepickarr/internal/integration/radarr"
	"moviepickarr/internal/repository"
)

const (
	radarrAcquisitionPollInterval      = 30 * time.Second
	radarrAutomaticSearchClockSkew     = time.Minute
	radarrAutomaticSearchUnknownReason = "The automatic search result is unknown. Retry will check Radarr without starting another search."
)

type radarrAbandonmentReview struct {
	Acquisition repository.RadarrAcquisition
	Activity    string
}

func (s *radarrService) listAcquisitions(
	ctx context.Context,
	query string,
) ([]repository.RadarrAcquisition, error) {
	return s.repo.ListAcquisitions(ctx, query)
}

func (s *radarrService) acquisition(
	ctx context.Context,
	id int64,
) (repository.RadarrAcquisition, error) {
	return s.repo.GetVisibleAcquisition(ctx, id)
}

func (s *radarrService) attentionCount(ctx context.Context) (int, error) {
	return s.repo.AttentionCount(ctx)
}

func (s *radarrService) selectPreset(
	ctx context.Context,
	acquisitionID, presetID int64,
	actorID int,
) (repository.RadarrAcquisition, error) {
	preset, err := s.repo.GetPreset(ctx, presetID)
	if err != nil {
		return repository.RadarrAcquisition{}, err
	}
	if preset.ArchivedAt != nil {
		return repository.RadarrAcquisition{}, domain.ErrConflict
	}
	if _, _, err := s.validateStoredPreset(ctx, preset); err != nil {
		return repository.RadarrAcquisition{}, err
	}
	acquisition, err := s.repo.SelectAcquisitionPreset(
		ctx, acquisitionID, presetID, actorID, s.now().UTC(),
	)
	if err != nil {
		return repository.RadarrAcquisition{}, err
	}
	return s.previewAcquisitionTarget(ctx, acquisition)
}

func (s *radarrService) selectAcquisitionIdentity(
	ctx context.Context,
	id int64,
	tmdbID int,
) (repository.RadarrAcquisition, error) {
	if tmdbID <= 0 {
		return repository.RadarrAcquisition{}, invalidRadarrField("tmdbId", "Select a valid TMDB movie.")
	}
	acquisition, err := s.repo.GetVisibleAcquisition(ctx, id)
	if err != nil {
		return repository.RadarrAcquisition{}, err
	}
	if acquisition.Terminal() || acquisition.TargetLocked() || acquisition.TargetInstanceID == nil ||
		acquisition.TMDBID != nil ||
		(acquisition.ActionReason != "identity_required" && acquisition.IdentitySource != "override") {
		return repository.RadarrAcquisition{}, domain.ErrConflict
	}
	instance, client, err := s.acquisitionClient(ctx, acquisition)
	if err != nil {
		return repository.RadarrAcquisition{}, err
	}
	exact, err := integrationradarr.TMDBIdentity(tmdbID)
	if err != nil {
		return repository.RadarrAcquisition{}, invalidRadarrField("tmdbId", "Select a valid TMDB movie.")
	}
	candidate, err := client.LookupMovie(ctx, exact)
	if err != nil {
		s.recordInstanceFailure(ctx, instance, err)
		if errors.Is(err, integrationradarr.ErrNotFound) {
			return repository.RadarrAcquisition{}, invalidRadarrField("tmdbId", "The selected movie is no longer available in Radarr.")
		}
		return repository.RadarrAcquisition{}, err
	}
	if candidate.TMDBID != tmdbID {
		s.recordInstanceFailure(ctx, instance, integrationradarr.ErrInvalidResponse)
		return repository.RadarrAcquisition{}, integrationradarr.ErrInvalidResponse
	}
	s.recordInstanceSuccess(ctx, instance)
	acquisition, err = s.repo.SetAcquisitionIdentityOverride(
		ctx, id, acquisition.Revision, tmdbID, s.now().UTC(),
	)
	if err != nil {
		return repository.RadarrAcquisition{}, err
	}
	if acquisition.TargetInstanceID == nil {
		return acquisition, nil
	}
	return s.previewAcquisitionTarget(ctx, acquisition)
}

func (s *radarrService) searchAcquisitionIdentity(
	ctx context.Context,
	id int64,
	query string,
) ([]integrationradarr.MovieCandidate, error) {
	acquisition, err := s.repo.GetVisibleAcquisition(ctx, id)
	if err != nil {
		return nil, err
	}
	if acquisition.Terminal() || acquisition.TargetLocked() || acquisition.TargetInstanceID == nil ||
		(acquisition.ActionReason != "identity_required" && acquisition.IdentitySource != "override") {
		return nil, domain.ErrConflict
	}
	query = strings.TrimSpace(query)
	lookup := integrationradarr.TitleQuery{Title: query}
	if query == "" {
		lookup.Title = acquisition.MovieTitle
		lookup.Year = acquisition.MovieYear
	}
	instance, client, err := s.acquisitionClient(ctx, acquisition)
	if err != nil {
		return nil, err
	}
	candidates, err := client.SearchMovies(ctx, lookup)
	if err != nil {
		s.recordInstanceFailure(ctx, instance, err)
		return nil, err
	}
	s.recordInstanceSuccess(ctx, instance)
	return candidates, nil
}

func (s *radarrService) previewAcquisitionTarget(
	ctx context.Context,
	acquisition repository.RadarrAcquisition,
) (repository.RadarrAcquisition, error) {
	instance, client, err := s.acquisitionClient(ctx, acquisition)
	if err != nil {
		return s.actionForClientFailure(ctx, acquisition, err)
	}
	catalog, err := s.catalogFor(ctx, instance)
	if err != nil {
		return s.actionForClientFailure(ctx, acquisition, err)
	}
	if err := validateAcquisitionTargetSnapshot(acquisition, catalog); err != nil {
		return s.transitionAdminRemoteObservation(ctx, acquisition, repository.RadarrAcquisitionTransition{
			Status: "action_needed", ActionReason: "configuration_invalid",
			FailureSummary: "The selected target no longer matches its Radarr instance.",
			QueueStatus:    "none", At: s.now().UTC(),
		})
	}
	acquisition, tmdbID, err := s.resolveAcquisitionIdentity(ctx, acquisition, client)
	if err != nil {
		if errors.Is(err, integrationradarr.ErrNotFound) || errors.Is(err, errRadarrIdentityMismatch) || errors.Is(err, errRadarrIdentityRequired) {
			return s.transitionAdminRemoteObservation(ctx, acquisition, repository.RadarrAcquisitionTransition{
				Status: "action_needed", ActionReason: "identity_required",
				FailureSummary: "Radarr could not resolve the movie's exact identity.",
				QueueStatus:    "none", At: s.now().UTC(),
			})
		}
		return s.actionForClientFailure(ctx, acquisition, err)
	}
	remote, err := client.FindMovieByTMDB(ctx, tmdbID)
	if err != nil {
		s.recordInstanceFailure(ctx, instance, err)
		return s.actionForClientFailure(ctx, acquisition, err)
	}
	s.recordInstanceSuccess(ctx, instance)
	effective := repository.RadarrEffectiveConfiguration{}
	if remote != nil {
		effective = effectiveConfiguration(*remote, catalog)
	}
	return s.recordAcquisitionTargetPreview(ctx, acquisition, remote != nil, effective)
}

func (s *radarrService) recordAcquisitionTargetPreview(
	ctx context.Context,
	acquisition repository.RadarrAcquisition,
	existing bool,
	effective repository.RadarrEffectiveConfiguration,
) (repository.RadarrAcquisition, error) {
	updated, err := s.repo.RecordAcquisitionTargetPreview(
		ctx, acquisition.ID, acquisition.Revision, existing, effective, s.now().UTC(),
	)
	if errors.Is(err, integration.ErrStaleRevision) {
		return s.repo.GetVisibleAcquisition(ctx, acquisition.ID)
	}
	return updated, err
}

func (s *radarrService) confirmAcquisitionTarget(
	ctx context.Context,
	id int64,
	actorID int,
) (repository.RadarrAcquisition, error) {
	acquisition, err := s.repo.GetVisibleAcquisition(ctx, id)
	if err != nil {
		return repository.RadarrAcquisition{}, err
	}
	if acquisition.Terminal() {
		return repository.RadarrAcquisition{}, domain.ErrConflict
	}
	if acquisition.TargetLocked() {
		return acquisition, nil
	}
	if acquisition.TargetInstanceID == nil || acquisition.PresetID == nil {
		return repository.RadarrAcquisition{}, domain.ErrConflict
	}
	if acquisition.MutationState != "idle" {
		return s.reconcileAmbiguousAdd(ctx, acquisition, actorID)
	}
	if acquisition.TargetPreviewedAt == nil {
		return repository.RadarrAcquisition{}, domain.ErrConflict
	}

	instance, client, err := s.acquisitionClient(ctx, acquisition)
	if err != nil {
		return s.actionForClientFailure(ctx, acquisition, err)
	}
	catalog, err := s.catalogFor(ctx, instance)
	if err != nil {
		return s.actionForClientFailure(ctx, acquisition, err)
	}
	if err := validateAcquisitionTargetSnapshot(acquisition, catalog); err != nil {
		return s.transitionAdminRemoteObservation(ctx, acquisition, repository.RadarrAcquisitionTransition{
			Status: "action_needed", ActionReason: "configuration_invalid",
			FailureSummary: "The selected target no longer matches its Radarr instance.",
			QueueStatus:    "none", At: s.now().UTC(),
		})
	}
	acquisition, tmdbID, err := s.resolveAcquisitionIdentity(ctx, acquisition, client)
	if err != nil {
		if errors.Is(err, integrationradarr.ErrNotFound) || errors.Is(err, errRadarrIdentityMismatch) || errors.Is(err, errRadarrIdentityRequired) {
			return s.transitionAdminRemoteObservation(ctx, acquisition, repository.RadarrAcquisitionTransition{
				Status: "action_needed", ActionReason: "identity_required",
				FailureSummary: "Select the correct Radarr movie identity.",
				QueueStatus:    "none", At: s.now().UTC(),
			})
		}
		return s.actionForClientFailure(ctx, acquisition, err)
	}

	remote, err := client.FindMovieByTMDB(ctx, tmdbID)
	if err != nil {
		s.recordInstanceFailure(ctx, instance, err)
		return s.actionForClientFailure(ctx, acquisition, err)
	}
	foundExisting := remote != nil
	previewEffective := repository.RadarrEffectiveConfiguration{}
	if remote != nil {
		previewEffective = effectiveConfiguration(*remote, catalog)
	}
	if foundExisting != acquisition.TargetPreviewExisting ||
		(foundExisting && !sameEffectiveConfiguration(previewEffective, acquisition.EffectiveConfiguration)) {
		if _, err := s.recordAcquisitionTargetPreview(ctx, acquisition, foundExisting, previewEffective); err != nil {
			return repository.RadarrAcquisition{}, err
		}
		return repository.RadarrAcquisition{}, domain.ErrConflict
	}
	now := s.now().UTC()
	acquisition, err = s.repo.BeginAcquisitionMutation(ctx, acquisition.ID, acquisition.Revision, "adding", now)
	if err != nil {
		return repository.RadarrAcquisition{}, err
	}
	if remote == nil {
		remoteMovie, addErr := client.AddMovie(ctx, integrationradarr.AddMovieRequest{
			TMDBID: tmdbID, Title: acquisition.MovieTitle,
			RootFolderPath:      acquisition.TargetRootFolderPath,
			QualityProfileID:    valueOrZero(acquisition.TargetQualityProfileID),
			TagIDs:              tagSnapshotIDs(acquisition.TargetTags),
			MinimumAvailability: integrationradarr.MinimumAvailability(acquisition.TargetMinimumAvailability),
			Mode:                integrationradarr.AcquisitionMode(acquisition.TargetAcquisitionMode),
		})
		if addErr != nil {
			s.recordInstanceFailure(ctx, instance, addErr)
			if radarrMutationOutcomeAmbiguous(addErr) {
				// The request may have reached Radarr. Keep the target in its
				// in-progress state until a read proves whether the movie exists.
				next := now.Add(radarrAcquisitionPollInterval)
				updated, transitionErr := s.repo.TransitionAcquisitionAtRevision(
					context.WithoutCancel(ctx), acquisition.ID, acquisition.Revision,
					repository.RadarrAcquisitionTransition{
						Status: "action_needed", ActionReason: "connection_failed",
						FailureSummary: "The add result is unknown. Retry will reconcile before sending another request.",
						QueueStatus:    "none", NextCheckAt: &next, At: now,
					},
				)
				if errors.Is(transitionErr, integration.ErrStaleRevision) {
					return s.repo.GetVisibleAcquisition(context.WithoutCancel(ctx), acquisition.ID)
				}
				return updated, transitionErr
			}
			return s.repo.ResetAcquisitionMutation(ctx, acquisition.ID, repository.RadarrAcquisitionTransition{
				Status: "action_needed", ActionReason: "add_failed",
				FailureSummary: "Radarr did not add the movie.", QueueStatus: "none", At: now,
			})
		}
		remote = &remoteMovie
	}
	s.recordInstanceSuccess(ctx, instance)
	return s.lockRemoteMovie(ctx, acquisition, *remote, foundExisting, catalog, actorID)
}

func (s *radarrService) lockRemoteMovie(
	ctx context.Context,
	acquisition repository.RadarrAcquisition,
	remote integrationradarr.Movie,
	existing bool,
	catalog integrationradarr.Catalog,
	actorID int,
) (repository.RadarrAcquisition, error) {
	now := s.now().UTC()
	status, reason, queueStatus, queueSummary := "needs_release", "release_required", "none", ""
	failureSummary := ""
	if remote.HasFile {
		status, reason = "downloaded", ""
	} else {
		instance, client, err := s.acquisitionClient(ctx, acquisition)
		if err != nil {
			return s.actionForClientFailure(ctx, acquisition, err)
		}
		queue, err := client.Queue(ctx, remote.ID)
		if err != nil {
			s.recordInstanceFailure(ctx, instance, err)
			return s.actionForClientFailure(ctx, acquisition, err)
		}
		if len(queue) > 0 {
			if queueReason, summary, failed := actionForQueue(queue); failed {
				status, reason, queueStatus = "action_needed", queueReason, "failed"
				queueSummary, failureSummary = summary, summary
			} else {
				status, queueStatus, queueSummary = stateForQueue(queue)
				reason = ""
			}
		} else if acquisition.TargetAcquisitionMode == "automatic" {
			status, reason = "waiting_for_radarr", ""
		}
	}
	locked, err := s.repo.LockAcquisitionTarget(ctx, acquisition.ID, repository.RadarrTargetLock{
		RadarrMovieID: remote.ID, Existing: existing,
		EffectiveConfiguration: effectiveConfiguration(remote, catalog),
		LockedBy:               actorID, Status: status, ActionReason: reason, At: now,
	})
	if err != nil {
		return repository.RadarrAcquisition{}, err
	}
	if queueSummary != "" {
		next := now.Add(radarrAcquisitionPollInterval)
		return s.repo.TransitionAcquisition(ctx, locked.ID, repository.RadarrAcquisitionTransition{
			Status: status, QueueStatus: queueStatus, QueueSummary: queueSummary,
			ActionReason: reason, FailureSummary: failureSummary, NextCheckAt: &next, At: now,
		})
	}
	if status == "downloaded" || locked.TargetAcquisitionMode != "automatic" {
		return locked, nil
	}
	return s.startAutomaticSearch(ctx, locked, remote.ID)
}

func (s *radarrService) startAutomaticSearch(
	ctx context.Context,
	acquisition repository.RadarrAcquisition,
	radarrMovieID int,
) (repository.RadarrAcquisition, error) {
	if acquisition.AutomaticSearchCommandID != nil && acquisition.AutomaticSearchCompletedAt == nil {
		return s.reconcileOne(ctx, acquisition)
	}
	now := s.now().UTC()
	claimed, err := s.repo.BeginAutomaticSearchAttempt(
		ctx, acquisition.ID, acquisition.Revision, now,
	)
	if errors.Is(err, integration.ErrStaleRevision) || errors.Is(err, domain.ErrConflict) {
		return s.repo.GetVisibleAcquisition(ctx, acquisition.ID)
	}
	if err != nil {
		return repository.RadarrAcquisition{}, err
	}
	if claimed.RadarrMovieID == nil || *claimed.RadarrMovieID != radarrMovieID {
		return s.resetAutomaticSearchClaim(ctx, claimed, repository.RadarrAcquisitionTransition{
			Status: "action_needed", ActionReason: "identity_required",
			FailureSummary: "The locked Radarr movie changed before the automatic search started.",
			QueueStatus:    "none", At: now,
		})
	}
	instance, client, err := s.acquisitionClient(ctx, claimed)
	if err != nil {
		return s.resetAutomaticSearchClaim(ctx, claimed, radarrClientFailureTransition(claimed, err, now))
	}
	observation, err := s.observeAutomaticSearchTarget(ctx, claimed, instance, client)
	if err != nil {
		return s.resetAutomaticSearchClaim(ctx, claimed, radarrClientFailureTransition(claimed, err, now))
	}
	if observation != nil {
		return s.resetAutomaticSearchClaim(ctx, claimed, *observation)
	}
	command, err := client.StartMoviesSearch(ctx, radarrMovieID)
	if err != nil {
		s.recordInstanceFailure(ctx, instance, err)
		if radarrMutationOutcomeAmbiguous(err) {
			return s.preserveUnknownAutomaticSearch(ctx, claimed, now)
		}
		reason := "release_failed"
		if errors.Is(err, integrationradarr.ErrAuthentication) {
			reason = "connection_failed"
		} else if errors.Is(err, integrationradarr.ErrValidation) {
			reason = "configuration_invalid"
		}
		return s.resetAutomaticSearchClaim(ctx, claimed, repository.RadarrAcquisitionTransition{
			Status: "action_needed", ActionReason: reason,
			FailureSummary: "Radarr did not start the automatic movie search.",
			QueueStatus:    "none", At: now,
		})
	}
	s.recordInstanceSuccess(ctx, instance)
	return s.repo.RecordAutomaticSearchCommand(
		context.WithoutCancel(ctx), claimed.ID, claimed.Revision, command.ID, now,
	)
}

func (s *radarrService) recoverAutomaticSearch(
	ctx context.Context,
	acquisition repository.RadarrAcquisition,
) (repository.RadarrAcquisition, error) {
	if acquisition.MutationState != "searching" || acquisition.RadarrMovieID == nil {
		return repository.RadarrAcquisition{}, domain.ErrConflict
	}
	now := s.now().UTC()
	if acquisition.ActionReason == "" && acquisition.NextCheckAt != nil && now.Before(*acquisition.NextCheckAt) {
		return acquisition, nil
	}
	instance, client, err := s.acquisitionClient(ctx, acquisition)
	if err != nil {
		return s.preserveUnknownAutomaticSearch(ctx, acquisition, now)
	}
	observation, err := s.observeAutomaticSearchTarget(ctx, acquisition, instance, client)
	if err != nil {
		return s.preserveUnknownAutomaticSearch(ctx, acquisition, now)
	}
	if observation != nil {
		return s.resetAutomaticSearchClaim(ctx, acquisition, *observation)
	}
	claimedAt := acquisition.UpdatedAt
	if acquisition.AutomaticSearchClaimedAt != nil {
		claimedAt = *acquisition.AutomaticSearchClaimedAt
	}
	command, err := client.FindRecentMoviesSearchCommand(
		ctx, *acquisition.RadarrMovieID, claimedAt.Add(-radarrAutomaticSearchClockSkew),
	)
	if err != nil {
		s.recordInstanceFailure(ctx, instance, err)
		return s.preserveUnknownAutomaticSearch(ctx, acquisition, now)
	}
	s.recordInstanceSuccess(ctx, instance)
	if command == nil {
		return s.preserveUnknownAutomaticSearch(ctx, acquisition, now)
	}
	return s.repo.RecordAutomaticSearchCommand(
		context.WithoutCancel(ctx), acquisition.ID, acquisition.Revision, command.ID, now,
	)
}

func (s *radarrService) observeAutomaticSearchTarget(
	ctx context.Context,
	acquisition repository.RadarrAcquisition,
	instance repository.RadarrInstance,
	client integrationradarr.Client,
) (*repository.RadarrAcquisitionTransition, error) {
	now := s.now().UTC()
	remote, err := client.GetMovie(ctx, valueOrZero(acquisition.RadarrMovieID))
	if err != nil {
		if errors.Is(err, integrationradarr.ErrNotFound) {
			s.recordInstanceSuccess(ctx, instance)
			return &repository.RadarrAcquisitionTransition{
				Status: "action_needed", ActionReason: "add_failed",
				FailureSummary: "The movie was removed from the locked Radarr target.",
				QueueStatus:    "none", At: now,
			}, nil
		}
		s.recordInstanceFailure(ctx, instance, err)
		return nil, err
	}
	if !acquisitionMatchesRemote(acquisition, remote) {
		return &repository.RadarrAcquisitionTransition{
			Status: "action_needed", ActionReason: "identity_required",
			FailureSummary: "The locked Radarr movie no longer matches the Acquisition identity.",
			QueueStatus:    "none", At: now,
		}, nil
	}
	if remote.HasFile {
		s.recordInstanceSuccess(ctx, instance)
		return &repository.RadarrAcquisitionTransition{
			Status: "downloaded", QueueStatus: "none", At: now,
		}, nil
	}
	queue, err := client.Queue(ctx, remote.ID)
	if err != nil {
		s.recordInstanceFailure(ctx, instance, err)
		return nil, err
	}
	s.recordInstanceSuccess(ctx, instance)
	if len(queue) == 0 {
		return nil, nil
	}
	next := now.Add(radarrAcquisitionPollInterval)
	transition := repository.RadarrAcquisitionTransition{NextCheckAt: &next, At: now}
	if reason, summary, failed := actionForQueue(queue); failed {
		transition.Status = "action_needed"
		transition.ActionReason = reason
		transition.FailureSummary = summary
		transition.QueueStatus = "failed"
		transition.QueueSummary = summary
	} else {
		transition.Status, transition.QueueStatus, transition.QueueSummary = stateForQueue(queue)
	}
	return &transition, nil
}

func (s *radarrService) resetAutomaticSearchClaim(
	ctx context.Context,
	acquisition repository.RadarrAcquisition,
	transition repository.RadarrAcquisitionTransition,
) (repository.RadarrAcquisition, error) {
	updated, err := s.repo.ResetAcquisitionMutationAtRevision(
		context.WithoutCancel(ctx), acquisition.ID, acquisition.Revision, transition,
	)
	if errors.Is(err, integration.ErrStaleRevision) || errors.Is(err, domain.ErrConflict) {
		return s.repo.GetVisibleAcquisition(context.WithoutCancel(ctx), acquisition.ID)
	}
	return updated, err
}

func (s *radarrService) preserveUnknownAutomaticSearch(
	ctx context.Context,
	acquisition repository.RadarrAcquisition,
	at time.Time,
) (repository.RadarrAcquisition, error) {
	next := at.Add(radarrAcquisitionPollInterval)
	updated, err := s.repo.TransitionAcquisitionAtRevision(
		context.WithoutCancel(ctx), acquisition.ID, acquisition.Revision,
		repository.RadarrAcquisitionTransition{
			Status: "action_needed", ActionReason: "connection_failed",
			FailureSummary: radarrAutomaticSearchUnknownReason,
			QueueStatus:    "none", NextCheckAt: &next, At: at,
		},
	)
	if errors.Is(err, integration.ErrStaleRevision) || errors.Is(err, domain.ErrConflict) {
		return s.repo.GetVisibleAcquisition(context.WithoutCancel(ctx), acquisition.ID)
	}
	return updated, err
}

func radarrMutationOutcomeAmbiguous(err error) bool {
	return errors.Is(err, integrationradarr.ErrTransient) ||
		errors.Is(err, integrationradarr.ErrInvalidResponse) ||
		errors.Is(err, integrationradarr.ErrResponseTooLarge) ||
		errors.Is(err, context.DeadlineExceeded) ||
		errors.Is(err, context.Canceled)
}

func (s *radarrService) reconcileAmbiguousAdd(
	ctx context.Context,
	acquisition repository.RadarrAcquisition,
	actorID int,
) (repository.RadarrAcquisition, error) {
	if acquisition.MutationState == "adding" && acquisition.NextCheckAt != nil &&
		s.now().UTC().Before(*acquisition.NextCheckAt) {
		return acquisition, nil
	}
	instance, client, err := s.acquisitionClient(ctx, acquisition)
	if err != nil {
		return s.actionForClientFailure(ctx, acquisition, err)
	}
	tmdbID, ok := acquisition.ResolvedTMDBID()
	if !ok {
		return s.repo.ResetAcquisitionMutation(ctx, acquisition.ID, repository.RadarrAcquisitionTransition{
			Status: "action_needed", ActionReason: "identity_required",
			FailureSummary: "The Acquisition has no verified TMDB identity.",
			QueueStatus:    "none", At: s.now().UTC(),
		})
	}
	remote, err := client.FindMovieByTMDB(ctx, tmdbID)
	if err != nil {
		s.recordInstanceFailure(ctx, instance, err)
		return s.actionForClientFailure(ctx, acquisition, err)
	}
	if remote == nil {
		return s.repo.ResetAcquisitionMutation(ctx, acquisition.ID, repository.RadarrAcquisitionTransition{
			Status: "action_needed", ActionReason: "add_failed",
			FailureSummary: "Radarr does not contain the movie. Confirm the same target to try again.",
			QueueStatus:    "none", At: s.now().UTC(),
		})
	}
	catalog, err := s.catalogFor(ctx, instance)
	if err != nil {
		return s.actionForClientFailure(ctx, acquisition, err)
	}
	// The preview records whether this was already managed by Radarr before the
	// confirmation mutation started. Preserve that fact across an ambiguous
	// response so recovery never changes an adopted movie's configuration.
	return s.lockRemoteMovie(
		ctx, acquisition, *remote, acquisition.TargetPreviewExisting, catalog, actorID,
	)
}

func (s *radarrService) retryAcquisition(
	ctx context.Context,
	id int64,
	actorID int,
) (repository.RadarrAcquisition, error) {
	acquisition, err := s.repo.GetVisibleAcquisition(ctx, id)
	if err != nil {
		return repository.RadarrAcquisition{}, err
	}
	if acquisition.Terminal() {
		return repository.RadarrAcquisition{}, domain.ErrConflict
	}
	if !acquisition.TargetLocked() {
		if acquisition.MutationState != "idle" {
			return s.reconcileAmbiguousAdd(ctx, acquisition, actorID)
		}
		if acquisition.TargetInstanceID == nil {
			return repository.RadarrAcquisition{}, domain.ErrConflict
		}
		return s.previewAcquisitionTarget(ctx, acquisition)
	}
	if acquisition.MutationState == "searching" {
		return s.recoverAutomaticSearch(ctx, acquisition)
	}
	if acquisition.MutationState == "grabbing" {
		return s.reconcileOne(ctx, acquisition)
	}
	now := s.now().UTC()
	switch acquisition.MutationState {
	case "idle":
	case "checking_replacement", "recreating":
		if acquisition.NextCheckAt != nil && now.Before(*acquisition.NextCheckAt) {
			return acquisition, nil
		}
		reclaimed, reclaimErr := s.repo.ReclaimLockedReplacement(
			ctx, acquisition.ID, acquisition.Revision, acquisition.MutationState, now,
		)
		if errors.Is(reclaimErr, integration.ErrStaleRevision) {
			return s.repo.GetVisibleAcquisition(ctx, acquisition.ID)
		}
		if reclaimErr != nil {
			return repository.RadarrAcquisition{}, reclaimErr
		}
		acquisition = reclaimed
	default:
		return repository.RadarrAcquisition{}, domain.ErrConflict
	}
	// A locked target can only be recreated on the same snapshot. Reconciliation
	// proves remote absence before any new add request.
	instance, client, err := s.acquisitionClient(ctx, acquisition)
	if err != nil {
		return s.actionForClientFailure(ctx, acquisition, err)
	}
	if acquisition.MutationState == "idle" {
		remote, getErr := client.GetMovie(ctx, valueOrZero(acquisition.RadarrMovieID))
		if getErr == nil {
			if acquisition.ActionReason == "monitoring_failed" && !acquisition.AdoptedExisting {
				if _, monitorErr := client.SetMonitored(ctx, remote.ID, true); monitorErr != nil {
					s.recordInstanceFailure(ctx, instance, monitorErr)
					return s.repo.TransitionAcquisition(ctx, acquisition.ID, repository.RadarrAcquisitionTransition{
						Status: "action_needed", ActionReason: "monitoring_failed",
						FailureSummary: "Radarr did not enable monitoring for the selected movie.",
						QueueStatus:    acquisition.QueueStatus, At: s.now().UTC(),
					})
				}
				s.recordInstanceSuccess(ctx, instance)
			}
			if !remote.HasFile && acquisition.TargetAcquisitionMode == "automatic" &&
				acquisition.ManualAttemptCount == 0 &&
				slices.Contains([]string{"connection_failed", "no_releases", "release_failed"}, acquisition.ActionReason) {
				if acquisition.AutomaticSearchCommandID != nil && acquisition.AutomaticSearchCompletedAt == nil {
					return s.reconcileOne(ctx, acquisition)
				}
				queue, queueErr := client.Queue(ctx, remote.ID)
				if queueErr != nil {
					s.recordInstanceFailure(ctx, instance, queueErr)
					return s.actionForClientFailure(ctx, acquisition, queueErr)
				}
				if len(queue) == 0 {
					return s.startAutomaticSearch(ctx, acquisition, remote.ID)
				}
			}
			return s.reconcileOne(ctx, acquisition)
		}
		if !errors.Is(getErr, integrationradarr.ErrNotFound) {
			s.recordInstanceFailure(ctx, instance, getErr)
			return s.actionForClientFailure(ctx, acquisition, getErr)
		}
		acquisition, err = s.repo.BeginLockedReplacementCheck(
			ctx, acquisition.ID, acquisition.Revision, now,
		)
		if err != nil {
			return repository.RadarrAcquisition{}, err
		}
	}
	catalog, err := s.catalogFor(ctx, instance)
	if err != nil {
		return s.actionForClientFailure(ctx, acquisition, err)
	}
	tmdbID, ok := acquisition.ResolvedTMDBID()
	if !ok {
		return repository.RadarrAcquisition{}, domain.ErrConflict
	}
	found, lookupErr := client.FindMovieByTMDB(ctx, tmdbID)
	if lookupErr != nil {
		s.recordInstanceFailure(ctx, instance, lookupErr)
		return s.actionForClientFailure(ctx, acquisition, lookupErr)
	}
	s.recordInstanceSuccess(ctx, instance)
	if found != nil {
		// The durable pre-add state proves that this movie existed before a new
		// AddMovie request. Preserve its live configuration.
		existing := acquisition.MutationState == "checking_replacement"
		if err := s.repo.ReplaceLockedRadarrMovie(
			ctx, acquisition.ID, acquisition.Revision, found.ID, existing,
			effectiveConfiguration(*found, catalog), actorID, s.now().UTC(),
		); err != nil {
			return repository.RadarrAcquisition{}, err
		}
		current, err := s.repo.GetVisibleAcquisition(ctx, acquisition.ID)
		if err != nil {
			return repository.RadarrAcquisition{}, err
		}
		return s.continueLockedReplacement(ctx, current, *found, instance, client)
	}
	if acquisition.MutationState == "checking_replacement" {
		acquisition, err = s.repo.BeginLockedRecreation(
			ctx, acquisition.ID, acquisition.Revision, s.now().UTC(),
		)
		if err != nil {
			return repository.RadarrAcquisition{}, err
		}
	}
	return s.recreateLockedAcquisition(ctx, acquisition, instance, client, catalog, actorID)
}

func (s *radarrService) recreateLockedAcquisition(
	ctx context.Context,
	acquisition repository.RadarrAcquisition,
	instance repository.RadarrInstance,
	client integrationradarr.Client,
	catalog integrationradarr.Catalog,
	actorID int,
) (repository.RadarrAcquisition, error) {
	tmdbID, ok := acquisition.ResolvedTMDBID()
	if !ok {
		return repository.RadarrAcquisition{}, domain.ErrConflict
	}
	if acquisition.MutationState != "recreating" {
		return repository.RadarrAcquisition{}, domain.ErrConflict
	}
	fenced, err := s.repo.RenewLockedRecreationLease(
		ctx, acquisition.ID, acquisition.Revision, s.now().UTC(),
	)
	if errors.Is(err, integration.ErrStaleRevision) {
		return s.repo.GetVisibleAcquisition(context.WithoutCancel(ctx), acquisition.ID)
	}
	if err != nil {
		return repository.RadarrAcquisition{}, err
	}
	acquisition = fenced
	remote, err := client.AddMovie(ctx, integrationradarr.AddMovieRequest{
		TMDBID: tmdbID, Title: acquisition.MovieTitle,
		RootFolderPath:      acquisition.TargetRootFolderPath,
		QualityProfileID:    valueOrZero(acquisition.TargetQualityProfileID),
		TagIDs:              tagSnapshotIDs(acquisition.TargetTags),
		MinimumAvailability: integrationradarr.MinimumAvailability(acquisition.TargetMinimumAvailability),
		Mode:                integrationradarr.AcquisitionMode(acquisition.TargetAcquisitionMode),
	})
	if err != nil {
		at := s.now().UTC()
		transition := repository.RadarrAcquisitionTransition{
			Status: "action_needed", ActionReason: "add_failed",
			FailureSummary: "Radarr did not recreate the movie on the locked target.",
			QueueStatus:    "none", At: at,
		}
		if radarrMutationOutcomeAmbiguous(err) {
			next := at.Add(radarrAcquisitionPollInterval)
			transition.ActionReason = "connection_failed"
			transition.FailureSummary = "The recreate result is unknown. Retry will reconcile before sending another request."
			transition.NextCheckAt = &next
			updated, transitionErr := s.repo.TransitionAcquisitionAtRevision(
				context.WithoutCancel(ctx), acquisition.ID, acquisition.Revision, transition,
			)
			if errors.Is(transitionErr, integration.ErrStaleRevision) {
				return s.repo.GetVisibleAcquisition(context.WithoutCancel(ctx), acquisition.ID)
			}
			return updated, transitionErr
		}
		return s.repo.ResetAcquisitionMutationAtRevision(ctx, acquisition.ID, acquisition.Revision, transition)
	}
	// Replace the remote id while the target snapshot and lock stay unchanged.
	if err := s.repo.ReplaceLockedRadarrMovie(
		ctx, acquisition.ID, acquisition.Revision, remote.ID, false,
		effectiveConfiguration(remote, catalog), actorID, s.now().UTC(),
	); err != nil {
		return repository.RadarrAcquisition{}, err
	}
	current, err := s.repo.GetVisibleAcquisition(ctx, acquisition.ID)
	if err != nil {
		return repository.RadarrAcquisition{}, err
	}
	return s.continueLockedReplacement(ctx, current, remote, instance, client)
}

func (s *radarrService) continueLockedReplacement(
	ctx context.Context,
	acquisition repository.RadarrAcquisition,
	remote integrationradarr.Movie,
	instance repository.RadarrInstance,
	client integrationradarr.Client,
) (repository.RadarrAcquisition, error) {
	if remote.HasFile || acquisition.TargetAcquisitionMode != "automatic" || acquisition.ManualAttemptCount != 0 {
		return s.reconcileOne(ctx, acquisition)
	}
	queue, err := client.Queue(ctx, remote.ID)
	if err != nil {
		s.recordInstanceFailure(ctx, instance, err)
		return s.actionForClientFailure(ctx, acquisition, err)
	}
	if len(queue) > 0 {
		return s.reconcileOne(ctx, acquisition)
	}
	return s.startAutomaticSearch(ctx, acquisition, remote.ID)
}

func (s *radarrService) searchReleases(
	ctx context.Context,
	id int64,
) ([]integrationradarr.Release, error) {
	acquisition, err := s.repo.GetVisibleAcquisition(ctx, id)
	if err != nil {
		return nil, err
	}
	if acquisition.Terminal() || !acquisition.TargetLocked() || acquisition.RadarrMovieID == nil ||
		acquisition.MutationState != "idle" {
		return nil, domain.ErrConflict
	}
	instance, client, err := s.acquisitionClient(ctx, acquisition)
	if err != nil {
		return nil, err
	}
	remote, err := client.GetMovie(ctx, *acquisition.RadarrMovieID)
	if err != nil {
		s.recordInstanceFailure(ctx, instance, err)
		return nil, err
	}
	if !acquisitionMatchesRemote(acquisition, remote) {
		if err := s.recordAdminRemoteObservation(ctx, acquisition, repository.RadarrAcquisitionTransition{
			Status: "action_needed", ActionReason: "identity_required",
			FailureSummary: "The locked Radarr movie no longer matches the Acquisition identity.",
			QueueStatus:    "none", At: s.now().UTC(),
		}); err != nil {
			return nil, err
		}
		return nil, domain.ErrConflict
	}
	if remote.HasFile {
		if err := s.recordAdminRemoteObservation(ctx, acquisition, repository.RadarrAcquisitionTransition{
			Status: "downloaded", QueueStatus: "none", At: s.now().UTC(),
		}); err != nil {
			return nil, err
		}
		return nil, domain.ErrConflict
	}
	queue, err := client.Queue(ctx, remote.ID)
	if err != nil {
		s.recordInstanceFailure(ctx, instance, err)
		return nil, err
	}
	if len(queue) > 0 {
		if err := s.recordAdminObservedQueue(ctx, acquisition, queue); err != nil {
			return nil, err
		}
		return nil, domain.ErrConflict
	}
	releases, err := client.SearchReleases(ctx, remote.ID)
	if err != nil {
		s.recordInstanceFailure(ctx, instance, err)
		return nil, err
	}
	s.recordInstanceSuccess(ctx, instance)
	if len(releases) == 0 {
		if err := s.recordAdminRemoteObservation(ctx, acquisition, repository.RadarrAcquisitionTransition{
			Status: "action_needed", ActionReason: "no_releases",
			FailureSummary: "Radarr returned no matched releases.", QueueStatus: "none", At: s.now().UTC(),
		}); err != nil {
			return nil, err
		}
	}
	s.cacheAcquisitionReleases(acquisition.ID, releases)
	return releases, nil
}

func (s *radarrService) recordAdminRemoteObservation(
	ctx context.Context,
	acquisition repository.RadarrAcquisition,
	transition repository.RadarrAcquisitionTransition,
) error {
	_, err := s.transitionAdminRemoteObservation(ctx, acquisition, transition)
	return err
}

func (s *radarrService) transitionAdminRemoteObservation(
	ctx context.Context,
	acquisition repository.RadarrAcquisition,
	transition repository.RadarrAcquisitionTransition,
) (repository.RadarrAcquisition, error) {
	updated, err := s.repo.TransitionAcquisitionAtRevision(
		ctx, acquisition.ID, acquisition.Revision, transition,
	)
	if errors.Is(err, integration.ErrStaleRevision) {
		return s.repo.GetVisibleAcquisition(ctx, acquisition.ID)
	}
	return updated, err
}

func (s *radarrService) recordAdminObservedQueue(
	ctx context.Context,
	acquisition repository.RadarrAcquisition,
	queue []integrationradarr.QueueItem,
) error {
	now := s.now().UTC()
	next := now.Add(radarrAcquisitionPollInterval)
	transition := repository.RadarrAcquisitionTransition{
		NextCheckAt: &next,
		At:          now,
	}
	if reason, summary, failed := actionForQueue(queue); failed {
		transition.Status = "action_needed"
		transition.ActionReason = reason
		transition.FailureSummary = summary
		transition.QueueStatus = "failed"
		transition.QueueSummary = summary
	} else {
		transition.Status, transition.QueueStatus, transition.QueueSummary = stateForQueue(queue)
	}
	return s.recordAdminRemoteObservation(ctx, acquisition, transition)
}

func (s *radarrService) finalizeReleaseAttempt(
	ctx context.Context,
	acquisition repository.RadarrAcquisition,
	transition repository.RadarrAcquisitionTransition,
	resetMutation bool,
) (repository.RadarrAcquisition, bool, error) {
	durableCtx := context.WithoutCancel(ctx)
	var (
		updated repository.RadarrAcquisition
		err     error
	)
	if resetMutation {
		updated, err = s.repo.ResetAcquisitionMutationAtRevision(
			durableCtx, acquisition.ID, acquisition.Revision, transition,
		)
	} else {
		updated, err = s.repo.TransitionAcquisitionAtRevision(
			durableCtx, acquisition.ID, acquisition.Revision, transition,
		)
	}
	if errors.Is(err, integration.ErrStaleRevision) || errors.Is(err, domain.ErrConflict) {
		current, currentErr := s.repo.GetVisibleAcquisition(durableCtx, acquisition.ID)
		return current, true, currentErr
	}
	return updated, false, err
}

func (s *radarrService) grabRelease(
	ctx context.Context,
	id int64,
	resultID string,
	override bool,
	actorID int,
) (repository.RadarrAcquisition, error) {
	acquisition, err := s.repo.GetVisibleAcquisition(ctx, id)
	if err != nil {
		return repository.RadarrAcquisition{}, err
	}
	if acquisition.Terminal() || !acquisition.TargetLocked() || acquisition.RadarrMovieID == nil ||
		acquisition.MutationState != "idle" {
		return repository.RadarrAcquisition{}, domain.ErrConflict
	}
	instance, client, err := s.acquisitionClient(ctx, acquisition)
	if err != nil {
		return repository.RadarrAcquisition{}, err
	}
	remote, err := client.GetMovie(ctx, *acquisition.RadarrMovieID)
	if err != nil {
		s.recordInstanceFailure(ctx, instance, err)
		return repository.RadarrAcquisition{}, err
	}
	if !acquisitionMatchesRemote(acquisition, remote) {
		if err := s.recordAdminRemoteObservation(ctx, acquisition, repository.RadarrAcquisitionTransition{
			Status: "action_needed", ActionReason: "identity_required",
			FailureSummary: "The locked Radarr movie no longer matches the Acquisition identity.",
			QueueStatus:    "none", At: s.now().UTC(),
		}); err != nil {
			return repository.RadarrAcquisition{}, err
		}
		return repository.RadarrAcquisition{}, domain.ErrConflict
	}
	if remote.HasFile {
		if err := s.recordAdminRemoteObservation(ctx, acquisition, repository.RadarrAcquisitionTransition{
			Status: "downloaded", QueueStatus: "none", At: s.now().UTC(),
		}); err != nil {
			return repository.RadarrAcquisition{}, err
		}
		return repository.RadarrAcquisition{}, domain.ErrConflict
	}
	queue, err := client.Queue(ctx, remote.ID)
	if err != nil {
		s.recordInstanceFailure(ctx, instance, err)
		return repository.RadarrAcquisition{}, err
	}
	if len(queue) > 0 {
		if err := s.recordAdminObservedQueue(ctx, acquisition, queue); err != nil {
			return repository.RadarrAcquisition{}, err
		}
		return repository.RadarrAcquisition{}, domain.ErrConflict
	}
	// Resolve the sanitized summary before the cache is consumed. The client
	// intentionally exposes no raw release key.
	resultID = strings.TrimSpace(resultID)
	if resultID == "" {
		return repository.RadarrAcquisition{}, invalidRadarrField("resultId", "Select a release.")
	}
	selected, ok := s.cachedAcquisitionRelease(acquisition.ID, resultID)
	if !ok {
		return repository.RadarrAcquisition{}, integrationradarr.ErrReleaseExpired
	}
	if selected.Rejected && !override {
		return repository.RadarrAcquisition{}, integrationradarr.ErrRejectedRelease
	}
	// The selected release summary is supplied by the short-lived cache only.
	// Search results are not persisted as a separate attempt table.
	acquisition, err = s.repo.BeginReleaseAttempt(
		ctx, acquisition.ID, acquisition.Revision,
		selected.Title, selected.Quality.Name, actorID, s.now().UTC(),
	)
	if err != nil {
		return repository.RadarrAcquisition{}, err
	}
	if err := client.GrabRelease(ctx, integrationradarr.GrabReleaseRequest{
		ResultID: resultID, AllowRejected: override,
	}); err != nil {
		if errors.Is(err, integrationradarr.ErrReleaseExpired) ||
			errors.Is(err, integrationradarr.ErrRejectedRelease) {
			current, stale, transitionErr := s.finalizeReleaseAttempt(ctx, acquisition, repository.RadarrAcquisitionTransition{
				Status: "needs_release", ActionReason: "release_required",
				QueueStatus: "none", At: s.now().UTC(),
			}, true)
			if transitionErr != nil {
				return repository.RadarrAcquisition{}, transitionErr
			}
			if stale {
				return current, nil
			}
			return repository.RadarrAcquisition{}, err
		}
		s.recordInstanceFailure(ctx, instance, err)
		reason := "release_failed"
		ambiguous := radarrMutationOutcomeAmbiguous(err)
		if ambiguous || errors.Is(err, integrationradarr.ErrAuthentication) {
			reason = "connection_failed"
		}
		at := s.now().UTC()
		transition := repository.RadarrAcquisitionTransition{
			Status: "action_needed", ActionReason: reason,
			FailureSummary: "Radarr did not accept the selected release.",
			QueueStatus:    "none", At: at,
		}
		if ambiguous {
			next := at.Add(radarrAcquisitionPollInterval)
			transition.NextCheckAt = &next
		}
		current, stale, transitionErr := s.finalizeReleaseAttempt(ctx, acquisition, transition, !ambiguous)
		if transitionErr != nil {
			return repository.RadarrAcquisition{}, transitionErr
		}
		if stale {
			return current, nil
		}
		return repository.RadarrAcquisition{}, err
	}
	s.recordInstanceSuccess(ctx, instance)
	if !acquisition.AdoptedExisting {
		if _, err := client.SetMonitored(ctx, *acquisition.RadarrMovieID, true); err != nil {
			s.recordInstanceFailure(ctx, instance, err)
			updated, _, transitionErr := s.finalizeReleaseAttempt(ctx, acquisition, repository.RadarrAcquisitionTransition{
				Status: "action_needed", ActionReason: "monitoring_failed",
				FailureSummary: "The release was sent, but Radarr did not enable monitoring.",
				QueueStatus:    "queued", At: s.now().UTC(),
			}, true)
			return updated, transitionErr
		}
	}
	next := s.now().UTC().Add(radarrAcquisitionPollInterval)
	updated, _, err := s.finalizeReleaseAttempt(ctx, acquisition, repository.RadarrAcquisitionTransition{
		Status: "queued", QueueStatus: "queued", NextCheckAt: &next, At: s.now().UTC(),
	}, true)
	return updated, err
}

func (s *radarrService) abandonAcquisition(
	ctx context.Context,
	id int64,
	actorID int,
	reason, acknowledgedActivity string,
) (repository.RadarrAcquisition, error) {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return repository.RadarrAcquisition{}, invalidRadarrField("reason", "Reason is required.")
	}
	if len(reason) > 500 {
		return repository.RadarrAcquisition{}, invalidRadarrField("reason", "Reason must be 500 characters or fewer.")
	}
	review, err := s.reviewAbandonment(ctx, id)
	if err != nil {
		return repository.RadarrAcquisition{}, err
	}
	if review.Acquisition.Terminal() {
		return repository.RadarrAcquisition{}, domain.ErrConflict
	}
	if (review.Activity == "active" || review.Activity == "unavailable") &&
		acknowledgedActivity != review.Activity {
		return repository.RadarrAcquisition{}, domain.ErrConflict
	}
	return s.repo.AbandonAcquisition(
		ctx, id, review.Acquisition.Revision, actorID, reason, s.now().UTC(),
	)
}

func (s *radarrService) reviewAbandonment(
	ctx context.Context,
	id int64,
) (radarrAbandonmentReview, error) {
	acquisition, err := s.repo.GetVisibleAcquisition(ctx, id)
	if err != nil {
		return radarrAbandonmentReview{}, err
	}
	if acquisition.Status == "downloaded" {
		return radarrAbandonmentReview{Acquisition: acquisition, Activity: "complete"}, nil
	}
	if acquisition.Status == "abandoned" {
		return radarrAbandonmentReview{}, domain.ErrConflict
	}
	if !acquisition.TargetLocked() || acquisition.RadarrMovieID == nil {
		activity := "not_applicable"
		if acquisition.MutationState != "idle" {
			activity = "unavailable"
		}
		return radarrAbandonmentReview{Acquisition: acquisition, Activity: activity}, nil
	}

	instance, client, err := s.acquisitionClient(ctx, acquisition)
	if err != nil {
		updated, updateErr := s.transitionAbandonmentObservation(
			ctx, acquisition, radarrClientFailureTransition(acquisition, err, s.now().UTC()), false,
		)
		return radarrAbandonmentReview{Acquisition: updated, Activity: "unavailable"}, updateErr
	}
	remote, err := client.GetMovie(ctx, *acquisition.RadarrMovieID)
	if err != nil && !errors.Is(err, integrationradarr.ErrNotFound) {
		s.recordInstanceFailure(ctx, instance, err)
		updated, updateErr := s.transitionAbandonmentObservation(
			ctx, acquisition, radarrClientFailureTransition(acquisition, err, s.now().UTC()), false,
		)
		return radarrAbandonmentReview{Acquisition: updated, Activity: "unavailable"}, updateErr
	}
	if err == nil && acquisitionMatchesRemote(acquisition, remote) && remote.HasFile {
		s.recordInstanceSuccess(ctx, instance)
		transition := repository.RadarrAcquisitionTransition{
			Status: "downloaded", QueueStatus: "none", At: s.now().UTC(),
		}
		var updated repository.RadarrAcquisition
		var updateErr error
		updated, updateErr = s.transitionAbandonmentObservation(
			ctx, acquisition, transition, acquisition.MutationState != "idle",
		)
		return radarrAbandonmentReview{Acquisition: updated, Activity: "complete"}, updateErr
	}

	queue, queueErr := client.Queue(ctx, *acquisition.RadarrMovieID)
	if queueErr != nil {
		s.recordInstanceFailure(ctx, instance, queueErr)
		updated, updateErr := s.transitionAbandonmentObservation(
			ctx, acquisition, radarrClientFailureTransition(acquisition, queueErr, s.now().UTC()), false,
		)
		return radarrAbandonmentReview{Acquisition: updated, Activity: "unavailable"}, updateErr
	}
	s.recordInstanceSuccess(ctx, instance)
	transition := abandonmentQueueTransition(acquisition, remote, err, queue, s.now().UTC())
	if acquisition.MutationState == "searching" && len(queue) == 0 &&
		!errors.Is(err, integrationradarr.ErrNotFound) && acquisitionMatchesRemote(acquisition, remote) {
		at := s.now().UTC()
		next := at.Add(radarrAcquisitionPollInterval)
		updated, updateErr := s.transitionAbandonmentObservation(
			ctx,
			acquisition,
			repository.RadarrAcquisitionTransition{
				Status: "action_needed", ActionReason: "connection_failed",
				FailureSummary: radarrAutomaticSearchUnknownReason,
				QueueStatus:    "none", NextCheckAt: &next, At: at,
			},
			false,
		)
		return radarrAbandonmentReview{Acquisition: updated, Activity: "unavailable"}, updateErr
	}
	var updated repository.RadarrAcquisition
	if acquisition.MutationState == "searching" {
		updated, err = s.transitionAbandonmentObservation(ctx, acquisition, transition, true)
	} else {
		updated, err = s.transitionAbandonmentObservation(ctx, acquisition, transition, false)
	}
	if err != nil {
		return radarrAbandonmentReview{}, err
	}
	activity := "inactive"
	if len(queue) > 0 {
		activity = "active"
	} else if acquisition.MutationState != "idle" {
		activity = "unavailable"
	}
	if updated.Terminal() {
		activity = "complete"
	}
	return radarrAbandonmentReview{Acquisition: updated, Activity: activity}, nil
}

func (s *radarrService) transitionAbandonmentObservation(
	ctx context.Context,
	acquisition repository.RadarrAcquisition,
	transition repository.RadarrAcquisitionTransition,
	resetMutation bool,
) (repository.RadarrAcquisition, error) {
	if resetMutation {
		return s.repo.ResetAcquisitionMutationAtRevision(
			ctx, acquisition.ID, acquisition.Revision, transition,
		)
	}
	if acquisition.MutationState != "idle" {
		// Abandonment review is observational. Do not invalidate or shorten an
		// in-flight mutation claim. The final abandon command still uses the
		// observed revision as its compare-and-swap boundary.
		return acquisition, nil
	}
	return s.repo.TransitionAcquisitionAtRevision(
		ctx, acquisition.ID, acquisition.Revision, transition,
	)
}

func abandonmentQueueTransition(
	acquisition repository.RadarrAcquisition,
	remote integrationradarr.Movie,
	remoteErr error,
	queue []integrationradarr.QueueItem,
	at time.Time,
) repository.RadarrAcquisitionTransition {
	transition := repository.RadarrAcquisitionTransition{
		Status: acquisition.Status, ActionReason: acquisition.ActionReason,
		QueueStatus: "none", At: at,
	}
	targetIssue := false
	if errors.Is(remoteErr, integrationradarr.ErrNotFound) {
		targetIssue = true
		transition.Status = "action_needed"
		transition.ActionReason = "add_failed"
		transition.FailureSummary = "The movie was removed from the locked Radarr target."
	} else if !acquisitionMatchesRemote(acquisition, remote) {
		targetIssue = true
		transition.Status = "action_needed"
		transition.ActionReason = "identity_required"
		transition.FailureSummary = "The locked Radarr movie no longer matches the Acquisition identity."
	}
	if len(queue) == 0 {
		if slices.Contains([]string{"queued", "downloading", "importing"}, transition.Status) {
			transition.Status = "waiting_for_radarr"
			transition.ActionReason = ""
		}
		return transition
	}
	next := at.Add(radarrAcquisitionPollInterval)
	transition.NextCheckAt = &next
	if reason, summary, failed := actionForQueue(queue); failed {
		transition.QueueStatus = "failed"
		transition.QueueSummary = summary
		if !targetIssue {
			transition.Status = "action_needed"
			transition.ActionReason = reason
			transition.FailureSummary = summary
		}
		return transition
	}
	_, transition.QueueStatus, transition.QueueSummary = stateForQueue(queue)
	if !targetIssue {
		transition.Status, _, _ = stateForQueue(queue)
		transition.ActionReason = ""
	}
	return transition
}

func (s *radarrService) resolveAcquisitionIdentity(
	ctx context.Context,
	acquisition repository.RadarrAcquisition,
	client integrationradarr.Client,
) (repository.RadarrAcquisition, int, error) {
	if tmdbID, ok := acquisition.ResolvedTMDBID(); ok {
		return acquisition, tmdbID, nil
	}
	if acquisition.IMDbID == "" {
		return acquisition, 0, errRadarrIdentityRequired
	}
	identity, err := integrationradarr.IMDbIdentity(acquisition.IMDbID)
	if err != nil {
		return acquisition, 0, errRadarrIdentityMismatch
	}
	candidate, err := client.LookupMovie(ctx, identity)
	if err != nil {
		return acquisition, 0, err
	}
	if candidate.TMDBID <= 0 || !strings.EqualFold(candidate.IMDbID, acquisition.IMDbID) {
		return acquisition, 0, errRadarrIdentityMismatch
	}
	resolved, err := s.repo.ResolveIMDbIdentity(
		ctx, acquisition.ID, acquisition.Revision, candidate.TMDBID, s.now().UTC(),
	)
	if err != nil {
		return acquisition, 0, err
	}
	return resolved, candidate.TMDBID, nil
}

func (s *radarrService) acquisitionClient(
	ctx context.Context,
	acquisition repository.RadarrAcquisition,
) (repository.RadarrInstance, integrationradarr.Client, error) {
	if acquisition.TargetInstanceID == nil {
		return repository.RadarrInstance{}, nil, domain.ErrConflict
	}
	instance, err := s.repo.GetInstance(ctx, *acquisition.TargetInstanceID)
	if err != nil {
		return repository.RadarrInstance{}, nil, err
	}
	client, err := s.clientFor(instance)
	if err != nil {
		state := radarrInstanceOffline
		reason := "Stored Radarr configuration could not create a client."
		if errors.Is(err, integration.ErrCredentialUnavailable) {
			state = radarrInstanceCredentialUnavailable
			reason = "Stored API key cannot be decrypted with the current integration key."
		}
		_ = s.repo.UpdateInstanceState(ctx, instance.ID, state, reason, s.now().UTC())
	}
	return instance, client, err
}

func acquisitionMatchesRemote(
	acquisition repository.RadarrAcquisition,
	remote integrationradarr.Movie,
) bool {
	tmdbID, ok := acquisition.ResolvedTMDBID()
	return ok && remote.TMDBID == tmdbID
}

func validateAcquisitionTargetSnapshot(
	acquisition repository.RadarrAcquisition,
	catalog integrationradarr.Catalog,
) error {
	if acquisition.TargetRootFolderID == nil || acquisition.TargetQualityProfileID == nil {
		return errors.New("target snapshot is incomplete")
	}
	rootOK := slices.ContainsFunc(catalog.RootFolders, func(root integrationradarr.RootFolder) bool {
		return root.ID == *acquisition.TargetRootFolderID && root.Path == acquisition.TargetRootFolderPath && root.Accessible
	})
	profileOK := slices.ContainsFunc(catalog.QualityProfiles, func(profile integrationradarr.QualityProfile) bool {
		return profile.ID == *acquisition.TargetQualityProfileID
	})
	if !rootOK || !profileOK {
		return errors.New("target root folder or quality profile no longer exists")
	}
	for _, selected := range acquisition.TargetTags {
		if !slices.ContainsFunc(catalog.Tags, func(tag integrationradarr.Tag) bool { return tag.ID == selected.ID }) {
			return errors.New("target tag no longer exists")
		}
	}
	return nil
}

func effectiveConfiguration(
	movie integrationradarr.Movie,
	catalog integrationradarr.Catalog,
) repository.RadarrEffectiveConfiguration {
	configuration := repository.RadarrEffectiveConfiguration{
		RootFolderPath: movie.RootFolderPath, QualityProfileID: movie.QualityProfileID,
		MinimumAvailability: string(movie.MinimumAvailability), Monitored: movie.Monitored,
	}
	if index := slices.IndexFunc(catalog.QualityProfiles, func(profile integrationradarr.QualityProfile) bool {
		return profile.ID == movie.QualityProfileID
	}); index >= 0 {
		configuration.QualityProfileName = catalog.QualityProfiles[index].Name
	}
	for _, id := range movie.TagIDs {
		label := ""
		if index := slices.IndexFunc(catalog.Tags, func(tag integrationradarr.Tag) bool { return tag.ID == id }); index >= 0 {
			label = catalog.Tags[index].Label
		}
		configuration.Tags = append(configuration.Tags, repository.RadarrTagSnapshot{ID: id, Label: label})
	}
	return configuration
}

func sameEffectiveConfiguration(
	a, b repository.RadarrEffectiveConfiguration,
) bool {
	return a.RootFolderPath == b.RootFolderPath &&
		a.QualityProfileID == b.QualityProfileID &&
		a.QualityProfileName == b.QualityProfileName &&
		a.MinimumAvailability == b.MinimumAvailability &&
		a.Monitored == b.Monitored &&
		slices.Equal(a.Tags, b.Tags)
}

func stateForQueue(queue []integrationradarr.QueueItem) (status, queueStatus, summary string) {
	status, queueStatus = "queued", "queued"
	for _, item := range queue {
		state := strings.ToLower(item.Status + " " + item.TrackedDownloadState + " " + item.TrackedDownloadStatus)
		switch {
		case strings.Contains(state, "import"):
			return "importing", "importing", "Radarr is importing the selected release."
		case strings.Contains(state, "download"):
			status, queueStatus = "downloading", "downloading"
		}
	}
	if status == "downloading" {
		return status, queueStatus, "Radarr is downloading the selected release."
	}
	return status, queueStatus, "Radarr has an active queue item."
}

func actionForQueue(queue []integrationradarr.QueueItem) (reason, summary string, failed bool) {
	for _, item := range queue {
		state := strings.ToLower(strings.TrimSpace(item.TrackedDownloadState))
		if state == "importblocked" {
			return "import_failed", "Radarr reports that the download is blocked from import.", true
		}
		trackedStatus := strings.ToLower(strings.TrimSpace(item.TrackedDownloadStatus))
		queueStatus := strings.ToLower(strings.TrimSpace(item.Status))
		if state == "failedpending" || state == "failed" || trackedStatus == "error" || queueStatus == "failed" {
			return "release_failed", "Radarr reports that the selected download failed.", true
		}
	}
	return "", "", false
}

func (s *radarrService) actionForClientFailure(
	ctx context.Context,
	acquisition repository.RadarrAcquisition,
	err error,
) (repository.RadarrAcquisition, error) {
	return s.transitionAdminRemoteObservation(
		ctx, acquisition, radarrClientFailureTransition(acquisition, err, s.now().UTC()),
	)
}

func radarrClientFailureTransition(
	acquisition repository.RadarrAcquisition,
	err error,
	at time.Time,
) repository.RadarrAcquisitionTransition {
	reason := "connection_failed"
	if errors.Is(err, integrationradarr.ErrValidation) {
		reason = "configuration_invalid"
	}
	return repository.RadarrAcquisitionTransition{
		Status: "action_needed", ActionReason: reason,
		FailureSummary: "Radarr could not complete the requested check.",
		QueueStatus:    acquisition.QueueStatus, At: at,
	}
}

func (s *radarrService) recordInstanceFailure(
	ctx context.Context,
	instance repository.RadarrInstance,
	err error,
) {
	if !errors.Is(err, integrationradarr.ErrAuthentication) &&
		!errors.Is(err, integrationradarr.ErrTransient) &&
		!errors.Is(err, integrationradarr.ErrRemote) &&
		!errors.Is(err, integrationradarr.ErrInvalidResponse) &&
		!errors.Is(err, integrationradarr.ErrResponseTooLarge) &&
		!errors.Is(err, context.DeadlineExceeded) {
		return
	}
	state := radarrInstanceOffline
	reason := "Radarr could not be reached or rejected the request."
	if errors.Is(err, integrationradarr.ErrAuthentication) {
		reason = "Radarr rejected the API key."
	}
	_ = s.repo.UpdateInstanceState(ctx, instance.ID, state, reason, s.now().UTC())
}

func (s *radarrService) recordInstanceSuccess(ctx context.Context, instance repository.RadarrInstance) {
	if instance.State != radarrInstanceConnected || instance.StateReason != "" {
		_ = s.repo.UpdateInstanceState(ctx, instance.ID, radarrInstanceConnected, "", s.now().UTC())
	}
}

func tagSnapshotIDs(tags []repository.RadarrTagSnapshot) []int {
	ids := make([]int, 0, len(tags))
	for _, tag := range tags {
		ids = append(ids, tag.ID)
	}
	return ids
}

func (s *radarrService) cacheAcquisitionReleases(
	acquisitionID int64,
	releases []integrationradarr.Release,
) {
	now := s.now().UTC()
	expiresAt := now.Add(30 * time.Minute)
	s.releasesMu.Lock()
	defer s.releasesMu.Unlock()
	for id, cached := range s.releases {
		if !cached.expiresAt.After(now) || cached.acquisitionID == acquisitionID {
			delete(s.releases, id)
		}
	}
	for _, release := range releases {
		s.releases[release.ID] = cachedAcquisitionRelease{
			acquisitionID: acquisitionID, release: release, expiresAt: expiresAt,
		}
	}
}

func (s *radarrService) cachedAcquisitionRelease(
	acquisitionID int64,
	resultID string,
) (integrationradarr.Release, bool) {
	now := s.now().UTC()
	s.releasesMu.Lock()
	defer s.releasesMu.Unlock()
	cached, ok := s.releases[resultID]
	if !ok || cached.acquisitionID != acquisitionID || !cached.expiresAt.After(now) {
		delete(s.releases, resultID)
		return integrationradarr.Release{}, false
	}
	return cached.release, true
}

func valueOrZero[T ~int | ~int64](value *T) T {
	if value == nil {
		return 0
	}
	return *value
}

var (
	errRadarrIdentityRequired = errors.New("missing Radarr identity")
	errRadarrIdentityMismatch = errors.New("mismatched Radarr identity")
)
