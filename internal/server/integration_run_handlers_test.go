package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"moviepickarr/internal/integration"

	"github.com/gofiber/fiber/v2"
)

type recordingRunLedger struct {
	integration.RunLedger
	filter integration.RunListFilter
	page   integration.RunPage
	calls  int
}

func (r *recordingRunLedger) List(_ context.Context, filter integration.RunListFilter) (integration.RunPage, error) {
	r.calls++
	r.filter = filter
	return r.page, nil
}

func mountIntegrationRunHistoryTest(h *handler) *fiber.App {
	app := fiber.New()
	app.Use(func(c *fiber.Ctx) error {
		c.Locals(localsRole, c.Get(testRoleHeader))
		return c.Next()
	})
	app.Get("/api/v1/integration-runs", h.handleListIntegrationRuns)
	return app
}

func TestHandleListIntegrationRuns_MapsFiltersAndReturnsDTO(t *testing.T) {
	t.Parallel()
	startedAt := time.Date(2026, time.August, 4, 13, 0, 0, 0, time.UTC)
	finishedAt := startedAt.Add(2 * time.Minute)
	initiatedBy := 7
	ledger := &recordingRunLedger{page: integration.RunPage{
		Runs: []integration.Run{{
			ID:             43,
			Integration:    "tmdb",
			Operation:      integration.RunOperationRefreshStale,
			Trigger:        integration.RunTriggerManual,
			InitiatedBy:    &initiatedBy,
			ConfigRevision: 12,
			Status:         integration.RunStatusCompletedWithErrors,
			StartedAt:      startedAt,
			FinishedAt:     &finishedAt,
			Progress: integration.RunProgress{
				Total: 9, Processed: 9, Succeeded: 7, Failed: 1, Skipped: 1,
			},
			ErrorSummary: "one movie failed",
			FailedSubjects: []integration.FailedSubject{{
				Subject: "movie:603", Error: "TMDB request timed out",
			}},
		}},
		Next: &integration.RunCursor{
			StartedAt: time.Date(2026, time.August, 4, 12, 30, 0, 0, time.UTC),
			ID:        41,
		},
	}}
	h := &handler{integrationRuns: ledger}
	app := mountIntegrationRunHistoryTest(h)
	request := httptest.NewRequest(http.MethodGet,
		"/api/v1/integration-runs?integration=tmdb&operation=refresh_stale&trigger=manual&status=completed_with_errors&cursor=2026-08-04T12%3A00%3A00Z%2C42&limit=25",
		nil,
	)
	response := doAs(t, app, request, 1, "admin")
	defer response.Body.Close()

	if response.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d, want 200", response.StatusCode)
	}
	if ledger.calls != 1 {
		t.Fatalf("list calls = %d, want 1", ledger.calls)
	}
	if ledger.filter.Integration != "tmdb" || ledger.filter.Status != integration.RunStatusCompletedWithErrors || ledger.filter.Limit != 25 {
		t.Fatalf("filter = %+v", ledger.filter)
	}
	if !ledger.filter.FinishedOnly {
		t.Fatal("history filter includes unfinished runs")
	}
	if ledger.filter.Operation != integration.RunOperationRefreshStale || ledger.filter.Trigger != integration.RunTriggerManual {
		t.Fatalf("operation/trigger filter = %+v", ledger.filter)
	}
	if ledger.filter.Before == nil || ledger.filter.Before.ID != 42 || !ledger.filter.Before.StartedAt.Equal(time.Date(2026, time.August, 4, 12, 0, 0, 0, time.UTC)) {
		t.Fatalf("cursor filter = %+v", ledger.filter.Before)
	}

	var body struct {
		Runs []struct {
			ID             int64  `json:"id"`
			Integration    string `json:"integration"`
			Operation      string `json:"operation"`
			Trigger        string `json:"trigger"`
			InitiatedBy    *int   `json:"initiatedBy"`
			ConfigRevision int64  `json:"configRevision"`
			Status         string `json:"status"`
			StartedAt      string `json:"startedAt"`
			FinishedAt     string `json:"finishedAt"`
			Progress       struct {
				Total     int `json:"total"`
				Processed int `json:"processed"`
				Succeeded int `json:"succeeded"`
				Failed    int `json:"failed"`
				Skipped   int `json:"skipped"`
				Remaining int `json:"remaining"`
			} `json:"progress"`
			ErrorSummary   string `json:"errorSummary"`
			FailedSubjects []struct {
				Subject string `json:"subject"`
				Error   string `json:"error"`
			} `json:"failedSubjects"`
		} `json:"runs"`
		NextCursor string `json:"nextCursor"`
	}
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(body.Runs) != 1 {
		t.Fatalf("runs = %d, want 1", len(body.Runs))
	}
	run := body.Runs[0]
	if run.ID != 43 || run.Integration != "tmdb" || run.Operation != "refresh_stale" || run.Trigger != "manual" {
		t.Fatalf("run identity = %+v", run)
	}
	if run.InitiatedBy == nil || *run.InitiatedBy != 7 || run.ConfigRevision != 12 || run.Status != "completed_with_errors" {
		t.Fatalf("run provenance/status = %+v", run)
	}
	if run.StartedAt != "2026-08-04T13:00:00Z" || run.FinishedAt != "2026-08-04T13:02:00Z" {
		t.Fatalf("run timestamps = %q / %q", run.StartedAt, run.FinishedAt)
	}
	if run.Progress.Total != 9 || run.Progress.Processed != 9 || run.Progress.Succeeded != 7 || run.Progress.Failed != 1 || run.Progress.Skipped != 1 || run.Progress.Remaining != 0 {
		t.Fatalf("run progress = %+v", run.Progress)
	}
	if run.ErrorSummary != "one movie failed" || len(run.FailedSubjects) != 1 || run.FailedSubjects[0].Subject != "movie:603" {
		t.Fatalf("run failures = %q / %+v", run.ErrorSummary, run.FailedSubjects)
	}
	if body.NextCursor != "2026-08-04T12:30:00Z,41" {
		t.Fatalf("next cursor = %q", body.NextCursor)
	}
}

