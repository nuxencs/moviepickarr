package repository

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"moviepickarr/internal/db"
	"moviepickarr/internal/integration"
)

func setupIntegrationRunRepo(t *testing.T) (context.Context, *SqliteIntegrationRunRepository, *db.Pool) {
	t.Helper()

	ctx := context.Background()
	pool, err := db.OpenSQLite(filepath.Join(t.TempDir(), "integration-run-test.db"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.RunMigrations(ctx, pool.Write); err != nil {
		t.Fatalf("run migrations: %v", err)
	}
	t.Cleanup(func() {
		if err := pool.Close(); err != nil {
			t.Fatalf("close sqlite: %v", err)
		}
	})

	return ctx, NewSqliteIntegrationRunRepository(pool), pool
}

func TestIntegrationRunRepo_StartMakesRunCurrent(t *testing.T) {
	t.Parallel()
	ctx, runs, pool := setupIntegrationRunRepo(t)

	if _, err := pool.Write.ExecContext(ctx, "INSERT INTO users (id, name) VALUES (7, 'Ada')"); err != nil {
		t.Fatalf("seed initiating admin: %v", err)
	}
	startedAt := time.Date(2026, time.August, 4, 12, 30, 0, 0, time.UTC)
	initiatedBy := 7

	started, err := runs.Start(ctx, integration.RunStart{
		Integration:    "tmdb",
		Operation:      integration.RunOperationRefreshStale,
		Trigger:        integration.RunTriggerManual,
		InitiatedBy:    &initiatedBy,
		ConfigRevision: 12,
		StartedAt:      startedAt,
		Total:          9,
	})
	if err != nil {
		t.Fatalf("start: %v", err)
	}

	current, err := runs.Current(ctx, "tmdb")
	if err != nil {
		t.Fatalf("current: %v", err)
	}
	if current == nil {
		t.Fatal("started run is not current")
	}
	if current.ID != started.ID || current.Integration != "tmdb" {
		t.Fatalf("identity = %+v, want id %d for tmdb", current, started.ID)
	}
	if current.Operation != integration.RunOperationRefreshStale || current.Trigger != integration.RunTriggerManual {
		t.Fatalf("kind = %q/%q, want refresh_stale/manual", current.Operation, current.Trigger)
	}
	if current.InitiatedBy == nil || *current.InitiatedBy != 7 || current.ConfigRevision != 12 {
		t.Fatalf("provenance = admin %v revision %d, want admin 7 revision 12", current.InitiatedBy, current.ConfigRevision)
	}
	if current.Status != integration.RunStatusRunning || !current.StartedAt.Equal(startedAt) || current.FinishedAt != nil {
		t.Fatalf("lifecycle = status %q, start %s, finish %v", current.Status, current.StartedAt, current.FinishedAt)
	}
	if current.Progress.Total != 9 || current.Progress.Remaining != 9 {
		t.Fatalf("progress = %+v, want total and remaining 9", current.Progress)
	}
}

func TestIntegrationRunRepo_CurrentLibraryIgnoresNewerSingleMovieRun(t *testing.T) {
	t.Parallel()
	ctx, runs, _ := setupIntegrationRunRepo(t)
	startedAt := time.Date(2026, time.August, 4, 12, 30, 0, 0, time.UTC)

	libraryRun, err := runs.Start(ctx, integration.RunStart{
		Integration: "tmdb",
		Operation:   integration.RunOperationRefreshStale,
		Trigger:     integration.RunTriggerManual,
		StartedAt:   startedAt,
		Total:       9,
	})
	if err != nil {
		t.Fatalf("start library run: %v", err)
	}
	if _, err := runs.Start(ctx, integration.RunStart{
		Integration: "tmdb",
		Operation:   integration.RunOperationEnrichMovie,
		Trigger:     integration.RunTriggerMovieAdded,
		StartedAt:   startedAt.Add(time.Second),
		Total:       1,
	}); err != nil {
		t.Fatalf("start single-movie run: %v", err)
	}

	current, err := runs.CurrentLibrary(ctx, "tmdb")
	if err != nil {
		t.Fatalf("current library run: %v", err)
	}
	if current == nil || current.ID != libraryRun.ID {
		t.Fatalf("current library run = %+v, want run %d", current, libraryRun.ID)
	}
}

func TestIntegrationRunRepo_UpdateReplacesProgressSnapshot(t *testing.T) {
	t.Parallel()
	ctx, runs, _ := setupIntegrationRunRepo(t)

	started, err := runs.Start(ctx, integration.RunStart{
		Integration: "tmdb",
		Operation:   integration.RunOperationRefreshStale,
		Trigger:     integration.RunTriggerScheduled,
		StartedAt:   time.Date(2026, time.August, 4, 13, 0, 0, 0, time.UTC),
		Total:       12,
	})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if err := runs.Update(ctx, started.ID, integration.RunProgress{
		Total: 12, Processed: 2, Succeeded: 1, Failed: 1, Remaining: 10,
	}); err != nil {
		t.Fatalf("first update: %v", err)
	}
	latest := integration.RunProgress{
		Total: 12, Processed: 7, Succeeded: 5, Failed: 1, Skipped: 1, Remaining: 5,
	}
	if err := runs.Update(ctx, started.ID, latest); err != nil {
		t.Fatalf("coalesced update: %v", err)
	}

	current, err := runs.Current(ctx, "tmdb")
	if err != nil {
		t.Fatalf("current: %v", err)
	}
	if current == nil || current.Progress != latest {
		t.Fatalf("progress = %+v, want latest snapshot %+v", current, latest)
	}
}

func TestIntegrationRunRepo_UpdateRejectsFinishedRun(t *testing.T) {
	t.Parallel()
	ctx, runs, _ := setupIntegrationRunRepo(t)
	startedAt := time.Date(2026, time.August, 4, 13, 30, 0, 0, time.UTC)
	started, err := runs.Start(ctx, integration.RunStart{
		Integration: "tmdb",
		Operation:   integration.RunOperationRefreshStale,
		Trigger:     integration.RunTriggerScheduled,
		StartedAt:   startedAt,
		Total:       2,
	})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	finalProgress := integration.RunProgress{Total: 2, Processed: 2, Succeeded: 2}
	if _, err := runs.Finish(ctx, started.ID, integration.RunFinish{
		Status:     integration.RunStatusCompleted,
		FinishedAt: startedAt.Add(time.Minute),
		Progress:   finalProgress,
	}); err != nil {
		t.Fatalf("finish: %v", err)
	}

	err = runs.Update(ctx, started.ID, integration.RunProgress{Total: 2, Processed: 1, Succeeded: 1, Remaining: 1})
	if !errors.Is(err, integration.ErrRunNotRunning) {
		t.Fatalf("late update error = %v, want ErrRunNotRunning", err)
	}
	latest, err := runs.Latest(ctx, "tmdb")
	if err != nil {
		t.Fatalf("latest: %v", err)
	}
	if latest == nil || latest.Progress != finalProgress {
		t.Fatalf("late update changed terminal progress: %+v", latest)
	}
}

func TestIntegrationRunRepo_FinishMovesCurrentRunToLatest(t *testing.T) {
	t.Parallel()
	ctx, runs, _ := setupIntegrationRunRepo(t)
	startedAt := time.Date(2026, time.August, 4, 14, 0, 0, 0, time.UTC)
	started, err := runs.Start(ctx, integration.RunStart{
		Integration: "tmdb",
		Operation:   integration.RunOperationReEnrichAll,
		Trigger:     integration.RunTriggerManual,
		StartedAt:   startedAt,
		Total:       4,
	})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	finishedAt := startedAt.Add(2 * time.Minute)
	finalProgress := integration.RunProgress{
		Total: 4, Processed: 4, Succeeded: 3, Failed: 1,
	}
	finished, err := runs.Finish(ctx, started.ID, integration.RunFinish{
		Status:     integration.RunStatusCompletedWithErrors,
		FinishedAt: finishedAt,
		Progress:   finalProgress,
	})
	if err != nil {
		t.Fatalf("finish: %v", err)
	}

	current, err := runs.Current(ctx, "tmdb")
	if err != nil {
		t.Fatalf("current: %v", err)
	}
	latest, err := runs.Latest(ctx, "tmdb")
	if err != nil {
		t.Fatalf("latest: %v", err)
	}
	if current != nil {
		t.Fatalf("current = %+v, want none after finish", current)
	}
	if latest == nil || latest.ID != started.ID || finished.ID != started.ID {
		t.Fatalf("latest/finished identity = %+v / %+v, want run %d", latest, finished, started.ID)
	}
	if latest.Status != integration.RunStatusCompletedWithErrors || latest.FinishedAt == nil || !latest.FinishedAt.Equal(finishedAt) {
		t.Fatalf("latest lifecycle = status %q finish %v", latest.Status, latest.FinishedAt)
	}
	if latest.Progress != finalProgress {
		t.Fatalf("latest progress = %+v, want %+v", latest.Progress, finalProgress)
	}
}

func TestIntegrationRunRepo_FinishBoundsFailedSubjectSample(t *testing.T) {
	t.Parallel()
	ctx, runs, _ := setupIntegrationRunRepo(t)
	startedAt := time.Date(2026, time.August, 4, 15, 0, 0, 0, time.UTC)
	started, err := runs.Start(ctx, integration.RunStart{
		Integration: "tmdb",
		Operation:   integration.RunOperationRefreshStale,
		Trigger:     integration.RunTriggerScheduled,
		StartedAt:   startedAt,
		Total:       30,
	})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	failures := make([]integration.FailedSubject, integration.FailedSubjectSampleLimit+5)
	for i := range failures {
		failures[i] = integration.FailedSubject{
			Subject: fmt.Sprintf("movie-%02d", i+1),
			Error:   "TMDB request timed out",
		}
	}

	_, err = runs.Finish(ctx, started.ID, integration.RunFinish{
		Status:         integration.RunStatusFailed,
		FinishedAt:     startedAt.Add(time.Minute),
		Progress:       integration.RunProgress{Total: 30, Processed: 30, Failed: 30},
		ErrorSummary:   "30 movies could not be refreshed",
		FailedSubjects: failures,
	})
	if err != nil {
		t.Fatalf("finish: %v", err)
	}

	latest, err := runs.Latest(ctx, "tmdb")
	if err != nil {
		t.Fatalf("latest: %v", err)
	}
	if latest.ErrorSummary != "30 movies could not be refreshed" {
		t.Fatalf("error summary = %q", latest.ErrorSummary)
	}
	if len(latest.FailedSubjects) != integration.FailedSubjectSampleLimit {
		t.Fatalf("failed-subject sample = %d, want %d", len(latest.FailedSubjects), integration.FailedSubjectSampleLimit)
	}
	if latest.FailedSubjects[0].Subject != "movie-01" || latest.FailedSubjects[24].Subject != "movie-25" {
		t.Fatalf("failed-subject sample bounds = %+v", latest.FailedSubjects)
	}
}

func TestIntegrationRunRepo_InterruptRunningRecoversStartupState(t *testing.T) {
	t.Parallel()
	ctx, runs, _ := setupIntegrationRunRepo(t)
	base := time.Date(2026, time.August, 4, 16, 0, 0, 0, time.UTC)

	for i, integrationID := range []string{"tmdb", "fanart"} {
		if _, err := runs.Start(ctx, integration.RunStart{
			Integration: integrationID,
			Operation:   integration.RunOperationRefreshStale,
			Trigger:     integration.RunTriggerScheduled,
			StartedAt:   base.Add(time.Duration(i) * time.Minute),
			Total:       5,
		}); err != nil {
			t.Fatalf("start %s: %v", integrationID, err)
		}
	}
	completed, err := runs.Start(ctx, integration.RunStart{
		Integration: "radarr",
		Operation:   integration.RunOperationRefreshStale,
		Trigger:     integration.RunTriggerManual,
		StartedAt:   base.Add(2 * time.Minute),
		Total:       1,
	})
	if err != nil {
		t.Fatalf("start completed control: %v", err)
	}
	if _, err := runs.Finish(ctx, completed.ID, integration.RunFinish{
		Status:     integration.RunStatusCompleted,
		FinishedAt: base.Add(3 * time.Minute),
		Progress:   integration.RunProgress{Total: 1, Processed: 1, Succeeded: 1},
	}); err != nil {
		t.Fatalf("finish completed control: %v", err)
	}

	interruptedAt := base.Add(10 * time.Minute)
	affected, err := runs.InterruptRunning(ctx, interruptedAt)
	if err != nil {
		t.Fatalf("interrupt running: %v", err)
	}
	if affected != 2 {
		t.Fatalf("interrupted rows = %d, want 2", affected)
	}
	for _, integrationID := range []string{"tmdb", "fanart"} {
		latest, err := runs.Latest(ctx, integrationID)
		if err != nil {
			t.Fatalf("latest %s: %v", integrationID, err)
		}
		if latest == nil || latest.Status != integration.RunStatusInterrupted || latest.FinishedAt == nil || !latest.FinishedAt.Equal(interruptedAt) {
			t.Fatalf("latest %s = %+v, want interrupted at %s", integrationID, latest, interruptedAt)
		}
	}
	latestCompleted, err := runs.Latest(ctx, "radarr")
	if err != nil {
		t.Fatalf("latest completed control: %v", err)
	}
	if latestCompleted == nil || latestCompleted.Status != integration.RunStatusCompleted {
		t.Fatalf("completed control changed: %+v", latestCompleted)
	}
}

func TestIntegrationRunRepo_ListUsesNewestFirstKeysetPagination(t *testing.T) {
	t.Parallel()
	ctx, runs, _ := setupIntegrationRunRepo(t)
	base := time.Date(2026, time.August, 4, 17, 0, 0, 0, time.UTC)
	startedTimes := []time.Time{
		base,
		base.Add(time.Minute),
		base.Add(time.Minute),
		base.Add(2 * time.Minute),
		base.Add(3 * time.Minute),
	}
	ids := make([]int64, 0, len(startedTimes))
	for _, startedAt := range startedTimes {
		run, err := runs.Start(ctx, integration.RunStart{
			Integration: "tmdb",
			Operation:   integration.RunOperationRefreshStale,
			Trigger:     integration.RunTriggerScheduled,
			StartedAt:   startedAt,
			Total:       1,
		})
		if err != nil {
			t.Fatalf("start at %s: %v", startedAt, err)
		}
		if _, err := runs.Finish(ctx, run.ID, integration.RunFinish{
			Status:     integration.RunStatusCompleted,
			FinishedAt: startedAt.Add(time.Second),
			Progress:   integration.RunProgress{Total: 1, Processed: 1, Succeeded: 1},
		}); err != nil {
			t.Fatalf("finish at %s: %v", startedAt, err)
		}
		ids = append(ids, run.ID)
	}

	first, err := runs.List(ctx, integration.RunListFilter{Limit: 2})
	if err != nil {
		t.Fatalf("first page: %v", err)
	}
	if len(first.Runs) != 2 || first.Runs[0].ID != ids[4] || first.Runs[1].ID != ids[3] || first.Next == nil {
		t.Fatalf("first page = %+v, want ids %d, %d and cursor", first, ids[4], ids[3])
	}
	second, err := runs.List(ctx, integration.RunListFilter{Limit: 2, Before: first.Next})
	if err != nil {
		t.Fatalf("second page: %v", err)
	}
	if len(second.Runs) != 2 || second.Runs[0].ID != ids[2] || second.Runs[1].ID != ids[1] || second.Next == nil {
		t.Fatalf("second page = %+v, want tied ids %d, %d and cursor", second, ids[2], ids[1])
	}
	third, err := runs.List(ctx, integration.RunListFilter{Limit: 2, Before: second.Next})
	if err != nil {
		t.Fatalf("third page: %v", err)
	}
	if len(third.Runs) != 1 || third.Runs[0].ID != ids[0] || third.Next != nil {
		t.Fatalf("third page = %+v, want final id %d and no cursor", third, ids[0])
	}
}

func TestIntegrationRunRepo_ListFinishedOnlyFiltersBeforePagination(t *testing.T) {
	t.Parallel()
	ctx, runs, _ := setupIntegrationRunRepo(t)
	base := time.Date(2026, time.August, 4, 17, 30, 0, 0, time.UTC)
	finishedIDs := make([]int64, 0, 3)

	for i := range 5 {
		startedAt := base.Add(time.Duration(i) * time.Minute)
		run, err := runs.Start(ctx, integration.RunStart{
			Integration: "tmdb",
			Operation:   integration.RunOperationRefreshStale,
			Trigger:     integration.RunTriggerScheduled,
			StartedAt:   startedAt,
			Total:       1,
		})
		if err != nil {
			t.Fatalf("start run %d: %v", i, err)
		}
		if i%2 != 0 {
			continue
		}
		if _, err := runs.Finish(ctx, run.ID, integration.RunFinish{
			Status:     integration.RunStatusCompleted,
			FinishedAt: startedAt.Add(time.Second),
			Progress:   integration.RunProgress{Total: 1, Processed: 1, Succeeded: 1},
		}); err != nil {
			t.Fatalf("finish run %d: %v", i, err)
		}
		finishedIDs = append(finishedIDs, run.ID)
	}

	first, err := runs.List(ctx, integration.RunListFilter{FinishedOnly: true, Limit: 2})
	if err != nil {
		t.Fatalf("first page: %v", err)
	}
	if len(first.Runs) != 2 || first.Runs[0].ID != finishedIDs[2] || first.Runs[1].ID != finishedIDs[1] || first.Next == nil {
		t.Fatalf("first page = %+v, want finished ids %d, %d and cursor", first, finishedIDs[2], finishedIDs[1])
	}

	second, err := runs.List(ctx, integration.RunListFilter{FinishedOnly: true, Limit: 2, Before: first.Next})
	if err != nil {
		t.Fatalf("second page: %v", err)
	}
	if len(second.Runs) != 1 || second.Runs[0].ID != finishedIDs[0] || second.Next != nil {
		t.Fatalf("second page = %+v, want finished id %d and no cursor", second, finishedIDs[0])
	}
}

func TestIntegrationRunRepo_ListFiltersHistory(t *testing.T) {
	t.Parallel()
	ctx, runs, _ := setupIntegrationRunRepo(t)
	base := time.Date(2026, time.August, 4, 18, 0, 0, 0, time.UTC)
	type runKind struct {
		integrationID string
		operation     integration.RunOperation
		trigger       integration.RunTrigger
		status        integration.RunStatus
	}
	kinds := []runKind{
		{"tmdb", integration.RunOperationRefreshStale, integration.RunTriggerScheduled, integration.RunStatusCompleted},
		{"tmdb", integration.RunOperationEnrichMovie, integration.RunTriggerMovieAdded, integration.RunStatusFailed},
		{"fanart", integration.RunOperationRefreshStale, integration.RunTriggerScheduled, integration.RunStatusCompleted},
		{"tmdb", integration.RunOperationRefreshStale, integration.RunTriggerManual, integration.RunStatusCancelled},
	}
	ids := make([]int64, 0, len(kinds))
	for i, kind := range kinds {
		startedAt := base.Add(time.Duration(i) * time.Minute)
		run, err := runs.Start(ctx, integration.RunStart{
			Integration: kind.integrationID,
			Operation:   kind.operation,
			Trigger:     kind.trigger,
			StartedAt:   startedAt,
			Total:       1,
		})
		if err != nil {
			t.Fatalf("start kind %d: %v", i, err)
		}
		if _, err := runs.Finish(ctx, run.ID, integration.RunFinish{
			Status:     kind.status,
			FinishedAt: startedAt.Add(time.Second),
			Progress:   integration.RunProgress{Total: 1, Processed: 1, Succeeded: 1},
		}); err != nil {
			t.Fatalf("finish kind %d: %v", i, err)
		}
		ids = append(ids, run.ID)
	}

	tests := []struct {
		name   string
		filter integration.RunListFilter
		want   []int64
	}{
		{"integration", integration.RunListFilter{Integration: "fanart"}, []int64{ids[2]}},
		{"operation", integration.RunListFilter{Operation: integration.RunOperationEnrichMovie}, []int64{ids[1]}},
		{"status", integration.RunListFilter{Status: integration.RunStatusCancelled}, []int64{ids[3]}},
		{"trigger", integration.RunListFilter{Trigger: integration.RunTriggerScheduled}, []int64{ids[2], ids[0]}},
		{"combined", integration.RunListFilter{
			Integration: "tmdb",
			Operation:   integration.RunOperationRefreshStale,
			Status:      integration.RunStatusCompleted,
			Trigger:     integration.RunTriggerScheduled,
		}, []int64{ids[0]}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			page, err := runs.List(ctx, tt.filter)
			if err != nil {
				t.Fatalf("list: %v", err)
			}
			if len(page.Runs) != len(tt.want) {
				t.Fatalf("listed ids = %v, want %v", runIDs(page.Runs), tt.want)
			}
			for i, want := range tt.want {
				if page.Runs[i].ID != want {
					t.Fatalf("listed ids = %v, want %v", runIDs(page.Runs), tt.want)
				}
			}
		})
	}
}

func TestIntegrationRunMigration_IndexesHistoryQueries(t *testing.T) {
	t.Parallel()
	ctx, _, pool := setupIntegrationRunRepo(t)
	epoch := time.Date(2026, time.August, 4, 18, 30, 0, 0, time.UTC).Unix()
	keysetQuery, keysetArgs, _ := integrationRunListQuery(integration.RunListFilter{
		FinishedOnly: true,
		Before:       &integration.RunCursor{StartedAt: time.Unix(epoch, 0), ID: 42},
	})
	integrationQuery, integrationArgs, _ := integrationRunListQuery(integration.RunListFilter{
		Integration:  "tmdb",
		FinishedOnly: true,
	})

	tests := []struct {
		name      string
		query     string
		args      []any
		wantIndex string
		wantSeek  bool
	}{
		{
			name:      "finished keyset",
			query:     keysetQuery,
			args:      keysetArgs,
			wantIndex: "integration_runs_history_index",
			wantSeek:  true,
		},
		{
			name:      "finished integration",
			query:     integrationQuery,
			args:      integrationArgs,
			wantIndex: "integration_runs_integration_history_index",
		},
		{
			name: "operation",
			query: `SELECT id FROM integration_runs WHERE operation = ?
				ORDER BY started_at DESC, id DESC LIMIT ?`,
			args:      []any{integration.RunOperationRefreshStale, 51},
			wantIndex: "integration_runs_operation_history_index",
		},
		{
			name: "status",
			query: `SELECT id FROM integration_runs WHERE status = ?
				ORDER BY started_at DESC, id DESC LIMIT ?`,
			args:      []any{integration.RunStatusCompleted, 51},
			wantIndex: "integration_runs_status_history_index",
		},
		{
			name: "trigger",
			query: `SELECT id FROM integration_runs WHERE trigger = ?
				ORDER BY started_at DESC, id DESC LIMIT ?`,
			args:      []any{integration.RunTriggerScheduled, 51},
			wantIndex: "integration_runs_trigger_history_index",
		},
		{
			name: "current",
			query: `SELECT id FROM integration_runs
				WHERE integration = ? AND status = 'running'
				ORDER BY started_at DESC, id DESC LIMIT 1`,
			args:      []any{"tmdb"},
			wantIndex: "integration_runs_current_index",
		},
		{
			name: "age retention",
			query: `DELETE FROM integration_runs
				WHERE status <> ? AND started_at < ?`,
			args:      []any{integration.RunStatusRunning, epoch},
			wantIndex: "integration_runs_history_index",
		},
		{
			name: "per-integration cap",
			query: `SELECT id FROM (
				SELECT id, row_number() OVER (
					PARTITION BY integration ORDER BY started_at DESC, id DESC
				) AS position
				FROM integration_runs WHERE status <> ?
			) WHERE position > ?`,
			args:      []any{integration.RunStatusRunning, integration.MaxRunsPerIntegration},
			wantIndex: "integration_runs_integration_history_index",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plan := explainQueryPlan(t, ctx, pool, tt.query, tt.args...)
			if !strings.Contains(plan, tt.wantIndex) {
				t.Fatalf("plan = %q, want index %q", plan, tt.wantIndex)
			}
			if strings.Contains(plan, "USE TEMP B-TREE FOR ORDER BY") {
				t.Fatalf("plan sorts history in a temporary b-tree: %q", plan)
			}
			if tt.wantSeek && !strings.Contains(plan, "SEARCH") {
				t.Fatalf("plan scans toward a deep cursor instead of seeking: %q", plan)
			}
		})
	}
}

