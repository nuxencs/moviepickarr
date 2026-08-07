package server

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"moviepickarr/internal/domain"
	"moviepickarr/internal/integration"
	integrationradarr "moviepickarr/internal/integration/radarr"
	"moviepickarr/internal/repository"
)

const (
	radarrReconcileInterval   = 30 * time.Second
	radarrGrabObservationTime = 2 * time.Minute
)

func (s *radarrService) reconcileDue(ctx context.Context, limit int) (int, error) {
	acquisitions, err := s.repo.DueAcquisitions(ctx, s.now().UTC(), limit)
	if err != nil {
		return 0, err
	}
	processed := 0
	var joined error
	for _, acquisition := range acquisitions {
		if _, err := s.reconcileOne(ctx, acquisition); err != nil {
			joined = errors.Join(joined, fmt.Errorf("reconcile acquisition %d: %w", acquisition.ID, err))
		}
		processed++
	}
	return processed, joined
}

func (s *radarrService) reconcileOne(
	ctx context.Context,
	acquisition repository.RadarrAcquisition,
) (repository.RadarrAcquisition, error) {
	if acquisition.Terminal() || !acquisition.TargetLocked() || acquisition.RadarrMovieID == nil {
		return acquisition, nil
	}
	if acquisition.MutationState == "searching" {
		return s.recoverAutomaticSearch(ctx, acquisition)
	}
	instance, client, err := s.acquisitionClient(ctx, acquisition)
	if err != nil {
		return s.reconcileClientFailure(ctx, acquisition, err)
	}
	remote, err := client.GetMovie(ctx, *acquisition.RadarrMovieID)
	if err != nil {
		s.recordInstanceFailure(ctx, instance, err)
		if errors.Is(err, integrationradarr.ErrNotFound) {
			return s.reconcileTransition(ctx, acquisition, repository.RadarrAcquisitionTransition{
				Status: "action_needed", ActionReason: "add_failed",
				FailureSummary: "The movie was removed from the locked Radarr target. Recreate it explicitly to continue.",
				QueueStatus:    "none", At: s.now().UTC(),
			})
		}
		return s.reconcileClientFailure(ctx, acquisition, err)
	}
	if tmdbID, ok := acquisition.ResolvedTMDBID(); !ok || remote.TMDBID != tmdbID {
		return s.reconcileTransition(ctx, acquisition, repository.RadarrAcquisitionTransition{
			Status: "action_needed", ActionReason: "identity_required",
			FailureSummary: "The locked Radarr movie no longer matches the Acquisition identity.",
			QueueStatus:    "none", At: s.now().UTC(),
		})
	}
	s.recordInstanceSuccess(ctx, instance)
	if remote.HasFile {
		if shouldRestoreManualMonitoring(acquisition, remote) {
			remote, err = s.restoreManualMonitoring(ctx, instance, client, remote)
			if err != nil {
				return s.reconcileTransition(ctx, acquisition, repository.RadarrAcquisitionTransition{
					Status: "action_needed", ActionReason: "monitoring_failed",
					FailureSummary: "The movie file is available, but Radarr did not enable monitoring.",
					QueueStatus:    "none", At: s.now().UTC(),
				})
			}
		}
		return s.reconcileTransition(ctx, acquisition, repository.RadarrAcquisitionTransition{
			Status: "downloaded", QueueStatus: "none", At: s.now().UTC(),
		})
	}
	queue, err := client.Queue(ctx, remote.ID)
	if err != nil {
		s.recordInstanceFailure(ctx, instance, err)
		return s.reconcileClientFailure(ctx, acquisition, err)
	}
	if len(queue) > 0 {
		if shouldRestoreManualMonitoring(acquisition, remote) {
			remote, err = s.restoreManualMonitoring(ctx, instance, client, remote)
			if err != nil {
				next := s.now().UTC().Add(radarrAcquisitionPollInterval)
				return s.reconcileTransition(ctx, acquisition, repository.RadarrAcquisitionTransition{
					Status: "action_needed", ActionReason: "monitoring_failed",
					FailureSummary: "The selected release is active, but Radarr did not enable monitoring.",
					QueueStatus:    "queued", NextCheckAt: &next, At: s.now().UTC(),
				})
			}
		}
		if reason, summary, failed := actionForQueue(queue); failed {
			next := s.now().UTC().Add(radarrAcquisitionPollInterval)
			return s.reconcileTransition(ctx, acquisition, repository.RadarrAcquisitionTransition{
				Status: "action_needed", ActionReason: reason,
				FailureSummary: summary, QueueStatus: "failed", QueueSummary: summary,
				NextCheckAt: &next, At: s.now().UTC(),
			})
		}
		status, queueStatus, summary := stateForQueue(queue)
		next := s.now().UTC().Add(radarrAcquisitionPollInterval)
		return s.reconcileTransition(ctx, acquisition, repository.RadarrAcquisitionTransition{
			Status: status, QueueStatus: queueStatus, QueueSummary: summary,
			NextCheckAt: &next, At: s.now().UTC(),
		})
	}
	if acquisition.AutomaticSearchCommandID != nil {
		command, err := client.GetCommand(ctx, *acquisition.AutomaticSearchCommandID)
		if err != nil {
			if errors.Is(err, integrationradarr.ErrNotFound) {
				if acquisition.AutomaticSearchCompletedAt != nil {
					reason, summary := completedAutomaticSearchFallback(acquisition)
					return s.reconcileTransition(ctx, acquisition, repository.RadarrAcquisitionTransition{
						Status: "action_needed", ActionReason: reason,
						FailureSummary: summary, QueueStatus: "none", At: s.now().UTC(),
					})
				}
				command, err = s.findReplacementAutomaticSearchCommand(ctx, acquisition, instance, client, remote.ID)
				if err != nil {
					return s.preserveUnknownAutomaticSearch(ctx, acquisition, s.now().UTC())
				}
				if command.ID == 0 {
					return s.preserveUnknownAutomaticSearch(ctx, acquisition, s.now().UTC())
				}
			} else {
				return s.reconcileClientFailure(ctx, acquisition, err)
			}
		}
		return s.reconcileAutomaticSearchCommand(ctx, acquisition, client, remote.ID, command)
	}

	if acquisition.ManualAttemptCount == 0 {
		if acquisition.TargetAcquisitionMode == "automatic" {
			return s.reconcileTransition(ctx, acquisition, repository.RadarrAcquisitionTransition{
				Status: "action_needed", ActionReason: "release_failed",
				FailureSummary: "Radarr has no file, queue item, or active automatic search.",
				QueueStatus:    "none", At: s.now().UTC(),
			})
		}
		return s.reconcileTransition(ctx, acquisition, repository.RadarrAcquisitionTransition{
			Status: "needs_release", ActionReason: "release_required",
			QueueStatus: "none", At: s.now().UTC(),
		})
	}
	if acquisition.MutationState == "grabbing" && acquisition.LatestReleaseSelectedAt != nil &&
		s.now().UTC().Sub(*acquisition.LatestReleaseSelectedAt) < radarrGrabObservationTime {
		next := s.now().UTC().Add(radarrAcquisitionPollInterval)
		return s.reconcileProgress(ctx, acquisition, repository.RadarrAcquisitionTransition{
			Status: "waiting_for_radarr", QueueStatus: "none", NextCheckAt: &next, At: s.now().UTC(),
		})
	}
	if acquisition.MutationState == "idle" && acquisition.ActionReason == "release_required" {
		next := s.now().UTC().Add(radarrAcquisitionPollInterval)
		return s.reconcileTransition(ctx, acquisition, repository.RadarrAcquisitionTransition{
			Status: "needs_release", ActionReason: "release_required",
			QueueStatus: "none", NextCheckAt: &next, At: s.now().UTC(),
		})
	}

	history, err := client.History(ctx, remote.ID)
	if err != nil {
		return s.reconcileClientFailure(ctx, acquisition, err)
	}
	if acquisition.MutationState == "grabbing" && historyShowsAcceptedGrab(history, acquisition.LatestReleaseSelectedAt) &&
		shouldRestoreManualMonitoring(acquisition, remote) {
		if _, err := s.restoreManualMonitoring(ctx, instance, client, remote); err != nil {
			return s.reconcileTransition(ctx, acquisition, repository.RadarrAcquisitionTransition{
				Status: "action_needed", ActionReason: "monitoring_failed",
				FailureSummary: "Radarr accepted the selected release but did not enable monitoring.",
				QueueStatus:    "none", At: s.now().UTC(),
			})
		}
	}
	reason, summary := historyFailure(history, acquisition.LatestReleaseSelectedAt)
	if reason == "" {
		reason = "release_failed"
		summary = "The selected release is no longer queued and no file was imported."
	}
	return s.reconcileTransition(ctx, acquisition, repository.RadarrAcquisitionTransition{
		Status: "action_needed", ActionReason: reason,
		FailureSummary: summary, QueueStatus: "none", At: s.now().UTC(),
	})
}