func TestHandleListIntegrationRuns_RequiresAdmin(t *testing.T) {
	t.Parallel()
	ledger := &recordingRunLedger{}
	app := mountIntegrationRunHistoryTest(&handler{integrationRuns: ledger})
	response := doAs(t, app, httptest.NewRequest(http.MethodGet, "/api/v1/integration-runs", nil), 1, "member")
	defer response.Body.Close()

	if response.StatusCode != fiber.StatusForbidden || problemCode(t, response) != "admin_required" {
		t.Fatalf("status = %d, want admin_required", response.StatusCode)
	}
	if ledger.calls != 0 {
		t.Fatalf("member reached ledger %d times", ledger.calls)
	}
}

func TestHandleListIntegrationRuns_AcceptsIntegrationOwnedIdentifiers(t *testing.T) {
	t.Parallel()
	ledger := &recordingRunLedger{}
	app := mountIntegrationRunHistoryTest(&handler{integrationRuns: ledger})
	response := doAs(t, app, httptest.NewRequest(
		http.MethodGet,
		"/api/v1/integration-runs?operation=sync_library&trigger=provider_webhook",
		nil,
	), 1, "admin")
	defer response.Body.Close()

	if response.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d, want 200", response.StatusCode)
	}
	if ledger.calls != 1 {
		t.Fatalf("list calls = %d, want 1", ledger.calls)
	}
	if ledger.filter.Operation != "sync_library" || ledger.filter.Trigger != "provider_webhook" {
		t.Fatalf("operation/trigger filter = %+v", ledger.filter)
	}
}

func TestHandleListIntegrationRuns_RejectsInvalidFilters(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		query string
	}{
		{"cursor", "cursor=not-a-cursor"},
		{"limit", "limit=0"},
		{"status", "status=unknown"},
		{"running status", "status=running"},
		{"malformed operation", "operation=Refresh.stale"},
		{"oversized operation", "operation=" + strings.Repeat("a", 65)},
		{"malformed trigger", "trigger=provider.webhook"},
		{"oversized trigger", "trigger=" + strings.Repeat("a", 65)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ledger := &recordingRunLedger{}
			app := mountIntegrationRunHistoryTest(&handler{integrationRuns: ledger})
			response := doAs(t, app, httptest.NewRequest(http.MethodGet, "/api/v1/integration-runs?"+tt.query, nil), 1, "admin")
			defer response.Body.Close()

			if response.StatusCode != fiber.StatusBadRequest || problemCode(t, response) != "invalid_request" {
				t.Fatalf("status = %d, want invalid_request", response.StatusCode)
			}
			if ledger.calls != 0 {
				t.Fatalf("invalid %s reached ledger %d times", tt.name, ledger.calls)
			}
		})
	}
}
