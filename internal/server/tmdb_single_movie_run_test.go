package server

import (
	"context"
	"errors"
	"testing"
	"time"

	"moviepickarr/internal/domain"
	"moviepickarr/internal/integration"
	integrationtmdb "moviepickarr/internal/integration/tmdb"
)

type singleMovieRunLedger struct {
	integration.RunLedger
	start            integration.RunStart
	finish           integration.RunFinish
	finishContextErr error
}

func (l *singleMovieRunLedger) Start(_ context.Context, start integration.RunStart) (*integration.Run, error) {
	l.start = start
	return &integration.Run{ID: 14}, nil
}

func (l *singleMovieRunLedger) Finish(ctx context.Context, _ int64, finish integration.RunFinish) (*integration.Run, error) {
	l.finish = finish
	l.finishContextErr = ctx.Err()
	return &integration.Run{ID: 14, Status: finish.Status}, nil
}

type fixedSingleMovieConfig struct {
	snapshot integrationtmdb.RuntimeSnapshot
}

func (c fixedSingleMovieConfig) Acquire(context.Context) (integrationtmdb.RuntimeSnapshot, error) {
	return c.snapshot, nil
}

type singleMovieSubjectEnricher struct {
	snapshot integrationtmdb.RuntimeSnapshot
	id       int
	err      error
}

func (e *singleMovieSubjectEnricher) EnrichOne(
	_ context.Context,
	snapshot integrationtmdb.RuntimeSnapshot,
	id int,
) (enrichResult, error) {
	e.snapshot = snapshot
	e.id = id
	return enrichResult{TMDBID: 603}, e.err
}

type singleMovieCandidates struct{}

func (singleMovieCandidates) NeedsEnrichment(context.Context, time.Time, int) ([]domain.EnrichmentCandidate, error) {
	return nil, nil
}

func TestTMDBSingleMovieLedgerEnricher_RecordsActualWork(t *testing.T) {
	t.Parallel()
	startedAt := time.Date(2026, time.August, 4, 15, 0, 0, 0, time.UTC)
	clock := &fixedTMDBRunClock{now: startedAt}
	snapshot := integrationtmdb.RuntimeSnapshot{
		Revision: 11,
		Config:   integrationtmdb.RuntimeConfig{Enabled: true, APIKey: "configured"},
	}
	ledger := &singleMovieRunLedger{}
	subject := &singleMovieSubjectEnricher{}
	enricher := newTMDBSingleMovieLedgerEnricher(
		singleMovieCandidates{}, subject, fixedSingleMovieConfig{snapshot: snapshot}, ledger, clock, nil,
	)

	result, err := enricher.EnrichOne(context.Background(), 42)
	if err != nil {
		t.Fatalf("enrich one: %v", err)
	}
	if result.TMDBID != 603 || subject.id != 42 || subject.snapshot.Revision != 11 {
		t.Fatalf("work = result %+v, id %d, snapshot %+v", result, subject.id, subject.snapshot)
	}
	if ledger.start.Operation != integration.RunOperationEnrichMovie || ledger.start.Trigger != integration.RunTriggerMovieAdded || ledger.start.Total != 1 || ledger.start.ConfigRevision != 11 {
		t.Fatalf("ledger start = %+v", ledger.start)
	}
	want := integration.RunProgress{Total: 1, Processed: 1, Succeeded: 1}
	if ledger.finish.Status != integration.RunStatusCompleted || ledger.finish.Progress != want {
		t.Fatalf("ledger finish = %+v", ledger.finish)
	}
}

func TestTMDBSingleMovieLedgerEnricher_RecordsMovieUpdateTrigger(t *testing.T) {
	t.Parallel()
	startedAt := time.Date(2026, time.August, 5, 10, 0, 0, 0, time.UTC)
	clock := &fixedTMDBRunClock{now: startedAt}
	snapshot := integrationtmdb.RuntimeSnapshot{
		Revision: 12,
		Config:   integrationtmdb.RuntimeConfig{Enabled: true, APIKey: "configured"},
	}
	ledger := &singleMovieRunLedger{}
	enricher := newTMDBSingleMovieLedgerEnricher(
		singleMovieCandidates{},
		&singleMovieSubjectEnricher{},
		fixedSingleMovieConfig{snapshot: snapshot},
		ledger,
		clock,
		nil,
	)

	if _, err := enricher.EnrichOneWithTrigger(
		context.Background(),
		42,
		integration.RunTriggerMovieUpdated,
	); err != nil {
		t.Fatalf("enrich edited movie: %v", err)
	}
	if ledger.start.Trigger != integration.RunTriggerMovieUpdated {
		t.Fatalf("ledger trigger = %q, want %q", ledger.start.Trigger, integration.RunTriggerMovieUpdated)
	}
}

func TestTMDBSingleMovieLedgerEnricher_RecordsShutdownCancellationAsInterrupted(t *testing.T) {
	t.Parallel()
	clock := &fixedTMDBRunClock{now: time.Date(2026, time.August, 5, 11, 0, 0, 0, time.UTC)}
	ledger := &singleMovieRunLedger{}
	enricher := newTMDBSingleMovieLedgerEnricher(
		singleMovieCandidates{},
		&singleMovieSubjectEnricher{err: context.Canceled},
		fixedSingleMovieConfig{snapshot: integrationtmdb.RuntimeSnapshot{Revision: 13}},
		ledger,
		clock,
		nil,
	)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := enricher.EnrichOne(ctx, 42)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("enrich error = %v, want context canceled", err)
	}
	wantProgress := integration.RunProgress{Total: 1, Remaining: 1}
	if ledger.finish.Status != integration.RunStatusInterrupted || ledger.finish.Progress != wantProgress {
		t.Fatalf("finish = %+v, want interrupted with progress %+v", ledger.finish, wantProgress)
	}
	if ledger.finishContextErr != nil {
		t.Fatalf("ledger finish context error = %v, want cancellation detached", ledger.finishContextErr)
	}
}
