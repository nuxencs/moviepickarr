package server

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"moviepickarr/internal/integration"
	integrationtmdb "moviepickarr/internal/integration/tmdb"
)

type fixedTMDBRunClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *fixedTMDBRunClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fixedTMDBRunClock) Advance(duration time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(duration)
	c.mu.Unlock()
}

type fakeTMDBRunConfigSource struct {
	snapshot integrationtmdb.RuntimeSnapshot
	err      error
	calls    int
}

func (s *fakeTMDBRunConfigSource) Acquire(context.Context) (integrationtmdb.RuntimeSnapshot, error) {
	s.calls++
	return s.snapshot, s.err
}

type recordingTMDBRunCandidates struct {
	staleBefore time.Time
	limit       int
	refresh     []tmdbRunSubject
	all         []tmdbRunSubject
	refreshErr  error
	allErr      error
	onRefresh   func()
	refreshes   int
	allCalls    int
}

type blockingTMDBRunCandidates struct {
	started chan struct{}
	release chan struct{}
}

type cancellableTMDBRunCandidates struct {
	started     chan struct{}
	cancelled   chan struct{}
	allowReturn chan struct{}
}

func (s *cancellableTMDBRunCandidates) RefreshStale(ctx context.Context, _ time.Time, _ int) ([]tmdbRunSubject, error) {
	close(s.started)
	select {
	case <-ctx.Done():
		close(s.cancelled)
		<-s.allowReturn
		return nil, ctx.Err()
	case <-s.allowReturn:
		return nil, nil
	}
}

func (s *cancellableTMDBRunCandidates) ReEnrichAll(context.Context) ([]tmdbRunSubject, error) {
	return nil, nil
}

func (s *blockingTMDBRunCandidates) RefreshStale(context.Context, time.Time, int) ([]tmdbRunSubject, error) {
	close(s.started)
	<-s.release
	return nil, nil
}

func (s *blockingTMDBRunCandidates) ReEnrichAll(context.Context) ([]tmdbRunSubject, error) {
	return nil, nil
}

func (s *recordingTMDBRunCandidates) RefreshStale(_ context.Context, staleBefore time.Time, limit int) ([]tmdbRunSubject, error) {
	s.refreshes++
	s.staleBefore = staleBefore
	s.limit = limit
	if s.onRefresh != nil {
		s.onRefresh()
	}
	return s.refresh, s.refreshErr
}

func (s *recordingTMDBRunCandidates) ReEnrichAll(context.Context) ([]tmdbRunSubject, error) {
	s.allCalls++
	return s.all, s.allErr
}

type recordingTMDBRunEnricher struct {
	mu        sync.Mutex
	ids       []int
	snapshots []integrationtmdb.RuntimeSnapshot
	fn        func(context.Context, integrationtmdb.RuntimeSnapshot, int) error
}

func (e *recordingTMDBRunEnricher) EnrichOne(ctx context.Context, snapshot integrationtmdb.RuntimeSnapshot, id int) (enrichResult, error) {
	e.mu.Lock()
	e.ids = append(e.ids, id)
	e.snapshots = append(e.snapshots, snapshot)
	e.mu.Unlock()
	if e.fn != nil {
		return enrichResult{}, e.fn(ctx, snapshot, id)
	}
	return enrichResult{TMDBID: id}, nil
}

type recordingTMDBRunLedger struct {
	integration.RunLedger
	mu         sync.Mutex
	start      integration.RunStart
	updates    []integration.RunProgress
	updateErrs []error
	finish     integration.RunFinish
	finishErr  error
	starts     int
	finishes   int
	nextID     int64
}

func (l *recordingTMDBRunLedger) Start(_ context.Context, start integration.RunStart) (*integration.Run, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.starts++
	l.start = start
	if l.nextID == 0 {
		l.nextID = 99
	}
	return &integration.Run{
		ID:             l.nextID,
		Integration:    start.Integration,
		Operation:      start.Operation,
		Trigger:        start.Trigger,
		InitiatedBy:    start.InitiatedBy,
		ConfigRevision: start.ConfigRevision,
		Status:         integration.RunStatusRunning,
		StartedAt:      start.StartedAt,
		Progress: integration.RunProgress{
			Total: start.Total, Remaining: start.Total,
		},
	}, nil
}

func (l *recordingTMDBRunLedger) Update(_ context.Context, _ int64, progress integration.RunProgress) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.updates = append(l.updates, progress)
	if index := len(l.updates) - 1; index < len(l.updateErrs) {
		return l.updateErrs[index]
	}
	return nil
}

func (l *recordingTMDBRunLedger) Finish(_ context.Context, _ int64, finish integration.RunFinish) (*integration.Run, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.finishes++
	l.finish = finish
	if l.finishErr != nil {
		return nil, l.finishErr
	}
	return &integration.Run{ID: l.nextID, Status: finish.Status, Progress: finish.Progress}, nil
}

func waitTMDBRun(t *testing.T, done <-chan error) {
	t.Helper()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("run completion: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("run did not finish")
	}
}

