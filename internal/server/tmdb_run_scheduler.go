package server

import (
	"context"
	"errors"
	"sync"
	"time"

	"moviepickarr/internal/integration"
	integrationtmdb "moviepickarr/internal/integration/tmdb"
)

type tmdbRunSchedulerRunStarter interface {
	Start(context.Context, tmdbRunStart) (tmdbRunStartResult, error)
}

type tmdbRunSchedulerConfigSource interface {
	tmdbRunConfigSource
	Get(context.Context) (integrationtmdb.ConfigView, error)
}

type tmdbRunSchedulerActivityStore interface {
	UpdateNextCheck(context.Context, string, *time.Time) error
	UpdateSchedule(context.Context, string, time.Time, *time.Time) error
}

type tmdbRunSchedulerTimer interface {
	Stop() bool
}

type tmdbRunSchedulerClock interface {
	Now() time.Time
	AfterFunc(time.Duration, func()) tmdbRunSchedulerTimer
}

type systemTMDBRunSchedulerClock struct{}

func (systemTMDBRunSchedulerClock) Now() time.Time { return time.Now().UTC() }

func (systemTMDBRunSchedulerClock) AfterFunc(delay time.Duration, callback func()) tmdbRunSchedulerTimer {
	return time.AfterFunc(delay, callback)
}

type tmdbRunScheduler struct {
	ctx      context.Context
	cancel   context.CancelFunc
	configs  tmdbRunSchedulerConfigSource
	runs     tmdbRunSchedulerRunStarter
	activity tmdbRunSchedulerActivityStore
	clock    tmdbRunSchedulerClock
	onError  func(error)

	operations sync.Mutex
	state      sync.Mutex
	timer      tmdbRunSchedulerTimer
	generation uint64
	started    bool
	closed     bool
	work       sync.WaitGroup
	closeOnce  sync.Once
}

func newTMDBRunScheduler(
	parent context.Context,
	configs tmdbRunSchedulerConfigSource,
	runs tmdbRunSchedulerRunStarter,
	activity tmdbRunSchedulerActivityStore,
	clock tmdbRunSchedulerClock,
	onError func(error),
) *tmdbRunScheduler {
	if parent == nil {
		parent = context.Background()
	}
	if clock == nil {
		clock = systemTMDBRunSchedulerClock{}
	}
	ctx, cancel := context.WithCancel(parent)
	return &tmdbRunScheduler{
		ctx:      ctx,
		cancel:   cancel,
		configs:  configs,
		runs:     runs,
		activity: activity,
		clock:    clock,
		onError:  onError,
	}
}

func (s *tmdbRunScheduler) Start() error {
	if !s.beginWork() {
		return context.Canceled
	}
	defer s.work.Done()

	s.operations.Lock()
	defer s.operations.Unlock()

	s.state.Lock()
	if s.started {
		s.state.Unlock()
		return nil
	}
	s.started = true
	generation := s.invalidateTimerLocked()
	s.state.Unlock()

	return s.configureFrom(generation, s.clock.Now())
}

func (s *tmdbRunScheduler) Reconfigure() error {
	if !s.beginWork() {
		return context.Canceled
	}
	defer s.work.Done()

	s.operations.Lock()
	defer s.operations.Unlock()

	s.state.Lock()
	s.started = true
	generation := s.invalidateTimerLocked()
	s.state.Unlock()

	return s.configureFrom(generation, s.clock.Now())
}

func (s *tmdbRunScheduler) AuthenticationRejected(revision int64) error {
	if !s.beginWork() {
		return context.Canceled
	}
	defer s.work.Done()

	s.operations.Lock()
	defer s.operations.Unlock()
	view, err := s.configs.Get(s.ctx)
	if err != nil {
		return err
	}
	if view.Revision != revision {
		return nil
	}

	s.state.Lock()
	s.invalidateTimerLocked()
	s.state.Unlock()
	return s.activity.UpdateNextCheck(s.ctx, "tmdb", nil)
}

func (s *tmdbRunScheduler) Close() {
	s.closeOnce.Do(func() {
		s.state.Lock()
		s.closed = true
		s.invalidateTimerLocked()
		s.cancel()
		s.state.Unlock()
		s.work.Wait()
	})
}

