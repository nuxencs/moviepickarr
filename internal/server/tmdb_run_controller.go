package server

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
	"unicode"

	"moviepickarr/internal/integration"
	integrationtmdb "moviepickarr/internal/integration/tmdb"
)

type tmdbRunSubject struct {
	MovieID int
	Label   string
}

type tmdbRunCandidateSource interface {
	RefreshStale(context.Context, time.Time, int) ([]tmdbRunSubject, error)
	ReEnrichAll(context.Context) ([]tmdbRunSubject, error)
}

type tmdbRunSubjectEnricher interface {
	EnrichOne(context.Context, integrationtmdb.RuntimeSnapshot, int) (enrichResult, error)
}

type tmdbRunConfigSource interface {
	Acquire(context.Context) (integrationtmdb.RuntimeSnapshot, error)
}

type tmdbRunClock interface {
	Now() time.Time
}

type tmdbRunStart struct {
	Operation   integration.RunOperation
	Trigger     integration.RunTrigger
	InitiatedBy *int
}

type tmdbRunStartResult struct {
	Run       *integration.Run
	NoWork    bool
	CheckedAt time.Time
	Done      <-chan error
}

type tmdbRunCompletion struct {
	RunID      int64
	Status     integration.RunStatus
	FinishedAt time.Time
}

type tmdbRunControllerOption func(*tmdbRunController)

func withTMDBRunCompletion(callback func(tmdbRunCompletion)) tmdbRunControllerOption {
	return func(controller *tmdbRunController) {
		controller.onCompletion = callback
	}
}

func withTMDBRunError(callback func(error)) tmdbRunControllerOption {
	return func(controller *tmdbRunController) {
		controller.onError = callback
	}
}

var (
	ErrTMDBLibraryRunActive = errors.New("TMDB library run is already active")
	ErrTMDBRunNotActive     = errors.New("TMDB run is not active")
)

const (
	tmdbRunProgressUpdateEvery    = 10
	tmdbRunProgressUpdateInterval = 2 * time.Second
)

type systemTMDBRunClock struct{}

func (systemTMDBRunClock) Now() time.Time { return time.Now().UTC() }

type tmdbRunController struct {
	ctx                      context.Context
	cancel                   context.CancelFunc
	candidates               tmdbRunCandidateSource
	enricher                 tmdbRunSubjectEnricher
	configs                  tmdbRunConfigSource
	ledger                   integration.RunLedger
	clock                    tmdbRunClock
	onAuthenticationRejected func(integrationtmdb.RuntimeSnapshot)
	onEnriched               func(int)
	onCompletion             func(tmdbRunCompletion)
	onError                  func(error)

	mu     sync.Mutex
	closed bool
	active *tmdbActiveRun
	wg     sync.WaitGroup
}

type tmdbActiveRun struct {
	id              int64
	cancelRequested bool
}

func newTMDBRunController(
	parent context.Context,
	candidates tmdbRunCandidateSource,
	enricher tmdbRunSubjectEnricher,
	configs tmdbRunConfigSource,
	ledger integration.RunLedger,
	clock tmdbRunClock,
	onAuthenticationRejected func(integrationtmdb.RuntimeSnapshot),
	options ...tmdbRunControllerOption,
) *tmdbRunController {
	if parent == nil {
		parent = context.Background()
	}
	if clock == nil {
		clock = systemTMDBRunClock{}
	}
	ctx, cancel := context.WithCancel(parent)
	controller := &tmdbRunController{
		ctx:                      ctx,
		cancel:                   cancel,
		candidates:               candidates,
		enricher:                 enricher,
		configs:                  configs,
		ledger:                   ledger,
		clock:                    clock,
		onAuthenticationRejected: onAuthenticationRejected,
	}
	for _, option := range options {
		if option != nil {
			option(controller)
		}
	}
	return controller
}