func explainQueryPlan(t *testing.T, ctx context.Context, pool *db.Pool, query string, args ...any) string {
	t.Helper()
	rows, err := pool.Read.QueryContext(ctx, "EXPLAIN QUERY PLAN "+query, args...)
	if err != nil {
		t.Fatalf("explain query plan: %v", err)
	}
	defer rows.Close()

	var details []string
	for rows.Next() {
		var id, parent, unused int
		var detail string
		if err := rows.Scan(&id, &parent, &unused, &detail); err != nil {
			t.Fatalf("scan query plan: %v", err)
		}
		details = append(details, detail)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read query plan: %v", err)
	}
	return strings.Join(details, "; ")
}

func TestIntegrationRunRepo_PruneRemovesHistoryOlderThanTwelveMonths(t *testing.T) {
	t.Parallel()
	ctx, runs, _ := setupIntegrationRunRepo(t)
	now := time.Date(2026, time.August, 31, 12, 0, 0, 0, time.UTC)
	cutoff := now.AddDate(0, -integration.RunRetentionMonths, 0)

	type seededRun struct {
		integrationID string
		startedAt     time.Time
	}
	for _, seed := range []seededRun{
		{"tmdb", cutoff.Add(-time.Second)},
		{"fanart", cutoff.Add(-time.Hour)},
		{"tmdb", cutoff},
	} {
		run, err := runs.Start(ctx, integration.RunStart{
			Integration: seed.integrationID,
			Operation:   integration.RunOperationRefreshStale,
			Trigger:     integration.RunTriggerScheduled,
			StartedAt:   seed.startedAt,
		})
		if err != nil {
			t.Fatalf("start %s at %s: %v", seed.integrationID, seed.startedAt, err)
		}
		if _, err := runs.Finish(ctx, run.ID, integration.RunFinish{
			Status:     integration.RunStatusCompleted,
			FinishedAt: seed.startedAt.Add(time.Second),
		}); err != nil {
			t.Fatalf("finish %s at %s: %v", seed.integrationID, seed.startedAt, err)
		}
	}

	removed, err := runs.Prune(ctx, now)
	if err != nil {
		t.Fatalf("prune: %v", err)
	}
	if removed != 2 {
		t.Fatalf("removed rows = %d, want 2", removed)
	}
	page, err := runs.List(ctx, integration.RunListFilter{})
	if err != nil {
		t.Fatalf("list retained: %v", err)
	}
	if len(page.Runs) != 1 || !page.Runs[0].StartedAt.Equal(cutoff) {
		t.Fatalf("retained history = %+v, want cutoff row", page.Runs)
	}
}

