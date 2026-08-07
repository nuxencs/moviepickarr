package tmdb_test

import (
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"moviepickarr/internal/integration/tmdb"
)

func TestRuntime_ReplacementLeavesAcquiredWorkOnItsOriginalSnapshot(t *testing.T) {
	runtime := tmdb.NewRuntime(tmdb.RuntimeConfig{
		Enabled:   true,
		APIKey:    "old-key",
		CastLimit: 15,
	}, 3)

	inFlight, err := runtime.Acquire()
	if err != nil {
		t.Fatalf("acquire original snapshot: %v", err)
	}

	runtime.Replace(tmdb.RuntimeConfig{
		Enabled:   true,
		APIKey:    "new-key",
		CastLimit: 30,
	}, 4, time.Time{})

	newWork, err := runtime.Acquire()
	if err != nil {
		t.Fatalf("acquire replacement snapshot: %v", err)
	}
	if inFlight.Revision != 3 || inFlight.Config.APIKey != "old-key" || inFlight.Config.CastLimit != 15 {
		t.Fatalf("in-flight snapshot changed: %+v", inFlight)
	}
	if newWork.Revision != 4 || newWork.Config.APIKey != "new-key" || newWork.Config.CastLimit != 30 {
		t.Fatalf("new work snapshot = %+v", newWork)
	}
}

func TestRuntime_FirstValidEnableRequestsOneStaleRefresh(t *testing.T) {
	runtime := tmdb.NewRuntime(tmdb.RuntimeConfig{}, 1)
	enabled := tmdb.RuntimeConfig{
		Enabled:         true,
		APIKey:          "valid-key",
		RefreshInterval: time.Hour,
	}

	first := runtime.Replace(enabled, 2, time.Date(2026, time.August, 4, 10, 0, 0, 0, time.UTC))
	second := runtime.Replace(enabled, 3, time.Date(2026, time.August, 4, 10, 1, 0, 0, time.UTC))

	if !first.RefreshStale {
		t.Fatal("first valid enable did not request a stale refresh")
	}
	if second.RefreshStale {
		t.Fatal("already-enabled replacement requested another stale refresh")
	}
}

func TestRuntime_RefreshIntervalChangeReschedulesFromReplacementTime(t *testing.T) {
	runtime := tmdb.NewRuntime(tmdb.RuntimeConfig{
		Enabled:         true,
		APIKey:          "valid-key",
		RefreshInterval: time.Hour,
	}, 1)
	replacedAt := time.Date(2026, time.August, 4, 10, 0, 0, 0, time.UTC)

	effects := runtime.Replace(tmdb.RuntimeConfig{
		Enabled:         true,
		APIKey:          "valid-key",
		RefreshInterval: 2 * time.Hour,
	}, 2, replacedAt)

	if !effects.Reschedule {
		t.Fatal("refresh interval change did not request rescheduling")
	}
	want := time.Date(2026, time.August, 4, 12, 0, 0, 0, time.UTC)
	if !effects.NextScheduledCheck.Equal(want) {
		t.Fatalf("next scheduled check = %v, want %v", effects.NextScheduledCheck, want)
	}
}

func TestRuntime_AvailabilityChangeReschedulesWithAnUnchangedInterval(t *testing.T) {
	interval := time.Hour
	replacedAt := time.Date(2026, time.August, 4, 10, 0, 0, 0, time.UTC)
	runtime := tmdb.NewRuntime(tmdb.RuntimeConfig{
		Enabled:         false,
		APIKey:          "retained-key",
		RefreshInterval: interval,
	}, 1)

	enabled := runtime.Replace(tmdb.RuntimeConfig{
		Enabled:         true,
		APIKey:          "retained-key",
		RefreshInterval: interval,
	}, 2, replacedAt)
	if !enabled.Reschedule || !enabled.NextScheduledCheck.Equal(replacedAt.Add(interval)) {
		t.Fatalf("enable effects = %+v, want schedule from replacement time", enabled)
	}

	disabled := runtime.Replace(tmdb.RuntimeConfig{
		Enabled:         false,
		APIKey:          "retained-key",
		RefreshInterval: interval,
	}, 3, replacedAt.Add(time.Minute))
	if !disabled.Reschedule || !disabled.NextScheduledCheck.IsZero() {
		t.Fatalf("disable effects = %+v, want cleared schedule", disabled)
	}
}