func (c *tmdbRunController) Start(ctx context.Context, request tmdbRunStart) (tmdbRunStartResult, error) {
	switch request.Trigger {
	case integration.RunTriggerManual,
		integration.RunTriggerConfiguration,
		integration.RunTriggerScheduled,
		integration.RunTriggerStartup:
	default:
		return tmdbRunStartResult{}, fmt.Errorf("unsupported TMDB library run trigger %q", request.Trigger)
	}
	if err := c.reserve(); err != nil {
		return tmdbRunStartResult{}, err
	}
	lifecycleOwned := true
	defer func() {
		if lifecycleOwned {
			c.finishLifecycle()
		}
	}()
	if ctx == nil {
		ctx = context.Background()
	}
	startCtx, cancelStart := context.WithCancel(ctx)
	stopControllerCancellation := context.AfterFunc(c.ctx, cancelStart)
	defer func() {
		stopControllerCancellation()
		cancelStart()
	}()

	snapshot, err := c.configs.Acquire(startCtx)
	if err != nil {
		return tmdbRunStartResult{}, err
	}
	if !snapshot.Config.Enabled {
		return tmdbRunStartResult{}, integrationtmdb.ErrRuntimeDisabled
	}
	startedAt := c.clock.Now()
	var subjects []tmdbRunSubject
	switch request.Operation {
	case integration.RunOperationRefreshStale:
		subjects, err = c.candidates.RefreshStale(startCtx, startedAt.Add(-snapshot.Config.TTL), snapshot.Config.BatchLimit)
	case integration.RunOperationReEnrichAll:
		subjects, err = c.candidates.ReEnrichAll(startCtx)
	default:
		err = fmt.Errorf("unsupported TMDB run operation %q", request.Operation)
	}
	if err != nil {
		return tmdbRunStartResult{}, err
	}
	if len(subjects) == 0 {
		return tmdbRunStartResult{NoWork: true, CheckedAt: c.clock.Now()}, nil
	}

	run, err := c.ledger.Start(startCtx, integration.RunStart{
		Integration:    "tmdb",
		Operation:      request.Operation,
		Trigger:        request.Trigger,
		InitiatedBy:    request.InitiatedBy,
		ConfigRevision: snapshot.Revision,
		StartedAt:      startedAt,
		Total:          len(subjects),
	})
	if err != nil {
		return tmdbRunStartResult{}, err
	}
	c.setActiveRunID(run.ID)
	done := make(chan error, 1)
	lifecycleOwned = false
	go c.execute(run.ID, snapshot, subjects, done)
	return tmdbRunStartResult{Run: run, Done: done}, nil
}

func (c *tmdbRunController) Cancel(runID int64) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.active == nil || c.active.id != runID {
		return ErrTMDBRunNotActive
	}
	c.active.cancelRequested = true
	return nil
}

func (c *tmdbRunController) reserve() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.active != nil {
		return ErrTMDBLibraryRunActive
	}
	if c.closed {
		return c.ctx.Err()
	}
	if err := c.ctx.Err(); err != nil {
		return err
	}
	c.active = &tmdbActiveRun{}
	c.wg.Add(1)
	return nil
}

func (c *tmdbRunController) setActiveRunID(runID int64) {
	c.mu.Lock()
	c.active.id = runID
	c.mu.Unlock()
}

func (c *tmdbRunController) cancellationRequested() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.active != nil && c.active.cancelRequested
}

func (c *tmdbRunController) setEnrichedCallback(callback func(int)) {
	c.mu.Lock()
	c.onEnriched = callback
	c.mu.Unlock()
}

func (c *tmdbRunController) notifyEnriched(movieID int) {
	c.mu.Lock()
	callback := c.onEnriched
	c.mu.Unlock()
	if callback != nil {
		callback(movieID)
	}
}

func (c *tmdbRunController) reportError(err error) {
	if err != nil && c.onError != nil {
		c.onError(err)
	}
}

func (c *tmdbRunController) release() {
	c.mu.Lock()
	c.active = nil
	c.mu.Unlock()
}

func (c *tmdbRunController) finishLifecycle() {
	c.release()
	c.wg.Done()
}