func TestIntegrationRunRepo_PruneCapsHistoryPerIntegration(t *testing.T) {
	ctx, runs, _ := setupIntegrationRunRepo(t)
	base := time.Date(2026, time.February, 1, 0, 0, 0, 0, time.UTC)
	var second *integration.Run
	for i := range integration.MaxRunsPerIntegration + 1 {
		run, err := runs.Start(ctx, integration.RunStart{
			Integration: "tmdb",
			Operation:   integration.RunOperationEnrichMovie,
			Trigger:     integration.RunTriggerMovieAdded,
			StartedAt:   base.Add(time.Duration(i) * time.Second),
		})
		if err != nil {
			t.Fatalf("start tmdb run %d: %v", i, err)
		}
		if i == 1 {
			second = run
		}
	}
	if _, err := runs.Start(ctx, integration.RunStart{
		Integration: "fanart",
		Operation:   integration.RunOperationRefreshStale,
		Trigger:     integration.RunTriggerScheduled,
		StartedAt:   base,
	}); err != nil {
		t.Fatalf("start other integration: %v", err)
	}
	now := base.AddDate(0, 6, 0)
	if _, err := runs.InterruptRunning(ctx, now); err != nil {
		t.Fatalf("interrupt seeded runs: %v", err)
	}

	removed, err := runs.Prune(ctx, now)
	if err != nil {
		t.Fatalf("prune: %v", err)
	}
	if removed != 1 {
		t.Fatalf("removed rows = %d, want oldest tmdb row only", removed)
	}
	olderThanSecond, err := runs.List(ctx, integration.RunListFilter{
		Integration: "tmdb",
		Before:      &integration.RunCursor{StartedAt: second.StartedAt, ID: second.ID},
	})
	if err != nil {
		t.Fatalf("list before retained boundary: %v", err)
	}
	if len(olderThanSecond.Runs) != 0 {
		t.Fatalf("oldest tmdb row survived cap: %v", runIDs(olderThanSecond.Runs))
	}
	other, err := runs.Latest(ctx, "fanart")
	if err != nil {
		t.Fatalf("latest other integration: %v", err)
	}
	if other == nil {
		t.Fatal("per-integration cap removed another integration's history")
	}
}

func runIDs(runs []integration.Run) []int64 {
	ids := make([]int64, len(runs))
	for i := range runs {
		ids[i] = runs[i].ID
	}
	return ids
}
