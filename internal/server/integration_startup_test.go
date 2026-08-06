package server

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"moviepickarr/internal/db"
	"moviepickarr/internal/integration"
	integrationtmdb "moviepickarr/internal/integration/tmdb"
	"moviepickarr/internal/repository"

	"github.com/rs/zerolog"
)

func TestLogTMDBEnvironmentIssuesUsesStructuredStaticEvent(t *testing.T) {
	t.Parallel()
	var output bytes.Buffer
	logTMDBEnvironmentIssues(zerolog.New(&output), []integrationtmdb.EnvironmentIssue{{
		Field:   "TMDB_ENRICH_TTL",
		Message: "must be a valid non-negative duration",
	}})

	var event map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(output.Bytes()), &event); err != nil {
		t.Fatalf("decode log event: %v", err)
	}
	want := map[string]string{
		"level":           "warn",
		"component":       "integration",
		"integration":     "tmdb",
		"environment_key": "TMDB_ENRICH_TTL",
		"reason":          "must be a valid non-negative duration",
		"message":         "invalid integration environment value; using lower-precedence setting",
	}
	for field, expected := range want {
		if event[field] != expected {
			t.Fatalf("log field %q = %v, want %q; event = %+v", field, event[field], expected, event)
		}
	}
}

func TestNewHandler_InterruptsAbandonedIntegrationRuns(t *testing.T) {
	t.Parallel()
	pool, err := db.OpenSQLite(filepath.Join(t.TempDir(), "startup.db"))
	if err != nil {
		t.Fatalf("open SQLite: %v", err)
	}
	t.Cleanup(func() { _ = pool.Close() })
	if err := db.RunMigrations(context.Background(), pool.Write); err != nil {
		t.Fatalf("run migrations: %v", err)
	}
	ledger := repository.NewSqliteIntegrationRunRepository(pool)
	started, err := ledger.Start(context.Background(), integration.RunStart{
		Integration: "tmdb", Operation: integration.RunOperationRefreshStale,
		Trigger: integration.RunTriggerScheduled, ConfigRevision: 1,
		StartedAt: time.Now().Add(-time.Minute), Total: 3,
	})
	if err != nil {
		t.Fatalf("seed running integration: %v", err)
	}

	h := newHandler(pool, zerolog.Nop())
	t.Cleanup(h.Close)
	if h.tmdbScheduler == nil {
		t.Fatal("new handler did not wire the TMDB run scheduler")
	}
	latest, err := ledger.Latest(context.Background(), "tmdb")
	if err != nil {
		t.Fatalf("read latest run: %v", err)
	}
	if latest == nil || latest.ID != started.ID || latest.Status != integration.RunStatusInterrupted || latest.FinishedAt == nil {
		t.Fatalf("abandoned run = %+v, want interrupted", latest)
	}
}

func TestTMDBRunStatusIsSuccessful(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		status integration.RunStatus
		want   bool
	}{
		{status: integration.RunStatusCompleted, want: true},
		{status: integration.RunStatusCompletedWithErrors, want: false},
		{status: integration.RunStatusFailed, want: false},
		{status: integration.RunStatusCancelled, want: false},
		{status: integration.RunStatusInterrupted, want: false},
	} {
		if got := tmdbRunStatusIsSuccessful(test.status); got != test.want {
			t.Fatalf("status %q successful = %t, want %t", test.status, got, test.want)
		}
	}
}