func TestRuntime_ReplacementAfterSuspensionRequestsScheduleResume(t *testing.T) {
	interval := time.Hour
	replacedAt := time.Date(2026, time.August, 4, 10, 0, 0, 0, time.UTC)
	runtime := tmdb.NewRuntime(tmdb.RuntimeConfig{
		Enabled:         true,
		APIKey:          "rejected-key",
		RefreshInterval: interval,
	}, 8)
	rejected, err := runtime.Acquire()
	if err != nil {
		t.Fatalf("acquire rejected snapshot: %v", err)
	}
	runtime.AuthenticationRejected(rejected)

	effects := runtime.Replace(tmdb.RuntimeConfig{
		Enabled:         true,
		APIKey:          "replacement-key",
		RefreshInterval: interval,
	}, 9, replacedAt)
	if !effects.Reschedule || !effects.NextScheduledCheck.Equal(replacedAt.Add(interval)) {
		t.Fatalf("replacement effects = %+v, want resumed schedule", effects)
	}
}

func TestRuntime_DisablingScheduledRefreshClearsTheNextCheck(t *testing.T) {
	runtime := tmdb.NewRuntime(tmdb.RuntimeConfig{
		Enabled:         true,
		APIKey:          "valid-key",
		RefreshInterval: time.Hour,
	}, 1)

	effects := runtime.Replace(tmdb.RuntimeConfig{
		Enabled: true,
		APIKey:  "valid-key",
	}, 2, time.Date(2026, time.August, 4, 10, 0, 0, 0, time.UTC))

	if !effects.Reschedule || !effects.NextScheduledCheck.IsZero() {
		t.Fatalf("disabled schedule effects = %+v", effects)
	}
}

func TestRuntime_OtherSettingChangesDoNotStartOrRescheduleWork(t *testing.T) {
	runtime := tmdb.NewRuntime(tmdb.RuntimeConfig{
		Enabled:         true,
		APIKey:          "old-key",
		CastLimit:       15,
		RefreshInterval: time.Hour,
		TTL:             30 * 24 * time.Hour,
		MinInterval:     250 * time.Millisecond,
		MaxRetries:      4,
		Backoff:         500 * time.Millisecond,
		BatchLimit:      200,
	}, 1)

	effects := runtime.Replace(tmdb.RuntimeConfig{
		Enabled:         true,
		APIKey:          "replacement-key",
		CastLimit:       30,
		RefreshInterval: time.Hour,
		TTL:             7 * 24 * time.Hour,
		MinInterval:     time.Second,
		MaxRetries:      2,
		Backoff:         2 * time.Second,
		BatchLimit:      100,
	}, 2, time.Date(2026, time.August, 4, 10, 0, 0, 0, time.UTC))

	if effects.RefreshStale || effects.Reschedule || !effects.NextScheduledCheck.IsZero() {
		t.Fatalf("other setting changes triggered work: %+v", effects)
	}
}

func TestRuntime_AuthenticationRejectionSuspendsNewWork(t *testing.T) {
	runtime := tmdb.NewRuntime(tmdb.RuntimeConfig{Enabled: true, APIKey: "rejected-key"}, 8)
	inFlight, err := runtime.Acquire()
	if err != nil {
		t.Fatalf("acquire work: %v", err)
	}

	if !runtime.AuthenticationRejected(inFlight) {
		t.Fatal("current snapshot rejection was ignored")
	}
	if _, err := runtime.Acquire(); !errors.Is(err, tmdb.ErrAPIKeyRejected) {
		t.Fatalf("acquire after rejection error = %v, want API key rejected", err)
	}
}