func (s *tmdbRunScheduler) configureFrom(generation uint64, scheduledFrom time.Time) error {
	snapshot, err := s.configs.Acquire(s.ctx)
	if err != nil {
		clearErr := s.activity.UpdateNextCheck(s.ctx, "tmdb", nil)
		if tmdbScheduleUnavailable(err) {
			return clearErr
		}
		return errors.Join(err, clearErr)
	}
	interval := snapshot.Config.RefreshInterval
	if !snapshot.Config.Enabled || snapshot.Config.APIKey == "" || interval <= 0 {
		return s.activity.UpdateNextCheck(s.ctx, "tmdb", nil)
	}

	next := scheduledFrom.Add(interval)
	if err := s.activity.UpdateNextCheck(s.ctx, "tmdb", &next); err != nil {
		return err
	}
	s.installTimer(generation, interval)
	return nil
}

func (s *tmdbRunScheduler) installTimer(generation uint64, interval time.Duration) {
	s.state.Lock()
	defer s.state.Unlock()
	if s.closed || !s.started || s.generation != generation {
		return
	}
	s.timer = s.clock.AfterFunc(interval, func() {
		s.fire(generation, interval)
	})
}

func (s *tmdbRunScheduler) fire(generation uint64, interval time.Duration) {
	if !s.beginTimerWork(generation) {
		return
	}
	defer s.work.Done()

	s.operations.Lock()
	err := s.fireLocked(generation, interval)
	s.operations.Unlock()
	if err != nil && s.onError != nil {
		s.onError(err)
	}
}

func (s *tmdbRunScheduler) fireLocked(generation uint64, interval time.Duration) error {
	if !s.generationCurrent(generation) {
		return nil
	}
	firedAt := s.clock.Now()
	result, startErr := s.runs.Start(s.ctx, tmdbRunStart{
		Operation: integration.RunOperationRefreshStale,
		Trigger:   integration.RunTriggerScheduled,
	})
	if s.ctx.Err() != nil {
		return nil
	}
	if tmdbScheduleUnavailable(startErr) {
		s.pauseGeneration(generation)
		return s.activity.UpdateNextCheck(s.ctx, "tmdb", nil)
	}

	checkedAt := time.Time{}
	if startErr == nil && result.NoWork {
		checkedAt = result.CheckedAt
		if checkedAt.IsZero() {
			checkedAt = firedAt
		}
	}
	if !s.generationCurrent(generation) {
		return startErr
	}
	next := firedAt.Add(interval)
	var scheduleErr error
	if checkedAt.IsZero() {
		scheduleErr = s.activity.UpdateNextCheck(s.ctx, "tmdb", &next)
	} else {
		scheduleErr = s.activity.UpdateSchedule(s.ctx, "tmdb", checkedAt, &next)
	}
	if scheduleErr == nil {
		s.installTimer(generation, interval)
	}
	if errors.Is(startErr, ErrTMDBLibraryRunActive) {
		startErr = nil
	}
	return errors.Join(startErr, scheduleErr)
}

func (s *tmdbRunScheduler) beginWork() bool {
	s.state.Lock()
	defer s.state.Unlock()
	if s.closed {
		return false
	}
	s.work.Add(1)
	return true
}

func (s *tmdbRunScheduler) beginTimerWork(generation uint64) bool {
	s.state.Lock()
	defer s.state.Unlock()
	if s.closed || !s.started || s.generation != generation {
		return false
	}
	s.timer = nil
	s.work.Add(1)
	return true
}

func (s *tmdbRunScheduler) invalidateTimerLocked() uint64 {
	s.generation++
	if s.timer != nil {
		s.timer.Stop()
		s.timer = nil
	}
	return s.generation
}

func (s *tmdbRunScheduler) pauseGeneration(generation uint64) {
	s.state.Lock()
	defer s.state.Unlock()
	if s.generation == generation {
		s.invalidateTimerLocked()
	}
}

func (s *tmdbRunScheduler) generationCurrent(generation uint64) bool {
	s.state.Lock()
	defer s.state.Unlock()
	return !s.closed && s.started && s.generation == generation
}

func tmdbScheduleUnavailable(err error) bool {
	return errors.Is(err, integrationtmdb.ErrRuntimeDisabled) ||
		errors.Is(err, integrationtmdb.ErrAPIKeyRejected) ||
		errors.Is(err, integrationtmdb.ErrAuthentication) ||
		errors.Is(err, integration.ErrCredentialUnavailable)
}