func TestTMDBRunController_RefreshStaleRunsAsynchronouslyToCompletion(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.August, 4, 12, 0, 0, 0, time.UTC)
	clock := &fixedTMDBRunClock{now: now}
	snapshot := integrationtmdb.RuntimeSnapshot{
		Revision: 8,
		Config: integrationtmdb.RuntimeConfig{
			Enabled: true, APIKey: "configured-key", TTL: 30 * 24 * time.Hour, BatchLimit: 2,
		},
	}
	candidates := &recordingTMDBRunCandidates{refresh: []tmdbRunSubject{
		{MovieID: 603, Label: "The Matrix"},
		{MovieID: 604, Label: "The Matrix Reloaded"},
	}}
	enricher := &recordingTMDBRunEnricher{}
	ledger := &recordingTMDBRunLedger{}
	controller := newTMDBRunController(
		context.Background(), candidates, enricher,
		&fakeTMDBRunConfigSource{snapshot: snapshot}, ledger, clock, nil,
	)
	t.Cleanup(controller.Close)
	adminID := 7

	result, err := controller.Start(context.Background(), tmdbRunStart{
		Operation:   integration.RunOperationRefreshStale,
		Trigger:     integration.RunTriggerManual,
		InitiatedBy: &adminID,
	})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if result.NoWork || result.Run == nil || result.Run.ID != 99 || result.Done == nil {
		t.Fatalf("start result = %+v", result)
	}
	waitTMDBRun(t, result.Done)

	if candidates.refreshes != 1 || candidates.allCalls != 0 {
		t.Fatalf("candidate calls = stale %d, all %d", candidates.refreshes, candidates.allCalls)
	}
	if !candidates.staleBefore.Equal(now.Add(-30*24*time.Hour)) || candidates.limit != 2 {
		t.Fatalf("stale selection = before %s limit %d", candidates.staleBefore, candidates.limit)
	}
	if ledger.starts != 1 || ledger.start.Integration != "tmdb" || ledger.start.Operation != integration.RunOperationRefreshStale || ledger.start.Trigger != integration.RunTriggerManual {
		t.Fatalf("ledger start = %+v", ledger.start)
	}
	if ledger.start.ConfigRevision != 8 || ledger.start.Total != 2 || ledger.start.InitiatedBy == nil || *ledger.start.InitiatedBy != 7 {
		t.Fatalf("ledger provenance/count = %+v", ledger.start)
	}
	if len(enricher.ids) != 2 || enricher.ids[0] != 603 || enricher.ids[1] != 604 {
		t.Fatalf("enriched ids = %v", enricher.ids)
	}
	for _, got := range enricher.snapshots {
		if got.Revision != 8 || got.Config.APIKey != "configured-key" {
			t.Fatalf("enrichment snapshot = %+v", got)
		}
	}
	if ledger.finishes != 1 || ledger.finish.Status != integration.RunStatusCompleted {
		t.Fatalf("ledger finish = %+v", ledger.finish)
	}
	wantProgress := integration.RunProgress{Total: 2, Processed: 2, Succeeded: 2}
	if ledger.finish.Progress != wantProgress {
		t.Fatalf("finish progress = %+v, want %+v", ledger.finish.Progress, wantProgress)
	}
}

func TestTMDBRunController_CancelLetsActiveSubjectFinish(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.August, 4, 13, 0, 0, 0, time.UTC)
	firstStarted := make(chan struct{})
	releaseFirst := make(chan struct{})
	enricher := &recordingTMDBRunEnricher{fn: func(ctx context.Context, _ integrationtmdb.RuntimeSnapshot, id int) error {
		if id != 603 {
			return nil
		}
		close(firstStarted)
		<-releaseFirst
		if ctx.Err() != nil {
			return errors.New("active request context was cancelled")
		}
		return nil
	}}
	ledger := &recordingTMDBRunLedger{}
	controller := newTMDBRunController(
		context.Background(),
		&recordingTMDBRunCandidates{refresh: []tmdbRunSubject{{MovieID: 603}, {MovieID: 604}}},
		enricher,
		&fakeTMDBRunConfigSource{snapshot: integrationtmdb.RuntimeSnapshot{
			Revision: 9,
			Config: integrationtmdb.RuntimeConfig{
				Enabled: true, TTL: 24 * time.Hour, BatchLimit: 10,
			},
		}},
		ledger,
		&fixedTMDBRunClock{now: now},
		nil,
	)
	t.Cleanup(controller.Close)
	result, err := controller.Start(context.Background(), tmdbRunStart{
		Operation: integration.RunOperationRefreshStale,
		Trigger:   integration.RunTriggerManual,
	})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	select {
	case <-firstStarted:
	case <-time.After(time.Second):
		t.Fatal("first subject did not start")
	}
	if err := controller.Cancel(result.Run.ID); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	close(releaseFirst)
	waitTMDBRun(t, result.Done)

	if len(enricher.ids) != 1 || enricher.ids[0] != 603 {
		t.Fatalf("enriched ids after cancel = %v, want active subject only", enricher.ids)
	}
	if ledger.finish.Status != integration.RunStatusCancelled {
		t.Fatalf("finish status = %q, want cancelled", ledger.finish.Status)
	}
	want := integration.RunProgress{Total: 2, Processed: 1, Succeeded: 1, Remaining: 1}
	if ledger.finish.Progress != want {
		t.Fatalf("finish progress = %+v, want %+v", ledger.finish.Progress, want)
	}
}