func (s *radarrService) findReplacementAutomaticSearchCommand(
	ctx context.Context,
	acquisition repository.RadarrAcquisition,
	instance repository.RadarrInstance,
	client integrationradarr.Client,
	movieID int,
) (integrationradarr.Command, error) {
	command, err := client.FindRecentMoviesSearchCommand(
		ctx, movieID, automaticSearchCommandBoundary(acquisition).Add(-radarrAutomaticSearchClockSkew),
	)
	if err != nil {
		s.recordInstanceFailure(ctx, instance, err)
		return integrationradarr.Command{}, err
	}
	s.recordInstanceSuccess(ctx, instance)
	if command == nil {
		return integrationradarr.Command{}, nil
	}
	return *command, nil
}

func automaticSearchCommandBoundary(acquisition repository.RadarrAcquisition) time.Time {
	if acquisition.AutomaticSearchClaimedAt != nil {
		return *acquisition.AutomaticSearchClaimedAt
	}
	if acquisition.TargetLockedAt != nil {
		return *acquisition.TargetLockedAt
	}
	return acquisition.CreatedAt
}

func (s *radarrService) reconcileAutomaticSearchCommand(
	ctx context.Context,
	acquisition repository.RadarrAcquisition,
	client integrationradarr.Client,
	movieID int,
	command integrationradarr.Command,
) (repository.RadarrAcquisition, error) {
	commandStatus := strings.ToLower(strings.TrimSpace(command.Status))
	switch commandStatus {
	case "completed", "failed", "aborted", "cancelled", "canceled":
		if acquisition.AutomaticSearchCompletedAt == nil {
			if err := s.repo.CompleteAutomaticSearch(
				ctx, acquisition.ID, acquisition.Revision,
				*acquisition.AutomaticSearchCommandID, s.now().UTC(),
			); errors.Is(err, integration.ErrStaleRevision) {
				return s.repo.GetVisibleAcquisition(ctx, acquisition.ID)
			} else if err != nil {
				return repository.RadarrAcquisition{}, err
			}
			var err error
			acquisition, err = s.repo.GetVisibleAcquisition(ctx, acquisition.ID)
			if err != nil {
				return repository.RadarrAcquisition{}, err
			}
		}
		fallbackReason := "release_failed"
		fallbackSummary := "The automatic Radarr search failed."
		if commandStatus == "completed" {
			fallbackReason = "no_releases"
			fallbackSummary = "The automatic search completed without a file or active queue item."
		}
		return s.reconcileFinishedAutomaticSearch(
			ctx, acquisition, client, movieID, command, fallbackReason, fallbackSummary,
		)
	default:
		if acquisition.AutomaticSearchCompletedAt != nil {
			reason, summary := completedAutomaticSearchFallback(acquisition)
			return s.reconcileTransition(ctx, acquisition, repository.RadarrAcquisitionTransition{
				Status: "action_needed", ActionReason: reason,
				FailureSummary: summary, QueueStatus: "none", At: s.now().UTC(),
			})
		}
		next := s.now().UTC().Add(radarrAcquisitionPollInterval)
		return s.reconcileTransition(ctx, acquisition, repository.RadarrAcquisitionTransition{
			Status: "waiting_for_radarr", QueueStatus: "none",
			NextCheckAt: &next, At: s.now().UTC(),
		})
	}
}

