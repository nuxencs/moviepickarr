package server

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"moviepickarr/internal/integration"

	"github.com/gofiber/fiber/v2"
)

var integrationRunFilterIdentifier = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,63}$`)

type integrationRunHistoryResponse struct {
	Runs       []integrationRunResponse `json:"runs"`
	NextCursor string                   `json:"nextCursor,omitempty"`
}

type integrationRunResponse struct {
	ID             int64                   `json:"id"`
	Integration    string                  `json:"integration"`
	Operation      string                  `json:"operation"`
	Trigger        string                  `json:"trigger"`
	InitiatedBy    *int                    `json:"initiatedBy"`
	ConfigRevision int64                   `json:"configRevision"`
	Status         string                  `json:"status"`
	StartedAt      string                  `json:"startedAt"`
	FinishedAt     string                  `json:"finishedAt,omitempty"`
	Progress       integrationRunProgress  `json:"progress"`
	ErrorSummary   string                  `json:"errorSummary,omitempty"`
	FailedSubjects []integrationRunFailure `json:"failedSubjects"`
}

type integrationRunProgress struct {
	Total     int `json:"total"`
	Processed int `json:"processed"`
	Succeeded int `json:"succeeded"`
	Failed    int `json:"failed"`
	Skipped   int `json:"skipped"`
	Remaining int `json:"remaining"`
}

type integrationRunFailure struct {
	Subject string `json:"subject"`
	Error   string `json:"error"`
}

func (h *handler) handleListIntegrationRuns(c *fiber.Ctx) error {
	if ok, err := h.requireAdmin(c); !ok {
		return err
	}
	filter, err := parseIntegrationRunListFilter(c)
	if err != nil {
		return writeProblem(c, fiber.StatusBadRequest, "invalid_request", err.Error())
	}
	page, err := h.integrationRuns.List(c.UserContext(), filter)
	if err != nil {
		return h.writeInternal(c, err, "reading integration run history failed")
	}

	response := integrationRunHistoryResponse{
		Runs: make([]integrationRunResponse, 0, len(page.Runs)),
	}
	for i := range page.Runs {
		response.Runs = append(response.Runs, toIntegrationRunResponse(page.Runs[i]))
	}
	if page.Next != nil {
		response.NextCursor = formatIntegrationRunCursor(*page.Next)
	}
	return c.Status(fiber.StatusOK).JSON(response)
}

func parseIntegrationRunListFilter(c *fiber.Ctx) (integration.RunListFilter, error) {
	filter := integration.RunListFilter{
		Integration:  strings.TrimSpace(c.Query("integration")),
		Operation:    integration.RunOperation(strings.TrimSpace(c.Query("operation"))),
		Status:       integration.RunStatus(strings.TrimSpace(c.Query("status"))),
		Trigger:      integration.RunTrigger(strings.TrimSpace(c.Query("trigger"))),
		FinishedOnly: true,
	}
	if !validIntegrationRunFilterIdentifier(string(filter.Operation)) {
		return integration.RunListFilter{}, fmt.Errorf("operation is invalid")
	}
	if !validIntegrationRunStatus(filter.Status) {
		return integration.RunListFilter{}, fmt.Errorf("status is invalid")
	}
	if !validIntegrationRunFilterIdentifier(string(filter.Trigger)) {
		return integration.RunListFilter{}, fmt.Errorf("trigger is invalid")
	}
	if rawLimit := strings.TrimSpace(c.Query("limit")); rawLimit != "" {
		limit, err := strconv.Atoi(rawLimit)
		if err != nil || limit <= 0 {
			return integration.RunListFilter{}, fmt.Errorf("limit must be a positive integer")
		}
		filter.Limit = limit
	}
	cursor, err := parseIntegrationRunCursor(strings.TrimSpace(c.Query("cursor")))
	if err != nil {
		return integration.RunListFilter{}, err
	}
	filter.Before = cursor
	return filter, nil
}

func validIntegrationRunFilterIdentifier(value string) bool {
	return value == "" || integrationRunFilterIdentifier.MatchString(value)
}

func validIntegrationRunStatus(status integration.RunStatus) bool {
	switch status {
	case "",
		integration.RunStatusCompleted,
		integration.RunStatusCompletedWithErrors,
		integration.RunStatusFailed,
		integration.RunStatusCancelled,
		integration.RunStatusInterrupted:
		return true
	default:
		return false
	}
}

func parseIntegrationRunCursor(raw string) (*integration.RunCursor, error) {
	if raw == "" {
		return nil, nil
	}
	startedAtRaw, idRaw, ok := strings.Cut(raw, ",")
	if !ok {
		return nil, fmt.Errorf("cursor is invalid")
	}
	startedAt, err := time.Parse(time.RFC3339, startedAtRaw)
	if err != nil {
		return nil, fmt.Errorf("cursor is invalid")
	}
	id, err := strconv.ParseInt(idRaw, 10, 64)
	if err != nil || id <= 0 {
		return nil, fmt.Errorf("cursor is invalid")
	}
	return &integration.RunCursor{StartedAt: startedAt.UTC(), ID: id}, nil
}

func formatIntegrationRunCursor(cursor integration.RunCursor) string {
	return cursor.StartedAt.UTC().Format(time.RFC3339) + "," + strconv.FormatInt(cursor.ID, 10)
}

func toIntegrationRunResponse(run integration.Run) integrationRunResponse {
	failedSubjects := make([]integrationRunFailure, 0, len(run.FailedSubjects))
	for _, failed := range run.FailedSubjects {
		failedSubjects = append(failedSubjects, integrationRunFailure{
			Subject: failed.Subject,
			Error:   failed.Error,
		})
	}
	return integrationRunResponse{
		ID:             run.ID,
		Integration:    run.Integration,
		Operation:      string(run.Operation),
		Trigger:        string(run.Trigger),
		InitiatedBy:    run.InitiatedBy,
		ConfigRevision: run.ConfigRevision,
		Status:         string(run.Status),
		StartedAt:      run.StartedAt.UTC().Format(time.RFC3339),
		FinishedAt:     formatTime(run.FinishedAt),
		Progress: integrationRunProgress{
			Total:     run.Progress.Total,
			Processed: run.Progress.Processed,
			Succeeded: run.Progress.Succeeded,
			Failed:    run.Progress.Failed,
			Skipped:   run.Progress.Skipped,
			Remaining: run.Progress.Remaining,
		},
		ErrorSummary:   run.ErrorSummary,
		FailedSubjects: failedSubjects,
	}
}