func TestTMDBRunController_AuthenticationRejectionStopsAndSanitizes(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.August, 4, 14, 0, 0, 0, time.UTC)
	snapshot := integrationtmdb.RuntimeSnapshot{
		Revision: 10,
		Config: integrationtmdb.RuntimeConfig{
			Enabled: true, APIKey: "rejected-secret", TTL: 24 * time.Hour, BatchLimit: 10,
		},
	}
	enricher := &recordingTMDBRunEnricher{fn: func(context.Context, integrationtmdb.RuntimeSnapshot, int) error {
		return errors.Join(errors.New("request contained api_key=rejected-secret"), integrationtmdb.ErrAPIKeyRejected)
	}}
	ledger := &recordingTMDBRunLedger{}
	rejected := make(chan integrationtmdb.RuntimeSnapshot, 1)
	controller := newTMDBRunController(
		context.Background(),
		&recordingTMDBRunCandidates{refresh: []tmdbRunSubject{
			{MovieID: 603, Label: "The Matrix"},
			{MovieID: 604, Label: "The Matrix Reloaded"},
			{MovieID: 605, Label: "The Matrix Revolutions"},
		}},
		enricher,
		&fakeTMDBRunConfigSource{snapshot: snapshot},
		ledger,
		&fixedTMDBRunClock{now: now},
		func(got integrationtmdb.RuntimeSnapshot) { rejected <- got },
	)
	t.Cleanup(controller.Close)
	result, err := controller.Start(context.Background(), tmdbRunStart{
		Operation: integration.RunOperationRefreshStale,
		Trigger:   integration.RunTriggerScheduled,
	})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	waitTMDBRun(t, result.Done)

	if len(enricher.ids) != 1 || enricher.ids[0] != 603 {
		t.Fatalf("enriched ids after rejection = %v", enricher.ids)
	}
	select {
	case got := <-rejected:
		if got.Revision != 10 || got.Config.APIKey != "rejected-secret" {
			t.Fatalf("rejected snapshot = %+v", got)
		}
	default:
		t.Fatal("authentication rejection callback was not called")
	}
	if ledger.finish.Status != integration.RunStatusFailed || ledger.finish.ErrorSummary != "API key rejected" {
		t.Fatalf("finish lifecycle = %+v", ledger.finish)
	}
	want := integration.RunProgress{Total: 3, Processed: 1, Failed: 1, Remaining: 2}
	if ledger.finish.Progress != want {
		t.Fatalf("finish progress = %+v, want %+v", ledger.finish.Progress, want)
	}
	if len(ledger.finish.FailedSubjects) != 1 || ledger.finish.FailedSubjects[0].Subject != "The Matrix" || ledger.finish.FailedSubjects[0].Error != "API key rejected" {
		t.Fatalf("failed-subject sample = %+v", ledger.finish.FailedSubjects)
	}
	if strings.Contains(ledger.finish.ErrorSummary, "rejected-secret") || strings.Contains(ledger.finish.FailedSubjects[0].Error, "rejected-secret") {
		t.Fatalf("raw credential reached ledger: %+v", ledger.finish)
	}
}

func TestTMDBRunController_RejectsNonLibraryTriggerBeforeWork(t *testing.T) {
	t.Parallel()
	configs := &fakeTMDBRunConfigSource{snapshot: integrationtmdb.RuntimeSnapshot{
		Config: integrationtmdb.RuntimeConfig{Enabled: true},
	}}
	candidates := &recordingTMDBRunCandidates{}
	ledger := &recordingTMDBRunLedger{}
	controller := newTMDBRunController(
		context.Background(), candidates, &recordingTMDBRunEnricher{}, configs, ledger, nil, nil,
	)
	t.Cleanup(controller.Close)

	_, err := controller.Start(context.Background(), tmdbRunStart{
		Operation: integration.RunOperationRefreshStale,
		Trigger:   integration.RunTriggerMovieAdded,
	})
	if err == nil || !strings.Contains(err.Error(), "trigger") {
		t.Fatalf("start error = %v, want unsupported-trigger error", err)
	}
	if configs.calls != 0 || candidates.refreshes != 0 || candidates.allCalls != 0 || ledger.starts != 0 {
		t.Fatalf("work started for invalid trigger: config=%d stale=%d all=%d ledger=%d", configs.calls, candidates.refreshes, candidates.allCalls, ledger.starts)
	}
}

func TestTMDBRunController_AcceptsStartupRefresh(t *testing.T) {
	t.Parallel()
	controller := newTMDBRunController(
		context.Background(),
		&recordingTMDBRunCandidates{},
		&recordingTMDBRunEnricher{},
		&fakeTMDBRunConfigSource{snapshot: integrationtmdb.RuntimeSnapshot{
			Config: integrationtmdb.RuntimeConfig{Enabled: true, TTL: 24 * time.Hour, BatchLimit: 20},
		}},
		&recordingTMDBRunLedger{},
		&fixedTMDBRunClock{now: time.Date(2026, time.August, 4, 14, 30, 0, 0, time.UTC)},
		nil,
	)
	t.Cleanup(controller.Close)

	result, err := controller.Start(context.Background(), tmdbRunStart{
		Operation: integration.RunOperationRefreshStale,
		Trigger:   integration.RunTriggerStartup,
	})
	if err != nil {
		t.Fatalf("startup refresh: %v", err)
	}
	if !result.NoWork {
		t.Fatalf("startup result = %+v, want explicit no-work result", result)
	}
}