func (s *radarrService) reconcileFinishedAutomaticSearch(
	ctx context.Context,
	acquisition repository.RadarrAcquisition,
	client integrationradarr.Client,
	movieID int,
	command integrationradarr.Command,
	fallbackReason string,
	fallbackSummary string,
) (repository.RadarrAcquisition, error) {
	history, err := client.History(ctx, movieID)
	if err != nil {
		return s.reconcileClientFailure(ctx, acquisition, err)
	}
	reason, summary := historyFailure(history, automaticSearchHistoryStart(acquisition, command))
	if reason == "" {
		reason = fallbackReason
		summary = fallbackSummary
	}
	return s.reconcileTransition(ctx, acquisition, repository.RadarrAcquisitionTransition{
		Status: "action_needed", ActionReason: reason,
		FailureSummary: summary, QueueStatus: "none", At: s.now().UTC(),
	})
}

func automaticSearchHistoryStart(
	acquisition repository.RadarrAcquisition,
	command integrationradarr.Command,
) *time.Time {
	if !command.Queued.IsZero() {
		queued := command.Queued.UTC()
		return &queued
	}
	if command.Started != nil && !command.Started.IsZero() {
		started := command.Started.UTC()
		return &started
	}
	if acquisition.AutomaticSearchClaimedAt != nil {
		return acquisition.AutomaticSearchClaimedAt
	}
	return acquisition.TargetLockedAt
}

func completedAutomaticSearchFallback(acquisition repository.RadarrAcquisition) (string, string) {
	switch acquisition.ActionReason {
	case "no_releases", "release_failed", "import_failed":
		return acquisition.ActionReason, acquisition.LatestFailureSummary
	default:
		return "release_failed", "Radarr has no file, queue item, or active automatic search."
	}
}

