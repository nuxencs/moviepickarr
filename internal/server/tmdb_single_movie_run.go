package server

import (
	"context"
	"errors"
	"fmt"
	"time"

	"moviepickarr/internal/domain"
	"moviepickarr/internal/integration"
	integrationtmdb "moviepickarr/internal/integration/tmdb"
)

type tmdbSingleMovieLedgerEnricher struct {
	candidates               enrichmentCandidateStore
	enricher                 tmdbRunSubjectEnricher
	configs                  tmdbRunConfigSource
	ledger                   integration.RunLedger
	clock                    tmdbRunClock
	onAuthenticationRejected func(integrationtmdb.RuntimeSnapshot)
}

func newTMDBSingleMovieLedgerEnricher(
	candidates enrichmentCandidateStore,
	enricher tmdbRunSubjectEnricher,
	configs tmdbRunConfigSource,
	ledger integration.RunLedger,
	clock tmdbRunClock,
	onAuthenticationRejected func(integrationtmdb.RuntimeSnapshot),
) *tmdbSingleMovieLedgerEnricher {
	if clock == nil {
		clock = systemTMDBRunClock{}
	}
	return &tmdbSingleMovieLedgerEnricher{
		candidates:               candidates,
		enricher:                 enricher,
		configs:                  configs,
		ledger:                   ledger,
		clock:                    clock,
		onAuthenticationRejected: onAuthenticationRejected,
	}
}

func (e *tmdbSingleMovieLedgerEnricher) NeedsEnrichment(
	ctx context.Context,
	staleBefore time.Time,
	limit int,
) ([]domain.EnrichmentCandidate, error) {
	return e.candidates.NeedsEnrichment(ctx, staleBefore, limit)
}

func (e *tmdbSingleMovieLedgerEnricher) EnrichOne(ctx context.Context, movieID int) (enrichResult, error) {
	return e.EnrichOneWithTrigger(ctx, movieID, integration.RunTriggerMovieAdded)
}

func (e *tmdbSingleMovieLedgerEnricher) EnrichOneWithTrigger(
	ctx context.Context,
	movieID int,
	trigger integration.RunTrigger,
) (enrichResult, error) {
	snapshot, err := e.configs.Acquire(ctx)
	if err != nil {
		return enrichResult{}, err
	}
	startedAt := e.clock.Now()
	run, err := e.ledger.Start(ctx, integration.RunStart{
		Integration:    "tmdb",
		Operation:      integration.RunOperationEnrichMovie,
		Trigger:        trigger,
		ConfigRevision: snapshot.Revision,
		StartedAt:      startedAt,
		Total:          1,
	})
	if err != nil {
		return enrichResult{}, err
	}

	result, workErr := e.enricher.EnrichOne(ctx, snapshot, movieID)
	finish := singleMovieRunFinish(movieID, e.clock.Now(), workErr)
	if errors.Is(workErr, errTMDBAuthentication) || errors.Is(workErr, integrationtmdb.ErrAPIKeyRejected) {
		if e.onAuthenticationRejected != nil {
			e.onAuthenticationRejected(snapshot)
		}
	}
	if _, err := e.ledger.Finish(context.WithoutCancel(ctx), run.ID, finish); err != nil {
		return result, errors.Join(workErr, fmt.Errorf("finish single-movie TMDB run: %w", err))
	}
	return result, workErr
}

func singleMovieRunFinish(movieID int, finishedAt time.Time, err error) integration.RunFinish {
	if errors.Is(err, context.Canceled) {
		return integration.RunFinish{
			Status:     integration.RunStatusInterrupted,
			FinishedAt: finishedAt,
			Progress: integration.RunProgress{
				Total:     1,
				Remaining: 1,
			},
		}
	}
	progress := integration.RunProgress{Total: 1, Processed: 1}
	finish := integration.RunFinish{
		Status:     integration.RunStatusCompleted,
		FinishedAt: finishedAt,
		Progress:   progress,
	}
	if err == nil {
		finish.Progress.Succeeded = 1
		return finish
	}
	if errors.Is(err, ErrEnrichNoIMDbID) ||
		errors.Is(err, ErrEnrichNotFound) ||
		errors.Is(err, ErrEnrichSuperseded) ||
		errors.Is(err, domain.ErrNotFound) {
		finish.Progress.Skipped = 1
		return finish
	}
	finish.Status = integration.RunStatusFailed
	finish.Progress.Failed = 1
	message := "TMDB enrichment failed"
	if errors.Is(err, errTMDBAuthentication) || errors.Is(err, integrationtmdb.ErrAPIKeyRejected) {
		message = "API key rejected"
	}
	finish.ErrorSummary = message
	finish.FailedSubjects = []integration.FailedSubject{{
		Subject: fmt.Sprintf("movie:%d", movieID),
		Error:   message,
	}}
	return finish
}

var _ Enricher = (*tmdbSingleMovieLedgerEnricher)(nil)