func TestTMDBRunController_ClassifiesSkipsAndSanitizesFailures(t *testing.T) {
	t.Parallel()
	enricher := &recordingTMDBRunEnricher{fn: func(_ context.Context, _ integrationtmdb.RuntimeSnapshot, id int) error {
		switch id {
		case 604:
			return fmt.Errorf("candidate URL included token=secret: %w", ErrEnrichNoIMDbID)
		case 605:
			return errors.New("upstream response included token=secret")
		default:
			return nil
		}
	}}
	candidates := &recordingTMDBRunCandidates{all: []tmdbRunSubject{
		{MovieID: 603, Label: "The Matrix"},
		{MovieID: 604, Label: "The Matrix Reloaded"},
		{MovieID: 605, Label: "The Matrix Revolutions"},
	}}
	ledger := &recordingTMDBRunLedger{}
	controller := newTMDBRunController(
		context.Background(),
		candidates,
		enricher,
		&fakeTMDBRunConfigSource{snapshot: integrationtmdb.RuntimeSnapshot{
			Config: integrationtmdb.RuntimeConfig{Enabled: true},
		}},
		ledger,
		&fixedTMDBRunClock{now: time.Date(2026, time.August, 4, 15, 0, 0, 0, time.UTC)},
		nil,
	)
	t.Cleanup(controller.Close)

	result, err := controller.Start(context.Background(), tmdbRunStart{
		Operation: integration.RunOperationReEnrichAll,
		Trigger:   integration.RunTriggerConfiguration,
	})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	waitTMDBRun(t, result.Done)

	wantProgress := integration.RunProgress{Total: 3, Processed: 3, Succeeded: 1, Failed: 1, Skipped: 1}
	if ledger.finish.Status != integration.RunStatusCompletedWithErrors || ledger.finish.Progress != wantProgress {
		t.Fatalf("finish = %+v, want status %q progress %+v", ledger.finish, integration.RunStatusCompletedWithErrors, wantProgress)
	}
	if ledger.finish.ErrorSummary != "1 subject failed" {
		t.Fatalf("error summary = %q", ledger.finish.ErrorSummary)
	}
	wantFailures := []integration.FailedSubject{{Subject: "The Matrix Revolutions", Error: "TMDB enrichment failed"}}
	if !reflect.DeepEqual(ledger.finish.FailedSubjects, wantFailures) {
		t.Fatalf("failure sample = %+v, want %+v", ledger.finish.FailedSubjects, wantFailures)
	}
	if candidates.allCalls != 1 || candidates.refreshes != 0 {
		t.Fatalf("candidate calls = stale %d, all %d", candidates.refreshes, candidates.allCalls)
	}
}

func TestTMDBRunController_NotifiesOnlySuccessfulEnrichments(t *testing.T) {
	t.Parallel()
	var enriched []int
	controller := newTMDBRunController(
		context.Background(),
		&recordingTMDBRunCandidates{all: []tmdbRunSubject{
			{MovieID: 603},
			{MovieID: 604},
			{MovieID: 605},
		}},
		&recordingTMDBRunEnricher{fn: func(_ context.Context, _ integrationtmdb.RuntimeSnapshot, id int) error {
			switch id {
			case 604:
				return ErrEnrichNotFound
			case 605:
				return errors.New("remote failure")
			default:
				return nil
			}
		}},
		&fakeTMDBRunConfigSource{snapshot: integrationtmdb.RuntimeSnapshot{
			Config: integrationtmdb.RuntimeConfig{Enabled: true},
		}},
		&recordingTMDBRunLedger{},
		&fixedTMDBRunClock{now: time.Date(2026, time.August, 4, 15, 15, 0, 0, time.UTC)},
		nil,
	)
	controller.setEnrichedCallback(func(movieID int) { enriched = append(enriched, movieID) })
	t.Cleanup(controller.Close)

	result, err := controller.Start(context.Background(), tmdbRunStart{
		Operation: integration.RunOperationReEnrichAll,
		Trigger:   integration.RunTriggerManual,
	})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	waitTMDBRun(t, result.Done)

	if !reflect.DeepEqual(enriched, []int{603}) {
		t.Fatalf("enriched callbacks = %v, want successful movie only", enriched)
	}
}

func TestTMDBRunController_SkippedWorkPreventsAllFailedStatus(t *testing.T) {
	t.Parallel()
	ledger := &recordingTMDBRunLedger{}
	controller := newTMDBRunController(
		context.Background(),
		&recordingTMDBRunCandidates{all: []tmdbRunSubject{{MovieID: 1}, {MovieID: 2}}},
		&recordingTMDBRunEnricher{fn: func(_ context.Context, _ integrationtmdb.RuntimeSnapshot, id int) error {
			if id == 1 {
				return ErrEnrichNotFound
			}
			return errors.New("temporary upstream failure")
		}},
		&fakeTMDBRunConfigSource{snapshot: integrationtmdb.RuntimeSnapshot{
			Config: integrationtmdb.RuntimeConfig{Enabled: true},
		}},
		ledger,
		&fixedTMDBRunClock{now: time.Date(2026, time.August, 4, 15, 30, 0, 0, time.UTC)},
		nil,
	)
	t.Cleanup(controller.Close)

	result, err := controller.Start(context.Background(), tmdbRunStart{
		Operation: integration.RunOperationReEnrichAll,
		Trigger:   integration.RunTriggerManual,
	})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	waitTMDBRun(t, result.Done)

	want := integration.RunProgress{Total: 2, Processed: 2, Failed: 1, Skipped: 1}
	if ledger.finish.Status != integration.RunStatusCompletedWithErrors || ledger.finish.Progress != want {
		t.Fatalf("finish = %+v, want completed-with-errors and %+v", ledger.finish, want)
	}
}

