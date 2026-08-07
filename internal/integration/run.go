package integration

import (
	"context"
	"errors"
	"time"
)

var ErrRunNotRunning = errors.New("integration run is not running")

const (
	FailedSubjectSampleLimit = 25
	DefaultRunListLimit      = 50
	MaxRunListLimit          = 100
	RunRetentionMonths       = 12
	MaxRunsPerIntegration    = 10_000
)

type RunStatus string

const (
	RunStatusRunning             RunStatus = "running"
	RunStatusCompleted           RunStatus = "completed"
	RunStatusCompletedWithErrors RunStatus = "completed_with_errors"
	RunStatusFailed              RunStatus = "failed"
	RunStatusCancelled           RunStatus = "cancelled"
	RunStatusInterrupted         RunStatus = "interrupted"
)

type RunOperation string

const (
	RunOperationRefreshStale RunOperation = "refresh_stale"
	RunOperationReEnrichAll  RunOperation = "re_enrich_all"
	RunOperationEnrichMovie  RunOperation = "enrich_movie"
)

type RunTrigger string

const (
	RunTriggerScheduled     RunTrigger = "scheduled"
	RunTriggerManual        RunTrigger = "manual"
	RunTriggerMovieAdded    RunTrigger = "movie_added"
	RunTriggerMovieUpdated  RunTrigger = "movie_updated"
	RunTriggerConfiguration RunTrigger = "configuration"
	RunTriggerStartup       RunTrigger = "startup"
)

type RunProgress struct {
	Total     int
	Processed int
	Succeeded int
	Failed    int
	Skipped   int
	Remaining int
}

type FailedSubject struct {
	Subject string `json:"subject"`
	Error   string `json:"error"`
}

type Run struct {
	ID             int64
	Integration    string
	Operation      RunOperation
	Trigger        RunTrigger
	InitiatedBy    *int
	ConfigRevision int64
	Status         RunStatus
	StartedAt      time.Time
	FinishedAt     *time.Time
	Progress       RunProgress
	ErrorSummary   string
	FailedSubjects []FailedSubject
}

type RunStart struct {
	Integration    string
	Operation      RunOperation
	Trigger        RunTrigger
	InitiatedBy    *int
	ConfigRevision int64
	StartedAt      time.Time
	Total          int
}

type RunFinish struct {
	Status         RunStatus
	FinishedAt     time.Time
	Progress       RunProgress
	ErrorSummary   string
	FailedSubjects []FailedSubject
}

type RunCursor struct {
	StartedAt time.Time
	ID        int64
}

type RunListFilter struct {
	Integration  string
	Operation    RunOperation
	Status       RunStatus
	Trigger      RunTrigger
	FinishedOnly bool
	Before       *RunCursor
	Limit        int
}

type RunPage struct {
	Runs []Run
	Next *RunCursor
}

type RunLedger interface {
	Start(ctx context.Context, start RunStart) (*Run, error)
	Update(ctx context.Context, id int64, progress RunProgress) error
	Finish(ctx context.Context, id int64, finish RunFinish) (*Run, error)
	InterruptRunning(ctx context.Context, interruptedAt time.Time) (int64, error)
	Prune(ctx context.Context, now time.Time) (int64, error)
	Current(ctx context.Context, integrationID string) (*Run, error)
	CurrentLibrary(ctx context.Context, integrationID string) (*Run, error)
	Latest(ctx context.Context, integrationID string) (*Run, error)
	List(ctx context.Context, filter RunListFilter) (RunPage, error)
}
