package server

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"moviepickarr/internal/integration"
	integrationtmdb "moviepickarr/internal/integration/tmdb"
)

type schedulerConfigSource struct {
	mu       sync.Mutex
	snapshot integrationtmdb.RuntimeSnapshot
	err      error
	calls    int
}

func (s *schedulerConfigSource) Acquire(context.Context) (integrationtmdb.RuntimeSnapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	return s.snapshot, s.err
}

func (s *schedulerConfigSource) Get(context.Context) (integrationtmdb.ConfigView, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return integrationtmdb.ConfigView{Revision: s.snapshot.Revision}, nil
}

func (s *schedulerConfigSource) set(snapshot integrationtmdb.RuntimeSnapshot, err error) {
	s.mu.Lock()
	s.snapshot = snapshot
	s.err = err
	s.mu.Unlock()
}

type schedulerRunStarter struct {
	mu      sync.Mutex
	starts  []tmdbRunStart
	result  tmdbRunStartResult
	err     error
	startFn func(context.Context, tmdbRunStart) (tmdbRunStartResult, error)
}

func (s *schedulerRunStarter) Start(ctx context.Context, start tmdbRunStart) (tmdbRunStartResult, error) {
	s.mu.Lock()
	s.starts = append(s.starts, start)
	startFn := s.startFn
	result := s.result
	err := s.err
	s.mu.Unlock()
	if startFn != nil {
		return startFn(ctx, start)
	}
	return result, err
}

func (s *schedulerRunStarter) startCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.starts)
}

type schedulerActivityStore struct {
	mu              sync.Mutex
	nextChecks      []*time.Time
	lastChecked     []time.Time
	scheduleUpdates int
}

func (s *schedulerActivityStore) UpdateNextCheck(_ context.Context, integrationID string, next *time.Time) error {
	if integrationID != "tmdb" {
		return errors.New("unexpected integration")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if next == nil {
		s.nextChecks = append(s.nextChecks, nil)
		return nil
	}
	copied := *next
	s.nextChecks = append(s.nextChecks, &copied)
	return nil
}

func (s *schedulerActivityStore) UpdateSchedule(
	_ context.Context,
	integrationID string,
	checkedAt time.Time,
	next *time.Time,
) error {
	if integrationID != "tmdb" {
		return errors.New("unexpected integration")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastChecked = append(s.lastChecked, checkedAt)
	s.scheduleUpdates++
	if next == nil {
		s.nextChecks = append(s.nextChecks, nil)
		return nil
	}
	copied := *next
	s.nextChecks = append(s.nextChecks, &copied)
	return nil
}

func (s *schedulerActivityStore) latestNext() *time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.nextChecks) == 0 || s.nextChecks[len(s.nextChecks)-1] == nil {
		return nil
	}
	copied := *s.nextChecks[len(s.nextChecks)-1]
	return &copied
}

func (s *schedulerActivityStore) checked() []time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]time.Time(nil), s.lastChecked...)
}

type manualSchedulerClock struct {
	mu     sync.Mutex
	now    time.Time
	timers []*manualSchedulerTimer
}

func (c *manualSchedulerClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *manualSchedulerClock) AfterFunc(delay time.Duration, callback func()) tmdbRunSchedulerTimer {
	c.mu.Lock()
	defer c.mu.Unlock()
	timer := &manualSchedulerTimer{delay: delay, callback: callback}
	c.timers = append(c.timers, timer)
	return timer
}

func (c *manualSchedulerClock) setNow(now time.Time) {
	c.mu.Lock()
	c.now = now
	c.mu.Unlock()
}

func (c *manualSchedulerClock) latestTimer(t *testing.T) *manualSchedulerTimer {
	t.Helper()
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.timers) == 0 {
		t.Fatal("expected a scheduled timer")
	}
	return c.timers[len(c.timers)-1]
}

func (c *manualSchedulerClock) timerCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.timers)
}

type manualSchedulerTimer struct {
	mu       sync.Mutex
	delay    time.Duration
	callback func()
	stopped  bool
	fired    bool
}

func (t *manualSchedulerTimer) Stop() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.stopped || t.fired {
		return false
	}
	t.stopped = true
	return true
}

func (t *manualSchedulerTimer) Fire() {
	t.mu.Lock()
	if t.stopped || t.fired {
		t.mu.Unlock()
		return
	}
	t.fired = true
	callback := t.callback
	t.mu.Unlock()
	callback()
}

func (t *manualSchedulerTimer) isStopped() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.stopped
}