func TestTMDBRunController_BoundsAndNormalizesFailureSample(t *testing.T) {
	t.Parallel()
	subjects := make([]tmdbRunSubject, 27)
	for i := range subjects {
		subjects[i] = tmdbRunSubject{MovieID: 1000 + i, Label: fmt.Sprintf("Movie %d", i)}
	}
	subjects[0].Label = ""
	subjects[1].Label = "  Bad\n\tTitle  "
	subjects[2].Label = strings.Repeat("x", 220)
	ledger := &recordingTMDBRunLedger{}
	controller := newTMDBRunController(
		context.Background(),
		&recordingTMDBRunCandidates{all: subjects},
		&recordingTMDBRunEnricher{fn: func(context.Context, integrationtmdb.RuntimeSnapshot, int) error {
			return errors.New("remote response with token=secret")
		}},
		&fakeTMDBRunConfigSource{snapshot: integrationtmdb.RuntimeSnapshot{
			Config: integrationtmdb.RuntimeConfig{Enabled: true},
		}},
		ledger,
		&fixedTMDBRunClock{now: time.Date(2026, time.August, 4, 16, 0, 0, 0, time.UTC)},
		nil,
	)
	t.Cleanup(controller.Close)

	result, err := controller.Start(context.Background(), tmdbRunStart{
		Operation: integration.RunOperationReEnrichAll,
		Trigger:   integration.RunTriggerManual,
	})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	waitTMDBRun(t, result.Done)

	if len(ledger.finish.FailedSubjects) != 25 {
		t.Fatalf("failure sample size = %d, want 25", len(ledger.finish.FailedSubjects))
	}
	if ledger.finish.FailedSubjects[0].Subject != "movie:1000" {
		t.Fatalf("blank subject = %q", ledger.finish.FailedSubjects[0].Subject)
	}
	if ledger.finish.FailedSubjects[1].Subject != "Bad Title" {
		t.Fatalf("control-character subject = %q", ledger.finish.FailedSubjects[1].Subject)
	}
	if got := utf8.RuneCountInString(ledger.finish.FailedSubjects[2].Subject); got != 160 {
		t.Fatalf("long subject length = %d, want 160", got)
	}
	for _, failure := range ledger.finish.FailedSubjects {
		if strings.Contains(failure.Error, "secret") || strings.ContainsAny(failure.Subject, "\n\t") {
			t.Fatalf("unsanitized failure reached ledger: %+v", failure)
		}
	}
}

func TestTMDBRunController_CoalescesProgressByCountAndFlushesTerminalState(t *testing.T) {
	t.Parallel()
	subjects := make([]tmdbRunSubject, 12)
	for i := range subjects {
		subjects[i] = tmdbRunSubject{MovieID: 2000 + i}
	}
	ledger := &recordingTMDBRunLedger{}
	controller := newTMDBRunController(
		context.Background(),
		&recordingTMDBRunCandidates{all: subjects},
		&recordingTMDBRunEnricher{},
		&fakeTMDBRunConfigSource{snapshot: integrationtmdb.RuntimeSnapshot{
			Config: integrationtmdb.RuntimeConfig{Enabled: true},
		}},
		ledger,
		&fixedTMDBRunClock{now: time.Date(2026, time.August, 4, 17, 0, 0, 0, time.UTC)},
		nil,
	)
	t.Cleanup(controller.Close)

	result, err := controller.Start(context.Background(), tmdbRunStart{
		Operation: integration.RunOperationReEnrichAll,
		Trigger:   integration.RunTriggerManual,
	})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	waitTMDBRun(t, result.Done)

	if len(ledger.updates) != 2 {
		t.Fatalf("progress writes = %d (%+v), want count flush plus terminal flush", len(ledger.updates), ledger.updates)
	}
	if ledger.updates[0].Processed != 10 || ledger.updates[1].Processed != 12 {
		t.Fatalf("progress write positions = %d, %d, want 10, 12", ledger.updates[0].Processed, ledger.updates[1].Processed)
	}
}

func TestTMDBRunController_RetriesTerminalProgressAfterUpdateFailure(t *testing.T) {
	t.Parallel()
	subjects := make([]tmdbRunSubject, 10)
	for i := range subjects {
		subjects[i] = tmdbRunSubject{MovieID: 3000 + i}
	}
	writeErr := errors.New("progress write failed")
	ledger := &recordingTMDBRunLedger{updateErrs: []error{writeErr, nil}}
	reported := make(chan error, 2)
	controller := newTMDBRunController(
		context.Background(),
		&recordingTMDBRunCandidates{all: subjects},
		&recordingTMDBRunEnricher{},
		&fakeTMDBRunConfigSource{snapshot: integrationtmdb.RuntimeSnapshot{
			Config: integrationtmdb.RuntimeConfig{Enabled: true},
		}},
		ledger,
		&fixedTMDBRunClock{now: time.Date(2026, time.August, 4, 17, 30, 0, 0, time.UTC)},
		nil,
		withTMDBRunError(func(err error) { reported <- err }),
	)
	t.Cleanup(controller.Close)

	result, err := controller.Start(context.Background(), tmdbRunStart{
		Operation: integration.RunOperationReEnrichAll,
		Trigger:   integration.RunTriggerManual,
	})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	select {
	case err := <-result.Done:
		if !errors.Is(err, writeErr) {
			t.Fatalf("completion error = %v, want progress write failure", err)
		}
	case <-time.After(time.Second):
		t.Fatal("run did not finish")
	}

	if len(ledger.updates) != 2 || ledger.updates[0].Processed != 10 || ledger.updates[1].Processed != 10 {
		t.Fatalf("progress retry writes = %+v, want two terminal writes", ledger.updates)
	}
	if ledger.finishes != 1 || ledger.finish.Status != integration.RunStatusCompleted {
		t.Fatalf("finish after progress retry = %+v", ledger.finish)
	}
	select {
	case err := <-reported:
		if !errors.Is(err, writeErr) {
			t.Fatalf("reported error = %v, want progress write failure", err)
		}
	default:
		t.Fatal("progress persistence failure was not reported")
	}
}