func TestRuntime_SuccessfulReplacementResumesNewWork(t *testing.T) {
	runtime := tmdb.NewRuntime(tmdb.RuntimeConfig{Enabled: true, APIKey: "rejected-key"}, 8)
	rejected, err := runtime.Acquire()
	if err != nil {
		t.Fatalf("acquire work: %v", err)
	}
	runtime.AuthenticationRejected(rejected)

	runtime.Replace(tmdb.RuntimeConfig{Enabled: true, APIKey: "replacement-key"}, 9, time.Time{})
	resumed, err := runtime.Acquire()
	if err != nil {
		t.Fatalf("acquire after replacement: %v", err)
	}
	if resumed.Revision != 9 || resumed.Config.APIKey != "replacement-key" {
		t.Fatalf("resumed snapshot = %+v", resumed)
	}
}

func TestRuntime_UnrelatedReplacementKeepsRejectedCredentialSuspended(t *testing.T) {
	runtime := tmdb.NewRuntime(tmdb.RuntimeConfig{
		Enabled:   true,
		APIKey:    "rejected-key",
		CastLimit: 15,
	}, 8)
	rejected, err := runtime.Acquire()
	if err != nil {
		t.Fatalf("acquire work: %v", err)
	}
	runtime.AuthenticationRejected(rejected)

	runtime.Replace(tmdb.RuntimeConfig{
		Enabled:   true,
		APIKey:    "rejected-key",
		CastLimit: 30,
	}, 9, time.Time{})
	if _, err := runtime.Acquire(); !errors.Is(err, tmdb.ErrAPIKeyRejected) {
		t.Fatalf("acquire after unrelated replacement error = %v, want API key rejected", err)
	}
}

func TestRuntime_SuccessfulConnectionTestResumesMatchingConfiguration(t *testing.T) {
	runtime := tmdb.NewRuntime(tmdb.RuntimeConfig{Enabled: true, APIKey: "working-again"}, 8)
	rejected, err := runtime.Acquire()
	if err != nil {
		t.Fatalf("acquire work: %v", err)
	}
	runtime.AuthenticationRejected(rejected)

	if !runtime.ConnectionSucceeded(8, "working-again") {
		t.Fatal("successful connection test did not resume its configuration")
	}
	if _, err := runtime.Acquire(); err != nil {
		t.Fatalf("acquire after successful connection test: %v", err)
	}
}

func TestRuntime_SuccessfulConnectionTestDoesNotReportAnAlreadyActiveRuntimeAsResumed(t *testing.T) {
	runtime := tmdb.NewRuntime(tmdb.RuntimeConfig{Enabled: true, APIKey: "working"}, 8)

	if runtime.ConnectionSucceeded(8, "working") {
		t.Fatal("already-active runtime reported a resume transition")
	}
}

func TestRuntime_CompletedConnectionTestOutranksEarlierRejectedWork(t *testing.T) {
	runtime := tmdb.NewRuntime(tmdb.RuntimeConfig{Enabled: true, APIKey: "working-again"}, 8)
	oldWork, err := runtime.Acquire()
	if err != nil {
		t.Fatalf("acquire work: %v", err)
	}
	runtime.AuthenticationRejected(oldWork)
	runtime.ConnectionSucceeded(8, "working-again")

	if runtime.AuthenticationRejected(oldWork) {
		t.Fatal("work acquired before the successful test suspended the resumed runtime")
	}
	if _, err := runtime.Acquire(); err != nil {
		t.Fatalf("acquire after late old rejection: %v", err)
	}
}

func TestRuntime_LateAuthenticationFailureCannotSuspendAReplacement(t *testing.T) {
	runtime := tmdb.NewRuntime(tmdb.RuntimeConfig{Enabled: true, APIKey: "old-key"}, 8)
	oldWork, err := runtime.Acquire()
	if err != nil {
		t.Fatalf("acquire old work: %v", err)
	}
	runtime.Replace(tmdb.RuntimeConfig{Enabled: true, APIKey: "new-key"}, 9, time.Time{})

	if runtime.AuthenticationRejected(oldWork) {
		t.Fatal("late rejection from old work suspended the replacement")
	}
	current, err := runtime.Acquire()
	if err != nil {
		t.Fatalf("acquire replacement after late rejection: %v", err)
	}
	if current.Revision != 9 || current.Config.APIKey != "new-key" {
		t.Fatalf("current snapshot = %+v", current)
	}
}