func availableSchedulerSnapshot(interval time.Duration) integrationtmdb.RuntimeSnapshot {
	return integrationtmdb.RuntimeSnapshot{
		Revision: 4,
		Config: integrationtmdb.RuntimeConfig{
			Enabled:         true,
			APIKey:          "configured",
			RefreshInterval: interval,
		},
	}
}

func newSchedulerFixture(now time.Time, interval time.Duration) (
	*tmdbRunScheduler,
	*schedulerConfigSource,
	*schedulerRunStarter,
	*schedulerActivityStore,
	*manualSchedulerClock,
) {
	configs := &schedulerConfigSource{snapshot: availableSchedulerSnapshot(interval)}
	runs := &schedulerRunStarter{}
	activity := &schedulerActivityStore{}
	clock := &manualSchedulerClock{now: now}
	scheduler := newTMDBRunScheduler(context.Background(), configs, runs, activity, clock, nil)
	return scheduler, configs, runs, activity, clock
}

func TestTMDBRunScheduler_StartSchedulesFromStartTimeAndPersistsNextCheck(t *testing.T) {
	startedAt := time.Date(2026, time.August, 4, 10, 0, 0, 0, time.UTC)
	scheduler, configs, runs, activity, clock := newSchedulerFixture(startedAt, time.Hour)
	t.Cleanup(scheduler.Close)

	if err := scheduler.Start(); err != nil {
		t.Fatalf("start scheduler: %v", err)
	}

	next := activity.latestNext()
	if next == nil || !next.Equal(startedAt.Add(time.Hour)) {
		t.Fatalf("next check = %v, want %v", next, startedAt.Add(time.Hour))
	}
	if timer := clock.latestTimer(t); timer.delay != time.Hour {
		t.Fatalf("timer delay = %v, want 1h", timer.delay)
	}
	if configs.calls != 1 {
		t.Fatalf("config acquire calls = %d, want 1", configs.calls)
	}
	if runs.startCount() != 0 {
		t.Fatalf("run starts = %d before timer fires, want 0", runs.startCount())
	}
}

func TestTMDBRunScheduler_UnavailableOrUnscheduledWaitsForReconfigure(t *testing.T) {
	tests := []struct {
		name     string
		snapshot integrationtmdb.RuntimeSnapshot
		err      error
		wantErr  bool
	}{
		{name: "disabled", snapshot: integrationtmdb.RuntimeSnapshot{Config: integrationtmdb.RuntimeConfig{RefreshInterval: time.Hour}}, err: integrationtmdb.ErrRuntimeDisabled},
		{name: "disabled snapshot", snapshot: integrationtmdb.RuntimeSnapshot{Config: integrationtmdb.RuntimeConfig{RefreshInterval: time.Hour}}},
		{name: "suspended", snapshot: availableSchedulerSnapshot(time.Hour), err: integrationtmdb.ErrAPIKeyRejected},
		{name: "credential unavailable", err: integration.ErrCredentialUnavailable},
		{name: "interval zero", snapshot: availableSchedulerSnapshot(0)},
		{name: "config read failure", err: errors.New("read config"), wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			startedAt := time.Date(2026, time.August, 4, 10, 0, 0, 0, time.UTC)
			scheduler, configs, runs, activity, clock := newSchedulerFixture(startedAt, time.Hour)
			configs.set(tt.snapshot, tt.err)
			t.Cleanup(scheduler.Close)

			err := scheduler.Start()
			if (err != nil) != tt.wantErr {
				t.Fatalf("Start() error = %v, want error %v", err, tt.wantErr)
			}
			if activity.latestNext() != nil {
				t.Fatalf("next check = %v, want cleared", activity.latestNext())
			}
			if clock.timerCount() != 0 {
				t.Fatalf("timer count = %d, want 0", clock.timerCount())
			}
			if runs.startCount() != 0 {
				t.Fatalf("run starts = %d, want 0", runs.startCount())
			}

			reconfiguredAt := startedAt.Add(20 * time.Minute)
			clock.setNow(reconfiguredAt)
			configs.set(availableSchedulerSnapshot(2*time.Hour), nil)
			if err := scheduler.Reconfigure(); err != nil {
				t.Fatalf("reconfigure scheduler: %v", err)
			}
			next := activity.latestNext()
			if next == nil || !next.Equal(reconfiguredAt.Add(2*time.Hour)) {
				t.Fatalf("next check after reconfigure = %v, want %v", next, reconfiguredAt.Add(2*time.Hour))
			}
		})
	}
}