func TestTMDBRunController_CoalescesProgressByElapsedTime(t *testing.T) {
	t.Parallel()
	clock := &fixedTMDBRunClock{now: time.Date(2026, time.August, 4, 18, 0, 0, 0, time.UTC)}
	ledger := &recordingTMDBRunLedger{}
	controller := newTMDBRunController(
		context.Background(),
		&recordingTMDBRunCandidates{all: []tmdbRunSubject{{MovieID: 1}, {MovieID: 2}, {MovieID: 3}}},
		&recordingTMDBRunEnricher{fn: func(context.Context, integrationtmdb.RuntimeSnapshot, int) error {
			clock.Advance(1100 * time.Millisecond)
			return nil
		}},
		&fakeTMDBRunConfigSource{snapshot: integrationtmdb.RuntimeSnapshot{
			Config: integrationtmdb.RuntimeConfig{Enabled: true},
		}},
		ledger,
		clock,
		nil,
	)
	t.Cleanup(controller.Close)

	result, err := controller.Start(context.Background(), tmdbRunStart{
		Operation: integration.RunOperationReEnrichAll,
		Trigger:   integration.RunTriggerScheduled,
	})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	waitTMDBRun(t, result.Done)

	if len(ledger.updates) != 2 || ledger.updates[0].Processed != 2 || ledger.updates[1].Processed != 3 {
		t.Fatalf("progress writes = %+v, want elapsed-time write at 2 and terminal write at 3", ledger.updates)
	}
}

func TestTMDBRunController_CloseInterruptsActiveRequestAndWaits(t *testing.T) {
	t.Parallel()
	started := make(chan struct{})
	requestStopped := make(chan struct{})
	var startedOnce sync.Once
	var stoppedOnce sync.Once
	ledger := &recordingTMDBRunLedger{}
	controller := newTMDBRunController(
		context.Background(),
		&recordingTMDBRunCandidates{all: []tmdbRunSubject{{MovieID: 1}, {MovieID: 2}}},
		&recordingTMDBRunEnricher{fn: func(ctx context.Context, _ integrationtmdb.RuntimeSnapshot, _ int) error {
			startedOnce.Do(func() { close(started) })
			<-ctx.Done()
			stoppedOnce.Do(func() { close(requestStopped) })
			return ctx.Err()
		}},
		&fakeTMDBRunConfigSource{snapshot: integrationtmdb.RuntimeSnapshot{
			Config: integrationtmdb.RuntimeConfig{Enabled: true},
		}},
		ledger,
		&fixedTMDBRunClock{now: time.Date(2026, time.August, 4, 19, 0, 0, 0, time.UTC)},
		nil,
	)

	result, err := controller.Start(context.Background(), tmdbRunStart{
		Operation: integration.RunOperationReEnrichAll,
		Trigger:   integration.RunTriggerManual,
	})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("active request did not start")
	}
	closed := make(chan struct{})
	go func() {
		controller.Close()
		close(closed)
	}()
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("close did not wait for and stop the active request")
	}
	select {
	case <-requestStopped:
	default:
		t.Fatal("close returned before active request stopped")
	}
	waitTMDBRun(t, result.Done)

	want := integration.RunProgress{Total: 2, Remaining: 2}
	if ledger.finish.Status != integration.RunStatusInterrupted || ledger.finish.Progress != want {
		t.Fatalf("interrupted finish = %+v, want status %q progress %+v", ledger.finish, integration.RunStatusInterrupted, want)
	}
	controller.Close()
}

func TestTMDBRunController_NoWorkReturnsCheckTimeWithoutLedgerRun(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.August, 4, 20, 0, 0, 0, time.UTC)
	clock := &fixedTMDBRunClock{now: now}
	candidates := &recordingTMDBRunCandidates{onRefresh: func() { clock.Advance(2 * time.Second) }}
	ledger := &recordingTMDBRunLedger{}
	controller := newTMDBRunController(
		context.Background(),
		candidates,
		&recordingTMDBRunEnricher{},
		&fakeTMDBRunConfigSource{snapshot: integrationtmdb.RuntimeSnapshot{
			Config: integrationtmdb.RuntimeConfig{Enabled: true, TTL: 24 * time.Hour, BatchLimit: 20},
		}},
		ledger,
		clock,
		nil,
	)
	t.Cleanup(controller.Close)

	result, err := controller.Start(context.Background(), tmdbRunStart{
		Operation: integration.RunOperationRefreshStale,
		Trigger:   integration.RunTriggerScheduled,
	})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	wantCheckedAt := now.Add(2 * time.Second)
	if !result.NoWork || result.Run != nil || result.Done != nil || !result.CheckedAt.Equal(wantCheckedAt) {
		t.Fatalf("no-work result = %+v, want check time %s and no run", result, wantCheckedAt)
	}
	if ledger.starts != 0 || ledger.finishes != 0 || len(ledger.updates) != 0 {
		t.Fatalf("no-work ledger writes = starts %d, updates %d, finishes %d", ledger.starts, len(ledger.updates), ledger.finishes)
	}

	candidates.refresh = []tmdbRunSubject{{MovieID: 603}}
	next, err := controller.Start(context.Background(), tmdbRunStart{
		Operation: integration.RunOperationRefreshStale,
		Trigger:   integration.RunTriggerManual,
	})
	if err != nil {
		t.Fatalf("start after no work: %v", err)
	}
	waitTMDBRun(t, next.Done)
}