func TestRuntime_StaleConnectionTestCannotResumeAnotherRevision(t *testing.T) {
	runtime := tmdb.NewRuntime(tmdb.RuntimeConfig{Enabled: true, APIKey: "current-key"}, 9)
	current, err := runtime.Acquire()
	if err != nil {
		t.Fatalf("acquire current work: %v", err)
	}
	runtime.AuthenticationRejected(current)

	if runtime.ConnectionSucceeded(8, "current-key") {
		t.Fatal("stale connection test resumed another revision")
	}
	if _, err := runtime.Acquire(); !errors.Is(err, tmdb.ErrAPIKeyRejected) {
		t.Fatalf("acquire after stale connection result error = %v, want API key rejected", err)
	}
}

func TestRuntime_UnsavedReplacementTestCannotResumeTheSavedCredential(t *testing.T) {
	runtime := tmdb.NewRuntime(tmdb.RuntimeConfig{Enabled: true, APIKey: "saved-rejected-key"}, 8)
	current, err := runtime.Acquire()
	if err != nil {
		t.Fatalf("acquire current work: %v", err)
	}
	runtime.AuthenticationRejected(current)

	if runtime.ConnectionSucceeded(8, "unsaved-working-key") {
		t.Fatal("test of an unsaved key resumed the saved rejected key")
	}
	if _, err := runtime.Acquire(); !errors.Is(err, tmdb.ErrAPIKeyRejected) {
		t.Fatalf("acquire after draft test error = %v, want API key rejected", err)
	}
}

func TestRuntime_DisabledOrUnconfiguredSnapshotsBlockNewWork(t *testing.T) {
	tests := []struct {
		name   string
		config tmdb.RuntimeConfig
	}{
		{name: "disabled", config: tmdb.RuntimeConfig{APIKey: "retained-key"}},
		{name: "missing credential", config: tmdb.RuntimeConfig{Enabled: true}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			runtime := tmdb.NewRuntime(test.config, 1)
			if _, err := runtime.Acquire(); !errors.Is(err, tmdb.ErrRuntimeDisabled) {
				t.Fatalf("acquire error = %v, want runtime disabled", err)
			}
		})
	}
}

func TestRuntime_ConcurrentReplacementPublishesWholeSnapshots(t *testing.T) {
	configForRevision := func(revision int64) tmdb.RuntimeConfig {
		return tmdb.RuntimeConfig{
			Enabled:   true,
			APIKey:    fmt.Sprintf("key-%d", revision),
			CastLimit: int(revision),
		}
	}
	runtime := tmdb.NewRuntime(configForRevision(1), 1)

	const (
		readerCount = 8
		iterations  = 1_000
	)
	start := make(chan struct{})
	errorsFound := make(chan string, readerCount)
	var readers sync.WaitGroup
	readers.Add(readerCount)
	for range readerCount {
		go func() {
			defer readers.Done()
			<-start
			for range iterations {
				snapshot, err := runtime.Acquire()
				if err != nil {
					errorsFound <- err.Error()
					return
				}
				wantKey := fmt.Sprintf("key-%d", snapshot.Revision)
				if snapshot.Config.APIKey != wantKey || snapshot.Config.CastLimit != int(snapshot.Revision) {
					errorsFound <- fmt.Sprintf("torn snapshot: %+v", snapshot)
					return
				}
			}
		}()
	}

	close(start)
	for revision := int64(2); revision <= iterations; revision++ {
		runtime.Replace(configForRevision(revision), revision, time.Time{})
	}
	readers.Wait()
	close(errorsFound)
	for failure := range errorsFound {
		t.Error(failure)
	}
}