func TestTMDBRunScheduler_RunAuthenticationFailureClearsSchedule(t *testing.T) {
	startedAt := time.Date(2026, time.August, 4, 10, 0, 0, 0, time.UTC)
	scheduler, _, runs, activity, clock := newSchedulerFixture(startedAt, time.Hour)
	t.Cleanup(scheduler.Close)
	runs.err = integrationtmdb.ErrAPIKeyRejected
	if err := scheduler.Start(); err != nil {
		t.Fatalf("start scheduler: %v", err)
	}

	clock.setNow(startedAt.Add(time.Hour))
	clock.latestTimer(t).Fire()

	if activity.latestNext() != nil {
		t.Fatalf("next check after authentication failure = %v, want nil", activity.latestNext())
	}
	if clock.timerCount() != 1 {
		t.Fatalf("timer count = %d, want no replacement timer", clock.timerCount())
	}
}

func TestTMDBRunScheduler_TimerStartsScheduledRefreshAndTouchesNoWorkCheck(t *testing.T) {
	startedAt := time.Date(2026, time.August, 4, 10, 0, 0, 0, time.UTC)
	scheduler, _, runs, activity, clock := newSchedulerFixture(startedAt, time.Hour)
	t.Cleanup(scheduler.Close)
	checkedAt := startedAt.Add(time.Hour)
	runs.result = tmdbRunStartResult{NoWork: true, CheckedAt: checkedAt}
	if err := scheduler.Start(); err != nil {
		t.Fatalf("start scheduler: %v", err)
	}

	clock.setNow(checkedAt)
	clock.latestTimer(t).Fire()

	if runs.startCount() != 1 {
		t.Fatalf("run starts = %d, want 1", runs.startCount())
	}
	if got := runs.starts[0]; got.Operation != integration.RunOperationRefreshStale || got.Trigger != integration.RunTriggerScheduled || got.InitiatedBy != nil {
		t.Fatalf("scheduled start = %#v", got)
	}
	checked := activity.checked()
	if len(checked) != 1 || !checked[0].Equal(checkedAt) {
		t.Fatalf("last checked writes = %v, want [%v]", checked, checkedAt)
	}
	if activity.scheduleUpdates != 1 {
		t.Fatalf("atomic schedule writes = %d, want 1", activity.scheduleUpdates)
	}
	next := activity.latestNext()
	if next == nil || !next.Equal(checkedAt.Add(time.Hour)) {
		t.Fatalf("next check = %v, want %v", next, checkedAt.Add(time.Hour))
	}
}

func TestTMDBRunScheduler_ActiveRunConflictDoesNotOverlapAndReschedules(t *testing.T) {
	startedAt := time.Date(2026, time.August, 4, 10, 0, 0, 0, time.UTC)
	scheduler, _, runs, activity, clock := newSchedulerFixture(startedAt, 30*time.Minute)
	t.Cleanup(scheduler.Close)
	runs.err = ErrTMDBLibraryRunActive
	if err := scheduler.Start(); err != nil {
		t.Fatalf("start scheduler: %v", err)
	}

	firedAt := startedAt.Add(30 * time.Minute)
	clock.setNow(firedAt)
	clock.latestTimer(t).Fire()

	if runs.startCount() != 1 {
		t.Fatalf("run starts = %d, want one rejected attempt", runs.startCount())
	}
	if len(activity.checked()) != 0 {
		t.Fatalf("active conflict must not touch last checked: %v", activity.checked())
	}
	next := activity.latestNext()
	if next == nil || !next.Equal(firedAt.Add(30*time.Minute)) {
		t.Fatalf("next check = %v, want %v", next, firedAt.Add(30*time.Minute))
	}
}

func TestTMDBRunScheduler_ReconfigureReplacesTimerFromSaveTime(t *testing.T) {
	startedAt := time.Date(2026, time.August, 4, 10, 0, 0, 0, time.UTC)
	scheduler, configs, _, activity, clock := newSchedulerFixture(startedAt, time.Hour)
	t.Cleanup(scheduler.Close)
	if err := scheduler.Start(); err != nil {
		t.Fatalf("start scheduler: %v", err)
	}
	oldTimer := clock.latestTimer(t)

	reconfiguredAt := startedAt.Add(5 * time.Minute)
	clock.setNow(reconfiguredAt)
	configs.set(availableSchedulerSnapshot(3*time.Hour), nil)
	if err := scheduler.Reconfigure(); err != nil {
		t.Fatalf("reconfigure scheduler: %v", err)
	}

	if !oldTimer.isStopped() {
		t.Fatal("previous timer was not stopped")
	}
	if timer := clock.latestTimer(t); timer == oldTimer || timer.delay != 3*time.Hour {
		t.Fatalf("replacement timer = %#v, want new 3h timer", timer)
	}
	next := activity.latestNext()
	if next == nil || !next.Equal(reconfiguredAt.Add(3*time.Hour)) {
		t.Fatalf("next check = %v, want %v", next, reconfiguredAt.Add(3*time.Hour))
	}
}