func (s *radarrService) reconcileClientFailure(
	ctx context.Context,
	acquisition repository.RadarrAcquisition,
	err error,
) (repository.RadarrAcquisition, error) {
	return s.reconcileTransition(
		ctx, acquisition, radarrClientFailureTransition(acquisition, err, s.now().UTC()),
	)
}

func shouldRestoreManualMonitoring(
	acquisition repository.RadarrAcquisition,
	remote integrationradarr.Movie,
) bool {
	return acquisition.TargetAcquisitionMode == "manual" &&
		!acquisition.AdoptedExisting &&
		!remote.Monitored &&
		(acquisition.MutationState == "grabbing" || acquisition.ActionReason == "monitoring_failed")
}

func (s *radarrService) restoreManualMonitoring(
	ctx context.Context,
	instance repository.RadarrInstance,
	client integrationradarr.Client,
	remote integrationradarr.Movie,
) (integrationradarr.Movie, error) {
	updated, err := client.SetMonitored(ctx, remote.ID, true)
	if err != nil {
		s.recordInstanceFailure(ctx, instance, err)
		return remote, err
	}
	s.recordInstanceSuccess(ctx, instance)
	return updated, nil
}

func historyShowsAcceptedGrab(history []integrationradarr.HistoryItem, since *time.Time) bool {
	for _, item := range history {
		if since != nil && item.Date.Before(*since) {
			continue
		}
		switch strings.ToLower(item.EventType) {
		case "grabbed", "downloadfailed", "downloadfolderimported", "moviefolderimported":
			return true
		}
	}
	return false
}

func (s *radarrService) reconcileTransition(
	ctx context.Context,
	acquisition repository.RadarrAcquisition,
	transition repository.RadarrAcquisitionTransition,
) (repository.RadarrAcquisition, error) {
	var (
		updated repository.RadarrAcquisition
		err     error
	)
	if acquisition.MutationState == "grabbing" {
		updated, err = s.repo.ResetAcquisitionMutationAtRevision(
			ctx, acquisition.ID, acquisition.Revision, transition,
		)
	} else {
		updated, err = s.repo.TransitionAcquisitionAtRevision(
			ctx, acquisition.ID, acquisition.Revision, transition,
		)
	}
	if errors.Is(err, integration.ErrStaleRevision) || errors.Is(err, domain.ErrConflict) {
		return s.repo.GetVisibleAcquisition(ctx, acquisition.ID)
	}
	return updated, err
}

// reconcileProgress records a poll result while an ambiguous remote mutation
// remains claimed. A later poll must prove whether Radarr accepted the request.
func (s *radarrService) reconcileProgress(
	ctx context.Context,
	acquisition repository.RadarrAcquisition,
	transition repository.RadarrAcquisitionTransition,
) (repository.RadarrAcquisition, error) {
	updated, err := s.repo.TransitionAcquisitionAtRevision(
		ctx, acquisition.ID, acquisition.Revision, transition,
	)
	if errors.Is(err, integration.ErrStaleRevision) || errors.Is(err, domain.ErrConflict) {
		return s.repo.GetVisibleAcquisition(ctx, acquisition.ID)
	}
	return updated, err
}

func historyFailure(history []integrationradarr.HistoryItem, since *time.Time) (reason, summary string) {
	for _, item := range history {
		if since != nil && item.Date.Before(*since) {
			continue
		}
		switch strings.ToLower(item.EventType) {
		case "downloadfailed":
			return "release_failed", "Radarr reports that the selected download failed."
		case "downloadfolderimported", "moviefolderimported":
			return "import_failed", "Radarr imported the download but no movie file is available."
		}
	}
	return "", ""
}

type radarrAcquisitionWorker struct {
	cancel context.CancelFunc
	done   chan struct{}
	onErr  func(error)
	onRun  func(int)
	once   sync.Once
}

func newRadarrAcquisitionWorker(
	parent context.Context,
	service *radarrService,
	onErr func(error),
	onRun func(int),
) *radarrAcquisitionWorker {
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithCancel(parent)
	worker := &radarrAcquisitionWorker{cancel: cancel, done: make(chan struct{}), onErr: onErr, onRun: onRun}
	go func() {
		defer close(worker.done)
		ticker := time.NewTicker(radarrReconcileInterval)
		defer ticker.Stop()
		worker.runOnce(ctx, service)
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				worker.runOnce(ctx, service)
			}
		}
	}()
	return worker
}

func (w *radarrAcquisitionWorker) runOnce(ctx context.Context, service *radarrService) {
	processed, err := service.reconcileDue(ctx, 50)
	if err != nil && ctx.Err() == nil && w.onErr != nil {
		w.onErr(err)
	}
	if processed > 0 && w.onRun != nil {
		w.onRun(processed)
	}
}

func (w *radarrAcquisitionWorker) Close() {
	if w == nil {
		return
	}
	w.once.Do(w.cancel)
	<-w.done
}