func TestTMDBRunController_ReportsTerminalStatusAndTimestamp(t *testing.T) {
	t.Parallel()
	startedAt := time.Date(2026, time.August, 4, 21, 0, 0, 0, time.UTC)
	clock := &fixedTMDBRunClock{now: startedAt}
	completed := make(chan tmdbRunCompletion, 1)
	ledger := &recordingTMDBRunLedger{nextID: 404}
	controller := newTMDBRunController(
		context.Background(),
		&recordingTMDBRunCandidates{all: []tmdbRunSubject{{MovieID: 603}}},
		&recordingTMDBRunEnricher{fn: func(context.Context, integrationtmdb.RuntimeSnapshot, int) error {
			clock.Advance(3 * time.Second)
			return nil
		}},
		&fakeTMDBRunConfigSource{snapshot: integrationtmdb.RuntimeSnapshot{
			Config: integrationtmdb.RuntimeConfig{Enabled: true},
		}},
		ledger,
		clock,
		nil,
		withTMDBRunCompletion(func(event tmdbRunCompletion) { completed <- event }),
	)
	t.Cleanup(controller.Close)

	result, err := controller.Start(context.Background(), tmdbRunStart{
		Operation: integration.RunOperationReEnrichAll,
		Trigger:   integration.RunTriggerManual,
	})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	waitTMDBRun(t, result.Done)

	select {
	case event := <-completed:
		wantFinishedAt := startedAt.Add(3 * time.Second)
		if event.RunID != 404 || event.Status != integration.RunStatusCompleted || !event.FinishedAt.Equal(wantFinishedAt) {
			t.Fatalf("completion = %+v, want run 404 completed at %s", event, wantFinishedAt)
		}
	default:
		t.Fatal("completion callback was not called before done")
	}
}

func TestTMDBRunController_FinishFailureDoesNotReportCompletion(t *testing.T) {
	t.Parallel()
	finishErr := errors.New("finish persistence failed")
	completed := make(chan tmdbRunCompletion, 1)
	reported := make(chan error, 1)
	ledger := &recordingTMDBRunLedger{finishErr: finishErr}
	controller := newTMDBRunController(
		context.Background(),
		&recordingTMDBRunCandidates{all: []tmdbRunSubject{{MovieID: 603}}},
		&recordingTMDBRunEnricher{},
		&fakeTMDBRunConfigSource{snapshot: integrationtmdb.RuntimeSnapshot{
			Config: integrationtmdb.RuntimeConfig{Enabled: true},
		}},
		ledger,
		&fixedTMDBRunClock{now: time.Date(2026, time.August, 4, 21, 30, 0, 0, time.UTC)},
		nil,
		withTMDBRunCompletion(func(event tmdbRunCompletion) { completed <- event }),
		withTMDBRunError(func(err error) { reported <- err }),
	)
	t.Cleanup(controller.Close)

	result, err := controller.Start(context.Background(), tmdbRunStart{
		Operation: integration.RunOperationReEnrichAll,
		Trigger:   integration.RunTriggerManual,
	})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	select {
	case err := <-result.Done:
		if !errors.Is(err, finishErr) {
			t.Fatalf("done error = %v, want finish persistence failure", err)
		}
	case <-time.After(time.Second):
		t.Fatal("run did not finish")
	}
	select {
	case event := <-completed:
		t.Fatalf("completion reported after failed finish: %+v", event)
	default:
	}
	select {
	case err := <-reported:
		if !errors.Is(err, finishErr) {
			t.Fatalf("reported error = %v, want finish persistence failure", err)
		}
	default:
		t.Fatal("finish persistence failure was not reported")
	}
}

func TestTMDBRunController_ReportsJoinedPersistenceFailuresOnce(t *testing.T) {
	t.Parallel()
	progressErr := errors.New("progress persistence failed")
	finishErr := errors.New("finish persistence failed")
	completed := make(chan tmdbRunCompletion, 1)
	reported := make(chan error, 2)
	ledger := &recordingTMDBRunLedger{
		updateErrs: []error{progressErr},
		finishErr:  finishErr,
	}
	controller := newTMDBRunController(
		context.Background(),
		&recordingTMDBRunCandidates{all: []tmdbRunSubject{{MovieID: 603}}},
		&recordingTMDBRunEnricher{},
		&fakeTMDBRunConfigSource{snapshot: integrationtmdb.RuntimeSnapshot{
			Config: integrationtmdb.RuntimeConfig{Enabled: true},
		}},
		ledger,
		&fixedTMDBRunClock{now: time.Date(2026, time.August, 4, 21, 45, 0, 0, time.UTC)},
		nil,
		withTMDBRunCompletion(func(event tmdbRunCompletion) { completed <- event }),
		withTMDBRunError(func(err error) { reported <- err }),
	)
	t.Cleanup(controller.Close)

	result, err := controller.Start(context.Background(), tmdbRunStart{
		Operation: integration.RunOperationReEnrichAll,
		Trigger:   integration.RunTriggerManual,
	})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	select {
	case err := <-result.Done:
		if !errors.Is(err, progressErr) || !errors.Is(err, finishErr) {
			t.Fatalf("done error = %v, want joined persistence failures", err)
		}
	case <-time.After(time.Second):
		t.Fatal("run did not finish")
	}

	select {
	case event := <-completed:
		t.Fatalf("completion reported after failed finish: %+v", event)
	default:
	}
	if len(reported) != 1 {
		t.Fatalf("reported persistence failures = %d, want one joined error", len(reported))
	}
	err = <-reported
	if !errors.Is(err, progressErr) || !errors.Is(err, finishErr) {
		t.Fatalf("reported error = %v, want joined persistence failures", err)
	}
}