func (c *tmdbRunController) execute(
	runID int64,
	snapshot integrationtmdb.RuntimeSnapshot,
	subjects []tmdbRunSubject,
	done chan<- error,
) {
	released := false
	defer func() {
		if !released {
			c.release()
		}
		c.wg.Done()
	}()
	progress := integration.RunProgress{Total: len(subjects), Remaining: len(subjects)}
	failedSubjects := make([]integration.FailedSubject, 0)
	status := integration.RunStatus("")
	errorSummary := ""
	updateCtx := context.WithoutCancel(c.ctx)
	lastWrittenProgress := progress
	lastWrittenAt := c.clock.Now()
	wroteProgress := false
	lastWriteSucceeded := false
	var progressErr error
	flushProgress := func(force bool) {
		now := c.clock.Now()
		if !force &&
			progress.Processed-lastWrittenProgress.Processed < tmdbRunProgressUpdateEvery &&
			now.Sub(lastWrittenAt) < tmdbRunProgressUpdateInterval {
			return
		}
		if wroteProgress && lastWriteSucceeded && progress == lastWrittenProgress {
			return
		}
		err := c.ledger.Update(updateCtx, runID, progress)
		if err != nil {
			reportedErr := fmt.Errorf("updating TMDB run %d progress: %w", runID, err)
			if progressErr == nil {
				progressErr = reportedErr
			}
		}
		lastWrittenProgress = progress
		lastWrittenAt = now
		wroteProgress = true
		lastWriteSucceeded = err == nil
	}
	for _, subject := range subjects {
		if c.ctx.Err() != nil {
			status = integration.RunStatusInterrupted
			break
		}
		if c.cancellationRequested() {
			status = integration.RunStatusCancelled
			break
		}
		_, err := c.enricher.EnrichOne(c.ctx, snapshot, subject.MovieID)
		if c.ctx.Err() != nil && (errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)) {
			status = integration.RunStatusInterrupted
			break
		}
		progress.Processed++
		progress.Remaining--
		stop := false
		switch {
		case err == nil:
			progress.Succeeded++
			c.notifyEnriched(subject.MovieID)
		case isTMDBRunSkip(err):
			progress.Skipped++
		case errors.Is(err, errTMDBAuthentication),
			errors.Is(err, integrationtmdb.ErrAuthentication),
			errors.Is(err, integrationtmdb.ErrAPIKeyRejected):
			progress.Failed++
			failedSubjects = appendTMDBRunFailure(failedSubjects, subject, "API key rejected")
			if c.onAuthenticationRejected != nil {
				c.onAuthenticationRejected(snapshot)
			}
			status = integration.RunStatusFailed
			errorSummary = "API key rejected"
			stop = true
		default:
			progress.Failed++
			failedSubjects = appendTMDBRunFailure(failedSubjects, subject, "TMDB enrichment failed")
		}
		flushProgress(false)
		if stop {
			break
		}
		if c.ctx.Err() != nil {
			status = integration.RunStatusInterrupted
			break
		}
		if c.cancellationRequested() {
			status = integration.RunStatusCancelled
			break
		}
	}

	if status == "" {
		status = integration.RunStatusCompleted
	}
	if status == integration.RunStatusCompleted && progress.Failed > 0 {
		status = integration.RunStatusCompletedWithErrors
		if progress.Failed == progress.Processed {
			status = integration.RunStatusFailed
		}
		if progress.Failed == 1 {
			errorSummary = "1 subject failed"
		} else {
			errorSummary = fmt.Sprintf("%d subjects failed", progress.Failed)
		}
	}
	flushProgress(true)
	finishedAt := c.clock.Now()
	_, finishErr := c.ledger.Finish(updateCtx, runID, integration.RunFinish{
		Status:         status,
		FinishedAt:     finishedAt,
		Progress:       progress,
		ErrorSummary:   errorSummary,
		FailedSubjects: failedSubjects,
	})
	if finishErr != nil {
		finishErr = fmt.Errorf("finishing TMDB run %d: %w", runID, finishErr)
	}
	c.release()
	released = true
	persistenceErr := errors.Join(progressErr, finishErr)
	c.reportError(persistenceErr)
	if finishErr == nil && c.onCompletion != nil {
		c.onCompletion(tmdbRunCompletion{RunID: runID, Status: status, FinishedAt: finishedAt})
	}
	done <- persistenceErr
	close(done)
}

func isTMDBRunSkip(err error) bool {
	return errors.Is(err, ErrEnrichNoIMDbID) ||
		errors.Is(err, ErrEnrichNotFound) ||
		errors.Is(err, ErrEnrichSuperseded)
}

func appendTMDBRunFailure(
	failed []integration.FailedSubject,
	subject tmdbRunSubject,
	message string,
) []integration.FailedSubject {
	if len(failed) >= integration.FailedSubjectSampleLimit {
		return failed
	}
	label := strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, subject.Label)
	label = strings.Join(strings.Fields(label), " ")
	if label == "" {
		label = fmt.Sprintf("movie:%d", subject.MovieID)
	}
	runes := []rune(label)
	if len(runes) > 160 {
		label = string(runes[:160])
	}
	return append(failed, integration.FailedSubject{Subject: label, Error: message})
}

func (c *tmdbRunController) Close() {
	c.cancel()
	c.mu.Lock()
	c.closed = true
	c.mu.Unlock()
	c.wg.Wait()
}