func TestTMDBRunScheduler_AuthenticationRejectionPausesUntilValidReconfigure(t *testing.T) {
	startedAt := time.Date(2026, time.August, 4, 10, 0, 0, 0, time.UTC)
	scheduler, configs, runs, activity, clock := newSchedulerFixture(startedAt, time.Hour)
	t.Cleanup(scheduler.Close)
	if err := scheduler.Start(); err != nil {
		t.Fatalf("start scheduler: %v", err)
	}
	oldTimer := clock.latestTimer(t)

	if err := scheduler.AuthenticationRejected(3); err != nil {
		t.Fatalf("ignore stale authentication rejection: %v", err)
	}
	if oldTimer.isStopped() {
		t.Fatal("stale authentication rejection stopped the current revision timer")
	}

	if err := scheduler.AuthenticationRejected(4); err != nil {
		t.Fatalf("pause scheduler: %v", err)
	}
	if !oldTimer.isStopped() || activity.latestNext() != nil {
		t.Fatalf("authentication pause left timer active or next check set: stopped=%v next=%v", oldTimer.isStopped(), activity.latestNext())
	}
	oldTimer.Fire()
	if runs.startCount() != 0 {
		t.Fatalf("run starts after rejected timer fire = %d, want 0", runs.startCount())
	}

	configs.set(integrationtmdb.RuntimeSnapshot{}, integrationtmdb.ErrAPIKeyRejected)
	if err := scheduler.Reconfigure(); err != nil {
		t.Fatalf("reconfigure suspended scheduler: %v", err)
	}
	if activity.latestNext() != nil {
		t.Fatalf("suspended reconfigure next check = %v, want nil", activity.latestNext())
	}

	reconfiguredAt := startedAt.Add(time.Minute)
	clock.setNow(reconfiguredAt)
	configs.set(availableSchedulerSnapshot(45*time.Minute), nil)
	if err := scheduler.Reconfigure(); err != nil {
		t.Fatalf("reconfigure with accepted credential: %v", err)
	}
	next := activity.latestNext()
	if next == nil || !next.Equal(reconfiguredAt.Add(45*time.Minute)) {
		t.Fatalf("resumed next check = %v, want %v", next, reconfiguredAt.Add(45*time.Minute))
	}
}

func TestTMDBRunScheduler_CloseIsIdempotentAndCancelsInFlightTick(t *testing.T) {
	startedAt := time.Date(2026, time.August, 4, 10, 0, 0, 0, time.UTC)
	scheduler, _, runs, _, clock := newSchedulerFixture(startedAt, time.Hour)
	entered := make(chan struct{})
	runs.startFn = func(ctx context.Context, _ tmdbRunStart) (tmdbRunStartResult, error) {
		close(entered)
		<-ctx.Done()
		return tmdbRunStartResult{}, ctx.Err()
	}
	if err := scheduler.Start(); err != nil {
		t.Fatalf("start scheduler: %v", err)
	}
	timer := clock.latestTimer(t)
	clock.setNow(startedAt.Add(time.Hour))
	fired := make(chan struct{})
	go func() {
		timer.Fire()
		close(fired)
	}()
	<-entered

	closed := make(chan struct{})
	go func() {
		scheduler.Close()
		scheduler.Close()
		close(closed)
	}()
	<-closed
	<-fired

	if runs.startCount() != 1 {
		t.Fatalf("run starts = %d, want 1", runs.startCount())
	}
}

func TestTMDBRunScheduler_CloseStopsPendingTimer(t *testing.T) {
	startedAt := time.Date(2026, time.August, 4, 10, 0, 0, 0, time.UTC)
	scheduler, _, runs, _, clock := newSchedulerFixture(startedAt, time.Hour)
	if err := scheduler.Start(); err != nil {
		t.Fatalf("start scheduler: %v", err)
	}
	timer := clock.latestTimer(t)

	scheduler.Close()
	scheduler.Close()
	timer.Fire()

	if !timer.isStopped() {
		t.Fatal("pending timer was not stopped")
	}
	if runs.startCount() != 0 {
		t.Fatalf("run starts after close = %d, want 0", runs.startCount())
	}
}