func TestTMDBRunController_AllowsOnlyOneQueuedOrRunningLibraryRun(t *testing.T) {
	t.Parallel()
	candidates := &blockingTMDBRunCandidates{started: make(chan struct{}), release: make(chan struct{})}
	controller := newTMDBRunController(
		context.Background(),
		candidates,
		&recordingTMDBRunEnricher{},
		&fakeTMDBRunConfigSource{snapshot: integrationtmdb.RuntimeSnapshot{
			Config: integrationtmdb.RuntimeConfig{Enabled: true, TTL: 24 * time.Hour, BatchLimit: 20},
		}},
		&recordingTMDBRunLedger{},
		&fixedTMDBRunClock{now: time.Date(2026, time.August, 4, 22, 0, 0, 0, time.UTC)},
		nil,
	)
	t.Cleanup(controller.Close)
	type startResponse struct {
		result tmdbRunStartResult
		err    error
	}
	first := make(chan startResponse, 1)
	go func() {
		result, err := controller.Start(context.Background(), tmdbRunStart{
			Operation: integration.RunOperationRefreshStale,
			Trigger:   integration.RunTriggerScheduled,
		})
		first <- startResponse{result: result, err: err}
	}()
	select {
	case <-candidates.started:
	case <-time.After(time.Second):
		t.Fatal("candidate selection did not start")
	}

	_, err := controller.Start(context.Background(), tmdbRunStart{
		Operation: integration.RunOperationRefreshStale,
		Trigger:   integration.RunTriggerManual,
	})
	if !errors.Is(err, ErrTMDBLibraryRunActive) {
		t.Fatalf("overlapping start error = %v, want %v", err, ErrTMDBLibraryRunActive)
	}
	close(candidates.release)
	select {
	case response := <-first:
		if response.err != nil || !response.result.NoWork {
			t.Fatalf("first start = result %+v, error %v", response.result, response.err)
		}
	case <-time.After(time.Second):
		t.Fatal("first start did not return after selection resumed")
	}
}

func TestTMDBRunController_CloseCancelsAndWaitsForCandidateSelection(t *testing.T) {
	t.Parallel()
	candidates := &cancellableTMDBRunCandidates{
		started: make(chan struct{}), cancelled: make(chan struct{}), allowReturn: make(chan struct{}),
	}
	controller := newTMDBRunController(
		context.Background(),
		candidates,
		&recordingTMDBRunEnricher{},
		&fakeTMDBRunConfigSource{snapshot: integrationtmdb.RuntimeSnapshot{
			Config: integrationtmdb.RuntimeConfig{Enabled: true, TTL: 24 * time.Hour, BatchLimit: 20},
		}},
		&recordingTMDBRunLedger{},
		&fixedTMDBRunClock{now: time.Date(2026, time.August, 4, 23, 0, 0, 0, time.UTC)},
		nil,
	)
	startResult := make(chan error, 1)
	go func() {
		_, err := controller.Start(context.Background(), tmdbRunStart{
			Operation: integration.RunOperationRefreshStale,
			Trigger:   integration.RunTriggerManual,
		})
		startResult <- err
	}()
	select {
	case <-candidates.started:
	case <-time.After(time.Second):
		t.Fatal("candidate selection did not start")
	}
	closed := make(chan struct{})
	go func() {
		controller.Close()
		close(closed)
	}()

	cancelled := false
	closedBeforeSelection := false
	select {
	case <-candidates.cancelled:
		cancelled = true
	case <-closed:
		closedBeforeSelection = true
	case <-time.After(time.Second):
	}
	close(candidates.allowReturn)
	select {
	case err := <-startResult:
		if cancelled && !errors.Is(err, context.Canceled) {
			t.Fatalf("start error = %v, want context cancellation", err)
		}
	case <-time.After(time.Second):
		t.Fatal("start did not return")
	}
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("close did not return after candidate selection stopped")
	}
	if !cancelled || closedBeforeSelection {
		t.Fatalf("shutdown lifecycle = cancelled %t, close-before-selection %t", cancelled, closedBeforeSelection)
	}
}

func TestTMDBRunController_ReleasesRunBeforeCompletionNotification(t *testing.T) {
	t.Parallel()
	type startResponse struct {
		result tmdbRunStartResult
		err    error
	}
	next := make(chan startResponse, 1)
	var controller *tmdbRunController
	var startNext sync.Once
	controller = newTMDBRunController(
		context.Background(),
		&recordingTMDBRunCandidates{all: []tmdbRunSubject{{MovieID: 603}}},
		&recordingTMDBRunEnricher{},
		&fakeTMDBRunConfigSource{snapshot: integrationtmdb.RuntimeSnapshot{
			Config: integrationtmdb.RuntimeConfig{Enabled: true},
		}},
		&recordingTMDBRunLedger{},
		&fixedTMDBRunClock{now: time.Date(2026, time.August, 5, 0, 0, 0, 0, time.UTC)},
		nil,
		withTMDBRunCompletion(func(tmdbRunCompletion) {
			startNext.Do(func() {
				result, err := controller.Start(context.Background(), tmdbRunStart{
					Operation: integration.RunOperationReEnrichAll,
					Trigger:   integration.RunTriggerScheduled,
				})
				next <- startResponse{result: result, err: err}
			})
		}),
	)
	t.Cleanup(controller.Close)

	first, err := controller.Start(context.Background(), tmdbRunStart{
		Operation: integration.RunOperationReEnrichAll,
		Trigger:   integration.RunTriggerManual,
	})
	if err != nil {
		t.Fatalf("first start: %v", err)
	}
	waitTMDBRun(t, first.Done)
	select {
	case response := <-next:
		if response.err != nil {
			t.Fatalf("start from completion notification: %v", response.err)
		}
		waitTMDBRun(t, response.result.Done)
	case <-time.After(time.Second):
		t.Fatal("completion notification did not start the next run")
	}
}
